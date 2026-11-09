// 真实执行器子进程测试：注册、多入口、异步 SDK、缓冲写入和隔离槽，不替代协议验收。
package streamexecutor

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"backend/internal/operationcontract"
	"backend/internal/streamexec"

	"github.com/stretchr/testify/require"
)

func TestProgramExecutorSubprocess(t *testing.T) {
	dir, binary := subprocessPaths(t)
	output, err := exec.Command("go", "build", "-buildvcs=false", "-o", binary, "../../../cmd/stream-executor").CombinedOutput()
	require.NoError(t, err, string(output))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	paths := []string{}
	for _, name := range []string{"one.sock", "two.sock"} {
		path := filepath.Join(dir, name)
		server := exec.CommandContext(ctx, binary, "-socket", path)
		require.NoError(t, server.Start())
		t.Cleanup(func() { cancel(); _ = server.Wait() })
		paths = append(paths, path)
	}
	client := New(strings.Join(paths, ","))
	require.Eventually(t, func() bool { return client.Ready(ctx) == nil }, 5*time.Second, 25*time.Millisecond)
	request := streamexec.ProgramRequest{Code: `module.exports=function(api){
	 const source=api.sources.use("sensors");
	 const topic=api.uns.use({topicId:"21"});
	 api.stream.every({id:"sample",intervalMs:1000},async function(context){
	  const [t,p]=await Promise.all([source.read("opcua:temperature"),source.read("opcua:pressure")]);
	  await topic.write({temperature:t.value,pressure:p.value});
	  api.log.info("buffered");
	 });
	 api.stream.onRequest({id:"get",request:{code:"fixture.temperature",name:"Temperature",contractVersion:"1",inputSchema:{},outputSchema:{},sideEffect:"read",idempotency:"optional",timeoutMs:30000}},async function(context){return await source.read("opcua:temperature");});
	};`, SourceAliases: map[string]streamexec.ProgramSource{"sensors": {ID: "11", Resources: []string{"opcua:temperature", "opcua:pressure"}}}, TimeoutMs: 1000, CheckOnly: true}
	registered, err := client.ExecuteProgram(ctx, request, nil)
	require.NoError(t, err)
	require.Len(t, registered.Manifest.Entries, 2)
	request.CheckOnly = false
	request.EntryID = "sample"
	request.Input = json.RawMessage(`{}`)
	request.Context = json.RawMessage(`{"runId":"test"}`)
	request.ExpectedManifest = &registered.Manifest
	// 新入口名称可在真实子进程中执行既有的冻结清单。
	request.Code = strings.ReplaceAll(request.Code, "api.stream.every(", "api.stream.onTimer(")
	calls := []streamexec.ProgramCall{}
	result, err := client.ExecuteProgram(ctx, request, func(_ context.Context, call streamexec.ProgramCall) (json.RawMessage, error) {
		calls = append(calls, call)
		return json.RawMessage(`{"value":42}`), nil
	})
	require.NoError(t, err)
	require.Len(t, calls, 2)
	require.Len(t, result.Outputs, 1)
	require.JSONEq(t, `{"temperature":42,"pressure":42}`, string(result.Outputs[0].Data))
	// Both prebuilt slots can execute independently; a context cancellation does not stop its neighbor.
	request.EntryID = "get"
	var jobs sync.WaitGroup
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		jobs.Add(1)
		go func() {
			defer jobs.Done()
			_, err := client.ExecuteProgram(ctx, request, func(ctx context.Context, _ streamexec.ProgramCall) (json.RawMessage, error) {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(50 * time.Millisecond):
					return json.RawMessage(`{"value":7}`), nil
				}
			})
			failures <- err
		}()
	}
	jobs.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	request.TimeoutMs = 20
	_, err = client.ExecuteProgram(ctx, request, func(ctx context.Context, _ streamexec.ProgramCall) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	require.ErrorIs(t, err, streamexec.ErrTimeout, "IPC must preserve the executor timeout identity")
	request.TimeoutMs = 1000
	_, err = client.ExecuteProgram(ctx, request, func(context.Context, streamexec.ProgramCall) (json.RawMessage, error) {
		return json.RawMessage(`{"value":9}`), nil
	})
	require.NoError(t, err)
	// The parent remains live: this is a child deadline, not transport cancellation.
	infinite := streamexec.ProgramRequest{Code: `module.exports=api=>api.stream.onRequest({id:"spin"},()=>{while(true){}});`, TimeoutMs: 1000, CheckOnly: true}
	registered, err = client.ExecuteProgram(ctx, infinite, nil)
	require.NoError(t, err)
	infinite.CheckOnly, infinite.EntryID, infinite.TimeoutMs = false, "spin", 20
	infinite.Input, infinite.ExpectedManifest = json.RawMessage(`{}`), &registered.Manifest
	infinite.Context = json.RawMessage(`{}`)
	_, err = client.ExecuteProgram(ctx, infinite, nil)
	require.ErrorIs(t, err, streamexec.ErrTimeout)
	require.NoError(t, ctx.Err(), "parent remains usable after the isolated child timed out")
	implicit := streamexec.ProgramRequest{Code: `module.exports=api=>api.stream.onRequest({id:"echo"},ctx=>ctx.input);`, TimeoutMs: 1000, CheckOnly: true}
	registered, err = client.ExecuteProgram(ctx, implicit, nil)
	require.NoError(t, err)
	frozen, err := streamexec.WithImplicitRequests(registered.Manifest, 123, implicit.TimeoutMs)
	require.NoError(t, err)
	implicit.CheckOnly, implicit.EntryID = false, "echo"
	implicit.Input, implicit.ExpectedManifest = json.RawMessage(`{"frozen":true}`), &frozen
	implicit.Context = json.RawMessage(`{}`)
	result, err = client.ExecuteProgram(ctx, implicit, nil)
	require.NoError(t, err, "generated contracts survive real executor IPC and registration")
	require.Equal(t, frozen, result.Manifest)
	require.JSONEq(t, string(implicit.Input), string(result.Data))
	implicit.Code = strings.Replace(implicit.Code, "ctx=>ctx.input", "()=>undefined", 1)
	_, err = client.ExecuteProgram(ctx, implicit, nil)
	require.ErrorIs(t, err, streamexec.ErrOutput, "invalid request output preserves its identity across IPC")
	// 已知执行器错误在真实 IPC 后保留类型；脚本正文仍不能成为公开错误码。
	implicit.Code = strings.Replace(implicit.Code, "()=>undefined", `()=>{throw new Error("SOURCE_SQL_AUTH_FAILED private script text");}`, 1)
	_, err = client.ExecuteProgram(ctx, implicit, nil)
	require.ErrorIs(t, err, streamexec.ErrExecution)
	require.NotContains(t, err.Error(), "private script text")

}

func TestExecutorPoolBoundedAdmission(t *testing.T) {
	client := New("/unavailable-one,/unavailable-two")
	ctx, cancel := context.WithCancel(context.Background())
	_, releaseOne, err := client.acquire(ctx)
	require.NoError(t, err)
	_, releaseTwo, err := client.acquire(ctx)
	require.NoError(t, err)
	waiting := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { _, _, err := client.acquire(ctx); waiting <- err }()
	}
	require.Eventually(t, func() bool { return len(client.admission) == 6 }, time.Second, time.Millisecond)
	_, _, err = client.acquire(ctx)
	require.Error(t, err)
	cancel()
	for i := 0; i < 4; i++ {
		require.Error(t, <-waiting)
	}
	releaseOne()
	releaseTwo()
	require.Empty(t, client.admission)
}

// Protocol fault fixtures exercise the real HTTP transport, not a business API.
func TestProgramExecutorLostTerminalIsUnknown(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		drop bool
		want error
	}{
		{name: "closed before response headers", drop: true, want: sourcestream.ErrUnknownResult},
		{name: "missing terminal", want: sourcestream.ErrUnknownResult},
		{name: "malformed frame", body: "{broken}\n", want: sourcestream.ErrUnknownResult},
		{name: "missing terminal payload", body: "{\"kind\":\"program-result\"}\n", want: sourcestream.ErrUnknownResult},
		{name: "trailing frame", body: "{\"kind\":\"program-result\",\"programResult\":{}}\n{}\n", want: sourcestream.ErrUnknownResult},
		{name: "known invalid output", body: "{\"kind\":\"program-result\",\"programResult\":{\"error\":\"SourceStream.scriptOutput\"}}\n", want: streamexec.ErrOutput},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, http.NewResponseController(w).EnableFullDuplex())
				first, err := streamexec.NewExchangeReader(r.Body).Read()
				require.NoError(t, err)
				require.Equal(t, "program-execute", first.Kind)
				if tc.drop {
					conn, _, err := http.NewResponseController(w).Hijack()
					require.NoError(t, err)
					_ = conn.Close()
					return
				}
				w.Header().Set("Connection", "close")
				_, _ = io.WriteString(w, tc.body)
			})
			_, err := client.ExecuteProgram(context.Background(), streamexec.ProgramRequest{
				Code: "module.exports=()=>{};", TimeoutMs: 1000,
				ExpectedManifest: &streamexec.ProgramManifest{},
			}, nil)
			require.ErrorIs(t, err, tc.want)
		})
	}
	client := New("invalid-socket")
	_, err := client.ExecuteProgram(context.Background(), streamexec.ProgramRequest{
		Code: "module.exports=()=>{};", TimeoutMs: 1000, ExpectedManifest: &streamexec.ProgramManifest{},
	}, nil)
	require.ErrorIs(t, err, sourcestream.ErrUnavailable, "failure before dispatch remains unavailable")
}

func TestProgramResultRequiresCompiledTargets(t *testing.T) {
	manifest := streamexec.ProgramManifest{Topics: []streamexec.ProgramTopic{{TopicID: "1"}}}
	request := streamexec.ProgramRequest{ExpectedManifest: &manifest}
	result := streamexec.ProgramResult{Manifest: manifest, Outputs: []streamexec.ProgramOutput{{TopicID: "2", Data: json.RawMessage(`{}`)}}}
	require.ErrorIs(t, validateProgramResult(request, result), streamexec.ErrOutput)
	result.Outputs[0].TopicID = "1"
	require.NoError(t, validateProgramResult(request, result))
	manifest.Entries = []streamexec.ProgramEntry{{ID: "read", Kind: "request", Request: &operationcontract.Resource{SideEffect: "read"}}}
	request.EntryID, result.Manifest = "read", manifest
	require.ErrorIs(t, validateProgramResult(request, result), streamexec.ErrOutput, "read-only requests cannot smuggle buffered writes across IPC")
	result.Outputs = nil
	result.Checkpoint = &streamexec.ProgramCheckpoint{}
	require.ErrorIs(t, validateProgramResult(request, result), streamexec.ErrOutput)
	result.Checkpoint = nil
	require.NoError(t, validateProgramResult(request, result))
}

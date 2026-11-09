package streamexecutor

// Opt-in integration binary for dedicated, hard-limited KVM execution slots.
// It uses the production Client and real Goja child, never a simulated server.
import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptrace"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

func kvmSlots(t *testing.T) []string {
	t.Helper()
	raw := os.Getenv("KVM_ISOLATION_SOCKETS")
	if raw == "" {
		t.Skip("explicit dedicated KVM executor sockets required")
	}
	paths := strings.Split(raw, ",")
	require.Len(t, paths, 4)
	seen := map[string]bool{}
	for _, path := range paths {
		require.True(t, strings.HasPrefix(path, "/var/lib/docker/volumes/tier0-stream-isolation-"), "refuse existing enterprise sockets")
		require.False(t, seen[path])
		seen[path] = true
	}
	return paths
}

func kvmProgram(t *testing.T, client *Client, body string, timeout int) streamexec.ProgramRequest {
	t.Helper()
	code := `module.exports=api=>{api.stream.onRequest({id:"test"},context=>{` + body + `});};`
	registration, err := client.ExecuteProgram(context.Background(), streamexec.ProgramRequest{Code: code, TimeoutMs: 1000, CheckOnly: true}, nil)
	require.NoError(t, err)
	return streamexec.ProgramRequest{Code: code, TimeoutMs: timeout, EntryID: "test", ExpectedManifest: &registration.Manifest,
		Input: json.RawMessage(`{"value":42}`), Context: json.RawMessage(`{"isolationTest":true}`)}
}

// 三种流式协议共用四个真实受限槽，顺序交接不能把终帧误认为服务端已完成。
// 仅运行在显式提供的专用 KVM Socket 上，不访问 Source 或设备。
func TestKVMStreamHandoffAcrossProtocols(t *testing.T) {
	client := New(strings.Join(kvmSlots(t), ","))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	require.NoError(t, client.Ready(ctx))
	request := kvmProgram(t, client, `return {recovered:true};`, 1000)
	for i := 0; i < 20; i++ {
		result, err := client.ExecuteProgram(ctx, request, nil)
		require.NoError(t, err, "v2 iteration %d", i)
		require.JSONEq(t, `{"recovered":true}`, string(result.Data))
		script := sourcestream.ScriptDefinition{Code: `return {data:input,drop:false};`, TimeoutMs: 1000}
		input := map[string]any{"recovered": true}
		controlled, err := client.TransformWithSources(ctx, script, input, nil, nil, nil)
		require.NoError(t, err, "controlled iteration %d", i)
		require.Equal(t, input, controlled.Data)
		legacy, err := client.TransformWithLegacySources(ctx, script, input, nil, []string{}, nil)
		require.NoError(t, err, "legacy iteration %d", i)
		require.Equal(t, input, legacy.Data)
	}
	require.Empty(t, client.admission)
	require.Len(t, client.available, 4)
}

func TestKVMStreamProgramSlots(t *testing.T) {
	paths := kvmSlots(t)
	client := New(strings.Join(paths, ","))
	require.NoError(t, client.Ready(context.Background()))
	switch os.Getenv("KVM_ISOLATION_CASE") {
	case "sandbox":
		request := kvmProgram(t, client, `return {value:context.input.value,require:typeof require,fetch:typeof fetch,process:typeof process,Buffer:typeof Buffer,hash:require("node:crypto").createHash("sha256").update("abc").digest("hex")};`, 1000)
		result, err := client.ExecuteProgram(context.Background(), request, nil)
		require.NoError(t, err)
		require.JSONEq(t, `{"value":42,"require":"function","fetch":"undefined","process":"undefined","Buffer":"undefined","hash":"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"}`, string(result.Data))
		for _, module := range []string{"fs", "node:http", "child_process"} {
			moduleJSON, err := json.Marshal(module)
			require.NoError(t, err)
			request := kvmProgram(t, client, `require(`+string(moduleJSON)+`);return 1;`, 1000)
			_, err = client.ExecuteProgram(context.Background(), request, nil)
			require.EqualError(t, err, streamexec.ErrExecution.Error(), "module %s must not expose native I/O", module)
		}
		t.Log("real v2 program: deterministic node:crypto only; filesystem/network/process modules and native globals forbidden")
	case "timeout":
		one := New(paths[0])
		request := kvmProgram(t, one, `while(true){}`, 300)
		started := time.Now()
		_, err := one.ExecuteProgram(context.Background(), request, nil)
		require.ErrorContains(t, err, streamexec.ErrTimeout.Error())
		require.Less(t, time.Since(started), 3*time.Second)
		request = kvmProgram(t, one, `return context.input;`, 1000)
		result, err := one.ExecuteProgram(context.Background(), request, nil)
		require.NoError(t, err)
		require.JSONEq(t, `{"value":42}`, string(result.Data))
		t.Log("timeout interrupted child; same execution slot accepted next real program")
	case "queue":
		request := kvmProgram(t, client, `return context.input;`, 5000)
		observed := time.Now()
		observe := func(id int, event string, details ...any) {
			t.Logf("queue timing elapsed=%s waiter=%d event=%s details=%v", time.Since(observed), id, event, details)
		}
		// Hold four normal scheduler leases to deterministically fill eight
		// waiting slots; the waiters execute real programs once leases release.
		var release []func()
		for range 4 {
			_, done, err := client.acquire(context.Background())
			require.NoError(t, err)
			release = append(release, done)
		}
		observe(-1, "four-leases-acquired")
		var once sync.Once
		unblock := func() {
			once.Do(func() {
				for _, done := range release {
					done()
				}
			})
		}
		defer unblock()
		results := make(chan error, 8)
		for index := range 8 {
			id := index
			go func() {
				// Trace only dedicated test HTTP lifecycle, never script or payload.
				// WroteRequest waits for duplex EOF; headers mark its initial send.
				trace := &httptrace.ClientTrace{
					GetConn: func(_ string) { observe(id, "get-connection") },
					GotConn: func(info httptrace.GotConnInfo) {
						observe(id, "got-connection", info.Reused, info.WasIdle, info.IdleTime)
					},
					WroteHeaders:         func() { observe(id, "wrote-headers") },
					GotFirstResponseByte: func() { observe(id, "first-response-byte") },
					WroteRequest:         func(info httptrace.WroteRequestInfo) { observe(id, "duplex-body-finished", info.Err) },
				}
				observe(id, "begin")
				result, err := client.ExecuteProgram(httptrace.WithClientTrace(context.Background(), trace), request, nil)
				observe(id, "end", err)
				if err == nil && string(result.Data) != `{"value":42}` {
					err = errors.New("unexpected actual program output")
				}
				results <- err
			}()
		}
		require.Eventually(t, func() bool { return len(client.admission) == 12 }, time.Second, 5*time.Millisecond)
		observe(-1, "twelve-admitted")
		started := time.Now()
		_, err := client.ExecuteProgram(context.Background(), request, nil)
		require.ErrorIs(t, err, sourcestream.ErrUnavailable)
		require.Less(t, time.Since(started), 200*time.Millisecond)
		observe(-1, "thirteenth-rejected", time.Since(started))
		unblock()
		observe(-1, "leases-released")
		// Retain every waiter's timing before an assertion ends this test.
		// The success requirement is unchanged: all eight must complete.
		completed := make([]error, 0, 8)
		for range 8 {
			completed = append(completed, <-results)
		}
		for _, err := range completed {
			require.NoError(t, err)
		}
		require.Empty(t, client.admission)
		require.Len(t, client.available, 4)
		t.Log("production Client: 4 leases + 8 waiters; 13th rejected promptly, all 8 actual queued programs completed")
	case "oom":
		one, other := New(paths[0]), New(paths[1])
		request := kvmProgram(t, one, `const retained=new Uint8Array(256*1024*1024);retained.fill(7);return retained.length;`, 5000)
		otherRequest := kvmProgram(t, other, `return context.input;`, 1000)
		failure := make(chan error, 1)
		go func() { _, err := one.ExecuteProgram(context.Background(), request, nil); failure <- err }()
		result, err := other.ExecuteProgram(context.Background(), otherRequest, nil)
		require.NoError(t, err)
		require.JSONEq(t, `{"value":42}`, string(result.Data))
		select {
		case err := <-failure:
			require.Error(t, err, "memory-stress script must not succeed")
			t.Logf("memory-stress request failed: %v (container cgroup evidence checked by dedicated orchestrator)", err)
		case <-time.After(8 * time.Second):
			t.Fatal("memory-stress request exceeded its bounded execution deadline")
		}
		require.NoError(t, other.Ready(context.Background()))
		t.Log("independent slot executed and remained ready during first-slot memory stress")
	default:
		t.Fatal("explicit KVM_ISOLATION_CASE sandbox|timeout|queue|oom required")
	}
}

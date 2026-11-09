package streamexecutor

// 实际构建并启动生产执行器，通过 Unix socket、全双工 HTTP 和真实子进程验证 SDK。
import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

func TestControlledExecutorSubprocess(t *testing.T) {
	dir, binary := subprocessPaths(t)
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binary, "../../../cmd/stream-executor")
	output, err := build.CombinedOutput()
	require.NoError(t, err, string(output))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socket := filepath.Join(dir, "executor.sock")
	server := exec.CommandContext(ctx, binary, "-socket", socket)
	require.NoError(t, server.Start())
	t.Cleanup(func() { cancel(); _ = server.Wait() })
	client := New(socket)
	require.Eventually(t, func() bool { return client.Ready(ctx) == nil }, 5*time.Second, 25*time.Millisecond)
	grants := []streamexec.SourceGrant{
		{Alias: "boiler", SourceID: 11, ConfigRevision: 2, ReadResources: []string{"opcua:temperature"}},
		{Alias: "pump", SourceID: 12, ConfigRevision: 1, ReadResources: []string{"modbus:pressure"}},
	}
	script := sourcestream.ScriptDefinition{Code: `
const t = sources.get("boiler").read("opcua:temperature");
const p = sources.get("pump").read("modbus:pressure");
return {outputs:[{topicId:21,data:t},{topicId:22,data:p}],drop:false,logs:[]};`, TimeoutMs: 1000}
	calls := []streamexec.SourceCall{}
	result, err := client.TransformWithSources(ctx, script, map[string]any{}, map[string]any{"runId": "controlled-test"}, grants, func(_ context.Context, call streamexec.SourceCall) (json.RawMessage, error) {
		calls = append(calls, call)
		return json.RawMessage(`{"value":42}`), nil
	})
	require.NoError(t, err)
	require.Len(t, calls, 2)
	require.Len(t, result.Outputs, 2)
	require.EqualValues(t, 42, result.Outputs[0].Data.(map[string]any)["value"])
	// 每次执行独立 Runtime，不能复用之前执行的依赖或变量。
	script.Code = `return {data:sources.get("boiler").read("opcua:temperature"),drop:false};`
	_, err = client.TransformWithSources(ctx, script, nil, nil, nil, func(context.Context, streamexec.SourceCall) (json.RawMessage, error) {
		t.Fatal("undeclared Source reached host")
		return nil, nil
	})
	require.EqualError(t, err, streamexec.ErrExecution.Error())
	// 旧的无宿主执行接口仍可执行普通转换，不强行引入 Source。
	script.Code = `return {data:input,drop:false};`
	result, err = client.Transform(ctx, script, map[string]any{"value": 7}, nil)
	require.NoError(t, err)
	require.EqualValues(t, 7, result.Data.(map[string]any)["value"])
	// 长时间只读操作会被单次执行超时终止，后续任务仍可执行。
	script.Code = `return {data:sources.get("boiler").read("opcua:temperature"),drop:false};`
	script.TimeoutMs = 30
	_, err = client.TransformWithSources(ctx, script, nil, nil, grants, func(ctx context.Context, _ streamexec.SourceCall) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	require.Error(t, err)
	script.Code, script.TimeoutMs = `return {data:{afterTimeout:true},drop:false};`, 1000
	_, err = client.TransformWithSources(ctx, script, nil, nil, nil, nil)
	require.NoError(t, err)
}

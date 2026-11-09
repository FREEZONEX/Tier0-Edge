package streamexecutor

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

// Exercise production UDS transports while every execution slot is occupied.
func TestExecutorReadinessDuringLongExecution(t *testing.T) {
	dir, binary := subprocessPaths(t)
	out, err := exec.Command("go", "build", "-buildvcs=false", "-o", binary, "../../../cmd/stream-executor").CombinedOutput()
	require.NoError(t, err, string(out))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
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
	request := streamexec.ProgramRequest{Code: `module.exports=api=>{const topic=api.uns.use({topicId:"11"});api.stream.onRequest({id:"read"},async()=>await topic.read({timeoutMs:10000}));};`, TimeoutMs: 15000, CheckOnly: true}
	registered, err := client.ExecuteProgram(ctx, request, nil)
	require.NoError(t, err)
	request.CheckOnly, request.EntryID = false, "read"
	request.Input, request.Context, request.ExpectedManifest = json.RawMessage(`{}`), json.RawMessage(`{}`), &registered.Manifest
	started, results := make(chan struct{}, 2), make(chan error, 2)
	release := make(chan struct{})
	defer close(release)
	for range 2 {
		go func() {
			_, err := client.ExecuteProgram(ctx, request, func(callCtx context.Context, _ streamexec.ProgramCall) (json.RawMessage, error) {
				started <- struct{}{}
				select {
				case <-release:
					return json.RawMessage(`{"value":7}`), nil
				case <-callCtx.Done():
					return nil, callCtx.Err()
				}
			})
			results <- err
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("execution did not occupy its slot")
		}
	}
	// Keep both executions active longer than the capability probe's deadline.
	timer := time.NewTimer(1100 * time.Millisecond)
	defer timer.Stop()
	<-timer.C
	probeCtx, stopProbe := context.WithTimeout(ctx, time.Second)
	defer stopProbe()
	require.NoError(t, client.Ready(probeCtx), "busy execution slots must not delay health probes")
	select {
	case err := <-results:
		t.Fatalf("long execution ended before release: %v", err)
	default:
	}
	// Release only after the probe, and verify normal execution still completes.
	for range 2 {
		release <- struct{}{}
	}
	for range 2 {
		require.NoError(t, <-results)
	}
}

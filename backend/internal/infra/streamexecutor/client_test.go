package streamexecutor

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := New("unused")
	c.http.Transport = &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}
	t.Cleanup(c.http.CloseIdleConnections)
	return c
}

func TestSourceBoundary(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		t.Run(map[bool]string{false: "denied alias", true: "per-call deadline"}[allowed], func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				controller := http.NewResponseController(w)
				require.NoError(t, controller.EnableFullDuplex())
				reader := streamexec.NewFrameReader(r.Body)
				var request streamexec.Request
				require.NoError(t, reader.Decode(&request))
				alias := "other-workspace"
				if allowed {
					alias = "device"
				}
				require.NoError(t, json.NewEncoder(w).Encode(streamexec.Frame{Type: "source", Call: &streamexec.LegacySourceCall{Alias: alias, Options: json.RawMessage(`{}`), TimeoutMs: 20}}))
				require.NoError(t, controller.Flush())
				if !allowed {
					return
				}
				var reply streamexec.LegacySourceReply
				require.NoError(t, reader.Decode(&reply))
				require.Equal(t, streamexec.ErrSourceReadTimeout.Error(), reply.Error)
				require.Equal(t, "null", string(reply.Data))
				_ = json.NewEncoder(w).Encode(streamexec.Frame{Type: "result", Result: &streamexec.Result{Error: reply.Error}})
			})
			calls := 0
			_, err := c.TransformWithLegacySources(context.Background(), sourcestream.ScriptDefinition{Code: "return {};", TimeoutMs: 1000}, nil, nil, []string{"device"}, func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
				calls++
				deadline, ok := ctx.Deadline()
				require.True(t, ok)
				require.Less(t, time.Until(deadline), 25*time.Millisecond)
				<-ctx.Done()
				return nil, ctx.Err()
			})
			if allowed {
				require.EqualError(t, err, streamexec.ErrSourceReadTimeout.Error())
				require.Equal(t, 1, calls)
			} else {
				require.ErrorIs(t, err, streamexec.ErrSourceAccess)
				require.Zero(t, calls)
			}
		})
	}
}

func TestExecutorFailureNeverFallsBackToBackend(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/execute-controlled" {
			require.NoError(t, http.NewResponseController(w).EnableFullDuplex())
			_, err := streamexec.NewExchangeReader(r.Body).Read()
			require.NoError(t, err)
			w.Header().Set("Connection", "close")
		} else {
			_, _ = io.Copy(io.Discard, r.Body)
		}
		http.Error(w, "unavailable", 503)
	})
	require.ErrorIs(t, c.Check(context.Background(), sourcestream.ScriptDefinition{Code: "return {};", TimeoutMs: 500}), sourcestream.ErrUnavailable)
	_, err := c.Transform(context.Background(), sourcestream.ScriptDefinition{Code: "while(true) {}", TimeoutMs: 500}, nil, nil)
	require.ErrorIs(t, err, sourcestream.ErrUnavailable)
}

func TestOversizedExecutorOutput(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		controller := http.NewResponseController(w)
		_ = controller.EnableFullDuplex()
		var request streamexec.Request
		_ = streamexec.NewFrameReader(r.Body).Decode(&request)
		_, _ = io.WriteString(w, strings.Repeat("x", streamexec.MaxFrameBytes+1)+"\n")
	})
	_, err := c.Transform(context.Background(), sourcestream.ScriptDefinition{Code: "return {};", TimeoutMs: 1000}, nil, nil)
	require.ErrorIs(t, err, streamexec.ErrOutput)
}

func TestOversizedLegacyExecutorFrame(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		controller := http.NewResponseController(w)
		require.NoError(t, controller.EnableFullDuplex())
		w.Header().Set("Connection", "close")
		var request streamexec.Request
		require.NoError(t, streamexec.NewFrameReader(r.Body).Decode(&request))
		_, _ = io.WriteString(w, strings.Repeat("x", streamexec.MaxFrameBytes+1)+"\n")
	})
	_, err := c.TransformWithLegacySources(context.Background(), sourcestream.ScriptDefinition{Code: "return {};", TimeoutMs: 1000}, nil, nil, []string{"device"}, func(context.Context, string, json.RawMessage) (json.RawMessage, error) {
		t.Fatal("oversized frame must not invoke a Source")
		return nil, nil
	})
	require.ErrorIs(t, err, streamexec.ErrExecution)
}

// Run only against a disposable, independently memory-limited executor container.
func TestIsolatedExecutorMemoryAndRecovery(t *testing.T) {
	socket := os.Getenv("TEST_STREAM_EXECUTOR_SOCKET")
	if socket == "" {
		t.Skip("requires disposable memory-limited executor container")
	}
	c := New(socket)
	ctx := context.Background()
	require.NoError(t, c.Ready(ctx))
	require.NoError(t, c.Check(ctx, sourcestream.ScriptDefinition{Code: "return {data:input,drop:false};", TimeoutMs: 1000}))
	_, err := c.TransformWithLegacySources(ctx, sourcestream.ScriptDefinition{Code: `return {data:await sources.get("device").read({timeoutMs:20000}),drop:false};`, TimeoutMs: 30000}, nil, nil, []string{"device"}, func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.Greater(t, time.Until(deadline), 19*time.Second)
		return json.RawMessage(`{"ok":true}`), nil
	})
	require.NoError(t, err)
	_, err = c.Transform(ctx, sourcestream.ScriptDefinition{Code: `while(true) {}`, TimeoutMs: 100}, nil, nil)
	require.Error(t, err)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	// Fill dense arrays so this commits memory, rather than reserving sparse storage.
	_, err = c.Transform(ctx, sourcestream.ScriptDefinition{Code: `const held=[]; while(true) { held.push(new Array(4*1024*1024).fill(1)); }`, TimeoutMs: 30000}, nil, nil)
	require.Error(t, err)
	require.NotEqual(t, streamexec.ErrTimeout.Error(), err.Error(), "memory exhaustion must fail before the script deadline")
	runtime.GC()
	runtime.ReadMemStats(&after)
	require.Less(t, int64(after.HeapInuse)-int64(before.HeapInuse), int64(64*1024*1024))
	require.Eventually(t, func() bool {
		result, err := c.Transform(ctx, sourcestream.ScriptDefinition{Code: `return {data:{recovered:true},drop:false};`, TimeoutMs: 1000}, nil, nil)
		return err == nil && result.Data != nil
	}, 20*time.Second, 250*time.Millisecond)
}

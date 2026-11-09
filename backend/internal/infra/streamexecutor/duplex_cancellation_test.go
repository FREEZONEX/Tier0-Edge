package streamexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

// The emergency body close only runs AFTER a failed bounded-return assertion.
// It prevents the pre-fix pipe deadlock from hanging the test process itself.
type duplexBodyGuard struct {
	transport http.RoundTripper
	mu        sync.Mutex
	bodies    []io.ReadCloser
}

func (g *duplexBodyGuard) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Body != nil {
		g.mu.Lock()
		g.bodies = append(g.bodies, request.Body)
		g.mu.Unlock()
	}
	return g.transport.RoundTrip(request)
}

func (g *duplexBodyGuard) closeBodies() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, body := range g.bodies {
		_ = body.Close()
	}
}

type duplexFlushWriter struct{ http.ResponseWriter }

func (w duplexFlushWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if err == nil {
		err = http.NewResponseController(w.ResponseWriter).Flush()
	}
	return n, err
}

func TestDuplexCancellationReturnsAndReleasesAdmission(t *testing.T) {
	for _, path := range []string{"program-v2", "controlled-v1"} {
		for _, failure := range []string{"deadline-before-headers", "cancel-before-headers", "reused-connection-close"} {
			t.Run(path+"/"+failure, func(t *testing.T) {
				runDuplexCancellationCase(t, path, failure)
			})
		}
	}
}

func runDuplexCancellationCase(t *testing.T, path, failure string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "t0-duplex-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "executor.sock")
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)
	var requests atomic.Int32
	var nextConnection atomic.Int64
	var readyConnection atomic.Int64
	type connectionKey struct{}
	received := make(chan int64, 1)
	stopHandler := make(chan struct{})
	server := &http.Server{ConnContext: func(ctx context.Context, _ net.Conn) context.Context {
		return context.WithValue(ctx, connectionKey{}, nextConnection.Add(1))
	}}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Context().Value(connectionKey{}).(int64)
		if r.URL.Path == "/readyz" {
			readyConnection.Store(id)
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusOK)
			return
		}
		if err := http.NewResponseController(w).EnableFullDuplex(); err != nil {
			t.Errorf("enable duplex: %v", err)
			return
		}
		if requests.Add(1) == 1 {
			_, err := streamexec.NewExchangeReader(r.Body).Read()
			if err != nil {
				t.Errorf("read initial exchange: %v", err)
				return
			}
			received <- id
			if failure == "reused-connection-close" {
				conn, _, err := http.NewResponseController(w).Hijack()
				if err != nil {
					t.Errorf("hijack: %v", err)
					return
				}
				_ = conn.Close()
				return
			}
			// Deliberately send neither headers nor a response after initial input.
			<-stopHandler
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush recovery headers: %v", err)
			return
		}
		if err := streamexec.RunControlledChild(r.Context(), r.Body, duplexFlushWriter{w}); err != nil {
			t.Errorf("real recovery executor: %v", err)
		}
	})
	serverDone := make(chan struct{})
	go func() { _ = server.Serve(listener); close(serverDone) }()
	client := New(socket)
	transport := client.slots[0].Transport.(*http.Transport)
	guard := &duplexBodyGuard{transport: transport}
	client.slots[0].Transport = guard
	t.Cleanup(func() {
		guard.closeBodies()
		close(stopHandler)
		transport.CloseIdleConnections()
		_ = server.Close()
		select {
		case <-serverDone:
		case <-time.After(time.Second):
			t.Error("owned Unix server did not stop")
		}
	})
	readyCtx, readyCancel := context.WithTimeout(context.Background(), time.Second)
	require.NoError(t, client.Ready(readyCtx))
	readyCancel()

	invoke := duplexTestInvoker(t, client, path, 50)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	if failure == "deadline-before-headers" {
		parent, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
	}
	done := make(chan error, 1)
	go func() { done <- invoke(parent) }()
	select {
	case id := <-received:
		if failure == "reused-connection-close" {
			require.Equal(t, readyConnection.Load(), id, "failure must hit the established keep-alive connection")
		}
	case <-time.After(time.Second):
		cancel()
		guard.closeBodies()
		t.Fatal("Unix server never received initial exchange")
	}
	if failure == "cancel-before-headers" {
		cancel()
	}
	bound := 700 * time.Millisecond
	if failure == "reused-connection-close" {
		// The execution request owns a timeout + 1s IPC grace period.
		bound = 1700 * time.Millisecond
	}
	select {
	case err := <-done:
		require.Error(t, err)
		if failure != "reused-connection-close" {
			require.ErrorIs(t, err, streamexec.ErrTimeout)
		}
	case <-time.After(bound):
		t.Error("duplex call exceeded its cancellation/deadline bound")
		cancel()
		guard.closeBodies()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("emergency pipe close failed to release original call")
		}
	}
	require.Empty(t, client.admission, "failed call must release admission")
	require.Len(t, client.available, 1, "failed call must return the prebuilt slot")
	recovery, recoveryCancel := context.WithTimeout(context.Background(), time.Second)
	defer recoveryCancel()
	// The deliberately tiny failure budget is not the recovery VM's budget.
	require.NoError(t, duplexTestInvoker(t, client, path, 500)(recovery), "same client must execute a real recovery request")
	require.Empty(t, client.admission)
	require.Len(t, client.available, 1)
}

func duplexTestInvoker(t *testing.T, client *Client, path string, timeoutMs int) func(context.Context) error {
	t.Helper()
	if path == "plain-json" {
		return func(ctx context.Context) error {
			return client.Check(ctx, sourcestream.ScriptDefinition{Code: `return {data:input,drop:false};`, TimeoutMs: timeoutMs})
		}
	}
	if path == "legacy" {
		return func(ctx context.Context) error {
			result, err := client.TransformWithLegacySources(ctx, sourcestream.ScriptDefinition{
				Code: `return {data:input,drop:false};`, TimeoutMs: timeoutMs,
			}, map[string]any{"recovered": true}, nil, []string{}, nil)
			if err != nil {
				return err
			}
			data, ok := result.Data.(map[string]any)
			if !ok || data["recovered"] != true {
				return errors.New("legacy recovery result mismatch")
			}
			return nil
		}
	}
	if path == "program-v2" {
		request := streamexec.ProgramRequest{Code: `module.exports = function(api) {
 api.stream.onRequest({id:"get"}, async function(context) { return context.input; });
};`, TimeoutMs: timeoutMs, CheckOnly: true}
		registered, err := streamexec.ExecuteProgram(context.Background(), request, nil)
		require.NoError(t, err)
		request.CheckOnly, request.EntryID = false, "get"
		request.ExpectedManifest = &registered.Manifest
		request.Input = json.RawMessage(`{"recovered":true}`)
		request.Context = json.RawMessage(`{}`)
		return func(ctx context.Context) error {
			result, err := client.ExecuteProgram(ctx, request, nil)
			if err != nil {
				return err
			}
			if string(result.Data) != `{"recovered":true}` {
				return errors.New("program recovery result mismatch")
			}
			return nil
		}
	}
	return func(ctx context.Context) error {
		result, err := client.TransformWithSources(ctx, sourcestream.ScriptDefinition{
			Code: `return {data:input,drop:false};`, TimeoutMs: timeoutMs,
		}, map[string]any{"recovered": true}, nil, nil, nil)
		if err != nil {
			return err
		}
		data, ok := result.Data.(map[string]any)
		if !ok || data["recovered"] != true {
			return fmt.Errorf("controlled recovery result mismatch")
		}
		return nil
	}
}

// 最终结果已到达时，客户端必须结束双工请求，不能等传输兜底超时才归还槽位。
// 服务端运行真实 Goja，再等待请求 EOF；失败时才通过 guard 解开测试连接。
func TestDuplexFinalResultRetiresRequestPipe(t *testing.T) {
	for _, path := range []string{"program-v2", "controlled-v1"} {
		for _, outcome := range []string{"success", "script-timeout"} {
			t.Run(path+"/"+outcome, func(t *testing.T) {
				dir, err := os.MkdirTemp("", "t0-result-")
				require.NoError(t, err)
				t.Cleanup(func() { _ = os.RemoveAll(dir) })
				socket := filepath.Join(dir, "executor.sock")
				listener, err := net.Listen("unix", socket)
				require.NoError(t, err)
				eof := make(chan struct{}, 2)
				server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if err := http.NewResponseController(w).EnableFullDuplex(); err != nil {
						t.Errorf("enable duplex: %v", err)
						return
					}
					w.Header().Set("Content-Type", "application/x-ndjson")
					if err := streamexec.RunControlledChild(r.Context(), r.Body, duplexFlushWriter{w}); err != nil {
						t.Errorf("real Goja exchange: %v", err)
						return
					}
					_, _ = io.Copy(io.Discard, r.Body)
					eof <- struct{}{}
				})}
				serverDone := make(chan struct{})
				go func() { _ = server.Serve(listener); close(serverDone) }()
				client := New(socket)
				transport := client.slots[0].Transport.(*http.Transport)
				guard := &duplexBodyGuard{transport: transport}
				client.slots[0].Transport = guard
				t.Cleanup(func() {
					guard.closeBodies()
					transport.CloseIdleConnections()
					_ = server.Close()
					<-serverDone
				})
				invoke := duplexTestInvoker(t, client, path, 1000)
				if outcome == "script-timeout" {
					if path == "program-v2" {
						request := streamexec.ProgramRequest{Code: `module.exports=api=>api.stream.onRequest({id:"spin"},()=>{while(true){}});`, TimeoutMs: 1000, CheckOnly: true}
						registration, err := streamexec.ExecuteProgram(context.Background(), request, nil)
						require.NoError(t, err)
						request.CheckOnly, request.EntryID, request.TimeoutMs = false, "spin", 100
						request.ExpectedManifest, request.Input = &registration.Manifest, json.RawMessage(`{}`)
						request.Context = json.RawMessage(`{}`)
						invoke = func(ctx context.Context) error { _, err := client.ExecuteProgram(ctx, request, nil); return err }
					} else {
						invoke = func(ctx context.Context) error {
							_, err := client.TransformWithSources(ctx, sourcestream.ScriptDefinition{Code: `while(true){}`, TimeoutMs: 100}, nil, nil, nil, nil)
							return err
						}
					}
				}
				started := time.Now()
				done := make(chan error, 1)
				go func() { done <- invoke(context.Background()) }()
				select {
				case err = <-done:
				case <-time.After(500 * time.Millisecond):
					t.Error("final result did not promptly return: duplex cleanup waited for the transport deadline")
					guard.closeBodies()
					err = <-done
				}
				if outcome == "success" {
					require.NoError(t, err)
				} else {
					require.EqualError(t, err, streamexec.ErrTimeout.Error())
				}
				t.Logf("final result returned in %s", time.Since(started))
				select {
				case <-eof:
				case <-time.After(time.Second):
					t.Fatal("completed request retained the server-side body")
				}
				require.Empty(t, client.admission)
				require.Len(t, client.available, 1)
				require.NoError(t, duplexTestInvoker(t, client, path, 1000)(context.Background()), "same client accepts a real recovery execution")
			})
		}
	}
}

// 最终帧并不是 HTTP 请求完成：服务端归还真实槽位之前，本地槽位不能重新分配。
func TestDuplexCompletionWaitsForServerSlotRelease(t *testing.T) {
	for _, path := range []string{"program-v2", "controlled-v1", "legacy", "plain-json"} {
		t.Run(path, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "t0-handoff-")
			require.NoError(t, err)
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			socket := filepath.Join(dir, "executor.sock")
			listener, err := net.Listen("unix", socket)
			require.NoError(t, err)
			final := make(chan struct{})
			releaseServer := make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(releaseServer) }) }
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := http.NewResponseController(w).EnableFullDuplex(); err != nil {
					t.Errorf("enable duplex: %v", err)
					return
				}
				w.Header().Set("Content-Type", "application/x-ndjson")
				if path == "legacy" || path == "plain-json" {
					var request streamexec.Request
					var decodeErr error
					if path == "legacy" {
						decodeErr = streamexec.NewFrameReader(r.Body).Decode(&request)
					} else {
						decodeErr = json.NewDecoder(r.Body).Decode(&request)
					}
					if decodeErr != nil {
						t.Errorf("decode execution: %v", decodeErr)
						return
					}
					result, err := streamexec.ExecuteWithLegacySources(r.Context(), request, nil)
					if err != nil {
						t.Errorf("real Goja execution: %v", err)
						return
					}
					var body any = result
					if path == "legacy" {
						body = streamexec.Frame{Type: "result", Result: &result}
					}
					if err := json.NewEncoder(duplexFlushWriter{w}).Encode(body); err != nil {
						t.Errorf("flush real result: %v", err)
						return
					}
				} else {
					if err := streamexec.RunControlledChild(r.Context(), r.Body, duplexFlushWriter{w}); err != nil {
						t.Errorf("real Goja exchange: %v", err)
						return
					}
				}
				// 终帧已经 flush；读取客户端请求 EOF，避免夹具带着未消费 Body 进入 keep-alive。
				_, _ = io.Copy(io.Discard, r.Body)
				close(final)
				// 模拟最终帧已 flush、真实槽位尚未归还的调度窗口。
				<-releaseServer
			})}
			go func() { _ = server.Serve(listener) }()
			client := New(socket)
			t.Cleanup(func() {
				release()
				_ = server.Close()
				client.slots[0].Transport.(*http.Transport).CloseIdleConnections()
			})
			invoke := duplexTestInvoker(t, client, path, 1000)
			done := make(chan error, 1)
			go func() { done <- invoke(context.Background()) }()
			select {
			case <-final:
			case <-time.After(time.Second):
				t.Fatal("real executor did not send the final frame")
			}
			returned := false
			select {
			case err := <-done:
				returned = true
				require.NoError(t, err)
				t.Error("client returned its local slot before server completion")
			case <-time.After(100 * time.Millisecond):
				require.Len(t, client.admission, 1)
				require.Empty(t, client.available)
			}
			release()
			if !returned {
				select {
				case err := <-done:
					require.NoError(t, err)
				case <-time.After(time.Second):
					t.Fatal("server completion did not release the client")
				}
			}
		})
	}
}

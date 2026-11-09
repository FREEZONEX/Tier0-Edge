package sourceprotocol

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"github.com/stretchr/testify/require"
)

func TestRealOPCUASharedConsumersCancelIndependently(t *testing.T) {
	socket := os.Getenv("TEST_SOURCE_OPCUA_SOCKET")
	if socket == "" {
		t.Skip("real OPC UA fixture not configured")
	}
	r := New()
	r.Register(sourcestream.OPCUA, NewOPCUA(socket))
	defer r.Close()
	def := sourcestream.SourceDefinition{Name: "shared", Protocol: sourcestream.OPCUA, Endpoint: "opc.tcp://localhost:14842", OPCUA: &sourcestream.OPCUAOptions{SecurityMode: "None", SecurityPolicy: "None", NodeIDs: []string{"ns=1;s=Sequence"}, SamplingIntervalMs: 500}}
	first, cancelFirst := context.WithCancel(context.Background())
	second, cancelSecond := context.WithCancel(context.Background())
	defer cancelFirst()
	defer cancelSecond()
	var a, b atomic.Int64
	done := make(chan error, 2)
	go func() {
		done <- r.Watch(first, 99118, def, sourcestream.Credential{}, func(Input) error { a.Add(1); return nil })
	}()
	go func() {
		done <- r.Watch(second, 99118, def, sourcestream.Credential{}, func(Input) error { b.Add(1); return nil })
	}()
	require.Eventually(t, func() bool { return a.Load() > 0 && b.Load() > 0 }, 10*time.Second, 25*time.Millisecond)
	r.watchMu.Lock()
	count := len(r.watches)
	r.watchMu.Unlock()
	require.Equal(t, 1, count)
	cancelFirst()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("first consumer did not stop")
	}
	before := b.Load()
	require.Eventually(t, func() bool { return b.Load() > before }, 3*time.Second, 25*time.Millisecond)
	cancelSecond()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("last consumer did not stop")
	}
	require.Eventually(t, func() bool { r.watchMu.Lock(); defer r.watchMu.Unlock(); return len(r.watches) == 0 }, 3*time.Second, 25*time.Millisecond)
}

func TestCloudOPCUAColdReadAndProbeReadiness(t *testing.T) {
	for _, operation := range []string{"probe", "read", "write", "cancel"} {
		t.Run(operation, func(t *testing.T) {
			var ready, dispatched atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/readyz" {
					if ready.Add(1) < 3 {
						w.WriteHeader(http.StatusServiceUnavailable)
					}
					return
				}
				dispatched.Add(1)
				_, _ = w.Write([]byte(`{"data":{"value":42}}`))
			}))
			defer server.Close()
			transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
			}}
			defer transport.CloseIdleConnections()
			driver := &OPCUADriver{http: &http.Client{Transport: transport}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if operation == "cancel" {
				cancel()
			}
			var err error
			if operation == "probe" || operation == "cancel" {
				err = driver.Probe(ctx, 1, sourcestream.SourceDefinition{}, sourcestream.Credential{})
			} else {
				_, err = driver.Invoke(ctx, 1, sourcestream.RuntimeSource{}, sourcestream.Credential{}, "opcua:node", map[string]any{"operation": operation})
			}
			if operation == "cancel" {
				require.ErrorIs(t, err, context.Canceled)
				require.Zero(t, ready.Load())
				require.Zero(t, dispatched.Load())
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 1, dispatched.Load(), "protocol operation must be dispatched exactly once")
			if operation == "write" {
				require.Zero(t, ready.Load(), "write admission must not acquire read retry semantics")
			} else {
				require.EqualValues(t, 3, ready.Load())
			}
		})
	}
}

// Runs the production Node entry over its real private Unix socket, not an
// HTTP fixture. The Node suite separately exercises remote OPC UA sessions.
func TestOPCUAServerPrivateRuntime(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "../../../../runtime/source-opcua"))
	require.FileExists(t, filepath.Join(root, "node_modules/node-opcua/package.json"), "install the pinned runtime dependencies before this integration test")
	node, err := exec.LookPath("node")
	require.NoError(t, err)
	dir := t.TempDir()
	socket := filepath.Join(dir, "runtime.sock")
	command := exec.Command(node, "server.mjs")
	command.Dir = root
	command.Env = append(os.Environ(), "SOURCE_OPCUA_SOCKET="+socket, "SOURCE_OPCUA_PKI="+dir, "SOURCE_OPCUA_SERVER_PORT=0")
	command.Stdout, command.Stderr = io.Discard, io.Discard
	require.NoError(t, command.Start())
	t.Cleanup(func() { _ = command.Process.Signal(os.Interrupt); _ = command.Wait() })
	driver := NewOPCUA(socket)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	require.Eventually(t, func() bool { return driver.Ready(ctx) == nil }, 10*time.Second, 50*time.Millisecond)
	def := sourcestream.SourceDefinition{Name: "Managed OPC UA", Protocol: sourcestream.OPCUA, Role: sourcestream.ServerRole,
		Endpoint: "opc.tcp://managed", RuntimeLocation: "local", CredentialRef: "credential-private",
		OPCUA: &sourcestream.OPCUAOptions{SecurityMode: "None", SecurityPolicy: "None", SamplingIntervalMs: 100,
			NodeIDs: []string{"command"}, ServerNodes: []sourcestream.OPCUAServerNode{{ID: "command", Name: "Command", NodeClass: "Variable", DataType: "String", Direction: "egress"}}}}
	credential := sourcestream.Credential{Kind: "basic", Username: "device", Secret: "private-test-credential"}
	require.NoError(t, def.Validate())
	require.NoError(t, driver.Probe(ctx, 501, def, credential))
	resources, err := driver.Discover(ctx, 501, def, credential, "")
	require.NoError(t, err)
	require.False(t, resources[0].Metadata["registered"].(bool), "probe/discovery must not activate a draft Source")
	_, err = driver.Invoke(ctx, 501, def, credential, "opcua:command", map[string]any{"operation": "write", "value": "reset"})
	require.Error(t, err)
	require.NoError(t, driver.SyncServerSources(ctx, []sourcestream.ManagedServerSource{{SourceID: 501, Definition: def, Credential: credential}}))
	resources, err = driver.Discover(ctx, 501, def, credential, "")
	require.NoError(t, err)
	require.True(t, resources[0].Metadata["registered"].(bool))
	raw, err := json.Marshal(resources)
	require.NoError(t, err)
	require.NotContains(t, string(raw), credential.Secret)
	result, err := driver.Invoke(ctx, 501, def, credential, "opcua:command", map[string]any{"operation": "write", "value": "reset"})
	require.NoError(t, err)
	require.Equal(t, false, result.(map[string]any)["deviceExecuted"])
	result, err = driver.Invoke(ctx, 501, def, credential, "opcua:command", map[string]any{"operation": "read"})
	require.NoError(t, err)
	require.Equal(t, "reset", result.(map[string]any)["value"])
	wrong := credential
	wrong.Secret = "wrong-password"
	_, err = driver.Invoke(ctx, 501, def, wrong, "opcua:command", map[string]any{"operation": "read"})
	require.ErrorIs(t, err, sourcestream.ErrCredential)
	_, err = driver.Discover(ctx, 501, def, credential, "another-source")
	require.Error(t, err)
	// A quiet Server input can be subscription-ready without a fabricated
	// sample. Consume the managed runtime's actual acknowledgement header.
	inputDef := def
	inputOptions := *def.OPCUA
	inputOptions.NodeIDs = []string{"sensor"}
	inputOptions.ServerNodes = []sourcestream.OPCUAServerNode{{ID: "sensor", Name: "Sensor", NodeClass: "Variable", DataType: "Double", Direction: "ingress"}}
	inputDef.OPCUA = &inputOptions
	require.NoError(t, driver.SyncServerSources(ctx, []sourcestream.ManagedServerSource{{SourceID: 501, Definition: def, Credential: credential}, {SourceID: 502, Definition: inputDef, Credential: credential}}))
	watchCtx, stop := context.WithCancel(ctx)
	defer stop()
	confirmed, samples := false, 0
	watchCtx = sourcestream.WithWatchObserver(watchCtx, func(ready bool) {
		if ready {
			confirmed = true
			stop()
		}
	})
	err = driver.Watch(watchCtx, 502, inputDef, credential, func(Input) error { samples++; return nil })
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, confirmed)
	require.Zero(t, samples, "readiness must not invent Source data")
	require.NoError(t, driver.SyncServerSources(ctx, nil))
	_, err = driver.Invoke(ctx, 501, def, credential, "opcua:command", map[string]any{"operation": "read"})
	require.ErrorIs(t, err, sourcestream.ErrCredential)
}

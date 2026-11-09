package sourceprotocol

// 私有 Unix socket + 真实 Modbus TCP：验证连接复用、Source 隔离、背压、
// 首轮完整采集就绪、配置释放、忙槽和写操作结果未知，不使用 HTTP 协议替身。
import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"github.com/simonvetter/modbus"
	"github.com/stretchr/testify/require"
)

func remoteModbusFixture(t *testing.T) (*ModbusRuntime, *RemoteModbusDriver, sourcestream.SourceDefinition, *registerFixture) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "tcp://" + probe.Addr().String()
	require.NoError(t, probe.Close())
	fixture := &registerFixture{}
	fixture.registers[0] = 37
	device, err := modbus.NewServer(&modbus.ServerConfiguration{URL: endpoint, Timeout: time.Second, MaxClients: 10, Logger: log.New(io.Discard, "", 0)}, fixture)
	require.NoError(t, err)
	require.NoError(t, device.Start())
	t.Cleanup(func() { require.NoError(t, device.Stop()) })
	runtime := NewModbusRuntime()
	socket := filepath.Join(t.TempDir(), "r.sock")
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)
	server := &http.Server{Handler: runtime, ReadHeaderTimeout: time.Second}
	go func() { _ = server.Serve(listener) }()
	client := NewRemoteModbus(socket)
	t.Cleanup(func() { client.Close(); runtime.Close(); _ = server.Close() })
	def := sourcestream.SourceDefinition{Name: "Protocol fixture", Protocol: sourcestream.ModbusTCP, Endpoint: endpoint, RuntimeLocation: "local", Modbus: &sourcestream.ModbusOptions{UnitID: 7, PollIntervalMs: 100, Ranges: []sourcestream.ModbusRange{
		{ID: "temperature", Table: "holding-register", Address: 0, Quantity: 1},
		{ID: "switch", Table: "coil", Address: 0, Quantity: 1},
	}}}
	return runtime, client, def, fixture
}

func TestRemoteModbusIPC(t *testing.T) {
	runtime, client, def, fixture := remoteModbusFixture(t)
	ctx, cancel := context.WithTimeout(WithSourceRevision(context.Background(), 1), 5*time.Second)
	defer cancel()
	credential := sourcestream.Credential{}
	require.NoError(t, client.Ready(ctx))
	require.NoError(t, client.Probe(ctx, 11, def, credential))
	resources, err := client.Discover(ctx, 11, def, credential, "")
	require.NoError(t, err)
	require.Len(t, resources, 2)
	read, err := client.Invoke(ctx, 11, def, credential, "modbus:temperature", map[string]any{"operation": "read"})
	require.NoError(t, err)
	require.Equal(t, float64(37), read.(map[string]any)["values"].([]any)[0])
	_, err = client.Invoke(ctx, 11, def, credential, "modbus:temperature", map[string]any{"operation": "write", "values": []int{42}})
	require.NoError(t, err)
	_, err = client.Invoke(ctx, 11, def, credential, "modbus:switch", map[string]any{"operation": "write", "values": []bool{true}})
	require.NoError(t, err)
	_, err = client.Invoke(ctx, 22, def, credential, "modbus:temperature", map[string]any{"operation": "read"})
	require.NoError(t, err)
	pool := runtime.runtime.drivers[sourcestream.ModbusTCP].(ModbusDriver).pool
	pool.mu.Lock()
	count := len(pool.entries)
	pool.mu.Unlock()
	require.Equal(t, 2, count, "operations reuse one lease per Source")
	client.ReleaseSource(11)
	pool.mu.Lock()
	count = len(pool.entries)
	pool.mu.Unlock()
	require.Equal(t, 1, count)
	fixture.mu.Lock()
	fixture.delayWrite = 250 * time.Millisecond
	fixture.mu.Unlock()
	short, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	defer stop()
	_, err = client.Invoke(short, 22, def, credential, "modbus:temperature", map[string]any{"operation": "write", "values": []int{51}})
	require.ErrorIs(t, err, sourcestream.ErrUnknownResult)
	fixture.mu.Lock()
	require.Equal(t, uint16(51), fixture.registers[0])
	fixture.mu.Unlock()
}

func TestRemoteModbusWatch(t *testing.T) {
	_, client, def, _ := remoteModbusFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	accepted, ready := 0, 0
	ctx = sourcestream.WithWatchObserver(ctx, func(value bool) {
		if value {
			ready++
			require.Equal(t, 2, accepted)
			cancel()
		}
	})
	err := client.Watch(ctx, 1, def, sourcestream.Credential{}, func(event Input) error { accepted++; require.NotEmpty(t, event.ResourceRef); return nil })
	require.Error(t, err)
	require.Equal(t, 1, ready)
	require.Equal(t, 2, accepted)
}

func TestRemoteModbusWatchFailure(t *testing.T) {
	_, client, def, _ := remoteModbusFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ready := false
	ctx = sourcestream.WithWatchObserver(ctx, func(value bool) { ready = ready || value })
	stop := errors.New("durable ingress rejected")
	err := client.Watch(ctx, 1, def, sourcestream.Credential{}, func(Input) error { return stop })
	require.ErrorIs(t, err, stop)
	require.False(t, ready, "socket acceptance is not durable ingress acceptance")
}

func TestRemoteModbusUnavailable(t *testing.T) {
	client := NewRemoteModbus(filepath.Join(t.TempDir(), "absent.sock"))
	defer client.Close()
	require.ErrorIs(t, client.Ready(context.Background()), sourcestream.ErrUnavailable)
	_, err := client.Invoke(context.Background(), 1, sourcestream.SourceDefinition{}, sourcestream.Credential{}, "x", map[string]any{"operation": "write"})
	require.ErrorIs(t, err, sourcestream.ErrUnknownResult, "a mutating transport failure must never invite replay")
	runtime, live, def, _ := remoteModbusFixture(t)
	for i := 0; i < cap(runtime.ops); i++ {
		runtime.ops <- struct{}{}
	}
	require.ErrorIs(t, live.Probe(context.Background(), 1, def, sourcestream.Credential{}), sourcestream.ErrUnavailable)
	for i := 0; i < cap(runtime.ops); i++ {
		<-runtime.ops
	}
	require.NoError(t, live.Probe(context.Background(), 1, def, sourcestream.Credential{}))
}

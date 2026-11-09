package sourceprotocol

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"github.com/simonvetter/modbus"
	"github.com/stretchr/testify/require"
)

// This opt-in fixture lives in a dedicated, non-root test-client container.
// The production Runtime is a different container reached only by its Unix
// socket. The test never starts a local Runtime or invokes Docker itself.
type containerModbusFixture struct {
	registerFixture
	reads          atomic.Int32
	registerWrites atomic.Int32
	coilWrites     atomic.Int32
}

func (f *containerModbusFixture) HandleHoldingRegisters(request *modbus.HoldingRegistersRequest) ([]uint16, error) {
	values, err := f.registerFixture.HandleHoldingRegisters(request)
	if err == nil {
		if request.IsWrite {
			f.registerWrites.Add(1)
		} else {
			f.reads.Add(1)
		}
	}
	return values, err
}

func (f *containerModbusFixture) HandleCoils(request *modbus.CoilsRequest) ([]bool, error) {
	values, err := f.registerFixture.HandleCoils(request)
	if err == nil && request.IsWrite {
		f.coilWrites.Add(1)
	}
	return values, err
}

func TestRemoteModbusIndependentContainer(t *testing.T) {
	socket := os.Getenv("TEST_SOURCE_MODBUS_SOCKET")
	if socket == "" {
		t.Skip("requires explicitly configured isolated Runtime and protocol test containers")
	}
	// Refuse to open a protocol listener on an ordinary host test run.
	require.Equal(t, "1", os.Getenv("TEST_SOURCE_MODBUS_CONTAINER"), "explicit isolated-container guard required")
	require.Equal(t, 65532, os.Geteuid(), "run the test container as the unprivileged Runtime UID")
	require.True(t, len(socket) > 0 && socket[0] == '/', "private socket must be absolute")
	endpoint := os.Getenv("TEST_SOURCE_MODBUS_DEVICE_ENDPOINT")
	if endpoint == "" {
		endpoint = "tcp://modbus-client:15020"
	}
	parsed, err := url.Parse(endpoint)
	require.NoError(t, err)
	require.Equal(t, "tcp", parsed.Scheme)
	require.NotEmpty(t, parsed.Hostname())
	require.NotEmpty(t, parsed.Port())
	fixture := &containerModbusFixture{}
	fixture.registers[0] = 37
	device, err := modbus.NewServer(&modbus.ServerConfiguration{
		URL: "tcp://" + net.JoinHostPort("0.0.0.0", parsed.Port()), Timeout: time.Second,
		MaxClients: 10, Logger: log.New(io.Discard, "", 0),
	}, fixture)
	require.NoError(t, err)
	require.NoError(t, device.Start())
	t.Cleanup(func() { require.NoError(t, device.Stop()) })
	client := NewRemoteModbus(socket)
	t.Cleanup(client.Close)
	ctx, cancel := context.WithTimeout(WithSourceRevision(context.Background(), 1), 60*time.Second)
	defer cancel()
	definition := sourcestream.SourceDefinition{
		Name: "Independent container protocol fixture", Protocol: sourcestream.ModbusTCP,
		Endpoint: endpoint, RuntimeLocation: "local",
		Modbus: &sourcestream.ModbusOptions{UnitID: 7, PollIntervalMs: 100, Ranges: []sourcestream.ModbusRange{
			{ID: "temperature", Table: "holding-register", Address: 0, Quantity: 1},
			{ID: "switch", Table: "coil", Address: 0, Quantity: 1},
		}},
	}
	credential := sourcestream.Credential{}
	const sourceID int64 = 701
	require.NoError(t, client.Ready(ctx))
	require.NoError(t, client.Probe(ctx, sourceID, definition, credential))
	resources, err := client.Discover(ctx, sourceID, definition, credential, "")
	require.NoError(t, err)
	require.Len(t, resources, 2)
	read := func(ref string) any {
		t.Helper()
		value, err := client.Invoke(ctx, sourceID, definition, credential, ref, map[string]any{"operation": "read"})
		require.NoError(t, err)
		return value.(map[string]any)["values"].([]any)[0]
	}
	require.Equal(t, float64(37), read("modbus:temperature"))
	// Prove the configured endpoint is this test's own protocol fixture before
	// sending either write; never mutate an accidentally configured real device.
	require.Positive(t, fixture.reads.Load())
	_, err = client.Invoke(ctx, sourceID, definition, credential, "modbus:temperature", map[string]any{"operation": "write", "values": []int{42}})
	require.NoError(t, err)
	_, err = client.Invoke(ctx, sourceID, definition, credential, "modbus:switch", map[string]any{"operation": "write", "values": []bool{true}})
	require.NoError(t, err)
	require.Equal(t, float64(42), read("modbus:temperature"))
	require.Equal(t, true, read("modbus:switch"))

	watchCtx, stopWatch := context.WithTimeout(ctx, 5*time.Second)
	defer stopWatch()
	ready, done := make(chan struct{}, 1), make(chan error, 1)
	var accepted atomic.Int32
	watchCtx = sourcestream.WithWatchObserver(watchCtx, func(value bool) {
		if value {
			select {
			case ready <- struct{}{}:
			default:
			}
		}
	})
	go func() {
		done <- client.Watch(watchCtx, sourceID, definition, credential, func(event Input) error {
			if event.ResourceRef == "modbus:temperature" || event.ResourceRef == "modbus:switch" {
				accepted.Add(1)
				return nil
			}
			return ErrProtocol
		})
	}()
	select {
	case <-ready:
		require.GreaterOrEqual(t, accepted.Load(), int32(2), "readiness requires both ranges accepted")
	case <-watchCtx.Done():
		t.Fatal("independent Runtime did not complete first protocol poll")
	}
	stopWatch()
	select {
	case err := <-done:
		require.Error(t, err, "canceling the live watch must terminate its Unix transport")
	case <-time.After(time.Second):
		t.Fatal("watch cancellation did not complete")
	}
	require.Equal(t, int32(1), fixture.registerWrites.Load())
	require.Equal(t, int32(1), fixture.coilWrites.Load())
	fmt.Fprintln(os.Stdout, "MODBUS_CONTAINER_RESTART_READY")

	waitReady := func(want bool) {
		t.Helper()
		phase, stop := context.WithTimeout(ctx, 20*time.Second)
		defer stop()
		for {
			attempt, finish := context.WithTimeout(phase, 500*time.Millisecond)
			err := client.Ready(attempt)
			finish()
			if (err == nil) == want {
				return
			}
			select {
			case <-phase.Done():
				t.Fatalf("independent Runtime readiness never became %v: %v", want, phase.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	waitReady(false)
	fmt.Fprintln(os.Stdout, "MODBUS_CONTAINER_RESTART_DOWN")
	waitReady(true)
	require.Equal(t, float64(42), read("modbus:temperature"))
	require.Equal(t, true, read("modbus:switch"))
	require.Equal(t, int32(1), fixture.registerWrites.Load(), "Runtime recovery must not replay a register write")
	require.Equal(t, int32(1), fixture.coilWrites.Load(), "Runtime recovery must not replay a coil write")
	fmt.Fprintln(os.Stdout, "MODBUS_CONTAINER_RESTART_RECOVERED")
}

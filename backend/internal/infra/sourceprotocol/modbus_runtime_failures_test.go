package sourceprotocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"github.com/simonvetter/modbus"
	"github.com/stretchr/testify/require"
)

// Exercise the actual Unix HTTP transport and real Modbus TCP frames. These
// fixtures deliberately remain protocol test services, not production devices.
func failureModbusSocket(t *testing.T, socket string) (*ModbusRuntime, func()) {
	t.Helper()
	runtime := NewModbusRuntime()
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)
	server := &http.Server{Handler: runtime, ReadHeaderTimeout: time.Second}
	finished := make(chan struct{})
	go func() { defer close(finished); _ = server.Serve(listener) }()
	var once sync.Once
	stop := func() { once.Do(func() { runtime.Close(); _ = server.Close(); <-finished }) }
	t.Cleanup(stop)
	return runtime, stop
}

func TestRemoteModbusRuntimeRestartsAndReadReconnects(t *testing.T) {
	endpoint, fixture := startReconnectModbus(t, time.Second)
	socket := filepath.Join(t.TempDir(), "runtime.sock")
	_, stop := failureModbusSocket(t, socket)
	client := NewRemoteModbus(socket)
	t.Cleanup(client.Close)
	definition := reconnectDefinition(endpoint)
	definition.Name, definition.RuntimeLocation = "Restart fixture", "local"
	read := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		value, err := client.Invoke(ctx, 80, definition, sourcestream.Credential{}, "modbus:point", map[string]any{"operation": "read"})
		require.NoError(t, err)
		require.Equal(t, float64(37), value.(map[string]any)["values"].([]any)[0])
	}
	read()
	stop()
	require.ErrorIs(t, client.Ready(context.Background()), sourcestream.ErrUnavailable)
	_, _ = failureModbusSocket(t, socket)
	require.NoError(t, client.Ready(context.Background()))
	read()
	require.Equal(t, int32(2), fixture.reads.Load())
	require.Zero(t, fixture.writes.Load(), "runtime recovery must not manufacture device writes")
}

func TestRemoteModbusBackpressureAndCancellation(t *testing.T) {
	endpoint, fixture := startReconnectModbus(t, time.Second)
	socket := filepath.Join(t.TempDir(), "watch.sock")
	runtime, _ := failureModbusSocket(t, socket)
	client := NewRemoteModbus(socket)
	t.Cleanup(client.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	definition := reconnectDefinition(endpoint)
	definition.Name, definition.RuntimeLocation = "Backpressure fixture", "local"
	go func() {
		done <- client.Watch(ctx, 81, definition, sourcestream.Credential{}, func(Input) error {
			select {
			case <-entered:
			default:
				close(entered)
			}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("no actual Modbus input received")
	}
	time.Sleep(250 * time.Millisecond)
	require.Equal(t, int32(1), fixture.reads.Load(), "no second device read before Backend acknowledges durable acceptance")
	cancel()
	close(release)
	select {
	case err := <-done:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("canceled watch did not release transport")
	}
	require.Eventually(t, func() bool { return len(runtime.watches) == 0 }, time.Second, 10*time.Millisecond)
}

func TestRemoteModbusMalformedAndOversizeIPC(t *testing.T) {
	runtime, client, def, _ := remoteModbusFixture(t)
	valid, err := json.Marshal(modbusRequest{SourceID: 3, Revision: 1, Definition: def})
	require.NoError(t, err)
	for _, body := range []string{"not-json\n", "{\"sourceId\":-1}\n", strings.Repeat("x", modbusWireLimit+2) + "\n", "{}\n"} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://source-modbus/probe", strings.NewReader(body))
		require.NoError(t, err)
		response, err := client.http.Do(req)
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.Equal(t, http.StatusBadRequest, response.StatusCode)
	}
	require.Zero(t, len(runtime.ops))
	request, err := http.NewRequest(http.MethodPost, "http://source-modbus/probe", bytes.NewReader(append(valid, '\n')))
	require.NoError(t, err)
	response, err := client.http.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode, "invalid frames must not poison subsequent requests")
}

func TestRemoteModbusWatchSaturationIsImmediateAndRecovers(t *testing.T) {
	runtime, client, def, _ := remoteModbusFixture(t)
	for i := 0; i < cap(runtime.watches); i++ {
		runtime.watches <- struct{}{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	started := time.Now()
	err := client.Watch(ctx, 90, def, sourcestream.Credential{}, func(Input) error { return nil })
	require.ErrorIs(t, err, sourcestream.ErrUnavailable)
	require.Less(t, time.Since(started), 500*time.Millisecond, "busy response must not wait for a never-ending watch body")
	for i := 0; i < cap(runtime.watches); i++ {
		<-runtime.watches
	}
	require.NoError(t, client.Probe(ctx, 90, def, sourcestream.Credential{}))
}

func TestRemoteModbusConfigurationRevisionReplacesOnlyItsSource(t *testing.T) {
	runtime, client, def, _ := remoteModbusFixture(t)
	pool := runtime.runtime.drivers[sourcestream.ModbusTCP].(ModbusDriver).pool
	ctx1 := WithSourceRevision(context.Background(), 1)
	ctx2 := WithSourceRevision(context.Background(), 2)
	require.NoError(t, client.Probe(ctx1, 91, def, sourcestream.Credential{}))
	require.NoError(t, client.Probe(ctx1, 92, def, sourcestream.Credential{}))
	first, err := pool.entry(ctx1, 91, def, sourcestream.Credential{})
	require.NoError(t, err)
	other, err := pool.entry(ctx1, 92, def, sourcestream.Credential{})
	require.NoError(t, err)
	require.NoError(t, client.Probe(ctx2, 91, def, sourcestream.Credential{}))
	updated, err := pool.entry(ctx2, 91, def, sourcestream.Credential{})
	require.NoError(t, err)
	unchanged, err := pool.entry(ctx1, 92, def, sourcestream.Credential{})
	require.NoError(t, err)
	require.NotSame(t, first, updated)
	require.Same(t, other, unchanged)
	require.ErrorIs(t, first.ctx.Err(), context.Canceled)
}

func TestRemoteModbusMultipleUnitsAndSourceIsolation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "tcp://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	fixture := &multiUnitFixture{values: map[uint8]uint16{1: 100, 21: 2100}}
	device, err := modbus.NewServer(&modbus.ServerConfiguration{URL: endpoint, Timeout: time.Second, MaxClients: 10, Logger: log.New(io.Discard, "", 0)}, fixture)
	require.NoError(t, err)
	require.NoError(t, device.Start())
	t.Cleanup(func() { require.NoError(t, device.Stop()) })
	socket := filepath.Join(t.TempDir(), "runtime.sock")
	runtime, _ := failureModbusSocket(t, socket)
	client := NewRemoteModbus(socket)
	t.Cleanup(client.Close)
	def := reconnectDefinition(endpoint)
	def.Name, def.RuntimeLocation = "Multi-unit fixture", "local"
	def.Modbus.UnitID = 1
	def.Modbus.Ranges = nil
	for _, unit := range []int{1, 21} {
		id := unit
		def.Modbus.Ranges = append(def.Modbus.Ranges, sourcestream.ModbusRange{ID: fmt.Sprintf("unit-%d", unit), Table: "holding-register", Quantity: 1, UnitID: &id})
	}
	var group sync.WaitGroup
	results := make(chan error, 16)
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			unit := []int{1, 21}[i%2]
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			value, err := client.Invoke(ctx, int64(100+i%3), def, sourcestream.Credential{}, fmt.Sprintf("modbus:unit-%d", unit), map[string]any{"operation": "read"})
			if err == nil && value.(map[string]any)["values"].([]any)[0] != float64(unit*100) {
				err = fmt.Errorf("wrong Unit ID result for %d", unit)
			}
			results <- err
		}(i)
	}
	group.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	pool := runtime.runtime.drivers[sourcestream.ModbusTCP].(ModbusDriver).pool
	pool.mu.Lock()
	count := len(pool.entries)
	pool.mu.Unlock()
	require.Equal(t, 3, count)
	require.Zero(t, fixture.writes)
}

func TestRemoteModbusLostWriteReceiptIsNeverReplayed(t *testing.T) {
	upstream, fixture := startReconnectModbus(t, time.Second)
	endpoint, connections := startDisconnectProxy(t, upstream, "discard-write-response")
	socket := filepath.Join(t.TempDir(), "runtime.sock")
	_, _ = failureModbusSocket(t, socket)
	client := NewRemoteModbus(socket)
	t.Cleanup(client.Close)
	definition := reconnectDefinition(endpoint)
	definition.Name, definition.RuntimeLocation = "Lost receipt fixture", "local"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := client.Invoke(ctx, 104, definition, sourcestream.Credential{}, "modbus:point", map[string]any{"operation": "write", "values": []int{41}})
	require.ErrorIs(t, err, sourcestream.ErrUnknownResult)
	require.Equal(t, int32(1), connections.Load())
	require.Equal(t, int32(1), fixture.writes.Load(), "accepted write with a lost reply must not be retried by either transport layer")
	fixture.mu.Lock()
	value := fixture.registers[0]
	fixture.mu.Unlock()
	require.Equal(t, uint16(41), value)
}

func TestRemoteModbusInvalidSourceRejectsUnfinishedBody(t *testing.T) {
	_, client, _, _ := remoteModbusFixture(t)
	for _, test := range []struct{ name, path, body string }{
		{"invalid-source", "/probe", "{}\n"},
		{"invalid-release", "/release", "{}\n"},
		{"malformed-json", "/probe", "not-json\n"},
		{"negative-source", "/probe", "{\"sourceId\":-1}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection, err := client.http.Transport.(*http.Transport).DialContext(context.Background(), "unix", "")
			require.NoError(t, err)
			defer connection.Close()
			require.NoError(t, connection.SetDeadline(time.Now().Add(700*time.Millisecond)))
			// Deliberately leave the request stream open. Invalid configuration
			// must be rejected without waiting for body EOF or the read timeout.
			started := time.Now()
			_, err = fmt.Fprintf(connection, "POST %s HTTP/1.1\r\nHost: source-modbus\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n", test.path, len(test.body), test.body)
			require.NoError(t, err)
			response, err := http.ReadResponse(bufio.NewReader(connection), nil)
			require.NoError(t, err, "invalid Source must receive immediate rejection, not wait for request EOF")
			defer response.Body.Close()
			require.Equal(t, http.StatusBadRequest, response.StatusCode)
			require.Less(t, time.Since(started), 500*time.Millisecond)
		})
	}
}

package sourceprotocol

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"github.com/simonvetter/modbus"
	"github.com/stretchr/testify/require"
)

// Actual Modbus TCP fixtures exercise idle expiry, missing responses and
// protocol exceptions; they are test services, not industrial hardware.
type reconnectRegisters struct {
	registerFixture
	reads  atomic.Int32
	writes atomic.Int32
}

func (fixture *reconnectRegisters) HandleHoldingRegisters(request *modbus.HoldingRegistersRequest) ([]uint16, error) {
	if request.IsWrite {
		fixture.writes.Add(1)
	} else {
		fixture.reads.Add(1)
	}
	return fixture.registerFixture.HandleHoldingRegisters(request)
}

func startReconnectModbus(t *testing.T, idle time.Duration) (string, *reconnectRegisters) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "tcp://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	fixture := &reconnectRegisters{}
	fixture.registers[0] = 37
	server, err := modbus.NewServer(&modbus.ServerConfiguration{URL: endpoint, Timeout: idle, MaxClients: 10, Logger: log.New(io.Discard, "", 0)}, fixture)
	require.NoError(t, err)
	require.NoError(t, server.Start())
	t.Cleanup(func() { require.NoError(t, server.Stop()) })
	return endpoint, fixture
}

func reconnectDefinition(endpoint string) sourcestream.SourceDefinition {
	return sourcestream.SourceDefinition{Protocol: sourcestream.ModbusTCP, Endpoint: endpoint,
		Modbus: &sourcestream.ModbusOptions{UnitID: 7, PollIntervalMs: 100, Ranges: []sourcestream.ModbusRange{{ID: "point", Table: "holding-register", Address: 0, Quantity: 1}}}}
}

func readModbusTestFrame(connection net.Conn) ([]byte, error) {
	header := make([]byte, 7)
	if _, err := io.ReadFull(connection, header); err != nil {
		return nil, err
	}
	length := int(binary.BigEndian.Uint16(header[4:6]))
	if length < 2 || length > 254 {
		return nil, errors.New("invalid test MBAP length")
	}
	payload := make([]byte, length-1)
	if _, err := io.ReadFull(connection, payload); err != nil {
		return nil, err
	}
	return append(header, payload...), nil
}

func startDisconnectProxy(t *testing.T, upstream, mode string) (string, *atomic.Int32) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	connections := &atomic.Int32{}
	var mutex sync.Mutex
	live := map[net.Conn]bool{}
	var workers sync.WaitGroup
	accepted := make(chan struct{})
	go func() {
		defer close(accepted)
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			mutex.Lock()
			live[client] = true
			mutex.Unlock()
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer client.Close()
				defer func() { mutex.Lock(); delete(live, client); mutex.Unlock() }()
				_ = client.SetDeadline(time.Now().Add(5 * time.Second))
				request, err := readModbusTestFrame(client)
				if err != nil || mode == "disconnect" {
					return
				}
				if mode == "timeout" {
					_, _ = client.Read(make([]byte, 1))
					return
				}
				server, err := net.Dial("tcp", upstream[len("tcp://"):])
				if err != nil {
					return
				}
				defer server.Close()
				_ = server.SetDeadline(time.Now().Add(5 * time.Second))
				for count := 0; count < 3; count++ {
					if _, err = server.Write(request); err != nil {
						return
					}
					response, err := readModbusTestFrame(server)
					if err != nil || mode == "discard-write-response" ||
						mode == "discard-cached-write-response" && (request[7] == 0x10 || request[7] == 0x0f) {
						return
					}
					if _, err = client.Write(response); err != nil || mode != "discard-cached-write-response" {
						return
					}
					request, err = readModbusTestFrame(client)
					if err != nil {
						return
					}
				}
			}()
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-accepted
		mutex.Lock()
		for client := range live {
			_ = client.Close()
		}
		mutex.Unlock()
		workers.Wait()
	})
	return "tcp://" + listener.Addr().String(), connections
}

func TestRealModbusIdleFirstReadReconnectsWithinSourceLease(t *testing.T) {
	endpoint, fixture := startReconnectModbus(t, 5*time.Second)
	pool := newSourceConnectionPool()
	t.Cleanup(pool.close)
	driver := ModbusDriver{pool: pool}
	definition := reconnectDefinition(endpoint)
	read := func() any {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		value, err := driver.Invoke(ctx, 71, definition, sourcestream.Credential{}, "modbus:point", map[string]any{"operation": "read"})
		require.NoError(t, err)
		return value
	}
	require.Equal(t, []uint16{37}, read().(map[string]any)["values"])
	entry, err := pool.entry(context.Background(), 71, definition, sourcestream.Credential{})
	require.NoError(t, err)
	before := entry.value
	// Production idle eviction remains 60s. The actual server closes at 5s.
	require.Equal(t, 60*time.Second, pool.idle)
	time.Sleep(5500 * time.Millisecond)
	require.Equal(t, []uint16{37}, read().(map[string]any)["values"])
	after, err := pool.entry(context.Background(), 71, definition, sourcestream.Credential{})
	require.NoError(t, err)
	require.Same(t, entry, after, "read recovery must preserve Source owner generation")
	require.NotSame(t, before, after.value, "the stale TCP transport must be replaced")
	require.Equal(t, int32(2), fixture.reads.Load())
}

func TestRealModbusIdleFirstWriteRecoversWithoutRepeatingEffect(t *testing.T) {
	endpoint, fixture := startReconnectModbus(t, 5*time.Second)
	pool := newSourceConnectionPool()
	t.Cleanup(pool.close)
	driver := ModbusDriver{pool: pool}
	definition := reconnectDefinition(endpoint)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := driver.Invoke(ctx, 74, definition, sourcestream.Credential{}, "modbus:point", map[string]any{"operation": "read"})
	require.NoError(t, err)
	// The Source lease is still valid, but the actual device has closed its TCP connection.
	time.Sleep(5500 * time.Millisecond)
	_, err = driver.Invoke(ctx, 74, definition, sourcestream.Credential{}, "modbus:point", map[string]any{"operation": "write", "values": []int{41}})
	require.NoError(t, err, "recover the stale transport before dispatch, never replay the write")
	require.Equal(t, int32(1), fixture.writes.Load())
	fixture.mu.Lock()
	require.Equal(t, uint16(41), fixture.registers[0])
	fixture.mu.Unlock()
}

func TestRealModbusReconnectReadBoundAndWriteNoRetry(t *testing.T) {
	for _, mode := range []string{"disconnect", "discard-write-response", "discard-cached-write-response", "protocol-error", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			upstream, fixture := startReconnectModbus(t, 5*time.Second)
			endpoint, connections := startDisconnectProxy(t, upstream, mode)
			pool := newSourceConnectionPool()
			t.Cleanup(pool.close)
			driver := ModbusDriver{pool: pool}
			definition := reconnectDefinition(endpoint)
			input := map[string]any{"operation": "read"}
			if mode == "discard-write-response" || mode == "discard-cached-write-response" {
				input = map[string]any{"operation": "write", "values": []int{41}}
			}
			if mode == "protocol-error" {
				definition.Modbus.Ranges[0].Address = 99
			}
			ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
			defer cancel()
			if mode == "discard-cached-write-response" {
				_, err := driver.Invoke(ctx, 72, definition, sourcestream.Credential{}, "modbus:point", map[string]any{"operation": "read"})
				require.NoError(t, err, "prime a reusable connection before the write")
			}
			started := time.Now()
			_, err := driver.Invoke(ctx, 72, definition, sourcestream.Credential{}, "modbus:point", input)
			require.Error(t, err)
			require.Less(t, time.Since(started), time.Second)
			if mode == "disconnect" {
				require.ErrorIs(t, err, ErrTransportDisconnected)
				require.ErrorIs(t, err, ErrProtocol)
				require.Equal(t, int32(2), connections.Load(), "only one reconnect is allowed")
			} else {
				require.Equal(t, int32(1), connections.Load(), "non-disconnect reads and writes must not reconnect")
			}
			if mode == "discard-write-response" || mode == "discard-cached-write-response" {
				require.ErrorIs(t, err, sourcestream.ErrUnknownResult)
				require.Equal(t, int32(1), fixture.writes.Load(), "accepted write with lost receipt is never retried")
			}
			if mode == "discard-cached-write-response" {
				require.Equal(t, int32(2), fixture.reads.Load(), "only the initial and scoped preflight reads precede the single write")
			}
			if mode == "protocol-error" {
				require.ErrorIs(t, err, ErrProtocol)
				require.NotErrorIs(t, err, ErrTransportDisconnected)
				require.Equal(t, int32(1), fixture.reads.Load())
			}
			if mode == "timeout" {
				require.NotErrorIs(t, err, ErrTransportDisconnected)
			}
			require.NotContains(t, err.Error(), endpoint)
		})
	}
}

func TestModbusReconnectClassification(t *testing.T) {
	for _, err := range []error{io.EOF, io.ErrUnexpectedEOF, net.ErrClosed, syscall.ECONNRESET, syscall.EPIPE, syscall.ENOTCONN} {
		require.True(t, modbusDisconnected(err))
		require.ErrorIs(t, safeModbusError(err), ErrTransportDisconnected)
	}
	for _, err := range []error{modbus.ErrRequestTimedOut, modbus.ErrIllegalDataAddress, modbus.ErrBadTransactionId, context.Canceled, context.DeadlineExceeded} {
		require.False(t, modbusDisconnected(err))
	}
}

func TestSourcePoolReconnectCannotReviveReleasedGeneration(t *testing.T) {
	pool := newSourceConnectionPool()
	t.Cleanup(pool.close)
	definition := reconnectDefinition("tcp://127.0.0.1:1502")
	ctx := context.Background()
	entry, err := pool.entry(ctx, 73, definition, sourcestream.Credential{})
	require.NoError(t, err)
	started, release := make(chan struct{}), make(chan struct{})
	var closed atomic.Int32
	initial := func(context.Context) (any, func(), error) { return 1, func() { closed.Add(1) }, nil }
	reconnect := func(context.Context) (any, func(), error) {
		close(started)
		<-release
		return 2, func() { closed.Add(1) }, nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := pool.withEntry(ctx, entry, initial, func(opCtx context.Context, _ any) (any, error) {
			return pool.replaceValue(opCtx, entry, reconnect)
		})
		done <- err
	}()
	<-started
	pool.releaseSource(73)
	current, err := pool.with(WithSourceRevision(ctx, 2), 73, definition, sourcestream.Credential{},
		func(context.Context) (any, func(), error) { return 3, func() {}, nil },
		func(_ context.Context, value any) (any, error) { return value, nil })
	require.NoError(t, err)
	require.Equal(t, 3, current)
	close(release)
	require.ErrorIs(t, <-done, context.Canceled)
	require.Eventually(t, func() bool { return closed.Load() == 2 }, time.Second, time.Millisecond)
	again, err := pool.with(WithSourceRevision(ctx, 2), 73, definition, sourcestream.Credential{},
		func(context.Context) (any, func(), error) {
			t.Error("new owner unexpectedly reconnected")
			return nil, func() {}, nil
		},
		func(_ context.Context, value any) (any, error) { return value, nil })
	require.NoError(t, err)
	require.Equal(t, current, again)
}

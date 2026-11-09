package sourceprotocol

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"github.com/simonvetter/modbus"
	"github.com/stretchr/testify/require"
)

// This is a protocol test server, not a real industrial device or HTTP mock.
type registerFixture struct {
	mu         sync.Mutex
	registers  [16]uint16
	coils      [16]bool
	delayWrite time.Duration
}

func (f *registerFixture) HandleHoldingRegisters(r *modbus.HoldingRegistersRequest) ([]uint16, error) {
	if r.UnitId != 7 || int(r.Addr)+int(r.Quantity) > len(f.registers) {
		return nil, modbus.ErrIllegalDataAddress
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.IsWrite {
		copy(f.registers[int(r.Addr):], r.Args)
		time.Sleep(f.delayWrite)
	}
	return append([]uint16(nil), f.registers[int(r.Addr):int(r.Addr)+int(r.Quantity)]...), nil
}
func (f *registerFixture) HandleInputRegisters(r *modbus.InputRegistersRequest) ([]uint16, error) {
	return f.HandleHoldingRegisters(&modbus.HoldingRegistersRequest{UnitId: r.UnitId, Addr: r.Addr, Quantity: r.Quantity})
}
func (f *registerFixture) HandleCoils(r *modbus.CoilsRequest) ([]bool, error) {
	if r.UnitId != 7 || int(r.Addr)+int(r.Quantity) > len(f.coils) {
		return nil, modbus.ErrIllegalDataAddress
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.IsWrite {
		copy(f.coils[int(r.Addr):], r.Args)
	}
	return append([]bool(nil), f.coils[int(r.Addr):int(r.Addr)+int(r.Quantity)]...), nil
}
func (f *registerFixture) HandleDiscreteInputs(r *modbus.DiscreteInputsRequest) ([]bool, error) {
	return f.HandleCoils(&modbus.CoilsRequest{UnitId: r.UnitId, Addr: r.Addr, Quantity: r.Quantity})
}

func TestRealModbusSourceReadWriteWatchAndUnknownReceipt(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "tcp://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	fixture := &registerFixture{}
	fixture.registers[0] = 37
	server, err := modbus.NewServer(&modbus.ServerConfiguration{URL: endpoint, Timeout: time.Second, MaxClients: 10, Logger: log.New(io.Discard, "", 0)}, fixture)
	require.NoError(t, err)
	require.NoError(t, server.Start())
	t.Cleanup(func() { require.NoError(t, server.Stop()) })
	def := sourcestream.RuntimeSource{Name: "Protocol fixture", Protocol: sourcestream.ModbusTCP, Endpoint: endpoint, RuntimeLocation: "local", Modbus: &sourcestream.ModbusOptions{UnitID: 7, PollIntervalMs: 100, Ranges: []sourcestream.ModbusRange{
		{ID: "temperature", Table: "holding-register", Address: 0, Quantity: 2},
		{ID: "readonly", Table: "input-register", Address: 2, Quantity: 1},
		{ID: "switch", Table: "coil", Address: 0, Quantity: 1},
		{ID: "status", Table: "discrete-input", Address: 0, Quantity: 1},
	}}}
	driver := ModbusDriver{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	credential := sourcestream.Credential{}
	require.NoError(t, driver.Probe(ctx, 1, def, credential))
	resources, err := driver.Discover(ctx, 1, def, credential, "")
	require.NoError(t, err)
	require.Len(t, resources, 4, "legacy Source-owned ranges remain discoverable")
	connectionOnly := def.Connection()
	connectionOnly.Modbus = &sourcestream.ModbusConnection{UnitID: 7}
	require.NoError(t, driver.Probe(ctx, 1, connectionOnly, credential))
	resources, err = driver.Discover(ctx, 1, connectionOnly, credential, "")
	require.NoError(t, err)
	require.Empty(t, resources, "connection-only discovery must not invent Stream registers")
	read, err := driver.Invoke(ctx, 1, def, credential, "modbus:temperature", map[string]any{"operation": "read"})
	require.NoError(t, err)
	require.Equal(t, []uint16{37, 0}, read.(map[string]any)["values"])
	_, err = driver.Invoke(ctx, 1, def, credential, "modbus:temperature", map[string]any{"operation": "write", "values": []int{41, 42}})
	require.NoError(t, err)
	_, err = driver.Invoke(ctx, 1, def, credential, "modbus:switch", map[string]any{"operation": "write", "values": []bool{true}})
	require.NoError(t, err)
	_, err = driver.Invoke(ctx, 1, def, credential, "modbus:readonly", map[string]any{"operation": "write", "values": []int{1}})
	require.ErrorIs(t, err, ErrProtocol)
	_, err = driver.Invoke(ctx, 1, def, credential, "modbus:temperature", map[string]any{"operation": "write", "values": []int{65536, 0}})
	require.ErrorIs(t, err, ErrProtocol)
	stop := errors.New("test watch complete")
	err = driver.Watch(ctx, 1, def, credential, func(event Input) error {
		require.Equal(t, "modbus:temperature", event.ResourceRef)
		require.Equal(t, []uint16{41, 42}, event.Data.(map[string]any)["values"])
		require.Positive(t, event.ReceivedAt)
		return stop
	})
	require.ErrorIs(t, err, stop)
	wrongUnit := def
	options := *def.Modbus
	options.UnitID = 8
	wrongUnit.Modbus = &options
	_, err = driver.Invoke(ctx, 1, wrongUnit, credential, "modbus:temperature", map[string]any{"operation": "read"})
	require.Error(t, err)
	// The server applies the write before withholding its response. A timeout
	// must not be represented as a definite failure safe to repeat.
	fixture.mu.Lock()
	fixture.delayWrite = 250 * time.Millisecond
	fixture.mu.Unlock()
	shortCtx, shortCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer shortCancel()
	_, err = driver.Invoke(shortCtx, 1, def, credential, "modbus:temperature", map[string]any{"operation": "write", "values": []int{51, 52}})
	require.ErrorIs(t, err, sourcestream.ErrUnknownResult)
	fixture.mu.Lock()
	require.Equal(t, uint16(51), fixture.registers[0])
	fixture.mu.Unlock()
}

// 真实 Modbus TCP：首轮全部范围成功才能ready；后续范围失败不得假ready。
func TestRealModbusWatchReadinessRequiresFullFirstPoll(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "tcp://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	server, err := modbus.NewServer(&modbus.ServerConfiguration{URL: endpoint, Timeout: time.Second, MaxClients: 10, Logger: log.New(io.Discard, "", 0)}, &registerFixture{})
	require.NoError(t, err)
	require.NoError(t, server.Start())
	t.Cleanup(func() { require.NoError(t, server.Stop()) })
	for _, invalidSecond := range []bool{false, true} {
		t.Run(fmt.Sprintf("invalidSecond=%t", invalidSecond), func(t *testing.T) {
			def := sourcestream.SourceDefinition{Protocol: sourcestream.ModbusTCP, Endpoint: endpoint, Modbus: &sourcestream.ModbusOptions{UnitID: 7, PollIntervalMs: 100, Ranges: []sourcestream.ModbusRange{{ID: "first", Table: "holding-register", Address: 0, Quantity: 1}, {ID: "second", Table: "holding-register", Address: 1, Quantity: 1}}}}
			if invalidSecond {
				def.Modbus.Ranges[1].Address = 99
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			accepted, readyCount := 0, 0
			ctx = sourcestream.WithWatchObserver(ctx, func(ready bool) {
				if ready {
					require.Equal(t, 2, accepted)
					readyCount++
					cancel()
				}
			})
			err = (ModbusDriver{}).Watch(ctx, 1, def, sourcestream.Credential{}, func(Input) error {
				accepted++
				require.Zero(t, readyCount, "a partial first poll is not ready")
				return nil
			})
			if invalidSecond {
				require.ErrorIs(t, err, ErrProtocol)
				require.Zero(t, readyCount)
			} else {
				require.ErrorIs(t, err, context.Canceled)
				require.Equal(t, 1, readyCount)
			}
		})
	}
}

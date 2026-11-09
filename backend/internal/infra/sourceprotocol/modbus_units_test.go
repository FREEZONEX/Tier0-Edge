package sourceprotocol

import (
	"context"
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

// A real TCP protocol fixture deliberately returns different values at the
// same register address for each station. It is not an HTTP device mock.
type multiUnitFixture struct {
	mu     sync.Mutex
	values map[uint8]uint16
	writes int
}

func (f *multiUnitFixture) HandleHoldingRegisters(r *modbus.HoldingRegistersRequest) ([]uint16, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	value, exists := f.values[r.UnitId]
	if !exists || r.Addr != 0 || r.Quantity != 1 {
		return nil, modbus.ErrIllegalDataAddress
	}
	if r.IsWrite {
		f.writes++
		f.values[r.UnitId] = r.Args[0]
		value = r.Args[0]
	}
	return []uint16{value}, nil
}
func (f *multiUnitFixture) HandleInputRegisters(r *modbus.InputRegistersRequest) ([]uint16, error) {
	return f.HandleHoldingRegisters(&modbus.HoldingRegistersRequest{UnitId: r.UnitId, Addr: r.Addr, Quantity: r.Quantity})
}
func (f *multiUnitFixture) HandleCoils(*modbus.CoilsRequest) ([]bool, error) {
	return nil, modbus.ErrIllegalDataAddress
}
func (f *multiUnitFixture) HandleDiscreteInputs(*modbus.DiscreteInputsRequest) ([]bool, error) {
	return nil, modbus.ErrIllegalDataAddress
}

func TestRealModbusSingleSourceMultipleUnitScopes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "tcp://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	units := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 21, 22, 23, 31, 41, 42}
	fixture := &multiUnitFixture{values: make(map[uint8]uint16)}
	definition := sourcestream.SourceDefinition{Protocol: sourcestream.ModbusTCP, Endpoint: endpoint,
		Modbus: &sourcestream.ModbusOptions{UnitID: 1, PollIntervalMs: 100}}
	for _, unit := range units {
		fixture.values[uint8(unit)] = uint16(unit * 100)
		unitID, writable := unit, unit == 21
		definition.Modbus.Ranges = append(definition.Modbus.Ranges, sourcestream.ModbusRange{
			ID: fmt.Sprintf("unit-%d", unit), Table: "holding-register", Address: 0, Quantity: 1,
			UnitID: &unitID, Writable: &writable,
		})
	}
	server, err := modbus.NewServer(&modbus.ServerConfiguration{URL: endpoint, Timeout: time.Second,
		MaxClients: 10, Logger: log.New(io.Discard, "", 0)}, fixture)
	require.NoError(t, err)
	require.NoError(t, server.Start())
	t.Cleanup(func() { require.NoError(t, server.Stop()) })
	pool := newSourceConnectionPool()
	t.Cleanup(pool.close)
	driver := ModbusDriver{pool: pool}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resources, err := driver.Discover(ctx, 81, definition, sourcestream.Credential{}, "")
	require.NoError(t, err)
	require.Len(t, resources, len(units))
	for i, unit := range units {
		require.Equal(t, unit, resources[i].Metadata["unitId"])
		if unit == 21 {
			require.Contains(t, resources[i].Models, sourcestream.Subscribe)
		} else {
			require.NotContains(t, resources[i].Models, sourcestream.Subscribe)
		}
	}
	// Concurrent callers share the Source connection gate, including switching
	// the Unit ID. A default Unit 1 result must never leak into another station.
	var group sync.WaitGroup
	results := make(chan error, len(units)*4)
	for repeat := 0; repeat < 4; repeat++ {
		for _, unit := range units {
			group.Add(1)
			go func(unit int) {
				defer group.Done()
				value, err := driver.Invoke(ctx, 81, definition, sourcestream.Credential{},
					fmt.Sprintf("modbus:unit-%d", unit), map[string]any{"operation": "read"})
				if err == nil {
					row := value.(map[string]any)
					if row["unitId"] != unit || row["values"].([]uint16)[0] != uint16(unit*100) {
						err = fmt.Errorf("station mismatch for unit %d", unit)
					}
				}
				results <- err
			}(unit)
		}
	}
	group.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	_, err = driver.Invoke(ctx, 81, definition, sourcestream.Credential{}, "modbus:unit-1",
		map[string]any{"operation": "write", "values": []uint16{99}})
	require.ErrorIs(t, err, ErrProtocol)
	require.Zero(t, fixture.writes, "explicit read-only ranges must not send writes")
	_, err = driver.Invoke(ctx, 81, definition, sourcestream.Credential{}, "modbus:unit-21",
		map[string]any{"operation": "write", "values": []uint16{2123}})
	require.NoError(t, err)
	readback, err := driver.Invoke(ctx, 81, definition, sourcestream.Credential{}, "modbus:unit-21", map[string]any{"operation": "read"})
	require.NoError(t, err)
	require.Equal(t, []uint16{2123}, readback.(map[string]any)["values"])
	fixture.mu.Lock()
	require.Equal(t, uint16(100), fixture.values[1])
	require.Equal(t, 1, fixture.writes)
	fixture.mu.Unlock()
	// The alternate bounded wire reader uses the same per-resource station.
	words, err := readModbusContext(ctx, definition, definition.Modbus.Ranges[17])
	require.NoError(t, err)
	require.Equal(t, []uint16{4200}, words)
}

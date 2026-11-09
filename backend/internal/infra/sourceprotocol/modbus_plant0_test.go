package sourceprotocol

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strconv"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"github.com/stretchr/testify/require"
)

// This opt-in test reads the approved protocol simulation factory only. It
// neither changes settings nor touches the simulation control plane.
func TestPlant0RealModbusReadOnlyGateway(t *testing.T) {
	if os.Getenv("PLANT0_REAL_ACCEPTANCE") != "1" {
		t.Skip("set PLANT0_REAL_ACCEPTANCE=1 for the actual Plant0 read-only protocol test")
	}
	definition := sourcestream.SourceDefinition{Protocol: sourcestream.ModbusTCP,
		Endpoint: "tcp://52.221.121.67:1502", Modbus: &sourcestream.ModbusOptions{UnitID: 1, PollIntervalMs: 1000}}
	units := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 21, 22, 23, 31, 41, 42}
	for _, unit := range units {
		unitID, writable := unit, false
		r := sourcestream.ModbusRange{ID: "unit-" + strconv.Itoa(unit), UnitID: &unitID, Writable: &writable, Table: "holding-register"}
		switch {
		case unit <= 12:
			r.Address, r.Quantity = 3059, 2
		case unit <= 23:
			r.Address, r.Quantity = 1, 1
		case unit == 31:
			r.Address, r.Quantity = 10, 1
		default:
			r.Table, r.Address, r.Quantity = "input-register", 0, 3
		}
		definition.Modbus.Ranges = append(definition.Modbus.Ranges, r)
		if unit <= 12 {
			energy := r
			energy.ID, energy.Address, energy.Quantity = "energy-"+strconv.Itoa(unit), 3203, 4
			definition.Modbus.Ranges = append(definition.Modbus.Ranges, energy)
		}
	}
	pool := newSourceConnectionPool()
	t.Cleanup(pool.close)
	driver := ModbusDriver{pool: pool}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	resources, err := driver.Discover(ctx, 981, definition, sourcestream.Credential{}, "")
	require.NoError(t, err)
	require.Len(t, resources, 30)
	for _, resource := range resources {
		require.NotContains(t, resource.Models, sourcestream.Subscribe)
		value, err := driver.Invoke(ctx, 981, definition, sourcestream.Credential{}, resource.ID, map[string]any{"operation": "read"})
		require.NoError(t, err, resource.ID)
		row := value.(map[string]any)
		require.Equal(t, resource.Metadata["unitId"], row["unitId"], "response must retain the actual station")
		words := row["values"].([]uint16)
		var decoded any
		switch {
		case resource.Metadata["address"] == 3059:
			bits := uint32(words[0])<<16 | uint32(words[1])
			decoded = math.Float32frombits(bits)
			require.False(t, math.IsNaN(float64(decoded.(float32))) || math.IsInf(float64(decoded.(float32)), 0))
		case resource.Metadata["address"] == 3203:
			var raw [8]byte
			for i, word := range words {
				binary.BigEndian.PutUint16(raw[i*2:], word)
			}
			// INT64 identity is a string, not a JS floating point value.
			decoded = strconv.FormatInt(int64(binary.BigEndian.Uint64(raw[:])), 10)
		case resource.Metadata["table"] == "input-register":
			decoded = map[string]any{"state": words[0], "counter": uint32(words[1])<<16 | uint32(words[2])}
		case resource.Metadata["unitId"] == 31:
			decoded = float64(words[0]) / 10
		default:
			decoded = float64(words[0]) / 1000
		}
		t.Logf("%s station=%v decoded=%v", resource.ID, row["unitId"], decoded)
	}
	t.Log(fmt.Sprintf("real protocol: %d stations / %d read-only native ranges; not an UNS write or vendor certification", len(units), len(resources)))
}

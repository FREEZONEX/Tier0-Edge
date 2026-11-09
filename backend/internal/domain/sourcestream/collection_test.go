package sourcestream

// Verify Stream-owned collection and reject missing configuration.
import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestStreamCollectionRequiredWithoutMutatingSource(t *testing.T) {
	source := connectionSource(mqttSource())
	doc := StreamDefinition{SourceID: 12, Model: Publish, NativeResourceRefs: []string{"mqtt:factory/+/value"}, Collection: &CollectionDefinition{MQTT: &MQTTCollection{QoS: 2}}}
	result, err := doc.EffectiveSource(source)
	require.NoError(t, err)
	require.Equal(t, map[string]byte{"factory/+/value": 2}, result.MQTT.Subscriptions)
	require.Equal(t, 2, result.MQTT.QoS)
	require.Equal(t, "tier0", source.MQTT.ClientID)
	doc.Collection = nil
	result, err = doc.EffectiveSource(source)
	assertFieldError(t, err, "collection")
	doc.Collection = &CollectionDefinition{MQTT: &MQTTCollection{QoS: 3}}
	_, err = doc.EffectiveSource(source)
	require.Error(t, err)
}
func TestCollectionProtocolAndIntervals(t *testing.T) {
	source := SourceDefinition{Name: "PLC", RuntimeLocation: "local", Protocol: OPCUA, Endpoint: "opc.tcp://localhost:4840", OPCUA: &OPCUAConnection{SecurityMode: "None", SecurityPolicy: "None"}}
	require.NoError(t, source.Validate())
	doc := StreamDefinition{Model: Publish, NativeResourceRef: "opcua:ns=2;s=Temperature", Collection: &CollectionDefinition{OPCUA: &OPCUACollection{SamplingIntervalMs: 2000}}}
	effective, err := doc.EffectiveSource(source)
	require.NoError(t, err)
	require.Equal(t, []string{"ns=2;s=Temperature"}, effective.OPCUA.NodeIDs)
	doc.Collection.OPCUA.SamplingIntervalMs = 0
	_, err = doc.EffectiveSource(source)
	require.Error(t, err)
	doc.Collection = &CollectionDefinition{MQTT: &MQTTCollection{QoS: 1}}
	_, err = doc.EffectiveSource(source)
	require.Error(t, err)
}

func TestConnectionDocumentsContainNoCollectionFields(t *testing.T) {
	for _, runtime := range []RuntimeSource{mqttSource(), opcuaSource(), modbusSource()} {
		source := connectionSource(runtime)
		require.NoError(t, source.Validate())
		raw, err := json.Marshal(source)
		require.NoError(t, err)
		for _, field := range []string{"schemaVersion", "topicFilters", "qos", "requestTopic", "nodeIds", "samplingIntervalMs", "pollIntervalMs", "ranges"} {
			require.NotContains(t, string(raw), `"`+field+`"`)
		}
	}
}

func TestModbusCollectionExecutesOnlySelectedRanges(t *testing.T) {
	source := SourceDefinition{Name: "PLC", Protocol: ModbusTCP, Endpoint: "tcp://localhost:502", RuntimeLocation: "local", Modbus: &ModbusConnection{UnitID: 1}}
	ranges := []ModbusRange{{ID: "unused", Table: "holding-register", Address: 0, Quantity: 1}, {ID: "selected", Table: "holding-register", Address: 10, Quantity: 2}}
	for _, model := range []AccessModel{Publish, Subscribe, RequestReply} {
		t.Run(string(model), func(t *testing.T) {
			doc := StreamDefinition{Model: model, NativeResourceRef: "modbus:selected", Collection: &CollectionDefinition{Modbus: &ModbusCollection{PollIntervalMs: 1000, Ranges: ranges}}}
			effective, err := doc.EffectiveSource(source)
			require.NoError(t, err)
			require.Equal(t, []ModbusRange{ranges[1]}, effective.Modbus.Ranges)
			require.Len(t, doc.Collection.Modbus.Ranges, 2)
			effective.Modbus.Ranges[0].Address = 20
			require.Equal(t, 10, doc.Collection.Modbus.Ranges[1].Address, "runtime must not mutate saved collection")
			doc.NativeResourceRef = "modbus:missing"
			_, err = doc.EffectiveSource(source)
			require.Error(t, err)
			doc.NativeResourceRef = ""
			effective, err = doc.EffectiveSource(source)
			require.NoError(t, err)
			require.Empty(t, effective.Modbus.Ranges, "an unfinished draft must not acquire every defined range")
		})
	}
	doc := StreamDefinition{Model: Publish, NativeResourceRef: "modbus:selected", Collection: &CollectionDefinition{Modbus: &ModbusCollection{PollIntervalMs: 1000, Ranges: append(append([]ModbusRange{}, ranges...), ranges[0])}}}
	_, err := doc.EffectiveSource(source)
	require.Error(t, err, "unselected duplicate definitions must still be rejected")
}

func TestModbusCollectionPreservesStationAndRejectsReadOnlyDownlink(t *testing.T) {
	source := SourceDefinition{Name: "gateway", Protocol: ModbusTCP, Endpoint: "tcp://localhost:502", RuntimeLocation: "local", Modbus: &ModbusConnection{UnitID: 1}}
	unitID, writable := 42, false
	doc := StreamDefinition{Model: Publish, NativeResourceRef: "modbus:press", Collection: &CollectionDefinition{Modbus: &ModbusCollection{
		PollIntervalMs: 1000, Ranges: []ModbusRange{{ID: "press", UnitID: &unitID, Writable: &writable, Table: "holding-register", Address: 0, Quantity: 1}},
	}}}
	effective, err := doc.EffectiveSource(source)
	require.NoError(t, err)
	require.Equal(t, 42, *effective.Modbus.Ranges[0].UnitID)
	require.False(t, *effective.Modbus.Ranges[0].Writable)
	doc.Model = Subscribe
	_, err = doc.EffectiveSource(source)
	require.Error(t, err, "read-only holding registers are not a write target")
	writable = true
	_, err = doc.EffectiveSource(source)
	require.NoError(t, err, "explicit writable station is permitted within the normal protocol limits")
}

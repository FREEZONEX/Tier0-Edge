package sourcestream

// 验证协议配置、映射基数和草稿/部署边界；不连接设备，不伪造运行状态。
import (
	"backend/internal/streamexec"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMultiInputSelectionAndOutputAllowlist(t *testing.T) {
	doc := StreamDefinition{Collection: &CollectionDefinition{MQTT: &MQTTCollection{QoS: 1}}, Name: "multi", SourceID: 1, Model: Publish, NativeResourceRefs: []string{"mqtt:temperature", "mqtt:pressure"}, TopicIDs: []int64{10, 20}, Script: ScriptDefinition{Code: "return {outputs:[],drop:false};", TimeoutMs: 1000}}
	assertFieldError(t, doc.ValidateDeployment(1, connectionSource(mqttSource())), "document")
	require.True(t, doc.MatchesResource("mqtt:temperature"))
	require.False(t, doc.MatchesResource("another-source"))
	targets, err := doc.OutputTargets(ScriptResult{Outputs: []TopicOutput{{TopicID: 20, Data: 42}}})
	require.NoError(t, err)
	require.Len(t, targets, 1)
	for _, outputs := range [][]TopicOutput{{{TopicID: 30}}, {{TopicID: 10}, {TopicID: 10}}} {
		_, err = doc.OutputTargets(ScriptResult{Outputs: outputs})
		require.Error(t, err)
	}
	doc.NativeResourceRef = "mqtt:temperature"
	require.Error(t, doc.ValidateDraft(), "do not merge singular and plural inputs implicitly")
	doc.NativeResourceRef = ""
	doc.NativeResourceRefs = []string{"mqtt:temperature", "mqtt:temperature"}
	require.Error(t, doc.ValidateDraft())
	doc.NativeResourceRefs = []string{"mqtt:temperature"}
	doc.Model = Subscribe
	require.Error(t, doc.ValidateDraft(), "native subscription operation is singular")
	_, err = doc.OutputTargets(ScriptResult{Outputs: []TopicOutput{}})
	require.Error(t, err, "directed Topic writes are publication-only")
}

func TestMQTTPublishFilterPreservesActualResource(t *testing.T) {
	for _, tc := range []struct {
		filter, topic string
		match         bool
	}{
		{"factory/+/telemetry", "factory/pump/telemetry", true},
		{"factory/+/telemetry", "factory//telemetry", true},
		{"factory/+/telemetry", "factory/pump/motor/telemetry", false},
		{"factory/#", "factory", true},
		{"factory/#", "factory/pump/telemetry", true},
		{"factory/#", "Factory/pump", false},
		{"+/telemetry", "$SYS/telemetry", false},
		{"#", "$SYS/broker/clients", false},
		{"$SYS/#", "$SYS/broker/clients", true},
		{"factory/#/invalid", "factory/pump/invalid", false},
		{"factory/p+mp", "factory/pump", false},
		{"factory/#", "factory/+", false},
	} {
		t.Run(tc.filter+"->"+tc.topic, func(t *testing.T) {
			doc := StreamDefinition{Model: Publish, NativeResourceRefs: []string{"mqtt:" + tc.filter}}
			ref := "mqtt:" + tc.topic
			require.Equal(t, tc.match, doc.MatchesResource(ref))
			require.Equal(t, "mqtt:"+tc.topic, ref, "do not replace actual Topic with the filter")
		})
	}
	for _, model := range []AccessModel{Subscribe, RequestReply} {
		doc := StreamDefinition{Model: model, NativeResourceRef: "mqtt:factory/#"}
		require.False(t, doc.MatchesResource("mqtt:factory/pump"))
	}
	require.False(t, (StreamDefinition{Model: Publish, NativeResourceRef: "opcua:plant/#"}).MatchesResource("opcua:plant/pump"))
}

func mqttSource() RuntimeSource {
	return RuntimeSource{Name: "车间 MQTT", Protocol: MQTT, Endpoint: "mqtt://broker:1883", RuntimeLocation: "local",
		MQTT: &MQTTOptions{ClientID: "source-test", QoS: 1, TopicFilters: []string{"factory/+/telemetry", "alarms/#"}, Subscriptions: map[string]byte{"factory/+/telemetry": 1, "alarms/#": 1}}}
}

func opcuaSource() RuntimeSource {
	return RuntimeSource{Name: "锅炉 OPC UA", Protocol: OPCUA, Endpoint: "opc.tcp://boiler:4840/ua", RuntimeLocation: "local",
		OPCUA: &OPCUAOptions{SecurityMode: "SignAndEncrypt", SecurityPolicy: "Basic256Sha256", SamplingIntervalMs: 1000}}
}

func modbusSource() RuntimeSource {
	return RuntimeSource{Name: "Modbus", Protocol: ModbusTCP, Endpoint: "tcp://plc:502", RuntimeLocation: "local",
		Modbus: &ModbusOptions{UnitID: 1, PollIntervalMs: 1000, Ranges: []ModbusRange{{ID: "mqtt:temperature", Table: "holding-register", Address: 0, Quantity: 2}}}}
}

// Connection-only documents deliberately exclude Stream-owned collection.
func connectionSource(runtime RuntimeSource) SourceDefinition {
	source := runtime.Connection()
	switch source.Protocol {
	case MQTT:
		source.MQTT = &MQTTConnection{ClientID: "tier0"}
	case OPCUA:
		source.OPCUA = &OPCUAConnection{SecurityMode: source.OPCUA.SecurityMode, SecurityPolicy: source.OPCUA.SecurityPolicy}
	case ModbusTCP:
		source.Modbus = &ModbusConnection{UnitID: source.Modbus.UnitID}
	}
	return source
}

func TestRuntimeConfigurationValidation(t *testing.T) {
	for _, source := range []RuntimeSource{mqttSource(), opcuaSource(), modbusSource()} {
		if err := source.validate(true); err != nil {
			t.Fatalf("%s: %v", source.Protocol, err)
		}
		if !source.Supports(Publish) || !source.Supports(Subscribe) || source.Supports("other") {
			t.Fatal("unexpected protocol eligibility")
		}
	}
	tests := []struct {
		name   string
		base   func() RuntimeSource
		change func(*RuntimeSource)
		field  string
	}{
		{"empty name", mqttSource, func(s *RuntimeSource) { s.Name = " " }, "name"},
		{"long unicode name", mqttSource, func(s *RuntimeSource) { s.Name = strings.Repeat("温", 64) }, "name"},
		{"long description", mqttSource, func(s *RuntimeSource) { s.Description = strings.Repeat("测", 513) }, "description"},
		{"remote runtime not implemented", mqttSource, func(s *RuntimeSource) { s.RuntimeLocation = "edge" }, "runtimeLocation"},
		{"invalid endpoint", mqttSource, func(s *RuntimeSource) { s.Endpoint = ":bad" }, "endpoint"},
		{"inline credentials", mqttSource, func(s *RuntimeSource) { s.Endpoint = "mqtt://user:secret@broker" }, "endpoint"},
		{"query credentials", mqttSource, func(s *RuntimeSource) { s.Endpoint = "mqtt://broker?token=secret" }, "endpoint"},
		{"missing host", mqttSource, func(s *RuntimeSource) { s.Endpoint = "mqtt:///" }, "endpoint"},
		{"wrong mqtt scheme", mqttSource, func(s *RuntimeSource) { s.Endpoint = "https://broker" }, "endpoint"},
		{"mixed configs", mqttSource, func(s *RuntimeSource) { s.OPCUA = opcuaSource().OPCUA }, "mqtt"},
		{"mqtt qos", mqttSource, func(s *RuntimeSource) { s.MQTT.QoS = 3 }, "mqtt"},
		{"client identity", mqttSource, func(s *RuntimeSource) { s.MQTT.ClientID = "" }, "mqtt"},
		{"bad topic wildcard", mqttSource, func(s *RuntimeSource) { s.MQTT.Subscriptions = map[string]byte{"a/#/b": 1} }, "mqtt.subscriptions"},
		{"partial request reply", mqttSource, func(s *RuntimeSource) { s.MQTT.RequestTopic = "command" }, "mqtt"},
		{"opcua config", opcuaSource, func(s *RuntimeSource) { s.OPCUA = nil }, "opcua"},
		{"opcua scheme", opcuaSource, func(s *RuntimeSource) { s.Endpoint = "https://boiler" }, "endpoint"},
		{"opcua interval", opcuaSource, func(s *RuntimeSource) { s.OPCUA.SamplingIntervalMs = 0 }, "opcua.samplingIntervalMs"},
		{"opcua policy downgrade", opcuaSource, func(s *RuntimeSource) { s.OPCUA.SecurityPolicy = "None" }, "opcua.securityPolicy"},
		{"opcua empty node", opcuaSource, func(s *RuntimeSource) { s.OPCUA.NodeIDs = []string{" "} }, "opcua.nodeIds"},
		{"modbus config", modbusSource, func(s *RuntimeSource) { s.Modbus = nil }, "modbus"},
		{"modbus scheme", modbusSource, func(s *RuntimeSource) { s.Endpoint = "http://plc" }, "endpoint"},
		{"modbus broadcast", modbusSource, func(s *RuntimeSource) { s.Modbus.UnitID = 0 }, "modbus.unitId"},
		{"modbus interval", modbusSource, func(s *RuntimeSource) { s.Modbus.PollIntervalMs = 0 }, "modbus.pollIntervalMs"},
		{"modbus duplicate range", modbusSource, func(s *RuntimeSource) { s.Modbus.Ranges = append(s.Modbus.Ranges, s.Modbus.Ranges[0]) }, "modbus.ranges[1].id"},
		{"modbus range overflow", modbusSource, func(s *RuntimeSource) { s.Modbus.Ranges[0].Address = 65535 }, "modbus.ranges[0]"},
		{"modbus unknown table", modbusSource, func(s *RuntimeSource) { s.Modbus.Ranges[0].Table = "bad" }, "modbus.ranges[0].table"},
		{"unknown protocol", mqttSource, func(s *RuntimeSource) { s.Protocol = "s7" }, "protocol"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.base()
			tt.change(&s)
			assertFieldError(t, s.validate(true), tt.field)
		})
	}
	s := mqttSource()
	s.Name = strings.Repeat("温", 63)
	if err := s.validate(true); err != nil {
		t.Fatal("name must count Unicode characters, not bytes", err)
	}
	for _, table := range []string{"coil", "discrete-input"} {
		s = modbusSource()
		s.Modbus.Ranges[0].Table = table
		s.Modbus.Ranges[0].Quantity = 2000
		if err := s.validate(true); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMQTTRequestReplyRequiresExplicitDeviceContract(t *testing.T) {
	s := mqttSource()
	if s.Supports(RequestReply) {
		t.Fatal("ordinary MQTT must not advertise request/reply")
	}
	s.MQTT.RequestTopic = "machine/command"
	s.MQTT.ReplyTopic = "machine/reply"
	s.MQTT.CorrelationField = "metadata.requestId"
	if !s.Supports(RequestReply) {
		t.Fatal("explicit reply contract should be eligible")
	}
	for _, field := range []string{"", "__proto__.id", "a.constructor", "prototype.id", "a..b", "a[0]"} {
		s.MQTT.CorrelationField = field
		assertFieldError(t, s.Validate(), "mqtt")
	}
	s.MQTT.CorrelationField = "requestId"
	s.MQTT.ReplyTopic = s.MQTT.RequestTopic
	assertFieldError(t, s.Validate(), "mqtt.replyTopic")

	s.MQTT.ReplyTopic = "tier0/reply/{correlationId}"
	assertFieldError(t, s.Validate(), "mqtt.replyTopicField")
	s.MQTT.ReplyTopicField = "metadata.replyTopic"
	if err := s.Validate(); err != nil {
		t.Fatalf("unique reply topic contract should be valid: %v", err)
	}
	s.MQTT.ReplyTopic = "tier0/reply/{unknown}"
	assertFieldError(t, s.Validate(), "mqtt.replyTopic")
	s.MQTT.ReplyTopic = "tier0/reply/{correlationId}"
	s.MQTT.ReplyTopicField = "__proto__.replyTopic"
	assertFieldError(t, s.Validate(), "mqtt.replyTopicField")
}

func TestMQTTTopicSyntax(t *testing.T) {
	for _, topic := range []string{"#", "+", "a/+", "a/#", "/"} {
		if !validMQTTTopic(topic, true) {
			t.Fatalf("valid filter rejected: %q", topic)
		}
	}
	for _, topic := range []string{"", "a\x00b", "a/#/b", "a/b+", "a/##"} {
		if validMQTTTopic(topic, true) {
			t.Fatalf("invalid filter accepted: %q", topic)
		}
	}
	if validMQTTTopic("a/+", false) {
		t.Fatal("cannot publish to wildcard")
	}
}

func streamDefinition() StreamDefinition {
	return StreamDefinition{Name: "完整程序", Model: ProgramAccess,
		Script: ScriptDefinition{Code: "module.exports=api=>{api.stream.onRequest({id:'read'},ctx=>ctx.input);};", TimeoutMs: 1000}}
}

func TestStreamDraftAndDeployment(t *testing.T) {
	current := streamDefinition()
	require.NoError(t, current.ValidateDraft())
	require.NoError(t, current.ValidateDeployment(0, SourceDefinition{}))
	require.NoError(t, current.ValidateDeployment(999, SourceDefinition{}), "Source bindings are compiled from the current program")
	current.Script.Code = ""
	require.NoError(t, current.ValidateDraft(), "an unfinished current draft may omit code")
	assertFieldError(t, current.ValidateDeployment(0, SourceDefinition{}), "script.code")
	for _, model := range []AccessModel{Publish, Subscribe, RequestReply, ScriptMode} {
		legacy := StreamDefinition{Name: "历史模型", Model: model, SourceID: 10,
			Script: ScriptDefinition{Code: "return {data:input};", TimeoutMs: 1000}}
		assertFieldError(t, legacy.ValidateDraft(), "document")
		assertFieldError(t, legacy.ValidateDeployment(10, connectionSource(mqttSource())), "document")
	}
}

func TestStreamInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name   string
		change func(*StreamDefinition)
		field  string
	}{
		{"name", func(s *StreamDefinition) { s.Name = "" }, "name"},
		{"model", func(s *StreamDefinition) { s.Model = "orchestration" }, "document"},
		{"owning Source", func(s *StreamDefinition) { s.SourceID = 10 }, "document"},
		{"legacy bindings", func(s *StreamDefinition) { s.Sources = []SourceBinding{} }, "document"},
		{"legacy collection", func(s *StreamDefinition) { s.Collection = &CollectionDefinition{} }, "document"},
		{"legacy program", func(s *StreamDefinition) { s.Program = &ProgramDefinition{} }, "document"},
		{"legacy target", func(s *StreamDefinition) { s.TopicIDs = []int64{20} }, "document"},
		{"legacy input", func(s *StreamDefinition) { s.NativeResourceRef = "mqtt:temperature" }, "document"},
		{"legacy inputs", func(s *StreamDefinition) { s.NativeResourceRefs = []string{} }, "document"},
		{"legacy response", func(s *StreamDefinition) { s.ResponseScript = &s.Script }, "document"},
		{"script too large", func(s *StreamDefinition) { s.Script.Code = strings.Repeat("x", MaxScriptBytes+1) }, "script.code"},
		{"zero timeout", func(s *StreamDefinition) { s.Script.TimeoutMs = 0 }, "script.timeoutMs"},
		{"negative timeout", func(s *StreamDefinition) { s.Script.TimeoutMs = -1 }, "script.timeoutMs"},
		{"excessive timeout", func(s *StreamDefinition) { s.Script.TimeoutMs = streamexec.MaxProgramTimeoutMs + 1 }, "timeoutMs"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := streamDefinition()
			test.change(&definition)
			assertFieldError(t, definition.ValidateDraft(), test.field)
			assertFieldError(t, definition.ValidateDeployment(0, SourceDefinition{}), test.field)
		})
	}
	current := streamDefinition()
	current.Script.TimeoutMs = streamexec.MaxProgramTimeoutMs
	require.NoError(t, current.ValidateDeployment(0, SourceDefinition{}))
}

func assertFieldError(t *testing.T, err error, field string) {
	t.Helper()
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Field != field || validation.Error() == "" {
		t.Fatalf("expected field %q, got %v", field, err)
	}
}

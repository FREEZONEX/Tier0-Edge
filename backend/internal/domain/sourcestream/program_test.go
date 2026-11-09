package sourcestream

// 验证代码型 Stream 的显式依赖、四类触发与 MQTT 实际 Topic 匹配。
// 草稿可不完整，部署须完整；禁止把触发器字段混用或扩大 SDK 授权。
import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProgram_TriggersAndDependencies(t *testing.T) {
	for _, test := range []struct {
		name    string
		program ProgramDefinition
		model   AccessModel
		deploy  bool
		valid   bool
	}{
		{"zero-source-sync", ProgramDefinition{Trigger: TriggerDefinition{Kind: ManualTrigger}}, RequestReply, true, true},
		{"zero-source-interval", ProgramDefinition{Trigger: TriggerDefinition{Kind: IntervalTrigger, IntervalMs: 5000}}, Publish, true, true},
		{"zero-source-uns", ProgramDefinition{Trigger: TriggerDefinition{Kind: UNSTrigger, TopicIDs: []int64{123}}}, Publish, true, true},
		{"uns-draft", ProgramDefinition{Trigger: TriggerDefinition{Kind: UNSTrigger}}, Publish, false, true},
		{"uns-missing-input", ProgramDefinition{Trigger: TriggerDefinition{Kind: UNSTrigger}}, Publish, true, false},
		{"interval-too-fast", ProgramDefinition{Trigger: TriggerDefinition{Kind: IntervalTrigger, IntervalMs: 1}}, Publish, true, false},
		{"unknown-trigger", ProgramDefinition{Trigger: TriggerDefinition{Kind: "eval"}}, Publish, false, false},
		{"manual-publish", ProgramDefinition{Trigger: TriggerDefinition{Kind: ManualTrigger}}, Publish, true, false},
		{"mixed-trigger", ProgramDefinition{Trigger: TriggerDefinition{Kind: IntervalTrigger, IntervalMs: 1000, TopicIDs: []int64{123}}}, Publish, true, false},
		{"duplicate-uns", ProgramDefinition{Trigger: TriggerDefinition{Kind: UNSTrigger, TopicIDs: []int64{123, 123}}}, Publish, true, false},
		{"empty-source-draft", ProgramDefinition{Trigger: TriggerDefinition{Kind: SourceTrigger}}, Publish, false, true},
		{"empty-source-deploy", ProgramDefinition{Trigger: TriggerDefinition{Kind: SourceTrigger}}, Publish, true, false},
		{"undeclared-trigger-alias", ProgramDefinition{Trigger: TriggerDefinition{Kind: SourceTrigger, SourceAlias: "device"}}, Publish, false, false},
		{"mqtt-trigger-only", ProgramDefinition{Sources: []SourceDependency{{Alias: "mqtt", SourceID: 123}}, Trigger: TriggerDefinition{Kind: SourceTrigger, SourceAlias: "mqtt", ResourceRefs: []string{"mqtt:factory/+/telemetry"}, QoS: 1}}, Publish, true, true},
		{"two-read-sources", ProgramDefinition{Sources: []SourceDependency{{Alias: "opc", SourceID: 123, ReadResources: []string{"opcua:temp"}}, {Alias: "modbus", SourceID: 456, ReadResources: []string{"modbus:hr:1"}}}, Trigger: TriggerDefinition{Kind: IntervalTrigger, IntervalMs: 5000}}, Publish, true, true},
		{"duplicate-alias", ProgramDefinition{Sources: []SourceDependency{{Alias: "opc", SourceID: 123}, {Alias: "opc", SourceID: 456}}, Trigger: TriggerDefinition{Kind: ManualTrigger}}, RequestReply, true, false},
		{"duplicate-source", ProgramDefinition{Sources: []SourceDependency{{Alias: "opc", SourceID: 123}, {Alias: "alias", SourceID: 123}}, Trigger: TriggerDefinition{Kind: ManualTrigger}}, RequestReply, true, false},
		{"unsafe-alias", ProgramDefinition{Sources: []SourceDependency{{Alias: "__proto__", SourceID: 123}}, Trigger: TriggerDefinition{Kind: ManualTrigger}}, RequestReply, true, false},
		{"duplicate-read", ProgramDefinition{Sources: []SourceDependency{{Alias: "opc", SourceID: 123, ReadResources: []string{"temp", "temp"}}}, Trigger: TriggerDefinition{Kind: ManualTrigger}}, RequestReply, true, false},
		{"blank-read", ProgramDefinition{Sources: []SourceDependency{{Alias: "opc", SourceID: 123, ReadResources: []string{" "}}}, Trigger: TriggerDefinition{Kind: ManualTrigger}}, RequestReply, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.program.Validate(test.model, test.deploy)
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestProgram_MatchesInput(t *testing.T) {
	p := ProgramDefinition{Sources: []SourceDependency{{Alias: "mqtt", SourceID: 123}}, Trigger: TriggerDefinition{Kind: SourceTrigger, SourceAlias: "mqtt", ResourceRefs: []string{"mqtt:factory/+/telemetry"}}}
	require.True(t, p.MatchesInput(123, "mqtt:factory/line1/telemetry"))
	require.False(t, p.MatchesInput(456, "mqtt:factory/line1/telemetry"))
	require.False(t, p.MatchesInput(123, "mqtt:factory/line1/control"))
	p.Trigger.ResourceRefs = []string{"mqtt:#"}
	require.False(t, p.MatchesInput(123, "mqtt:$SYS/status"))
	p.Trigger.ResourceRefs = []string{"opcua:temp"}
	require.True(t, p.MatchesInput(123, "opcua:temp"))
	require.False(t, p.MatchesInput(123, "opcua:pressure"))
	p.Trigger.Kind = IntervalTrigger
	require.False(t, p.MatchesInput(123, "opcua:temp"))
}

package sourcestream

import (
	"context"
	"encoding/json"
	"regexp"

	"backend/internal/contextx"
	"backend/internal/domain/common"
	"backend/internal/repo"
)

type SourceReader func(context.Context, string, json.RawMessage) (json.RawMessage, error)
type SourceScriptExecutor interface {
	TransformWithLegacySources(context.Context, ScriptDefinition, any, map[string]any, []string, SourceReader) (ScriptResult, error)
}

var bindingAlias = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (d StreamDefinition) validateScriptDraft() error {
	if err := validateName(d.Name, d.Description); err != nil {
		return err
	}
	if err := validateScript(d.Script, "script", false); err != nil {
		return err
	}
	if d.SourceID != 0 || d.Collection != nil || len(d.ResourceRefs()) > 0 || d.ResponseScript != nil {
		return invalid("script", "unexpectedAcquisitionConfiguration")
	}
	// 首版手动脚本返回运行结果，禁止误把未实现的输出配置保存成功。
	if len(d.TopicIDs) > 0 || d.MetadataTarget != nil {
		return invalid("script", "resultOnly")
	}
	if len(d.Sources) > MaxScriptSources {
		return invalid("sources", "tooMany")
	}
	aliases := map[string]bool{}
	for _, binding := range d.Sources {
		if common.ExceedMaxLength(binding.Alias, common.AliasMaxLen) || !bindingAlias.MatchString(binding.Alias) || aliases[binding.Alias] || binding.SourceID <= 0 || binding.ConfigRevision <= 0 {
			return invalid("sources", "invalidBinding")
		}
		aliases[binding.Alias] = true
	}
	return nil
}

func validateScriptSources(ctx context.Context, def StreamDefinition, enabled bool) error {
	if len(def.Sources) == 0 {
		return nil
	}
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || !contextx.HasResource(subject, "uns.source") {
		return ErrForbidden
	}
	r := repo.NewSourceStreamRepo(ctx)
	for _, binding := range def.Sources {
		source, err := r.Source(ctx, binding.SourceID)
		if err != nil {
			return err
		}
		if source.ConfigRevision != binding.ConfigRevision {
			return repo.ErrConflict
		}
	}
	return nil
}

func scriptExecutionView(ctx context.Context, item repo.Stream) (map[string]any, error) {
	view := streamView(item)
	view["executionMode"] = subscriptionMode(item.Document)
	state := item.Status
	if state == "enabled" {
		r := repo.NewSourceStreamRepo(ctx)
		version, err := r.Version(ctx, item.ID, item.ActiveVersion)
		if err != nil {
			return nil, err
		}
		var def StreamDefinition
		if json.Unmarshal([]byte(version.Document), &def) != nil {
			return nil, repo.ErrInvalidArgument
		}
		view["activeDocument"] = def
		state = "ready"
		for _, binding := range def.Sources {
			source, err := r.Source(ctx, binding.SourceID)
			if err != nil || source.ConfigRevision != binding.ConfigRevision {
				state = "needs-redeploy"
				break
			}
			if source.DesiredState == "disabled" {
				state = "source-disabled"
				break
			}
		}
	}
	view["executionState"] = state
	return view, nil
}

func (s *Service) transformScript(ctx context.Context, def StreamDefinition, input any, runContext map[string]any) (ScriptResult, error) {
	executor, ok := s.executor.(SourceScriptExecutor)
	if !ok {
		return ScriptResult{}, ErrUnavailable
	}
	if err := validateScriptSources(ctx, def, true); err != nil {
		return ScriptResult{}, err
	}
	aliases := make([]string, 0, len(def.Sources))
	bindings := map[string]SourceBinding{}
	for _, binding := range def.Sources {
		aliases = append(aliases, binding.Alias)
		bindings[binding.Alias] = binding
	}
	script := def.Script
	sub, err := parseSubscription(script.Code)
	if err != nil {
		return ScriptResult{}, err
	}
	if sub != nil {
		script.Code = "return (" + sub.Handler + ")(input, context);"
		if sub.Variable != "" {
			alias, _ := json.Marshal(sub.Alias)
			script.Code = "const " + sub.Variable + " = sources.get(" + string(alias) + ");\n" + script.Code
		}
	}
	result, err := executor.TransformWithLegacySources(ctx, script, input, runContext, aliases, func(callCtx context.Context, alias string, options json.RawMessage) (json.RawMessage, error) {
		binding, ok := bindings[alias]
		if !ok {
			return nil, ErrForbidden
		}
		return s.readScriptSource(callCtx, binding, options)
	})
	if err == nil && result.Outputs != nil {
		return result, invalid("outputs", "scriptResultOnly")
	}
	return result, err
}

// 协议请求只能使用保存的连接与当前声明的 Source；调用参数不能覆盖地址和凭据。
func (s *Service) readScriptSource(ctx context.Context, binding SourceBinding, raw json.RawMessage) (json.RawMessage, error) {
	row, err := repo.NewSourceStreamRepo(ctx).Source(ctx, binding.SourceID)
	if err != nil {
		return nil, err
	}
	if row.ConfigRevision != binding.ConfigRevision {
		return nil, repo.ErrConflict
	}
	source, credential, revision, err := s.connection(ctx, binding.SourceID, nil)
	if err != nil {
		return nil, err
	}
	if revision != row.Revision {
		return nil, repo.ErrConflict
	}
	def := source.Connection()
	var value any
	switch source.Protocol {
	case HTTP, OAuth:
		value, err = s.readHTTPSource(ctx, binding.SourceID, source, raw)
		if err != nil {
			return nil, err
		}
	case MQTT:
		options, _, err := parseDocument[struct {
			Topic string `json:"topic"`
			QoS   int    `json:"qos"`
		}](json.RawMessage(raw))
		if err != nil || !validMQTTTopic(options.Topic, true) || options.QoS < 0 || options.QoS > 2 {
			return nil, invalid("read", "invalidMQTTOptions")
		}
		doc := StreamDefinition{SourceID: binding.SourceID, Model: Publish, NativeResourceRef: "mqtt:" + options.Topic, Collection: &CollectionDefinition{MQTT: &MQTTCollection{QoS: options.QoS}}}
		runtime, ok := s.runtime.(interface {
			Watch(context.Context, int64, RuntimeSource, Credential, func(SourceInput) error) error
		})
		if !ok {
			return nil, ErrUnavailable
		}
		def, err = doc.EffectiveSource(source)
		if err != nil {
			return nil, err
		}
		def.MQTT.Ephemeral = true
		samples := make(chan SourceInput, 1)
		err = runtime.Watch(ctx, binding.SourceID, def, credential, func(input SourceInput) error {
			select {
			case samples <- input:
			default:
			}
			return sampleComplete
		})
		select {
		case input := <-samples:
			value = input.Data
		default:
			if err == nil {
				err = ErrUnavailable
			}
			return nil, err
		}
	case OPCUA:
		options, _, err := parseDocument[struct {
			NodeID string `json:"nodeId"`
		}](json.RawMessage(raw))
		if err != nil || options.NodeID == "" {
			return nil, invalid("read", "invalidOPCUAOptions")
		}
		def.OPCUA.NodeIDs = []string{options.NodeID}
		def.OPCUA.SamplingIntervalMs = 1000
		if err = def.Validate(); err != nil {
			return nil, err
		}
		value, err = s.runtime.Invoke(ctx, binding.SourceID, def, credential, "opcua:"+options.NodeID, map[string]any{"operation": "read"})
		if err != nil {
			return nil, err
		}
	case ModbusTCP:
		options, _, err := parseDocument[struct {
			Table    string `json:"table"`
			Address  int    `json:"address"`
			Quantity int    `json:"quantity"`
		}](json.RawMessage(raw))
		if err != nil {
			return nil, err
		}
		def.Modbus.PollIntervalMs = 1000
		def.Modbus.Ranges = []ModbusRange{{ID: "read", Table: options.Table, Address: options.Address, Quantity: options.Quantity}}
		if err = def.Validate(); err != nil {
			return nil, err
		}
		value, err = s.runtime.Invoke(ctx, binding.SourceID, def, credential, "modbus:read", map[string]any{"operation": "read"})
		if err != nil {
			return nil, err
		}
	default:
		return nil, ErrUnavailable
	}
	return json.Marshal(value)
}

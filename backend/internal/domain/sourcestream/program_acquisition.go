package sourcestream

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// 仅为当前 Stream 构造采集配置，原 Source 的连接、凭据和写权限保持不变。
func programAcquiredSource(source SourceDefinition, resources []string, acquisition map[string]json.RawMessage) (SourceDefinition, error) {
	result := source.Connection()
	allowed := map[string]bool{"resourceRefs": true}
	switch source.Protocol {
	case ModbusTCP:
		allowed["ranges"], allowed["pollIntervalMs"] = true, true
	case MQTT:
		allowed["topicFilters"], allowed["qos"] = true, true
	case OPCUA:
		allowed["nodeIds"], allowed["samplingIntervalMs"] = true, true
	}
	for field, raw := range acquisition {
		path := "acquisition." + field
		if !allowed[field] || string(raw) == "null" {
			return result, invalid(path, "unsupportedField")
		}
		switch field {
		case "pollIntervalMs", "samplingIntervalMs", "qos":
			var value int
			if json.Unmarshal(raw, &value) != nil {
				return result, invalid(path, "invalidInteger")
			}
			switch field {
			case "pollIntervalMs":
				if result.Modbus == nil || value < 100 || value > 3600000 {
					return result, invalid(path, "outOfRange")
				}
				result.Modbus.PollIntervalMs = value
			case "samplingIntervalMs":
				if result.OPCUA == nil || value < 100 || value > 60000 {
					return result, invalid(path, "outOfRange")
				}
				result.OPCUA.SamplingIntervalMs = value
			case "qos":
				if result.MQTT == nil || value < 0 || value > 2 {
					return result, invalid(path, "outOfRange")
				}
				result.MQTT.QoS = value
			}
		case "resourceRefs", "topicFilters", "nodeIds":
			var selected []string
			if json.Unmarshal(raw, &selected) != nil || len(selected) == 0 || !uniqueResourceRefs(selected) {
				return result, invalid(path, "invalidOrDuplicate")
			}
			prefix := ""
			if field == "topicFilters" {
				prefix = "mqtt:"
			}
			if field == "nodeIds" {
				prefix = "opcua:"
			}
			for i := range selected {
				selected[i] = prefix + selected[i]
			}
			if len(selected) != len(resources) {
				return result, invalid(path, "resourceMismatch")
			}
			for _, ref := range resources {
				if !slices.Contains(selected, ref) {
					return result, invalid(path, "resourceMismatch")
				}
			}
		case "ranges":
			var selected []struct {
				ID       string `json:"id"`
				Table    string `json:"table"`
				Address  *int   `json:"address"`
				Quantity int    `json:"quantity"`
			}
			decoder := json.NewDecoder(strings.NewReader(string(raw)))
			decoder.DisallowUnknownFields()
			if result.Modbus == nil || decoder.Decode(&selected) != nil || len(selected) == 0 || len(selected) > 1000 {
				return result, invalid(path, "invalidRanges")
			}
			seen := map[string]bool{}
			for _, item := range selected {
				if seen[item.ID] || item.Address == nil {
					return result, invalid(path, "invalidRanges")
				}
				seen[item.ID] = true
				found := false
				for _, native := range result.Modbus.Ranges {
					if native.ID == item.ID && native.Table == item.Table && native.Address == *item.Address && native.Quantity == item.Quantity {
						found = true
						break
					}
				}
				if !found {
					return result, invalid(path, "undeclaredRange")
				}
			}
			for _, ref := range resources {
				if !strings.HasPrefix(ref, "modbus:") || !seen[strings.TrimPrefix(ref, "modbus:")] {
					return result, invalid(path, "resourceMismatch")
				}
			}
		}
	}
	return result, nil
}

func (d StreamDefinition) programSourceBinding(id string) ProgramSourceBinding {
	for _, binding := range d.programBindings() {
		if binding.SourceID == id {
			return binding
		}
	}
	return ProgramSourceBinding{}
}

// 就绪和订阅复用均包含采集参数，防止另一条 Stream 的周期被当成当前配置。
func (p ExecutionPlan) SourceAcquisitionKey() string {
	if p.Entry == nil || p.Entry.Kind != "source-event" {
		return ""
	}
	binding := p.Definition.programSourceBinding(strconv.FormatInt(p.SourceID, 10))
	parameters := map[string]json.RawMessage{}
	for _, field := range []string{"qos", "pollIntervalMs", "samplingIntervalMs"} {
		if value, ok := binding.Acquisition[field]; ok {
			parameters[field] = value
		}
	}
	if len(parameters) == 0 {
		return ""
	}
	raw, _ := json.Marshal(parameters)
	return string(raw)
}

func (p ExecutionPlan) ProgramWatchQuery() WatchQuery {
	refs, qos, _ := p.SourceEventScope()
	binding := p.Definition.programSourceBinding(strconv.FormatInt(p.SourceID, 10))
	if raw := binding.Acquisition["qos"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &qos)
	}
	return WatchQuery{SourceID: p.SourceID, ConfigRevision: p.SourceRevision, ResourceRefs: refs, QoS: qos, AcquisitionKey: p.SourceAcquisitionKey()}
}

func (p ExecutionPlan) programWatchSource(source SourceDefinition) (RuntimeSource, error) {
	binding := p.Definition.programSourceBinding(strconv.FormatInt(p.SourceID, 10))
	resources := binding.Resources
	if resources == nil && p.Manifest != nil {
		for _, dependency := range p.Manifest.Sources {
			if dependency.ID == binding.SourceID {
				resources = dependency.Resources
			}
		}
	}
	effective, err := programAcquiredSource(source, resources, binding.Acquisition)
	if err != nil {
		return effective, err
	}
	query := p.ProgramWatchQuery()
	if effective.MQTT != nil {
		effective.MQTT.QoS = query.QoS
		effective.MQTT.TopicFilters = []string{}
		for _, ref := range query.ResourceRefs {
			effective.MQTT.TopicFilters = append(effective.MQTT.TopicFilters, strings.TrimPrefix(ref, "mqtt:"))
		}
	}
	runtime, err := (StreamDefinition{Model: Publish, NativeResourceRefs: query.ResourceRefs}).EffectiveSource(effective)
	if err != nil {
		return runtime, fmt.Errorf("program acquisition: %w", err)
	}
	return runtime, nil
}

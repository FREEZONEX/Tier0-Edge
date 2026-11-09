package sourcestream

import (
	"sort"
	"strings"
)

// CollectionDefinition is required and owned exclusively by Stream.
type CollectionDefinition struct {
	MQTT   *MQTTCollection   `json:"mqtt,omitempty"`
	OPCUA  *OPCUACollection  `json:"opcua,omitempty"`
	Modbus *ModbusCollection `json:"modbus,omitempty"`
}
type MQTTCollection struct {
	QoS              int    `json:"qos"`
	RequestTopic     string `json:"requestTopic,omitempty"`
	ReplyTopic       string `json:"replyTopic,omitempty"`
	ReplyTopicField  string `json:"replyTopicField,omitempty"`
	CorrelationField string `json:"correlationField,omitempty"`
}
type OPCUACollection struct {
	SamplingIntervalMs int `json:"samplingIntervalMs"`
}
type ModbusCollection struct {
	PollIntervalMs int           `json:"pollIntervalMs"`
	Ranges         []ModbusRange `json:"ranges"`
}

// 原生资源选择只有一份，MQTT topic/OPC UA node 来自 nativeResourceRefs。
func (d StreamDefinition) EffectiveSource(source SourceDefinition) (RuntimeSource, error) {
	if d.Collection == nil {
		// Legacy Source-backed documents carry acquisition options in Source.
		// Resolve once into runtime-only collection; new documents keep it in Stream.
		d.Collection = &CollectionDefinition{}
		switch source.Protocol {
		case USBHID:
			// Text input has no tunable acquisition parameters.
		case MQTT:
			if source.MQTT == nil || source.MQTT.TopicFilters == nil {
				return RuntimeSource{}, invalid("collection", "required")
			}
			d.Collection.MQTT = &MQTTCollection{QoS: source.MQTT.QoS, RequestTopic: source.MQTT.RequestTopic, ReplyTopic: source.MQTT.ReplyTopic, ReplyTopicField: source.MQTT.ReplyTopicField, CorrelationField: source.MQTT.CorrelationField}
		case OPCUA:
			if source.OPCUA == nil || source.OPCUA.SamplingIntervalMs == 0 {
				return RuntimeSource{}, invalid("collection", "required")
			}
			d.Collection.OPCUA = &OPCUACollection{SamplingIntervalMs: source.OPCUA.SamplingIntervalMs}
		case ModbusTCP:
			if source.Modbus == nil || source.Modbus.PollIntervalMs == 0 {
				return RuntimeSource{}, invalid("collection", "required")
			}
			d.Collection.Modbus = &ModbusCollection{PollIntervalMs: source.Modbus.PollIntervalMs, Ranges: source.Modbus.Ranges}
		default:
			return RuntimeSource{}, invalid("collection", "required")
		}
	}
	result := source.Connection()
	c := d.Collection
	switch source.Protocol {
	case USBHID:
		refs := d.ResourceRefs()
		if d.Model != Publish || len(refs) != 1 || refs[0] != HIDTextResource || *c != (CollectionDefinition{}) {
			return result, invalid("nativeResourceRef", "missingOrUnsupported")
		}
	case MQTT:
		if c.MQTT == nil || c.OPCUA != nil || c.Modbus != nil || result.MQTT == nil {
			return result, invalid("collection", "protocolConfigMismatch")
		}
		result.MQTT.QoS = c.MQTT.QoS
		result.MQTT.Subscriptions = make(map[string]byte)
		result.MQTT.RequestTopic, result.MQTT.ReplyTopic, result.MQTT.ReplyTopicField, result.MQTT.CorrelationField = c.MQTT.RequestTopic, c.MQTT.ReplyTopic, c.MQTT.ReplyTopicField, c.MQTT.CorrelationField
		for _, ref := range d.ResourceRefs() {
			if d.Model == RequestReply {
				if ref != "mqtt-request-reply" {
					return result, invalid("nativeResourceRef", "missingOrUnsupported")
				}
				continue
			}
			if !strings.HasPrefix(ref, "mqtt:") || !validMQTTTopic(strings.TrimPrefix(ref, "mqtt:"), d.Model == Publish) {
				return result, invalid("nativeResourceRef", "missingOrUnsupported")
			}
			topic := strings.TrimPrefix(ref, "mqtt:")
			result.MQTT.Subscriptions[topic] = byte(c.MQTT.QoS)
		}
		result.MQTT.TopicFilters = nil
		for filter := range result.MQTT.Subscriptions {
			result.MQTT.TopicFilters = append(result.MQTT.TopicFilters, filter)
		}
		sort.Strings(result.MQTT.TopicFilters)
	case OPCUA:
		if c.OPCUA == nil || c.MQTT != nil || c.Modbus != nil || result.OPCUA == nil {
			return result, invalid("collection", "protocolConfigMismatch")
		}
		result.OPCUA.SamplingIntervalMs = c.OPCUA.SamplingIntervalMs
		result.OPCUA.NodeIDs = nil
		for _, ref := range d.ResourceRefs() {
			if !strings.HasPrefix(ref, "opcua:") || strings.TrimPrefix(ref, "opcua:") == "" {
				return result, invalid("nativeResourceRef", "missingOrUnsupported")
			}
			result.OPCUA.NodeIDs = append(result.OPCUA.NodeIDs, strings.TrimPrefix(ref, "opcua:"))
		}
	case ModbusTCP:
		if c.Modbus == nil || c.MQTT != nil || c.OPCUA != nil || result.Modbus == nil {
			return result, invalid("collection", "protocolConfigMismatch")
		}
		result.Modbus.PollIntervalMs, result.Modbus.Ranges = c.Modbus.PollIntervalMs, c.Modbus.Ranges
		// Validate the entire definition before limiting execution to selected inputs.
		if err := result.validate(true); err != nil {
			return result, err
		}
		result.Modbus.Ranges = nil
		for _, ref := range d.ResourceRefs() {
			found := false
			for _, r := range c.Modbus.Ranges {
				if ref == "modbus:"+r.ID {
					found = true
					result.Modbus.Ranges = append(result.Modbus.Ranges, r)
					if d.Model == Subscribe && ((r.Writable != nil && !*r.Writable) || !((r.Table == "holding-register" && r.Quantity <= 123) || (r.Table == "coil" && r.Quantity <= 1968))) {
						return result, invalid("nativeResourceRef", "missingOrUnsupported")
					}
				}
			}
			if !found {
				return result, invalid("nativeResourceRef", "missingOrUnsupported")
			}
		}
	default:
		return result, invalid("collection", "protocolConfigMismatch")
	}
	return result, result.validate(true)
}

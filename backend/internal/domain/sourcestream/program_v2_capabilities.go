package sourcestream

import (
	"encoding/json"
	"strings"

	"backend/internal/streamexec"
)

// Resolve the same discovered resource for both publication and dispatch.
// Connection defaults must not replace a resource's station or write boundary.
func programV2ResourceDefinition(def SourceDefinition, resource *NativeResource) (SourceDefinition, error) {
	if resource == nil {
		return def, streamexec.ErrSourceDenied
	}
	if def.Protocol != ModbusTCP {
		return def, nil
	}
	if def.Modbus == nil || !strings.HasPrefix(resource.ID, "modbus:") {
		return def, streamexec.ErrSourceDenied
	}
	raw, err := json.Marshal(resource.Metadata)
	var native ModbusRange
	if err != nil || json.Unmarshal(raw, &native) != nil {
		return def, streamexec.ErrSourceDenied
	}
	native.ID = strings.TrimPrefix(resource.ID, "modbus:")
	options := *def.Modbus
	options.Ranges = []ModbusRange{native}
	def.Modbus = &options
	if def.Validate() != nil {
		return def, streamexec.ErrSourceDenied
	}
	return def, nil
}

// These are native host capabilities, independent of a particular payload.
// Publication uses the same rules as the host, which still validates arguments.
func programV2NativeActions(def SourceDefinition, resource *NativeResource) []string {
	if resource == nil {
		return nil
	}
	hasModel := func(model AccessModel) bool {
		for _, supported := range resource.Models {
			if supported == model {
				return true
			}
		}
		return false
	}
	actions := []string{}
	switch def.Protocol {
	case MQTT:
		if def.MQTT == nil {
			return nil
		}
		if hasModel(Subscribe) && strings.HasPrefix(resource.ID, "mqtt:") && validMQTTTopic(strings.TrimPrefix(resource.ID, "mqtt:"), false) {
			actions = append(actions, "source.publish")
		}
		if hasModel(RequestReply) && resource.ID == "mqtt-request-reply" && def.MQTT.RequestTopic != "" && def.MQTT.ReplyTopic != "" && def.MQTT.CorrelationField != "" {
			actions = append(actions, "source.invoke")
		}
	case OPCUA:
		class, _ := json.Marshal(resource.Metadata["nodeClass"])
		if string(class) == "2" {
			if hasModel(Publish) {
				actions = append(actions, "source.read")
			}
			if hasModel(Subscribe) {
				actions = append(actions, "source.write")
			}
		}
		if string(class) == "4" && hasModel(RequestReply) && def.ConnectionRole() == ClientRole {
			actions = append(actions, "source.invoke")
		}
	case ModbusTCP:
		if def.Modbus == nil {
			return nil
		}
		for _, selected := range def.Modbus.Ranges {
			if resource.ID != "modbus:"+selected.ID {
				continue
			}
			if hasModel(Publish) {
				actions = append(actions, "source.read")
			}
			if hasModel(Subscribe) && selected.CanWrite() {
				actions = append(actions, "source.write")
			}
		}
	}
	return actions
}

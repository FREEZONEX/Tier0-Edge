package flow

import (
	"fmt"
	"strings"

	"backend/internal/config"
)

const managedMQTTBrokerMarker = "_tier0ManagedMQTTBroker"

func mqttEndpoint(endpoints []config.MQTTEndpoint) config.MQTTEndpoint {
	if len(endpoints) > 0 && endpoints[0].Host != "" {
		return endpoints[0]
	}
	endpoint, _ := config.ParseMQTTEndpoint(config.DefaultMQTTBrokers)
	return endpoint
}

// IsInternalMQTTBrokerNode recognizes platform nodes independently of their
// configured address. The legacy empty/emqx check also upgrades existing flows.
func IsInternalMQTTBrokerNode(node map[string]any) bool {
	if strings.TrimSpace(asString(node["type"])) != "mqtt-broker" {
		return false
	}
	if marked, _ := node[managedMQTTBrokerMarker].(bool); marked || strings.EqualFold(asString(node[managedMQTTBrokerMarker]), "true") {
		return true
	}
	broker := strings.TrimSpace(asString(node["broker"]))
	if broker == "" {
		broker = strings.TrimSpace(asString(node["host"]))
	}
	return broker == "" || strings.EqualFold(broker, "emqx")
}

// ApplyMQTTEndpoint changes only the listener settings of a platform broker.
// Credentials and MQTT topic/session settings belong to the existing flow.
func ApplyMQTTEndpoint(node map[string]any, endpoint config.MQTTEndpoint) {
	endpoint = mqttEndpoint([]config.MQTTEndpoint{endpoint})
	node[managedMQTTBrokerMarker] = true
	node["broker"] = endpoint.Host
	if strings.Contains(endpoint.Host, ":") {
		node["broker"] = "[" + endpoint.Host + "]"
	}
	if endpoint.WebSocket() {
		node["broker"] = endpoint.URL()
	}
	node["port"] = endpoint.Port
	node["usetls"] = endpoint.TLS()
	delete(node, "host")
	delete(node, "url")
	if endpoint.TLS() {
		// Node-RED otherwise defaults this old broker-level option to false.
		node["verifyservercert"] = true
	} else {
		delete(node, "tls")
	}
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

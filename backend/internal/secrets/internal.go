package secrets

import (
	"os"
	"strings"
)

const (
	legacyMQTTBackendPassword    = "T0BackendMqtt_8nV4qZ7pK2rY6mL"
	legacyMQTTSourceflowPassword = "T0SourceflowMqtt_6pR9xN2kQ8vT4zC"
	legacyMQTTEventflowPassword  = "T0EventflowMqtt_4mK7qV2nY9pL6xR"
	legacyEMQXAPIKey             = "emqx-admin"
	legacyEMQXAPISecret          = "T0EmqxApi_7Yq2N9vL4Pz8Hc6M"
)

// InternalMQTTPassword centralizes the transitional fallback for legacy
// deployments whose .env.runtime has not been backfilled yet.
func InternalMQTTPassword(username string) string {
	// Stream deployments no longer run legacy Node-RED services. Do not keep
	// their broker identities usable after retiring the corresponding routes.
	if strings.TrimSpace(os.Getenv("SOURCE_STREAM_NATS_URL")) != "" &&
		(strings.TrimSpace(username) == "sourceflow" || strings.TrimSpace(username) == "eventflow") {
		return ""
	}
	switch strings.TrimSpace(username) {
	case "backend":
		return envOrFallback("MQTT_BACKEND_PASSWORD", legacyMQTTBackendPassword)
	case "sourceflow":
		return envOrFallback("MQTT_SOURCEFLOW_PASSWORD", legacyMQTTSourceflowPassword)
	case "eventflow":
		return envOrFallback("MQTT_EVENTFLOW_PASSWORD", legacyMQTTEventflowPassword)
	default:
		return ""
	}
}

func AllowInternalMQTTUser(username, password string) bool {
	want := InternalMQTTPassword(username)
	return want != "" && strings.TrimSpace(password) == want
}

func EMQXAPIKey() (string, string) {
	return envOrFallback("EMQX_API_KEY", legacyEMQXAPIKey),
		envOrFallback("EMQX_API_SECRET", legacyEMQXAPISecret)
}

func InternalToken(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func envOrFallback(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

package config

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const DefaultMQTTBrokers = "tcp://emqx:1883"

// DefaultMQTTWSPort 是 EMQX 内部明文 WebSocket listener 端口（emqx.conf 常开）。
const DefaultMQTTWSPort = "8083"

// MQTTEndpoint is one MQTT listener. Generated Node-RED and SDK clients use the
// first configured broker; DataIngest retains its complete failover broker list.
type MQTTEndpoint struct {
	Scheme string
	Host   string
	Port   string
	Path   string
}

func ParseMQTTEndpoint(brokers string) (MQTTEndpoint, error) {
	if strings.TrimSpace(brokers) == "" {
		brokers = DefaultMQTTBrokers
	}
	parts := strings.FieldsFunc(brokers, func(r rune) bool { return r == ',' || r == ';' })
	var raw string
	for _, part := range parts {
		if raw = strings.TrimSpace(part); raw != "" {
			break
		}
	}
	if raw == "" {
		return MQTTEndpoint{}, fmt.Errorf("MQTT broker list is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "tcp://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return MQTTEndpoint{}, fmt.Errorf("MQTT broker must contain a valid host and port")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return MQTTEndpoint{}, fmt.Errorf("MQTT broker must not contain credentials, query parameters or a fragment")
	}
	endpoint := MQTTEndpoint{Scheme: strings.ToLower(parsed.Scheme), Host: parsed.Hostname(), Port: parsed.Port(), Path: parsed.Path}
	defaultPort := "1883"
	switch endpoint.Scheme {
	case "tcp", "mqtt":
	case "ssl", "tls", "tcps", "mqtts", "mqtt+ssl":
		defaultPort = "8883"
	case "ws":
		defaultPort = "80"
	case "wss":
		defaultPort = "443"
	default:
		return MQTTEndpoint{}, fmt.Errorf("unsupported MQTT broker scheme %q", endpoint.Scheme)
	}
	if !endpoint.WebSocket() && endpoint.Path != "" && endpoint.Path != "/" {
		return MQTTEndpoint{}, fmt.Errorf("TCP MQTT broker must not contain a path")
	}
	if endpoint.Port == "" {
		endpoint.Port = defaultPort
	}
	if err := validateMQTTPort(endpoint.Port); err != nil {
		return MQTTEndpoint{}, err
	}
	if endpoint.WebSocket() && endpoint.Path == "" {
		endpoint.Path = "/mqtt"
	}
	return endpoint, nil
}

func (e MQTTEndpoint) WebSocket() bool { return e.Scheme == "ws" || e.Scheme == "wss" }

func (e MQTTEndpoint) TLS() bool {
	return e.Scheme == "ssl" || e.Scheme == "tls" || e.Scheme == "tcps" || e.Scheme == "mqtts" || e.Scheme == "mqtt+ssl" || e.Scheme == "wss"
}

func (e MQTTEndpoint) URL() string {
	return (&url.URL{Scheme: e.Scheme, Host: net.JoinHostPort(e.Host, e.Port), Path: e.Path}).String()
}

// SDKMQTTAddress resolves the App SDK's WebSocket endpoint. A TCP listener's port
// is never used as a WebSocket port. Explicit SDK settings override derivation.
func (c Config) SDKMQTTAddress() (host, port string, err error) {
	endpoint, err := ParseMQTTEndpoint(c.DataIngest.MqttBrokers)
	if err != nil {
		return "", "", err
	}
	fullURL := endpoint.WebSocket() || endpoint.TLS()
	if !endpoint.WebSocket() {
		useTLS := endpoint.TLS()
		endpoint.Scheme, endpoint.Port, endpoint.Path = "ws", "8083", "/mqtt"
		if useTLS {
			endpoint.Scheme, endpoint.Port = "wss", "8084"
		}
	}
	if override := strings.TrimSpace(c.Tier0Sdk.MqttHost); override != "" {
		if strings.Contains(override, "://") {
			fullURL = true
			endpoint, err = ParseMQTTEndpoint(override)
			if err != nil || !endpoint.WebSocket() {
				return "", "", fmt.Errorf("TIER0_SDK_MQTT_HOST must be a hostname or ws/wss URL")
			}
		} else {
			// Host overrides contain no port; TIER0_SDK_MQTT_PORT owns that value.
			hostname := strings.Trim(override, "[]")
			if hostname == "" || strings.ContainsAny(hostname, "/?#@,; []\t\r\n") || (strings.Contains(hostname, ":") && net.ParseIP(hostname) == nil) {
				return "", "", fmt.Errorf("TIER0_SDK_MQTT_HOST must be a hostname or ws/wss URL")
			}
			endpoint.Host = hostname
		}
	}
	if override := strings.TrimSpace(c.Tier0Sdk.MqttPort); override != "" {
		if err := validateMQTTPort(override); err != nil {
			return "", "", fmt.Errorf("TIER0_SDK_MQTT_PORT: %w", err)
		}
		endpoint.Port = override
	}
	// Tier0-sdk preserves full ws/wss URLs ending in /mqtt; other paths would
	// have /mqtt appended by the SDK and must not silently target another URL.
	endpoint.Path = strings.TrimRight(endpoint.Path, "/")
	if !strings.HasSuffix(endpoint.Path, "/mqtt") {
		return "", "", fmt.Errorf("App SDK MQTT WebSocket path must end in /mqtt")
	}
	if !fullURL {
		// Preserve the existing host/port environment contract for plain WS.
		host := endpoint.Host
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		return host, endpoint.Port, nil
	}
	return endpoint.URL(), endpoint.Port, nil
}

func validateMQTTPort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("MQTT port must be an integer between 1 and 65535")
	}
	return nil
}

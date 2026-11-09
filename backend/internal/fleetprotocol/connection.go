package fleetprotocol

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const (
	ManagementTopicRoot      = "fleet/v1"
	FleetAgentClientIDSuffix = "-fleet-agent"
	defaultMQTTPort          = "1883"
	defaultSecureMQTTPort    = "8883"
	defaultWebSocketPort     = "80"
	defaultSecureSocketPort  = "443"
)

const (
	TopicHello           = "hello"
	TopicHelloAck        = "helloAck"
	TopicError           = "error"
	TopicHeartbeat       = "heartbeat"
	TopicSnapshot        = "snapshot"
	TopicSnapshotRequest = "snapshotRequest"
	TopicCommand         = "command"
	TopicCommandAck      = "commandAck"
	TopicCommandResult   = "commandResult"
	TopicDisconnect      = "disconnect"
)

var ErrInvalidConnectionToken = errors.New("invalid Fleet connection token")

// ConnectionToken is the existing EdgeNode/CloudSync token contract used by
// both business synchronization and Fleet management. The raw token remains
// the only user-facing credential; callers derive endpoints and identity from
// its metadata instead of asking users to enter them separately.
type ConnectionToken struct {
	NetworkMode         string
	CenterType          string
	NetworkBootstrapURL string
	Raw                 string
	NodeID              string
	AuthID              string
	ClientID            string
	Username            string
	Password            string
	HTTPBaseURL         string
	MQTTBrokers         string
	BusinessTopic       string
}

type connectionTokenMetadata struct {
	CenterType          string `json:"centerType,omitempty"`
	NetworkBootstrapURL string `json:"networkBootstrapURL,omitempty"`
	NodeID              string `json:"nodeID,omitempty"`
	ClientID            string `json:"clientID,omitempty"`
	Username            string `json:"username,omitempty"`
	Password            string `json:"password,omitempty"`
	HTTPBaseURL         string `json:"httpBaseURL,omitempty"`
	MQTTURL             string `json:"mqttURL,omitempty"`
	MQTTBrokers         string `json:"mqttBrokers,omitempty"`
	TopicRoot           string `json:"topicRoot,omitempty"`
}

func ParseConnectionToken(raw string) (ConnectionToken, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ConnectionToken{}, ErrInvalidConnectionToken
	}
	authToken, metadata, ok := splitConnectionToken(raw)
	if !ok {
		return ConnectionToken{}, ErrInvalidConnectionToken
	}
	clientID := strings.TrimSpace(metadata.ClientID)
	authID := connectionAuthID(clientID)
	password := strings.TrimSpace(metadata.Password)
	if password == "" && strings.HasPrefix(authToken, clientID+"&") {
		password = strings.TrimSpace(strings.TrimPrefix(authToken, clientID+"&"))
	}
	username := strings.TrimSpace(metadata.Username)
	if username == "" {
		username = clientID + "&edgeNode"
	}
	nodeID := normalizeConnectionNodeID(metadata.NodeID)
	mqttBrokers := normalizeConnectionMQTTBrokers(firstConnectionValue(metadata.MQTTURL, metadata.MQTTBrokers))
	if clientID == "" || authID == "" || password == "" || nodeID == "" || mqttBrokers == "" {
		return ConnectionToken{}, ErrInvalidConnectionToken
	}
	mode := ""
	switch metadata.CenterType {
	case "enterprise":
		mode = NetworkModeDirect
	case "cloud":
		mode = NetworkModeTailscale
	case "":
		// Old tokens may still be read for diagnostics. Connection negotiation
		// requires an explicit, authenticated center type and never guesses it.
	default:
		return ConnectionToken{}, ErrInvalidConnectionToken
	}
	return ConnectionToken{
		NetworkMode:         mode,
		CenterType:          metadata.CenterType,
		Raw:                 raw,
		NetworkBootstrapURL: strings.TrimSpace(metadata.NetworkBootstrapURL),
		NodeID:              nodeID,
		AuthID:              authID,
		ClientID:            clientID,
		Username:            username,
		Password:            password,
		HTTPBaseURL:         normalizeConnectionHTTPBaseURL(metadata.HTTPBaseURL),
		MQTTBrokers:         mqttBrokers,
		BusinessTopic:       firstConnectionValue(strings.Trim(strings.TrimSpace(metadata.TopicRoot), "/"), "cloudsync"),
	}, nil
}

func (t ConnectionToken) AgentClientID() string {
	return strings.TrimSpace(t.ClientID) + FleetAgentClientIDSuffix
}

func (t ConnectionToken) BrokerURLs() []string {
	parts := strings.Split(t.MQTTBrokers, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func ManagementTopic(authID, kind string) (string, error) {
	authID = cleanManagementSegment(authID)
	kind = cleanManagementSegment(kind)
	if authID == "" || kind == "" {
		return "", errors.New("invalid Fleet management topic")
	}
	return fmt.Sprintf("%s/%s/%s", ManagementTopicRoot, authID, kind), nil
}

func MustManagementTopic(authID, kind string) string {
	topic, err := ManagementTopic(authID, kind)
	if err != nil {
		return ""
	}
	return topic
}

func ParseManagementTopic(topic string) (authID, kind string, ok bool) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(topic), "/"), "/")
	if len(parts) != 4 || parts[0] != "fleet" || parts[1] != "v1" {
		return "", "", false
	}
	authID = cleanManagementSegment(parts[2])
	kind = cleanManagementSegment(parts[3])
	return authID, kind, authID != "" && kind != ""
}

func MarshalEnvelope(messageType MessageType, messageID, replyTo string, payload any) ([]byte, error) {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(Envelope{
		ProtocolVersion: ProtocolVersion,
		MessageID:       strings.TrimSpace(messageID),
		Type:            messageType,
		SentAt:          NowMillis(),
		ReplyTo:         strings.TrimSpace(replyTo),
		Payload:         rawPayload,
	})
}

func splitConnectionToken(raw string) (string, connectionTokenMetadata, bool) {
	parts := strings.SplitN(raw, "#", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return "", connectionTokenMetadata{}, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(parts[1]))
	if err != nil {
		return "", connectionTokenMetadata{}, false
	}
	var metadata connectionTokenMetadata
	if err := json.Unmarshal(decoded, &metadata); err != nil {
		return "", connectionTokenMetadata{}, false
	}
	return strings.TrimSpace(parts[0]), metadata, true
}

func connectionAuthID(clientID string) string {
	parts := strings.Split(strings.TrimSpace(clientID), "&")
	if len(parts) >= 2 {
		return cleanManagementSegment(parts[1])
	}
	return cleanManagementSegment(clientID)
}

func normalizeConnectionNodeID(nodeID string) string {
	nodeID = strings.TrimSpace(nodeID)
	if strings.HasPrefix(strings.ToLower(nodeID), "edge.") {
		return strings.TrimSpace(nodeID[len("edge."):])
	}
	return nodeID
}

func normalizeConnectionHTTPBaseURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	return raw
}

func normalizeConnectionMQTTBrokers(raw string) string {
	parts := strings.FieldsFunc(raw, func(character rune) bool { return character == ',' || character == ';' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "://") {
			if strings.HasSuffix(part, ":"+defaultSecureMQTTPort) {
				part = "ssl://" + part
			} else if strings.Contains(part, ":") {
				part = "tcp://" + part
			} else {
				part = "ssl://" + part + ":" + defaultSecureMQTTPort
			}
		}
		parsed, err := url.Parse(part)
		if err != nil || parsed.Hostname() == "" {
			continue
		}
		parsed.Scheme = strings.ToLower(parsed.Scheme)
		defaultPort := connectionMQTTDefaultPort(parsed.Scheme)
		if defaultPort == "" {
			continue
		}
		port := parsed.Port()
		if port == "" {
			port = defaultPort
		}
		portNumber, err := strconv.ParseUint(port, 10, 16)
		if err != nil || portNumber == 0 {
			continue
		}
		parsed.Host = net.JoinHostPort(parsed.Hostname(), port)
		result = append(result, parsed.String())
	}
	return strings.Join(result, ",")
}

func connectionMQTTDefaultPort(scheme string) string {
	switch scheme {
	case "tcp", "mqtt":
		return defaultMQTTPort
	case "ssl", "mqtts":
		return defaultSecureMQTTPort
	case "ws":
		return defaultWebSocketPort
	case "wss":
		return defaultSecureSocketPort
	default:
		return ""
	}
}

func cleanManagementSegment(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "/+#") {
		return ""
	}
	return raw
}

func firstConnectionValue(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

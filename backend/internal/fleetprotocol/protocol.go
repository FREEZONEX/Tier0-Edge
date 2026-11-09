package fleetprotocol

import (
	"encoding/json"
	"strings"
	"time"
)

const ProtocolVersion = "1.0"

const ErrorCodeSessionUnknown = "FLEET_SESSION_UNKNOWN"

const (
	HeartbeatInterval = 15 * time.Second
	OfflineThreshold  = 60 * time.Second
	SnapshotInterval  = 60 * time.Second
	ReconnectMaxDelay = 60 * time.Second
	CommandTimeout    = 120 * time.Second
	WriteTimeout      = 10 * time.Second
	MaxMessageBytes   = 2 << 20
)

type MessageType string

const (
	MessageTypeHello           MessageType = "hello"
	MessageTypeHelloAck        MessageType = "helloAck"
	MessageTypeError           MessageType = "error"
	MessageTypeHeartbeat       MessageType = "heartbeat"
	MessageTypeSnapshot        MessageType = "snapshot"
	MessageTypeSnapshotRequest MessageType = "snapshotRequest"
	MessageTypeCommand         MessageType = "command"
	MessageTypeCommandAck      MessageType = "commandAck"
	MessageTypeCommandResult   MessageType = "commandResult"
	MessageTypeDisconnect      MessageType = "disconnect"
)

func NowMillis() int64 { return time.Now().UTC().UnixMilli() }

const (
	ConnectionUnconfigured = "unconfigured"
	ConnectionDisconnected = "disconnected"
	ConnectionConnecting   = "connecting"
	ConnectionConnected    = "connected"
	ConnectionStandby      = "standby"
)

const (
	OperationContainerDetail  = "container.detail"
	OperationContainerLogs    = "container.logs"
	OperationContainerRestart = "container.restart"
)

type Envelope struct {
	ProtocolVersion string          `json:"protocolVersion"`
	MessageID       string          `json:"messageID"`
	Type            MessageType     `json:"type"`
	SentAt          int64           `json:"sentAt"`
	ReplyTo         string          `json:"replyTo,omitempty"`
	Payload         json.RawMessage `json:"payload"`
}

type HelloPayload struct {
	FleetNodeID    string   `json:"fleetNodeID"`
	ConnectionID   string   `json:"connectionID"`
	AgentVersion   string   `json:"agentVersion"`
	OS             string   `json:"os"`
	Arch           string   `json:"arch"`
	InstallationID string   `json:"installationID"`
	Capabilities   []string `json:"capabilities"`
}

type DisconnectPayload struct {
	ConnectionID string `json:"connectionID"`
	SessionID    string `json:"sessionID,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type HelloAckPayload struct {
	ServerTime               int64  `json:"serverTime"`
	SessionID                string `json:"sessionID"`
	HeartbeatIntervalSeconds int    `json:"heartbeatIntervalSeconds"`
	SnapshotIntervalSeconds  int    `json:"snapshotIntervalSeconds"`
	MaxMessageBytes          int    `json:"maxMessageBytes"`
}

type ErrorPayload struct {
	Code      string `json:"code"`
	Retryable bool   `json:"retryable"`
	Message   string `json:"message,omitempty"`
}

type HeartbeatPayload struct {
	SessionID      string `json:"sessionID"`
	AgentTime      int64  `json:"agentTime"`
	LastSnapshotID string `json:"lastSnapshotID,omitempty"`
}

type SnapshotRequestPayload struct {
	ConnectionID string `json:"connectionID"`
	SessionID    string `json:"sessionID"`
	Reason       string `json:"reason,omitempty"`
}

type SnapshotPayload struct {
	ConnectionID string            `json:"connectionID"`
	SessionID    string            `json:"sessionID"`
	SnapshotID   string            `json:"snapshotID"`
	ObservedAt   int64             `json:"observedAt"`
	Host         HostSnapshot      `json:"host"`
	Containers   ContainerSnapshot `json:"containers"`
}

type HostSnapshot struct {
	Hostname   string      `json:"hostname"`
	OS         string      `json:"os"`
	Arch       string      `json:"arch"`
	LogicalCPU int         `json:"logicalCPU"`
	CPU        CPUUsage    `json:"cpu"`
	Memory     MemoryUsage `json:"memory"`
	Disks      []DiskUsage `json:"disks,omitempty"`
}

type CPUUsage struct {
	UsagePercent float64 `json:"usagePercent"`
	Available    bool    `json:"available"`
	ErrorCode    string  `json:"errorCode,omitempty"`
}

type MemoryUsage struct {
	TotalBytes uint64 `json:"totalBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
	Available  bool   `json:"available"`
	ErrorCode  string `json:"errorCode,omitempty"`
}

type DiskUsage struct {
	Name       string `json:"name"`
	MountPoint string `json:"mountPoint,omitempty"`
	TotalBytes uint64 `json:"totalBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
	Available  bool   `json:"available"`
	ErrorCode  string `json:"errorCode,omitempty"`
}

type PortBinding struct {
	ContainerPort uint16 `json:"containerPort"`
	Protocol      string `json:"protocol"`
	HostIP        string `json:"hostIP,omitempty"`
	HostPort      string `json:"hostPort,omitempty"`
}

type HealthCheckSummary struct {
	StartedAt  int64 `json:"startedAt"`
	FinishedAt int64 `json:"finishedAt"`
	ExitCode   int   `json:"exitCode"`
}

type HealthSummary struct {
	Status        string              `json:"status"`
	FailingStreak int                 `json:"failingStreak"`
	LastCheck     *HealthCheckSummary `json:"lastCheck,omitempty"`
}

type Summary struct {
	ContainerName  string            `json:"containerName,omitempty"`
	SupersededBy   string            `json:"supersededBy,omitempty"`
	ContainerID    string            `json:"containerID"`
	ServiceName    string            `json:"serviceName"`
	DisplayName    string            `json:"displayName"`
	Image          string            `json:"image"`
	State          string            `json:"state"`
	HealthStatus   string            `json:"healthStatus"`
	RestartCount   int               `json:"restartCount"`
	StartedAt      int64             `json:"startedAt"`
	Restartable    bool              `json:"restartable"`
	Ports          []PortBinding     `json:"ports"`
	ComposeService string            `json:"composeService,omitempty"`
	ComposeProject string            `json:"composeProject,omitempty"`
	Labels         map[string]string `json:"labels"`
}

type Detail struct {
	Summary
	ImageID    string        `json:"imageID"`
	CreatedAt  int64         `json:"createdAt"`
	FinishedAt int64         `json:"finishedAt"`
	Health     HealthSummary `json:"health"`
}

type CollectionError struct {
	ContainerID string `json:"containerID,omitempty"`
	Code        string `json:"code"`
}

type SnapshotSummary struct {
	Historical int `json:"historical"`
	Total      int `json:"total"`
	Running    int `json:"running"`
	Unhealthy  int `json:"unhealthy"`
}

type EngineSummary struct {
	Version    string `json:"version"`
	APIVersion string `json:"apiVersion"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
}

type ContainerSnapshot struct {
	SnapshotID     string            `json:"snapshotID"`
	ObservedAt     int64             `json:"observedAt"`
	InstallationID string            `json:"installationID"`
	Engine         EngineSummary     `json:"engine"`
	Containers     []Summary         `json:"containers"`
	Summary        SnapshotSummary   `json:"summary"`
	Partial        bool              `json:"partial"`
	Errors         []CollectionError `json:"errors"`
}

type ContainerLogsRequest struct {
	Tail       int   `json:"tail"`
	Since      int64 `json:"since,omitempty"`
	Timestamps *bool `json:"timestamps,omitempty"`
	LimitBytes int   `json:"limitBytes,omitempty"`
}

type ContainerLogsResult struct {
	OperationID string `json:"operationID,omitempty"`
	ContainerID string `json:"containerID"`
	Logs        string `json:"logs"`
	LineCount   int    `json:"lineCount"`
	Bytes       int    `json:"bytes"`
	Truncated   bool   `json:"truncated"`
	ObservedAt  int64  `json:"observedAt"`
}

type ContainerRestartRequest struct {
	OperationID string `json:"operationID"`
}

type ContainerRestartResult struct {
	OperationID string  `json:"operationID"`
	ContainerID string  `json:"containerID"`
	Status      string  `json:"status"`
	StartedAt   int64   `json:"startedAt"`
	FinishedAt  int64   `json:"finishedAt"`
	Container   Summary `json:"container"`
}

type CommandTarget struct {
	ContainerID string `json:"containerID"`
}

type CommandPayload struct {
	ConnectionID  string          `json:"connectionID"`
	SessionID     string          `json:"sessionID"`
	OperationID   string          `json:"operationID"`
	OperationType string          `json:"operationType"`
	Target        CommandTarget   `json:"target"`
	Parameters    json.RawMessage `json:"parameters,omitempty"`
	Deadline      int64           `json:"deadline"`
}

type CommandAckPayload struct {
	ConnectionID string `json:"connectionID"`
	SessionID    string `json:"sessionID"`
	OperationID  string `json:"operationID"`
	Accepted     bool   `json:"accepted"`
	ErrorCode    string `json:"errorCode,omitempty"`
}

type CommandResultPayload struct {
	ConnectionID string          `json:"connectionID"`
	SessionID    string          `json:"sessionID"`
	OperationID  string          `json:"operationID"`
	Status       string          `json:"status"`
	Result       json.RawMessage `json:"result,omitempty"`
	ErrorCode    string          `json:"errorCode,omitempty"`
	FinishedAt   int64           `json:"finishedAt"`
}

type AgentStatus struct {
	Service            string       `json:"service"`
	Version            string       `json:"version"`
	ProtocolVersion    string       `json:"protocolVersion"`
	StartedAt          int64        `json:"startedAt"`
	Configured         bool         `json:"configured"`
	ConnectionState    string       `json:"connectionState"`
	FleetNodeID        string       `json:"fleetNodeID,omitempty"`
	CenterURL          string       `json:"centerURL,omitempty"`
	InstallationID     string       `json:"installationID,omitempty"`
	ComposeProjectName string       `json:"composeProjectName,omitempty"`
	Host               HostSnapshot `json:"host"`
	Capabilities       []string     `json:"capabilities"`
}

// SummarizeContainers distinguishes retained deployment backups from current
// containers without hiding stopped services or grouping legitimate replicas.
// A backup is a stopped container renamed <original>-before-YYYYMMDDhhmmss,
// and must have a distinct replacement with the original name and same service
// in this installation's already authorized snapshot. Unknown names stay current.
func SummarizeContainers(containers []Summary) SnapshotSummary {
	byName := make(map[string]Summary, len(containers))
	for _, item := range containers {
		if item.ContainerName != "" {
			byName[item.ContainerName] = item
		}
	}
	var result SnapshotSummary
	for i := range containers {
		item := &containers[i]
		item.SupersededBy = ""
		if item.State == "exited" || item.State == "stopped" {
			if at := strings.LastIndex(item.ContainerName, "-before-"); at > 0 {
				stamp := item.ContainerName[at+len("-before-"):]
				_, validStamp := time.Parse("20060102150405", stamp)
				replacement, exists := byName[item.ContainerName[:at]]
				if validStamp == nil && exists && replacement.ContainerID != item.ContainerID && item.ServiceName != "" && replacement.ServiceName == item.ServiceName {
					item.SupersededBy = replacement.ContainerID
				}
			}
		}
		if item.SupersededBy != "" {
			result.Historical++
			continue
		}
		result.Total++
		if strings.EqualFold(item.State, "running") {
			result.Running++
		}
		if !strings.EqualFold(item.State, "running") || strings.EqualFold(item.HealthStatus, "unhealthy") {
			result.Unhealthy++
		}
	}
	return result
}

package fleetprotocol

import "context"

// RuntimeProvider isolates installation-specific process/container ownership.
type RuntimeProvider interface {
	List(context.Context) (RuntimeSnapshot, error)
	Logs(context.Context, RuntimeRequest) (RuntimeLogs, error)
	Control(context.Context, RuntimeRequest) (RuntimeOperation, error)
	Operation(context.Context, string) (RuntimeOperation, error)
}

type RuntimeRequest struct {
	Action      string `json:"action"`
	ResourceID  string `json:"resourceId,omitempty"`
	OperationID string `json:"operationId,omitempty"`
	Verb        string `json:"verb,omitempty"`
	Tail        int    `json:"tail,omitempty"`
}
type RuntimeItem struct {
	InstallationID string   `json:"installationId,omitempty"`
	Dependencies   []string `json:"dependencies"`
	Protection     string   `json:"protection,omitempty"`
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	State          string   `json:"state"`
	Health         string   `json:"health"`
	PID            int      `json:"pid,omitempty"`
	Version        string   `json:"version,omitempty"`
	StartedAt      int64    `json:"startedAt"`
	RestartCount   int      `json:"restartCount"`
	Actions        []string `json:"actions"`
	Reason         string   `json:"reason,omitempty"`
}
type RuntimeSnapshot struct {
	RuntimeMode string        `json:"runtimeMode"`
	ObservedAt  int64         `json:"observedAt"`
	Items       []RuntimeItem `json:"items"`
}
type RuntimeLogs struct {
	ResourceID string `json:"resourceId"`
	Text       string `json:"text"`
	Truncated  bool   `json:"truncated"`
}
type RuntimeOperation struct {
	ID         string `json:"id"`
	ResourceID string `json:"resourceId"`
	Verb       string `json:"verb"`
	State      string `json:"state"`
	Error      string `json:"error,omitempty"`
	UpdatedAt  int64  `json:"updatedAt"`
}

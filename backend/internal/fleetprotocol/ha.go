package fleetprotocol

type HAServiceGroupStatus struct {
	Name            string   `json:"name"`
	Expected        string   `json:"expected"`
	Total           int      `json:"total"`
	Running         int      `json:"running"`
	Stopped         int      `json:"stopped"`
	RequiredStopped int      `json:"requiredStopped"`
	Unhealthy       int      `json:"unhealthy"`
	Failures        []string `json:"failures,omitempty"`
	ObservedAt      int64    `json:"observedAt,omitempty"`
}

type HASyncStatus struct {
	Enabled         bool     `json:"enabled"`
	Peers           []string `json:"peers,omitempty"`
	State           string   `json:"state"`
	LastStartedAt   int64    `json:"lastStartedAt,omitempty"`
	LastSucceededAt int64    `json:"lastSucceededAt,omitempty"`
	LastError       string   `json:"lastError,omitempty"`
}

type HAStatus struct {
	Enabled           bool                 `json:"enabled"`
	MemberID          string               `json:"memberId"`
	Role              string               `json:"role"`
	Eligible          bool                 `json:"eligible"`
	CanAcquireVIP     bool                 `json:"canAcquireVip"`
	ShouldReleaseVIP  bool                 `json:"shouldReleaseVip"`
	Decision          string               `json:"decision"`
	DecisionReason    string               `json:"decisionReason,omitempty"`
	EligibilityReason string               `json:"eligibilityReason,omitempty"`
	KeepalivedState   string               `json:"keepalivedState,omitempty"`
	VIPOwned          bool                 `json:"vipOwned"`
	FailureSince      int64                `json:"failureSince,omitempty"`
	GraceRemainingMS  int64                `json:"graceRemainingMs,omitempty"`
	LastTransitionAt  int64                `json:"lastTransitionAt,omitempty"`
	LastTransitionID  string               `json:"lastTransitionId,omitempty"`
	LastErrorCode     string               `json:"lastErrorCode,omitempty"`
	ServiceGroup      HAServiceGroupStatus `json:"serviceGroup"`
	Sync              HASyncStatus         `json:"sync"`
}

type HAContainerStatus struct {
	ContainerID   string `json:"containerId"`
	ServiceName   string `json:"serviceName"`
	Order         int    `json:"order"`
	Required      bool   `json:"required"`
	ExpectedState string `json:"expectedState"`
	State         string `json:"state"`
	HealthStatus  string `json:"healthStatus"`
	DriftRelevant bool   `json:"driftRelevant"`
	ReasonCode    string `json:"reasonCode,omitempty"`
}

type HAContainerSnapshot struct {
	Group         string               `json:"group"`
	ExpectedState string               `json:"expectedState"`
	Summary       HAServiceGroupStatus `json:"summary"`
	Containers    []HAContainerStatus  `json:"containers"`
	ObservedAt    int64                `json:"observedAt"`
}

type HASyncRequest struct {
	FileSet string `json:"fileSet"`
	// Wait is reserved for small, failover-critical file sets such as a newly
	// activated license. Ordinary periodic synchronization stays queued.
	Wait bool `json:"wait,omitempty"`
}

type HASyncResponse struct {
	FileSet string `json:"fileSet"`
	Status  string `json:"status"`
}

type HASyncHistoryItem struct {
	ID           int64  `json:"id"`
	OperationID  string `json:"operationId"`
	Peer         string `json:"peer"`
	FileSet      string `json:"fileSet"`
	Revision     string `json:"revision"`
	Direction    string `json:"direction"`
	TriggerType  string `json:"triggerType"`
	Status       string `json:"status"`
	StartedAt    int64  `json:"startedAt"`
	FinishedAt   int64  `json:"finishedAt"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

type HASyncHistoryResponse struct {
	Items    []HASyncHistoryItem `json:"items"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"pageSize"`
}

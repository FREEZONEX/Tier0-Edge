package ha

type Role string

const (
	RoleStandby   Role = "standby"
	RolePromoting Role = "promoting"
	RoleActive    Role = "active"
	RoleDemoting  Role = "demoting"
	RoleFaulted   Role = "faulted"
)

type Decision string

const (
	DecisionKeep         Decision = "keep"
	DecisionRelease      Decision = "release"
	DecisionBlockAcquire Decision = "blockAcquire"
)

type KeepalivedState string

const (
	KeepalivedMaster KeepalivedState = "MASTER"
	KeepalivedBackup KeepalivedState = "BACKUP"
	KeepalivedFault  KeepalivedState = "FAULT"
	KeepalivedStop   KeepalivedState = "STOP"
)

type Eligibility struct {
	Enabled          bool               `json:"enabled"`
	Eligible         bool               `json:"eligible"`
	MemberID         string             `json:"memberId"`
	Role             Role               `json:"role"`
	CanAcquireVIP    bool               `json:"canAcquireVip"`
	ShouldReleaseVIP bool               `json:"shouldReleaseVip"`
	Decision         Decision           `json:"decision"`
	ReasonCode       string             `json:"reasonCode,omitempty"`
	FailureSince     int64              `json:"failureSince,omitempty"`
	GraceRemainingMS int64              `json:"graceRemainingMs,omitempty"`
	ServiceGroup     ServiceGroupStatus `json:"serviceGroup"`
	CheckedAt        int64              `json:"checkedAt"`
}

type Status struct {
	Enabled           bool               `json:"enabled"`
	MemberID          string             `json:"memberId"`
	Role              Role               `json:"role"`
	Eligible          bool               `json:"eligible"`
	CanAcquireVIP     bool               `json:"canAcquireVip"`
	ShouldReleaseVIP  bool               `json:"shouldReleaseVip"`
	Decision          Decision           `json:"decision"`
	DecisionReason    string             `json:"decisionReason,omitempty"`
	EligibilityReason string             `json:"eligibilityReason,omitempty"`
	KeepalivedState   KeepalivedState    `json:"keepalivedState,omitempty"`
	VIPOwned          bool               `json:"vipOwned"`
	FailureSince      int64              `json:"failureSince,omitempty"`
	GraceRemainingMS  int64              `json:"graceRemainingMs,omitempty"`
	LastTransitionAt  int64              `json:"lastTransitionAt,omitempty"`
	LastTransitionID  string             `json:"lastTransitionId,omitempty"`
	LastErrorCode     string             `json:"lastErrorCode,omitempty"`
	ServiceGroup      ServiceGroupStatus `json:"serviceGroup"`
	Sync              SyncStatus         `json:"sync"`
}

type ServiceGroupStatus struct {
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

type SyncStatus struct {
	Enabled               bool     `json:"enabled"`
	OutboundEnabled       bool     `json:"outboundEnabled"`
	OutboundBlockedReason string   `json:"outboundBlockedReason,omitempty"`
	Peers                 []string `json:"peers,omitempty"`
	State                 string   `json:"state"`
	LastStartedAt         int64    `json:"lastStartedAt,omitempty"`
	LastSucceededAt       int64    `json:"lastSucceededAt,omitempty"`
	LastError             string   `json:"lastError,omitempty"`
}

type TransitionRequest struct {
	KeepalivedState KeepalivedState `json:"keepalivedState"`
	TransitionID    string          `json:"transitionId"`
}

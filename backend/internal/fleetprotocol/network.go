package fleetprotocol

func ConnectionDiagnostic(err error) string {
	if err == nil {
		return ""
	}
	if code := ConnectionDiagnosticCode(err.Error()); code != "" {
		return code
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			if code := ConnectionDiagnostic(cause); code != "" {
				return code
			}
		}
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return ConnectionDiagnostic(wrapped.Unwrap())
	}
	return ""
}

// ConnectionDiagnosticCode only exposes known codes, never command output,
// URLs containing credentials, or token parsing errors.
func ConnectionDiagnosticCode(code string) string {
	switch code {
	case "FLEET_INVALID_TOKEN", "FLEET_AGENT_UNAVAILABLE", "FLEET_AGENT_NOT_CONFIGURED", "FLEET_AGENT_AUTH_FAILED",
		"FLEET_NETWORK_CLIENT_UNAVAILABLE", "FLEET_NETWORK_PREFERENCES_UNAVAILABLE",
		"FLEET_NETWORK_STATE_PERMISSIONS_INVALID", "FLEET_NETWORK_SOCKS_ADDRESS_INVALID", "FLEET_NETWORK_SOCKS_UNAVAILABLE", "FLEET_INGRESS_FORWARD_FAILED",
		"FLEET_NETWORK_BOOTSTRAP_UNAVAILABLE", "FLEET_NETWORK_BOOTSTRAP_REJECTED", "FLEET_CENTER_ENDPOINT_NOT_FOUND",
		"FLEET_NETWORK_DISABLED", "FLEET_CONNECTION_MODE_MISMATCH", "FLEET_NETWORK_STATE_ISOLATION_REQUIRED",
		"FLEET_NETWORK_IDENTITY_ALREADY_BOUND", "FLEET_NETWORK_IDENTITY_MISMATCH", "FLEET_NETWORK_CONTROL_PLANE_CONFLICT",
		"FLEET_NETWORK_PROFILES_INVALID", "FLEET_NETWORK_PROFILE_ROLLBACK_PENDING", "FLEET_NETWORK_PROFILE_RESTORE_FAILED",
		"FLEET_NETWORK_KEY_UNAVAILABLE", "FLEET_NETWORK_KEY_EXPIRED", "FLEET_NETWORK_REAUTH_REQUIRED",
		"FLEET_NETWORK_CREDENTIAL_UNAVAILABLE", "FLEET_NETWORK_PROFILE_LOGIN_FAILED", "FLEET_NETWORK_PROFILE_NAME_FAILED",
		"FLEET_NETWORK_PROFILE_UNAVAILABLE", "FLEET_NETWORK_INITIALIZING", "FLEET_CONNECTION_PREPARE_FAILED":
		return code
	default:
		return ""
	}
}

// Only infrastructure enrollment data; never business payload or user identity.
type SSHAdvertisement struct {
	HostPublicKey        string `json:"hostPublicKey"`
	Username             string `json:"username"`
	Port                 int    `json:"port"`
	TrustedCAFingerprint string `json:"trustedCAFingerprint,omitempty"`
	ErrorCode            string `json:"errorCode,omitempty"`
}
type SSHBootstrap struct {
	CAPublicKey string `json:"caPublicKey"`
}
type NetworkBootstrapRequest struct {
	Inspect       bool              `json:"inspect,omitempty"`
	SSH           *SSHAdvertisement `json:"ssh,omitempty"`
	NetworkNodeID string            `json:"networkNodeId,omitempty"`
	NodeID        string            `json:"nodeId"`
	ClientID      string            `json:"clientId"`
	NeedKey       bool              `json:"needKey"`
}

const NetworkModeDirect = "direct"
const NetworkModeTailscale = "tailscale"

type NetworkBootstrapResponse struct {
	NetworkMode   string        `json:"networkMode,omitempty"`
	CenterType    string        `json:"centerType,omitempty"`
	NetworkNodeID string        `json:"networkNodeId,omitempty"`
	SSH           *SSHBootstrap `json:"ssh,omitempty"`
	Enabled       bool          `json:"enabled"`
	LoginServer   string        `json:"loginServer"`
	AuthKey       string        `json:"authKey,omitempty"`
}

// Local-only infrastructure operations, protected by the installation's Agent
// token. AuthKey is accepted on enrollment and is never returned by status.
type NetworkEnrollRequest struct {
	LoginServer string `json:"loginServer"`
	AuthKey     string `json:"authKey,omitempty"`
	Hostname    string `json:"hostname"`
}
type NetworkStatus struct {
	Mode        string   `json:"mode,omitempty"`
	State       string   `json:"state"`
	NodeID      string   `json:"nodeId,omitempty"`
	LoginServer string   `json:"loginServer,omitempty"`
	Addresses   []string `json:"addresses,omitempty"`
	ErrorCode   string   `json:"errorCode,omitempty"`
	CheckedAt   int64    `json:"checkedAt"`
}

const DefaultNetworkIngressPort = 9443

// Local installation IPC with the root-owned maintenance helper. Deliberately
// excludes executable/socket paths, URLs, user names and enrollment credentials.
type NetworkProfileRequest struct {
	StateDomain     string `json:"stateDomain"`
	RequestID       string `json:"requestId"`
	Action          string `json:"action"`
	ExpectedProfile string `json:"expectedProfile"`
	TargetProfile   string `json:"targetProfile"`
	CreatedAt       int64  `json:"createdAt"`
}

type NetworkProfileResult struct {
	StateDomain string `json:"stateDomain"`
	RequestID   string `json:"requestId"`
	Action      string `json:"action"`
	Status      string `json:"status"`
	ErrorCode   string `json:"errorCode"`
	Profile     string `json:"profile"`
	Operator    string `json:"operator"`
}

// Center product type is explicit, never inferred from its URL or license label.
func ValidCenterNetworkMode(centerType, mode string) bool {
	return centerType == "enterprise" && mode == NetworkModeDirect || centerType == "cloud" && mode == NetworkModeTailscale
}

package mcpserver

// The wire protocol and JavaScript bindings follow cloud uns-swe dc7b7c235.
// Credentials and operation authorization remain in the Enterprise parent.
import (
	"encoding/json"
	"fmt"
)

type apiRequest struct {
	FleetNodeID      string            `json:"fleetNodeId,omitempty"`
	CredentialImport string            `json:"credentialImport,omitempty"`
	Method           string            `json:"method"`
	Path             string            `json:"path"`
	Query            map[string]string `json:"query,omitempty"`
	Body             json.RawMessage   `json:"body,omitempty"`
	RawBody          *string           `json:"rawBody,omitempty"`
	ContentType      string            `json:"contentType,omitempty"`
}
type apiResponse struct {
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	Body        string `json:"body"`
	RawJSON     bool   `json:"rawJSON,omitempty"`
}

func requestLimitError(limit int) error {
	return fmt.Errorf("tier0.request exceeded the %d request limit", limit)
}

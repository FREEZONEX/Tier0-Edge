package common

import (
	"strconv"
	"strings"
)

const (
	// PlatformWorkspaceID is the single workspace used by Enterprise.
	PlatformWorkspaceID = int64(1)

	apiKeyWorkspacePrefix = "ws"
	apiKeyWorkspaceSep    = "_"
)

// FormatWorkspaceAPIKey keeps Enterprise API keys compatible with Cloud and
// with the SDK workspace parser.
func FormatWorkspaceAPIKey(prefix string, workspaceID int64, token string) string {
	prefix = strings.TrimSpace(prefix)
	token = strings.TrimSpace(token)
	if prefix == "" || token == "" || workspaceID <= 0 {
		return ""
	}
	if !strings.HasSuffix(prefix, "-") {
		prefix += "-"
	}
	return prefix + apiKeyWorkspacePrefix + strconv.FormatInt(workspaceID, 36) + apiKeyWorkspaceSep + token
}

// ParseWorkspaceIDFromAPIKey parses the Cloud-compatible workspace segment
// from keys such as sk-svc-ws1_token.
func ParseWorkspaceIDFromAPIKey(apiKey string) int64 {
	apiKey = strings.TrimSpace(apiKey)
	parts := strings.SplitN(apiKey, "-", 3)
	if len(parts) != 3 || parts[0] != "sk" {
		return 0
	}
	payload := parts[2]
	if !strings.HasPrefix(payload, apiKeyWorkspacePrefix) {
		return 0
	}
	sepIndex := strings.Index(payload, apiKeyWorkspaceSep)
	if sepIndex <= len(apiKeyWorkspacePrefix) {
		return 0
	}
	workspaceID, err := strconv.ParseInt(payload[len(apiKeyWorkspacePrefix):sepIndex], 36, 64)
	if err != nil || workspaceID <= 0 {
		return 0
	}
	return workspaceID
}

package repo

import (
	"strings"
	"testing"

	domaincommon "backend/internal/domain/common"
)

func TestGenerateWorkspaceAPIKeyUsesCloudPrefixes(t *testing.T) {
	tests := []struct {
		keyType string
		prefix  string
	}{
		{keyType: "personal", prefix: "sk-per-ws1_"},
		{keyType: "service", prefix: "sk-svc-ws1_"},
		{keyType: "agent", prefix: "sk-agent-ws1_"},
		{keyType: "app", prefix: "sk-app-ws1_"},
	}
	for _, test := range tests {
		t.Run(test.keyType, func(t *testing.T) {
			key, err := GenerateWorkspaceAPIKey(test.keyType, domaincommon.PlatformWorkspaceID)
			if err != nil {
				t.Fatalf("GenerateWorkspaceAPIKey() error = %v", err)
			}
			if !strings.HasPrefix(key, test.prefix) {
				t.Fatalf("GenerateWorkspaceAPIKey() = %q, want prefix %q", key, test.prefix)
			}
			if got := domaincommon.ParseWorkspaceIDFromAPIKey(key); got != domaincommon.PlatformWorkspaceID {
				t.Fatalf("ParseWorkspaceIDFromAPIKey() = %d, want %d", got, domaincommon.PlatformWorkspaceID)
			}
		})
	}
}

package contextx

import "context"

// FleetTaskSubject is populated only by the Fleet authentication middleware.
// It grants no local user identity, role, or administrator permissions.
type FleetTaskSubject struct {
	Automatic     bool
	NodeID        string
	Scope         string
	ResourceID    string
	GrantID       string
	GrantRevision int64
}

type Subject struct {
	FleetTask        *FleetTaskSubject
	UserID           int64
	UserName         string
	Email            string
	AuthType         string
	APIKeyID         int64
	APIKeyName       string
	APIKeyPrefix     string
	APIKeyAppID      string
	APIKeyUsageType  string
	KeyType          string
	APIKeyPermission string
	MCPRestricted    bool     `json:"mcpRestricted,omitempty"`
	MCPScopes        []string `json:"mcpScopes,omitempty"`
	MCPGrantID       string   `json:"mcpGrantId,omitempty"`
	ResourceKeys     map[string]struct{}
	UIActions        []string
}

type subjectKey struct{}

func WithSubject(ctx context.Context, subject Subject) context.Context {
	return context.WithValue(ctx, subjectKey{}, subject)
}

func SubjectFrom(ctx context.Context) (Subject, bool) {
	subject, ok := ctx.Value(subjectKey{}).(Subject)
	return subject, ok
}

func HasResource(subject Subject, resourceKey string) bool {
	if resourceKey == "" {
		return true
	}
	_, ok := subject.ResourceKeys[resourceKey]
	return ok
}

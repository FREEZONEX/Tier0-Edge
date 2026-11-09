package unsv2

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"backend/internal/contextx"
	"backend/internal/repo"
)

var ErrNativeAppAccess = errors.New("App access requires a matching workspace business role")

func IsNativeAppEntry(id string, data AppData) bool {
	return data.Entry != nil && data.Entry.Environment == "" && data.Entry.BasePath == "/apps/"+id
}

func NativeAppRoles(app AppData, workspace map[string]string) ([]Role, bool) {
	roles := []Role{}
	for _, role := range app.Roles {
		if name, ok := workspace[role.ID]; ok {
			roles = append(roles, Role{ID: role.ID, Name: name})
		}
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].ID < roles[j].ID })
	return roles, len(app.Roles) == 0 || len(roles) > 0
}

// The native gateway authenticates the human before invoking this check.
func ResolveNativeAppRoles(ctx context.Context, appID string, userID int64) ([]Role, error) {
	member, err := repo.IsActiveWorkspaceMember(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !member {
		return nil, ErrNativeAppAccess
	}
	row, err := repo.NewUnsMetaRepo(ctx).Get(ctx, appID, false, "app")
	if err != nil {
		return nil, err
	}
	var app AppData
	if err = json.Unmarshal(row.Data, &app); err != nil {
		return nil, err
	}
	if row.MountID != nil || app.SchemaVersion != 1 || !IsNativeAppEntry(appID, app) {
		return nil, ErrNativeAppAccess
	}
	var workspace map[string]string
	if len(app.Roles) > 0 {
		workspace, err = repo.UserBusinessRoles(ctx, userID)
		if err != nil {
			return nil, err
		}
	}
	roles, ok := NativeAppRoles(app, workspace)
	if !ok {
		return nil, ErrNativeAppAccess
	}
	return roles, nil
}

// Apply before pagination, counts and graph traversal. API keys can administer
// definitions with explicit App management authority, but never inherit a human role.
func nativeAppVisibility(ctx context.Context) (string, []any, error) {
	actor, _ := contextx.SubjectFrom(ctx)
	if contextx.HasResource(actor, "uns.app.manage") {
		return "TRUE", nil, nil
	}
	external := "(" + repo.NativeAppDefinitions + ") IS NOT TRUE"
	if actor.AuthType != "session" || actor.MCPRestricted {
		return external, nil, nil
	}
	member, err := repo.IsActiveWorkspaceMember(ctx, actor.UserID)
	if err != nil || !member {
		return external, nil, err
	}
	roles, err := repo.UserBusinessRoles(ctx, actor.UserID)
	if err != nil {
		return "", nil, err
	}
	keys := make([]string, 0, len(roles))
	for k := range roles {
		keys = append(keys, k)
	}
	var ids []string
	if err = repo.GetCommonConn(ctx).Model(&repo.AppInstallation{}).Where("stage<>'uninstalled'").Pluck("app_id", &ids).Error; err != nil {
		return "", nil, err
	}
	return "(" + external + " OR (deleted_at=0 AND id IN ? AND (COALESCE(NULLIF(data->'roles','null'),'[]')='[]' OR EXISTS(SELECT 1 FROM jsonb_array_elements(data->'roles') r WHERE r->>'id' IN ?))))", []any{ids, keys}, nil
}

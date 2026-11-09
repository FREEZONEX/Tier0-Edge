package appinstall

import (
	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/domain/unsv2"
	"backend/internal/repo"
	"backend/internal/testkit"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
)

func TestWorkspaceAppLifecycleAndRoleImport(t *testing.T) {
	t.Setenv("TIER0_RUNTIME_MODE", "native")
	store, _ := testkit.NewTestDB(t)
	actor := contextx.Subject{UserID: 1, AuthType: "session", ResourceKeys: map[string]struct{}{"uns.app.manage": {}, "uns.app.read": {}, "iam.role.view": {}, "iam.role.create": {}, "iam.role.update": {}, "iam.role.delete": {}}}
	ctx := contextx.WithSubject(context.Background(), actor)
	viewer := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, AuthType: "session"})
	m := &Management{uns: unsv2.New(unsmeta.New(), nil), installLocks: map[string]*sync.Mutex{}}
	id := uuid.NewString()
	id2 := uuid.NewString()
	roles, err := readBundleRoles([]byte(`{"roles":[{"role_key":"planner","name":"Planner"},{"role_key":"quality","name":"Quality"}]}`))
	require.NoError(t, err)
	plan, err := previewRolePlan(ctx, roles)
	require.NoError(t, err)
	require.Error(t, m.provisionDefinition(ctx, InstallRequest{AppID: id}, Bundle{Name: "QA", Roles: roles}))
	require.NoError(t, m.provisionDefinition(ctx, InstallRequest{AppID: id, RolePlanDigest: plan.Digest}, Bundle{Name: "QA", Roles: roles}))
	require.Error(t, m.provisionDefinition(ctx, InstallRequest{AppID: id2, RolePlanDigest: plan.Digest}, Bundle{Name: "Another", Roles: roles}))
	plan, err = previewRolePlan(ctx, roles)
	require.NoError(t, err)
	require.NoError(t, m.provisionDefinition(ctx, InstallRequest{AppID: id2, RolePlanDigest: plan.Digest}, Bundle{Name: "Another", Roles: roles}))
	_, err = unsv2.ResolveNativeAppRoles(ctx, id, 1)
	require.ErrorIs(t, err, unsv2.ErrNativeAppAccess)
	require.NoError(t, store.DB().Exec("INSERT INTO sys_user_group(id,group_name) VALUES (32,'QA')").Error)
	require.NoError(t, store.DB().Exec("INSERT INTO sys_user_group_member(user_id,group_id) VALUES (1,32)").Error)
	role := repo.BusinessRole{Key: "quality", Name: "Quality", Revision: 1, GroupIDs: []string{"32"}}
	require.Error(t, SaveBusinessRole(viewer, role))
	require.NoError(t, SaveBusinessRole(ctx, role))
	require.ErrorIs(t, SaveBusinessRole(ctx, role), repo.ErrConflict)
	require.NoError(t, SaveBusinessRole(ctx, repo.BusinessRole{Key: "planner", Name: "Planner", Revision: 1, UserIDs: []string{"1"}}))
	for _, app := range []string{id, id2} {
		keys, err := unsv2.ResolveNativeAppRoles(ctx, app, 1)
		require.NoError(t, err)
		require.Equal(t, []unsv2.Role{{ID: "planner", Name: "Planner"}, {ID: "quality", Name: "Quality"}}, keys)
	}
	require.NoError(t, store.DB().Exec("DELETE FROM sys_user_group_member WHERE group_id=32").Error)
	keys, err := unsv2.ResolveNativeAppRoles(ctx, id, 1)
	require.NoError(t, err)
	require.Equal(t, []unsv2.Role{{ID: "planner", Name: "Planner"}}, keys)
	dir, err := BusinessRoles(ctx, true)
	require.NoError(t, err)
	raw, err := json.Marshal(dir)
	require.NoError(t, err)
	require.Contains(t, string(raw), id2)
	for _, role := range dir.(map[string]any)["roles"].([]repo.BusinessRole) {
		require.False(t, role.CanDelete)
	}
	require.Error(t, DeleteBusinessRole(ctx, "planner", 2))
	require.Error(t, m.SetTrashed(viewer, id, 1, true))
	require.NoError(t, m.SetTrashed(ctx, id, 1, true))
	dir, err = BusinessRoles(ctx, true)
	require.NoError(t, err)
	raw, err = json.Marshal(dir)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"trashed":true`)
	require.Error(t, DeleteBusinessRole(ctx, "planner", 2))
	_, err = unsv2.ResolveNativeAppRoles(ctx, id, 1)
	require.ErrorIs(t, err, repo.ErrNotFound)
	active, err := m.Catalog(ctx, false, false)
	require.NoError(t, err)
	require.Equal(t, 1, active.(map[string]any)["total"])
	trash, err := m.Catalog(ctx, true, false)
	require.NoError(t, err)
	require.Equal(t, 1, trash.(map[string]any)["total"])
	require.ErrorIs(t, m.SetTrashed(ctx, id, 1, false), repo.ErrConflict)
	require.NoError(t, m.SetTrashed(ctx, id, 2, false))
	require.NoError(t, SaveBusinessRole(ctx, repo.BusinessRole{Key: "planner", Name: "Planner", Revision: 2}))
	_, err = unsv2.ResolveNativeAppRoles(ctx, id, 1)
	require.ErrorIs(t, err, unsv2.ErrNativeAppAccess)
}

// Exercise the shared App-role directory and atomic per-member/group assignment.
func TestBusinessRoleAssignmentScopesAndConflicts(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, AuthType: "session", ResourceKeys: map[string]struct{}{"uns.app.manage": {}}})
	governance := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, AuthType: "session", ResourceKeys: map[string]struct{}{"iam.role.update": {}, "iam.role.view": {}}})
	empty, err := BusinessRoles(ctx, true)
	require.NoError(t, err)
	raw, err := json.Marshal(empty)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"roles":[]`)
	require.Contains(t, string(raw), `"groups":[]`)
	apiKey := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, AuthType: "apikey", ResourceKeys: map[string]struct{}{"uns.app.manage": {}}})
	require.Error(t, ReplaceBusinessRoleAssignments(apiKey, "user", "1", nil, nil))
	require.NoError(t, store.DB().Exec("INSERT INTO sys_user_info(user_id,user_name,password,status) VALUES (11,'role-member','unused-test-hash',1)").Error)
	require.NoError(t, store.DB().Exec("INSERT INTO sys_workspace_user(id,workspace_id,user_id,role_id,role_code) VALUES(101,1,11,2,'operator')").Error)
	require.NoError(t, store.DB().Exec("INSERT INTO sys_user_group(id,group_name) VALUES(32,'Quality')").Error)
	require.NoError(t, store.DB().Exec("INSERT INTO sys_user_group_member(user_id,group_id) VALUES(1,32),(11,32)").Error)
	for _, key := range []string{"planner", "quality"} {
		require.NoError(t, SaveBusinessRole(ctx, repo.BusinessRole{Key: key, Name: key}))
	}
	_, err = BusinessRoles(governance, true)
	require.Error(t, err)
	require.Error(t, ReplaceBusinessRoleAssignments(governance, "user", "1", []string{"quality"}, nil))
	require.NoError(t, ReplaceBusinessRoleAssignments(ctx, "group", "32", []string{"quality"}, nil))
	require.NoError(t, ReplaceBusinessRoleAssignments(ctx, "user", "1", []string{"quality", "planner"}, nil))
	require.NoError(t, ReplaceBusinessRoleAssignments(ctx, "user", "11", []string{"planner"}, nil))
	require.ErrorIs(t, ReplaceBusinessRoleAssignments(ctx, "user", "1", []string{"planner"}, nil), repo.ErrConflict)
	require.ErrorIs(t, ReplaceBusinessRoleAssignments(ctx, "user", "1", []string{"unknown"}, []string{"quality", "planner"}), repo.ErrInvalidArgument)
	roles, err := repo.UserBusinessRoles(ctx, 1)
	require.NoError(t, err)
	require.Len(t, roles, 2)
	require.NoError(t, ReplaceBusinessRoleAssignments(ctx, "user", "1", []string{}, []string{"quality", "planner"}))
	roles, err = repo.UserBusinessRoles(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"quality": "quality"}, roles)
	other, err := repo.UserBusinessRoles(ctx, 11)
	require.NoError(t, err)
	require.Len(t, other, 2)
	directory, err := BusinessRoles(ctx, true)
	require.NoError(t, err)
	snapshot := directory.(map[string]any)
	groups := snapshot["groups"].([]repo.BusinessRoleOption)
	require.Len(t, groups, 1)
	require.ElementsMatch(t, []string{"1", "11"}, groups[0].MemberIDs)
	for _, role := range snapshot["roles"].([]repo.BusinessRole) {
		require.True(t, role.CanDelete)
	}
	require.NoError(t, ReplaceBusinessRoleAssignments(ctx, "group", "32", []string{}, []string{"quality"}))
	roles, err = repo.UserBusinessRoles(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, roles)
	other, err = repo.UserBusinessRoles(ctx, 11)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"planner": "planner"}, other)
	require.ErrorIs(t, ReplaceBusinessRoleAssignments(ctx, "group", "missing", nil, nil), repo.ErrNotFound)
	require.Error(t, ReplaceBusinessRoleAssignments(ctx, "arbitrary", "1", nil, nil))
	require.ErrorIs(t, ReplaceBusinessRoleAssignments(ctx, "user", "11", []string{"quality", "quality"}, []string{"planner"}), repo.ErrInvalidArgument)
	require.NoError(t, store.DB().Exec("UPDATE sys_user_info SET status=0 WHERE user_id=11").Error)
	require.ErrorIs(t, ReplaceBusinessRoleAssignments(ctx, "user", "11", nil, []string{"planner"}), repo.ErrNotFound)
}

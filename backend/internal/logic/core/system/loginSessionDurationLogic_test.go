package system

// 测试目标:登录有效期设置的 GET/PUT logic 与权限判定(真实 PG 库,复用 schema 迁移)。
// 覆盖的正常路径:从未写入时 GET 返回默认 12h;PUT 保存后 GET 读回同值;admin 角色经 seed 放行 GET/PUT。
// 覆盖的异常情况:PUT 非法枚举值返回 400 且不落库;operator 角色无该资源,GET/PUT 均拒绝。

import (
	"strings"
	"testing"

	"backend/internal/contextx"
	backendpermission "backend/internal/permission"
	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestLoginSessionDurationGet_DefaultWhenMissing(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewLoginSessionDurationGetLogic(ctx, svcCtx)
	resp, err := l.LoginSessionDurationGet()
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "12h", data["duration"])
}

func TestLoginSessionDurationPut_SaveAndReadBack(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	put := NewLoginSessionDurationPutLogic(ctx, svcCtx)
	resp, err := put.LoginSessionDurationPut(&types.SystemLoginSessionDurationReq{Duration: "30d"})
	require.NoError(t, err)
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "30d", data["duration"])

	get := NewLoginSessionDurationGetLogic(ctx, svcCtx)
	readBack, err := get.LoginSessionDurationGet()
	require.NoError(t, err)
	readData, ok := readBack.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "30d", readData["duration"])
}

func TestLoginSessionDurationPut_InvalidDuration(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewLoginSessionDurationPutLogic(ctx, svcCtx)
	_, err := l.LoginSessionDurationPut(&types.SystemLoginSessionDurationReq{Duration: "1w"})
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "invalid login session duration"), "unexpected error: %v", err)

	// 非法值不应落库:GET 仍返回默认 12h
	get := NewLoginSessionDurationGetLogic(ctx, svcCtx)
	readBack, getErr := get.LoginSessionDurationGet()
	require.NoError(t, getErr)
	readData, ok := readBack.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "12h", readData["duration"])
}

func TestLoginSessionDurationPermission(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	keys, err := repo.NewIAMRepo(ctx).ResourceKeysForUser(ctx, 1)
	require.NoError(t, err)
	require.Contains(t, keys, "system.session.manage", "admin 角色应通过 seed 获得该资源")

	adminSubject := contextx.Subject{
		UserID:       1,
		UserName:     "tier0",
		AuthType:     "session",
		ResourceKeys: toKeySet(keys),
	}
	for _, method := range []string{"GET", "PUT"} {
		allowed, matchedKey, allowErr := backendpermission.New(ctx).Allow(
			ctx, adminSubject, "api", method, "/api/core/system/login-session-duration")
		require.NoError(t, allowErr)
		require.Truef(t, allowed, "%s 应放行 admin(matched=%s)", method, matchedKey)
	}

	// operator 角色不持有该资源,GET/PUT 都拒绝
	db := svcCtx.App.Store.CommonDB()
	now := db.NowFunc()
	require.NoError(t, db.Exec(`INSERT INTO sys_user_info
		(user_id, user_name, nick_name, password, status, created_by, updated_by, created_time, updated_time)
		VALUES (1000000001, 'ops-user', 'Ops', 'x', 1, 1, 1, ?, ?)`, now, now).Error)
	require.NoError(t, db.Exec(`INSERT INTO sys_workspace_user
		(workspace_id, user_id, role_code, role_id, created_by, updated_by, created_time, updated_time)
		VALUES (1, 1000000001, 'operator', 2, 1, 1, ?, ?)`, now, now).Error)

	operatorKeys, err := repo.NewIAMRepo(ctx).ResourceKeysForUser(svctest.CtxWithUser(1000000001), 1000000001)
	require.NoError(t, err)
	require.NotContains(t, operatorKeys, "system.session.manage")

	operatorSubject := contextx.Subject{
		UserID:       1000000001,
		UserName:     "ops-user",
		AuthType:     "session",
		ResourceKeys: toKeySet(operatorKeys),
	}
	for _, method := range []string{"GET", "PUT"} {
		allowed, _, allowErr := backendpermission.New(ctx).Allow(
			ctx, operatorSubject, "api", method, "/api/core/system/login-session-duration")
		require.NoError(t, allowErr)
		require.Falsef(t, allowed, "%s 不应放行 operator", method)
	}
}

func toKeySet(keys []string) map[string]struct{} {
	set := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		set[key] = struct{}{}
	}
	return set
}

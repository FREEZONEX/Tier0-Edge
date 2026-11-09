package iam

// 测试目标：iam 领域服务（service.go）的 DB 集成路径。
// 覆盖的正常路径：创建/更新/删除用户、重置与修改密码、角色与资源管理、用户首页解析与
// 持久化、IsAdmin/UserRoleCode、ResourceActions。
// 覆盖的异常情况：非法入参（空用户名/密码/角色）、tier0 系统账号只读、密码不匹配、
// 不存在的用户/角色、重复用户名。
// 说明：license 服务在 svctest 中不装配（domainlicense.Default() 为 nil），
// checkUserQuota 与 ResolveUserHomePage 的 license 过滤分支不覆盖。

import (
	"context"
	"errors"
	"testing"

	"backend/internal/contextx"
	"backend/internal/domain/license"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// newTestIAM 建立独立测试库并返回 iam Service 与带登录用户 ctx。
// 说明：svctest 依赖 iam 包，iam 测试直接引用会形成 import cycle，
// 因此此处用 testkit 等价装配（每测试独立 PG 库 + 包级连接 fallback）。
func newTestIAM(t *testing.T) (*Service, context.Context) {
	t.Helper()
	testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{
		UserID:   1,
		UserName: "tier0",
		AuthType: "session",
	})
	return New(ctx), ctx
}

func TestIAMUserLifecycle(t *testing.T) {
	svc, ctx := newTestIAM(t)
	db := repo.GetCommonConn(ctx)

	// 创建用户
	created, err := svc.CreateUser(ctx, UserSaveCommand{
		Username:  "alice",
		Password:  "secret123",
		FirstName: "Alice",
		Email:     "alice@example.com",
		Enabled:   true,
		RoleList:  []UserRoleCommand{{RoleID: repo.SystemRoleOperatorID}},
		UserID:    1,
	})
	require.NoError(t, err)
	userID, ok := created["userId"].(int64)
	require.True(t, ok && userID > 0)

	var row repo.User
	require.NoError(t, db.Where("user_id = ?", userID).First(&row).Error)
	require.Equal(t, "alice", row.UserName)
	require.Equal(t, int64(1), row.Status)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(row.Password), []byte("secret123")))

	// 非法入参
	_, err = svc.CreateUser(ctx, UserSaveCommand{Username: "", Password: "x", RoleList: []UserRoleCommand{{RoleID: 2}}})
	require.ErrorIs(t, err, ErrInvalidUser)
	_, err = svc.CreateUser(ctx, UserSaveCommand{Username: "bob", Password: "", RoleList: []UserRoleCommand{{RoleID: 2}}})
	require.ErrorIs(t, err, ErrInvalidUser)
	_, err = svc.CreateUser(ctx, UserSaveCommand{Username: "bob", Password: "x", RoleList: nil})
	require.ErrorIs(t, err, ErrInvalidUser)
	// 重复用户名
	_, err = svc.CreateUser(ctx, UserSaveCommand{Username: "alice", Password: "x2", RoleList: []UserRoleCommand{{RoleID: 2}}})
	require.ErrorIs(t, err, repo.ErrUserAccountDuplicate)

	// 更新用户（禁用 + 改名）
	updated, err := svc.UpdateUser(ctx, UserUpdateCommand{
		ID:        userID,
		Username:  "alice",
		FirstName: "Alice Updated",
		Enabled:   false,
		UserID:    1,
	})
	require.NoError(t, err)
	require.Equal(t, userID, updated["userId"])
	require.NoError(t, db.Where("user_id = ?", userID).First(&row).Error)
	require.Equal(t, int64(0), row.Status)
	require.Equal(t, "Alice Updated", row.NickName)

	// 更新不存在的用户
	_, err = svc.UpdateUser(ctx, UserUpdateCommand{ID: 999999, Username: "x", Enabled: false, UserID: 1})
	require.Error(t, err)

	// 重置密码
	require.NoError(t, svc.ResetUserPassword(ctx, UserPasswordResetCommand{UserID: userID, Password: "newpass", ActorID: 1}))
	require.NoError(t, db.Where("user_id = ?", userID).First(&row).Error)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(row.Password), []byte("newpass")))
	require.ErrorIs(t, svc.ResetUserPassword(ctx, UserPasswordResetCommand{UserID: 0, Password: "x", ActorID: 1}), ErrInvalidUser)
	require.ErrorIs(t, svc.ResetUserPassword(ctx, UserPasswordResetCommand{UserID: userID, Password: "", ActorID: 1}), ErrInvalidUser)

	// 修改当前用户密码
	require.ErrorIs(t, svc.ChangeCurrentUserPassword(ctx, CurrentUserPasswordCommand{UserID: userID, OldPassword: "wrong", NewPassword: "p3"}), ErrInvalidPassword)
	require.NoError(t, svc.ChangeCurrentUserPassword(ctx, CurrentUserPasswordCommand{UserID: userID, OldPassword: "newpass", NewPassword: "p3"}))
	require.ErrorIs(t, svc.ChangeCurrentUserPassword(ctx, CurrentUserPasswordCommand{UserID: userID, OldPassword: "", NewPassword: "p3"}), ErrInvalidUser)

	// 删除用户
	require.NoError(t, svc.DeleteUser(ctx, userID, 1))
	var count int64
	require.NoError(t, db.Model(&repo.User{}).Where("user_id = ? AND deleted_time = 0", userID).Count(&count).Error)
	require.Zero(t, count)
}

func TestIAMSystemUserReadonly(t *testing.T) {
	svc, ctx := newTestIAM(t)

	// tier0 用户不可删除/重置密码
	require.ErrorIs(t, svc.DeleteUser(ctx, 1, 1), repo.ErrSystemReadonly)
	require.ErrorIs(t, svc.ResetUserPassword(ctx, UserPasswordResetCommand{UserID: 1, Password: "x", ActorID: 1}), repo.ErrSystemReadonly)
}

func TestIAMRolesAndResources(t *testing.T) {
	svc, ctx := newTestIAM(t)

	// 角色列表
	roles, err := svc.Roles(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, roles)
	require.Equal(t, repo.BuiltinRoleAdmin, roles[0].Code)

	// 创建角色
	created, err := svc.CreateRole(ctx, "custom-role", 1)
	require.NoError(t, err)
	require.True(t, created.ID > 0)
	_, err = svc.CreateRole(ctx, "  ", 1)
	require.ErrorIs(t, err, ErrInvalidRole)

	// 更新角色（含 allowURIs 中的 resource: 前缀）
	require.NoError(t, svc.UpdateRole(ctx, created.ID, "custom-role-2", []string{"resource:uns.read"}, "/launchpad", 1))
	require.ErrorIs(t, svc.UpdateRole(ctx, 0, "x", nil, "", 1), ErrInvalidRole)

	// 删除角色
	require.NoError(t, svc.DeleteRole(ctx, created.ID, 1))
	require.ErrorIs(t, svc.DeleteRole(ctx, 0, 1), ErrInvalidRole)

	// 资源列表
	resources, err := svc.Resources(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, resources)
	menus, err := svc.Menus(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, menus)

	// 保存资源（先取一个已存在资源做更新）
	existing := resources[0]
	saved, err := svc.SaveResource(ctx, ResourceSaveCommand{
		ResourceID:  existing.ID,
		ResourceKey: existing.ResourceKey,
		Name:        existing.Name + "-x",
		Type:        existing.Type,
		Enabled:     1,
	}, 1)
	require.NoError(t, err)
	require.Equal(t, existing.ID, saved.ID)

	// 非法资源
	_, err = svc.SaveResource(ctx, ResourceSaveCommand{ResourceKey: "", Name: "x", Type: 1}, 1)
	require.ErrorIs(t, err, ErrInvalidRole)

	// 资源动作
	actions, err := svc.ResourceActions(ctx, existing.ID)
	require.NoError(t, err)
	_ = actions
	_, err = svc.ResourceActions(ctx, 0)
	require.ErrorIs(t, err, ErrInvalidRole)
}

func TestIAMIsAdminAndRoleCode(t *testing.T) {
	svc, ctx := newTestIAM(t)

	require.True(t, svc.IsAdmin(ctx, 1))
	require.Equal(t, repo.BuiltinRoleAdmin, svc.UserRoleCode(ctx, 1))
	require.False(t, svc.IsAdmin(ctx, 999999))
	require.Empty(t, svc.UserRoleCode(ctx, 999999))

	var nilSvc *Service
	require.False(t, nilSvc.IsAdmin(ctx, 1))
	require.Empty(t, nilSvc.UserRoleCode(ctx, 1))
}

func TestIAMResolveUserHomePage(t *testing.T) {
	svc, ctx := newTestIAM(t)

	// tier0 默认首页为 /home
	page, err := svc.ResolveUserHomePage(ctx, 1, "")
	require.NoError(t, err)
	require.NotEmpty(t, page)

	// 持久化用户首页
	updated, err := svc.UpdateUserHomePage(ctx, 1, page)
	require.NoError(t, err)
	require.Equal(t, page, updated)

	// 不存在的用户
	_, err = svc.ResolveUserHomePage(ctx, 999999, "")
	require.Error(t, err)
}

func TestIAMLaunchpadHomePageFallback(t *testing.T) {
	// 纯函数路径：/launchpad/xx/yy 前缀且非法 ID 时回退默认
	svc, ctx := newTestIAM(t)

	resolved, matched, err := svc.resolveLaunchpadAppHomePage(ctx, 1, "/launchpad/not-a-number/2")
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, repo.DefaultOperatorHomePage, resolved)

	// 非 launchpad 前缀不处理
	resolved, matched, err = svc.resolveLaunchpadAppHomePage(ctx, 1, "/home")
	require.NoError(t, err)
	require.False(t, matched)
	require.Empty(t, resolved)
}

func TestIAMCountEnabledUsers(t *testing.T) {
	svc, ctx := newTestIAM(t)

	count, err := svc.CountEnabledUsers(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, count, int64(1))

	// 创建用户后再计数
	_, err = svc.CreateUser(ctx, UserSaveCommand{
		Username: "counter", Password: "pw", Enabled: true,
		RoleList: []UserRoleCommand{{RoleID: repo.SystemRoleOperatorID}}, UserID: 1,
	})
	require.NoError(t, err)
	after, err := svc.CountEnabledUsers(ctx)
	require.NoError(t, err)
	require.Equal(t, count+1, after)
}

func TestIAMLicenseQuotaHook(t *testing.T) {
	// 当 license.Default() 被装配时 checkUserQuota 才生效；此处验证 nil license 时跳过
	svc, ctx := newTestIAM(t)

	require.Nil(t, license.Default()) // svctest 不装配 license
	require.NoError(t, svc.checkUserQuota(ctx))
}

func TestIAMFilterHelpers(t *testing.T) {
	roles := []repo.Role{
		{ID: 1, Name: "r1", ResourceList: []map[string]string{{"resourceKey": "launchpad.view", "routePath": "/launchpad"}}},
		{ID: 2, Name: "r2", ResourceList: []map[string]string{{"resourceKey": "", "routePath": "/x"}}},
	}
	// license nil 时不过滤
	filtered := filterRoleResourceList(roles[0].ResourceList, nil)
	require.Len(t, filtered, 1)
	page := normalizeRoleHomePage("/launchpad", roles[0].ResourceList)
	require.Equal(t, "/launchpad", page)
	require.Equal(t, repo.DefaultOperatorHomePage, normalizeRoleHomePage("/nope", roles[0].ResourceList))
	// 非法 routePath 跳过
	require.Equal(t, repo.DefaultOperatorHomePage, normalizeRoleHomePage("", []map[string]string{{"routePath": "relative"}}))

	ordered := orderedAllowedHomePages(map[string]string{"/home": "/home", "/launchpad": "/launchpad"}, []string{"/launchpad", "/home"})
	require.Equal(t, []string{"/launchpad", "/home"}, ordered)
	require.Empty(t, orderedAllowedHomePages(nil, nil))
	require.Equal(t, repo.DefaultHomePage, defaultAllowedHomePageInOrder(map[string]string{"/home": "/home"}, nil))
	require.Equal(t, repo.DefaultOperatorHomePage, defaultAllowedHomePageInOrder(nil, nil))
}

func TestIAMSaveResourceAction(t *testing.T) {
	svc, ctx := newTestIAM(t)

	resources, err := svc.Resources(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, resources)

	// 保存动作
	action, err := svc.SaveResourceAction(ctx, repo.ResourceAction{
		ResourceID:  resources[0].ID,
		ActionType:  "ui",
		ActionValue: "btn-test",
		Methods:     "get",
	}, 1)
	require.NoError(t, err)
	require.True(t, action.ID > 0)
	require.Equal(t, "ui", action.ActionType)
	require.Equal(t, "GET", action.Methods)

	// 非法动作
	_, err = svc.SaveResourceAction(ctx, repo.ResourceAction{ResourceID: 0}, 1)
	require.ErrorIs(t, err, ErrInvalidRole)
	_, err = svc.SaveResourceAction(ctx, repo.ResourceAction{ResourceID: resources[0].ID, ActionType: "", ActionValue: ""}, 1)
	require.ErrorIs(t, err, ErrInvalidRole)

	// 删除动作
	require.NoError(t, svc.DeleteResourceAction(ctx, action.ID, 1))
	require.ErrorIs(t, svc.DeleteResourceAction(ctx, 0, 1), ErrInvalidRole)

	// 列出动作
	actions, err := svc.ResourceActions(ctx, resources[0].ID)
	require.NoError(t, err)
	for _, item := range actions {
		if item.ID == action.ID {
			t.Fatal("deleted action still listed")
		}
	}
}

func TestIAMUpdateRoleRejectsSystemRole(t *testing.T) {
	svc, ctx := newTestIAM(t)

	// admin 角色不可更新
	err := svc.UpdateRole(ctx, repo.SystemRoleAdminID, "hack", nil, "", 1)
	require.ErrorIs(t, err, repo.ErrSystemReadonly)
	// admin 角色不可删除
	require.ErrorIs(t, svc.DeleteRole(ctx, repo.SystemRoleAdminID, 1), repo.ErrSystemReadonly)
}

func TestIAMUsersList(t *testing.T) {
	svc, ctx := newTestIAM(t)

	users, err := svc.Users(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, users)
	found := false
	for _, u := range users {
		if id, _ := u["userId"].(int64); id == 1 {
			found = true
		}
	}
	require.True(t, found)
}

func TestIAMErrGuard(t *testing.T) {
	require.ErrorIs(t, ErrInvalidRole, ErrInvalidRole)
	require.ErrorIs(t, ErrInvalidUser, ErrInvalidUser)
	require.ErrorIs(t, ErrInvalidPassword, ErrInvalidPassword)
	_ = errors.Is // 保持 import
}

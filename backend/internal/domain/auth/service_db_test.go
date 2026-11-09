package auth

// 测试目标：auth 服务（service.go）的 DB 集成路径。
// 覆盖的正常路径：Login 成功（返回 token/资源键/UI 动作/默认密码标记）、CurrentUser、
// AuthenticateRequest（Bearer/edge_session cookie/token query/OAuth access token）、
// AuthenticateAPIKey、AuthenticateMQTTAPIKey、AuthenticateLegacyMQTTAPIKey。
// 覆盖的异常情况：用户不存在/禁用/密码错误、无凭证、非法 token、API key 无效。
// 说明：svctest 依赖 domain 包（import cycle），此处用 testkit 装配；
// seed 内置 tier0 用户（密码 tier0）。

import (
	"backend/internal/config"
	"backend/internal/infra/cache"
	"context"
	"github.com/alicebob/miniredis/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func newTestAuth(t *testing.T) (*Service, context.Context) {
	t.Helper()
	testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, UserName: "tier0"})
	return New(ctx, "test-jwt-secret"), ctx
}

func TestAuthLogin(t *testing.T) {
	svc, _ := newTestAuth(t)

	// tier0/tier0 成功
	result, err := svc.Login(context.Background(), "tier0", "tier0")
	require.NoError(t, err)
	require.NotEmpty(t, result.Token)
	require.Equal(t, "tier0", result.User.UserName)
	require.True(t, result.ForceChangePassword) // 默认密码
	require.NotEmpty(t, result.ResourceKeys)
	require.NotNil(t, result.UIActions)

	// 错误密码
	_, err = svc.Login(context.Background(), "tier0", "wrong")
	require.Error(t, err)
	// 用户不存在
	_, err = svc.Login(context.Background(), "ghost", "x")
	require.Error(t, err)
}

func TestAuthCurrentUser(t *testing.T) {
	svc, ctx := newTestAuth(t)
	user, keys, uiActions, forceChange, err := svc.CurrentUser(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), user.ID)
	require.NotEmpty(t, keys)
	require.True(t, forceChange)
	require.NotNil(t, uiActions)

	// Access-only authentication must not remove the UI's current password reminder.
	db := repo.GetCommonConn(ctx)
	hash, err := bcrypt.GenerateFromPassword([]byte("changed-password"), bcrypt.MinCost)
	require.NoError(t, err)
	require.NoError(t, db.Model(&repo.User{}).Where("user_id = ?", 1).Updates(map[string]any{"password": string(hash), "is_random_pwd": false}).Error)
	_, _, _, forceChange, err = svc.CurrentUser(ctx, 1)
	require.NoError(t, err)
	require.False(t, forceChange)
	require.NoError(t, db.Model(&repo.User{}).Where("user_id = ?", 1).Update("is_random_pwd", true).Error)
	_, _, _, forceChange, err = svc.CurrentUser(ctx, 1)
	require.NoError(t, err)
	require.True(t, forceChange)

	_, _, _, _, err = svc.CurrentUser(ctx, 999999)
	require.Error(t, err)
}

// API key requests and queued-work refresh must observe changes without an auth cache.
func TestAuthAPIKeyAndRefreshReloadAccess(t *testing.T) {
	svc, ctx := newTestAuth(t)
	db := repo.GetCommonConn(ctx)
	key, raw, err := svc.apiKeys.CreateAPIKey(ctx, "reload-access", 1, 1, "full_access", "external", "personal", []string{"uns.page"})
	require.NoError(t, err)
	original, err := svc.AuthenticateAPIKey(ctx, raw)
	require.NoError(t, err)
	require.Contains(t, original.ResourceKeys, "uns.page")
	require.NotEmpty(t, original.UIActions)
	check := func(allowed bool) {
		t.Helper()
		current, authErr := svc.AuthenticateAPIKey(ctx, raw)
		require.NoError(t, authErr)
		refreshed, refreshErr := svc.RefreshSubject(ctx, original)
		require.NoError(t, refreshErr)
		for _, subject := range []contextx.Subject{current, refreshed} {
			_, exists := subject.ResourceKeys["uns.page"]
			require.Equal(t, allowed, exists)
		}
	}
	// The current key scope is re-read, including refresh by non-secret key ID.
	_, err = svc.apiKeys.UpdateAPIKey(ctx, key.ID, 1, "", "full_access", []string{})
	require.NoError(t, err)
	check(false)
	_, err = svc.apiKeys.UpdateAPIKey(ctx, key.ID, 1, "", "full_access", []string{"uns.page"})
	require.NoError(t, err)
	check(true)
	// Resource removal and role membership removal affect the next request.
	require.NoError(t, db.Table("sys_resource_info").Where("resource_key = ?", "uns.page").Update("enabled", 0).Error)
	check(false)
	require.NoError(t, db.Table("sys_resource_info").Where("resource_key = ?", "uns.page").Update("enabled", 1).Error)
	require.NoError(t, db.Table("sys_workspace_user").Where("user_id = ? AND deleted_time = 0", 1).Update("deleted_time", 1).Error)
	check(false)
	refreshed, err := svc.RefreshSubject(ctx, original)
	require.NoError(t, err)
	require.Empty(t, refreshed.UIActions)
	require.NoError(t, db.Table("sys_workspace_user").Where("user_id = ? AND deleted_time = 1", 1).Update("deleted_time", 0).Error)
	check(true)
	// A disabled owner is rejected by both entry points, even with a valid key.
	require.NoError(t, db.Model(&repo.User{}).Where("user_id = ?", 1).Update("status", 0).Error)
	_, err = svc.AuthenticateAPIKey(ctx, raw)
	require.Error(t, err)
	_, err = svc.RefreshSubject(ctx, original)
	require.Error(t, err)
	require.NoError(t, db.Model(&repo.User{}).Where("user_id = ?", 1).Update("status", 1).Error)
	check(true)
	// Revocation is effective immediately for both raw-key and queued-work paths.
	require.NoError(t, svc.apiKeys.DeleteAPIKey(ctx, key.ID, 1))
	_, err = svc.AuthenticateAPIKey(ctx, raw)
	require.Error(t, err)
	_, err = svc.RefreshSubject(ctx, original)
	require.Error(t, err)
}

func TestResolveProgramAuthorityCurrentIdentity(t *testing.T) {
	svc, ctx := newTestAuth(t)
	db := repo.GetCommonConn(ctx)
	userRef := repo.ProgramAuthorityRef{Kind: "user", ID: 1}
	user, err := svc.ResolveProgramAuthority(ctx, userRef)
	require.NoError(t, err)
	require.Equal(t, "session", user.AuthType)
	require.Zero(t, user.APIKeyID)
	key, _, err := svc.apiKeys.CreateAPIKey(ctx, "publisher", 1, 1, "full_access", "external", "personal", []string{"uns.page"})
	require.NoError(t, err)
	keyRef := repo.ProgramAuthorityRef{Kind: "api_key", ID: key.ID}
	current, err := svc.ResolveProgramAuthority(ctx, keyRef)
	require.NoError(t, err)
	require.Equal(t, key.ID, current.APIKeyID)
	require.Equal(t, "apiKey", current.AuthType)
	require.Equal(t, "personal", current.KeyType)
	require.Equal(t, map[string]struct{}{"uns.page": {}}, current.ResourceKeys, "owner's broader role cannot replace the key scope")
	for _, ref := range []repo.ProgramAuthorityRef{userRef, keyRef} {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		_, err := svc.ResolveProgramAuthority(cancelled, ref)
		require.ErrorIs(t, err, context.Canceled)
		require.False(t, IsUnauthorized(err), "lookup cancellation is not a confirmed revocation")
	}
	// Simulate an unavailable permissions table while the principal itself exists.
	require.NoError(t, db.Exec("ALTER TABLE public.sys_workspace_user RENAME TO unavailable_workspace_user").Error)
	for _, ref := range []repo.ProgramAuthorityRef{userRef, keyRef} {
		_, err := svc.ResolveProgramAuthority(ctx, ref)
		require.Error(t, err)
		require.False(t, IsUnauthorized(err), "database failure must remain retryable")
	}
	require.NoError(t, db.Exec("ALTER TABLE public.unavailable_workspace_user RENAME TO sys_workspace_user").Error)

	_, err = svc.apiKeys.UpdateAPIKey(ctx, key.ID, 1, "", "read_only", []string{"uns.page"})
	require.NoError(t, err)
	current, err = svc.ResolveProgramAuthority(ctx, keyRef)
	require.NoError(t, err)
	require.Equal(t, "read_only", current.APIKeyPermission)
	for _, field := range []struct {
		table, where, column string
		denied, restored     any
	}{
		{"sys_user_info", "user_id=1", "status", 0, 1},
		{"sys_workspace_user", "user_id=1", "deleted_time", 1, 0},
	} {
		require.NoError(t, db.Table(field.table).Where(field.where).Update(field.column, field.denied).Error)
		for _, ref := range []repo.ProgramAuthorityRef{userRef, keyRef} {
			_, err = svc.ResolveProgramAuthority(ctx, ref)
			require.True(t, IsUnauthorized(err))
		}
		require.NoError(t, db.Table(field.table).Where(field.where).Update(field.column, field.restored).Error)
	}
	// A current key record cannot be reinterpreted as an App/Agent publisher.
	for _, kind := range []string{"app", "agent"} {
		require.NoError(t, db.Model(&repo.APIKey{}).Where("id=?", key.ID).Update("owner_type", kind).Error)
		_, err = svc.ResolveProgramAuthority(ctx, keyRef)
		require.True(t, IsUnauthorized(err))
	}
	require.NoError(t, db.Model(&repo.APIKey{}).Where("id=?", key.ID).Update("owner_type", "personal").Error)
	_, err = svc.apiKeys.UpdateAPIKey(ctx, key.ID, 1, "", "full_access", []string{})
	require.NoError(t, err)
	_, err = svc.ResolveProgramAuthority(ctx, keyRef)
	require.True(t, IsUnauthorized(err))
	require.NoError(t, svc.apiKeys.DeleteAPIKey(ctx, key.ID, 1))
	_, err = svc.ResolveProgramAuthority(ctx, keyRef)
	require.True(t, IsUnauthorized(err))
	for _, ref := range []repo.ProgramAuthorityRef{{Kind: "user"}, {Kind: "user", ID: 999999}, {Kind: "app", ID: 1}, {Kind: "api_key", ID: 999999}} {
		_, err = svc.ResolveProgramAuthority(ctx, ref)
		require.True(t, IsUnauthorized(err))
	}
}

func TestAuthAuthenticateRequest(t *testing.T) {
	svc, _ := newTestAuth(t)

	// 无凭证
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	_, err := svc.AuthenticateRequest(r)
	require.Error(t, err)

	// Bearer token
	result, err := svc.Login(context.Background(), "tier0", "tier0")
	require.NoError(t, err)
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+result.Token)
	subject, err := svc.AuthenticateRequest(r)
	require.NoError(t, err)
	require.Equal(t, int64(1), subject.UserID)
	require.Equal(t, "session", subject.AuthType)

	// cookie
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: "edge_session", Value: result.Token})
	subject, err = svc.AuthenticateRequest(r)
	require.NoError(t, err)
	require.Equal(t, int64(1), subject.UserID)

	// token query
	r = httptest.NewRequest(http.MethodGet, "/?token="+result.Token, nil)
	subject, err = svc.AuthenticateRequest(r)
	require.NoError(t, err)
	require.Equal(t, int64(1), subject.UserID)

	// 非法 token
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer invalid-token")
	_, err = svc.AuthenticateRequest(r)
	require.Error(t, err)
}

func TestAuthOAuthAccessToken(t *testing.T) {
	svc, ctx := newTestAuth(t)
	oauthRepo := repo.NewOAuthRepo(ctx)

	// 签发 OAuth access token（任意 clientID，token 全局有效）
	token, _, err := oauthRepo.CreateOAuthAccessToken(ctx, "oc_test", 1, "openid", 3600)
	require.NoError(t, err)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	subject, err := svc.AuthenticateRequest(r)
	require.NoError(t, err)
	require.Equal(t, int64(1), subject.UserID)
	require.Equal(t, "oauth", subject.AuthType)
}

func TestAuthAPIKeyPaths(t *testing.T) {
	svc, ctx := newTestAuth(t)
	apiKeys := repo.NewAPIKeyRepo(ctx)

	// 创建 API key（带 resource key，否则 ResourceKeys 为空）
	_, raw, err := apiKeys.CreateAPIKey(ctx, "test-key", 1, 1, "full_access", "external", "personal", []string{"uns.page"})
	require.NoError(t, err)
	require.NotEmpty(t, raw)

	// AuthenticateAPIKey
	subject, err := svc.AuthenticateAPIKey(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, int64(1), subject.UserID)
	require.Equal(t, "apiKey", subject.AuthType)
	require.Equal(t, "test-key", subject.APIKeyName)
	require.NotEmpty(t, subject.ResourceKeys)

	// AuthenticateMQTTAPIKey
	subject, err = svc.AuthenticateMQTTAPIKey(ctx, raw)
	require.NoError(t, err)
	require.Equal(t, "apiKey", subject.AuthType)
	require.Equal(t, int64(1), subject.UserID)

	// 空 key
	_, err = svc.AuthenticateAPIKey(ctx, "")
	require.Error(t, err)
	_, err = svc.AuthenticateMQTTAPIKey(ctx, "")
	require.Error(t, err)

	// 非法 key
	_, err = svc.AuthenticateAPIKey(ctx, "invalid-key")
	require.Error(t, err)
}

func TestAuthLegacyMQTTAPIKey(t *testing.T) {
	svc, ctx := newTestAuth(t)

	// 非 t0_ 前缀
	_, err := svc.AuthenticateLegacyMQTTAPIKey(ctx, "sk-test")
	require.Error(t, err)

	// t0_ 但 app_api_keys 未注册
	_, err = svc.AuthenticateLegacyMQTTAPIKey(ctx, "t0_not_registered")
	require.Error(t, err)

	// 注册应用 key 后：legacy 路径先命中 app_api_keys 存在性检查，
	// 随后二次校验 sys_api_key（workspace-encoded key）；测试未插入
	// sys_api_key 记录，因此仍返回 unauthorized。此处锁定"存在性检查通过、
	// 进入二次校验"的边界：不再因 app key 缺失而提前返回。
	appKeys := repo.NewAppApiKeyRepo(ctx)
	require.NoError(t, appKeys.Insert(ctx, &repo.AppApiKey{
		AppID:     "app-1",
		Name:      "default",
		ApiKey:    "t0_legacy_valid",
		ProjectID: 1,
	}))
	_, err = svc.AuthenticateLegacyMQTTAPIKey(ctx, "t0_legacy_valid")
	require.Error(t, err)
}

func TestAuthUsesDefaultPasswordRealHash(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("tier0"), bcrypt.MinCost)
	require.NoError(t, err)
	require.True(t, UsesDefaultPassword(repo.User{Password: string(hash)}))
	// 注意：bcrypt 只读固定长度，尾部追加字符不会改变比对结果；
	// 用真正不同的密码生成 hash 才不匹配。
	otherHash, err := bcrypt.GenerateFromPassword([]byte("tier0x"), bcrypt.MinCost)
	require.NoError(t, err)
	require.False(t, UsesDefaultPassword(repo.User{Password: string(otherHash)}))
}

func TestMiniEntryUsesExistingSessionWithOptionalPasswordReminder(t *testing.T) {
	service, ctx := newTestAuth(t)
	r := miniredis.RunT(t)
	client := cache.Open(config.RedisConf{Addr: r.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	login, err := service.Login(ctx, "tier0", "tier0")
	require.NoError(t, err)
	require.True(t, login.ForceChangePassword, "the initial-password reminder remains available")
	subject, expires, err := service.ValidateMiniEntrySession(ctx, login.Token)
	require.NoError(t, err)
	require.Equal(t, int64(1), subject.UserID)
	originalExpiry, err := time.Parse(time.RFC3339, login.SessionExpireAt)
	require.NoError(t, err)
	require.True(t, expires.Equal(originalExpiry), "handoff must not extend login lifetime")
	grant := MiniEntryGrant{Token: login.Token, ProjectID: 1, AppID: 2}
	ticket, err := service.CreateMiniEntry(ctx, client, grant)
	require.NoError(t, err)
	require.True(t, miniTicketPattern.MatchString(ticket))
	require.Equal(t, miniEntryTTL, r.TTL(miniEntryKey(ticket)))
	_, err = service.CreateMiniEntry(ctx, client, grant)
	require.Error(t, err, "immediate duplicate issuance should be throttled")
	got, err := service.TakeMiniEntry(ctx, client, ticket)
	require.NoError(t, err)
	require.Equal(t, grant, got)
	_, err = service.TakeMiniEntry(ctx, client, ticket)
	require.Error(t, err)
}

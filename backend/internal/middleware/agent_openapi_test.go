package middleware

// 真实种子与 API Key 验证：已有 Key 不获得新管理能力，凭据/部署/执行独立授权。
import (
	"backend/internal/domain/auth"
	"backend/internal/permission"
	"backend/internal/repo"
	"backend/internal/testkit"
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentOpenAPIExplicitScopes(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	keys := repo.NewAPIKeyRepo(ctx)
	m := NewApiKeyAuthMiddleware(auth.New(ctx, "test-jwt-secret"), permission.New(ctx), nil)
	paths := map[string]string{
		"/sources/list": "uns.source", "/sources/create": "uns.source.manage", "/sources/validate": "uns.source.manage", "/sources/probe": "uns.source.manage",
		"/sources/42/credentials": "uns.source.credentials", "/sources/42/oauth/start": "uns.source.credentials", "/sources/42/oauth/complete": "uns.source.credentials",
		"/streams/list": "uns.stream", "/streams/test": "uns.stream.manage", "/streams/42/deploy": "uns.stream.deploy", "/streams/42/disable": "uns.stream.deploy", "/streams/42/invoke": "uns.stream.execute",
		"/topics/create": "uns.manage", "/topics/validate": "uns.manage", "/topics/42/detail": "uns.read",
	}
	check := func(key, path string, want int) {
		t.Helper()
		req := httptest.NewRequest("POST", "/openapi/v1/uns"+path, nil)
		req.Header.Set("X-API-Key", key)
		w := httptest.NewRecorder()
		m.Handle(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })(w, req)
		require.Equal(t, want, w.Code, path)
	}
	check("", "/sources/list", 401)
	for i, profile := range []string{"read_only", "data_writer", "full_access"} {
		_, key, err := keys.CreateAPIKey(ctx, fmt.Sprintf("default-%d", i), 1, 1, profile, "external", "personal", repo.APIKeyResourceKeysForPermission(profile))
		require.NoError(t, err)
		check(key, "/sources/create", 403)
		check(key, "/sources/42/credentials", 403)
		check(key, "/streams/42/deploy", 403)
	}
	n := 0
	for path, scope := range paths {
		n++
		_, key, err := keys.CreateAPIKey(ctx, fmt.Sprintf("explicit-%d", n), 1, 1, "read_only", "external", "personal", []string{scope})
		require.NoError(t, err)
		check(key, path, 204)
		if scope != "uns.source.credentials" {
			check(key, "/sources/42/credentials", 403)
		}
		if scope != "uns.stream.deploy" {
			check(key, "/streams/42/deploy", 403)
		}
	}
	// Role removal immediately narrows a previously issued key.
	_, key, err := keys.CreateAPIKey(ctx, "revoked-role", 1, 1, "full_access", "external", "personal", []string{"uns.stream.deploy"})
	require.NoError(t, err)
	check(key, "/streams/42/deploy", 204)
	require.NoError(t, repo.GetCommonConn(ctx).Exec("DELETE FROM sys_role_resource WHERE resource_id = ?", 3312).Error)
	check(key, "/streams/42/deploy", 403)
}

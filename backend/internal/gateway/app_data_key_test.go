package gateway

import (
	"context"
	"net/http/httptest"
	"testing"

	"backend/internal/domain/auth"
	"backend/internal/permission"
	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestAppDataKeyCannotUseGatewayProxy(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	_, raw, err := repo.NewAPIKeyRepo(ctx).CreateAppDataKey(ctx, "app-runtime", "maintenance", 1, 1, []string{"uns.read"})
	require.NoError(t, err)
	g := &HTTPGateway{auth: auth.New(ctx, "test"), permission: permission.New(ctx)}
	r := httptest.NewRequest("POST", "/custom-data-proxy", nil)
	r.Header.Set("X-API-Key", raw)
	w := httptest.NewRecorder()
	require.False(t, g.authorizeRoute(w, r, repo.GatewayRoute{AuthPolicy: "apikey", ResourceKey: "uns.read"}))
	require.Equal(t, 403, w.Code)
}

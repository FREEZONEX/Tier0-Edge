package gateway

import (
	"backend/internal/domain/auth"
	"backend/internal/permission"
	"backend/internal/repo"
	"backend/internal/testkit"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

type testAppPort int

func (p testAppPort) Resolve(string) (int, error) { return int(p), nil }

func TestNativeAppGatewayReadsLiveRoles(t *testing.T) {
	t.Setenv("TIER0_RUNTIME_MODE", "native")
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	authSvc := auth.New(ctx, "test gateway")
	login, err := authSvc.Login(ctx, "tier0", "tier0")
	require.NoError(t, err)
	id := uuid.NewString()
	app := map[string]any{"schemaVersion": 1, "name": "App", "entry": map[string]string{"basePath": "/apps/" + id, "path": "/"}, "roles": []any{}}
	raw, _ := json.Marshal(app)
	require.NoError(t, store.UnsDB().Create(&repo.UnsMeta{Kind: "app", ID: id, Data: raw, Revision: 1}).Error)
	received := make(chan http.Header, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received <- r.Header.Clone(); w.WriteHeader(200) }))
	defer upstream.Close()
	g := &HTTPGateway{auth: authSvc, permission: permission.New(ctx), nativeApps: testAppPort(upstream.Listener.Addr().(*net.TCPAddr).Port)}
	visit := func(signed bool) int {
		req := httptest.NewRequest("GET", "/apps/"+id+"/asset.js", nil)
		if signed {
			req.Header.Set("Authorization", "Bearer "+login.Token)
		}
		req.Header.Set("X-Tier0-Active-Role", "forged")
		req.Header.Set("X-App-User-Role", "forged")
		req.Header.Set("Connection", "X-Tier0-Business-Roles, X-Tier0-Active-Role")
		resp := httptest.NewRecorder()
		require.True(t, g.handleNativeApp(resp, req))
		return resp.Code
	}
	require.Equal(t, 401, visit(false))
	require.Equal(t, 200, visit(true))
	h := <-received
	require.Empty(t, h.Get("Authorization"))
	require.Empty(t, h.Get("X-App-User-Role"))
	require.Empty(t, h.Get("X-Tier0-Active-Role"))
	require.Equal(t, "deployed", h.Get("X-Tier0-Runtime"))
	require.NoError(t, store.UnsDB().Exec(`UPDATE uns_meta SET data=jsonb_set(data,'{roles}','[{"id":"quality","name":"Quality"}]') WHERE kind='app' AND id=?`, id).Error)
	require.Equal(t, 403, visit(true))
	require.NoError(t, store.UnsDB().Exec(`INSERT INTO workspace_business_role(role_key,name,user_ids) VALUES ('quality','Quality','["1"]')`).Error)
	require.Equal(t, 200, visit(true))
	h = <-received
	require.Equal(t, "quality", h.Get("X-Tier0-Active-Role"))
	require.Equal(t, "quality", h.Get("X-Tier0-Business-Roles"))
	require.NoError(t, store.UnsDB().Exec("UPDATE workspace_business_role SET user_ids='[]'").Error)
	require.Equal(t, 403, visit(true))
}

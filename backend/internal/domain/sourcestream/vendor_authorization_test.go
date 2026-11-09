package sourcestream

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestVendorNGPMerchantAuthorizationLifecycle(t *testing.T) {
	testkit.NewTestDB(t)
	var exchanges atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/oauth2/token", r.URL.Path)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "authorization_code", r.PostForm.Get("grant_type"))
		require.Equal(t, "app", r.PostForm.Get("app_key"))
		require.Equal(t, "secret", r.PostForm.Get("app_secret"))
		require.Equal(t, "granted", r.PostForm.Get("code"))
		require.Equal(t, "http://127.0.0.1:5178/uns", r.PostForm.Get("redirect_uri"))
		exchanges.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"merchant-access","refresh_token":"merchant-refresh","expires_in":3600}`))
	}))
	defer server.Close()
	s := New(nil, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	httpTestManager(t, s.httpManager, server)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.source": {}, "uns.source.credentials": {}}})
	d := SourceDefinition{Name: "NGP merchant", SourceType: "ngp", Protocol: HTTP, RuntimeLocation: "local", Endpoint: "http://fixture.internal/", HTTP: &HTTPConnection{Authentication: "custom", Parameters: map[string]any{"redirectUri": "http://127.0.0.1:5178/uns"}}}
	view, err := s.CreateSource(ctx, d, 0)
	require.NoError(t, err)
	id := view["id"].(int64)
	view, err = s.SaveCredential(ctx, id, view["revision"].(int64), Credential{Kind: "custom", Values: map[string]string{"appKey": "app", "appSecret": "secret"}}, "")
	require.NoError(t, err)
	view, err = s.Source(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "required", view["oauthStatus"])
	revision := view["revision"]
	_, err = s.OAuthStart(ctx, id, "https://unregistered.test/uns")
	require.Error(t, err)
	start, err := s.OAuthStart(ctx, id, "http://127.0.0.1:5178/uns")
	require.NoError(t, err)
	destination, err := url.Parse(start["authorizationUrl"].(string))
	require.NoError(t, err)
	require.Equal(t, "app", destination.Query().Get("app_key"))
	require.Empty(t, destination.Query().Get("app_secret"))
	require.Empty(t, destination.Query().Get("client_id"))
	other := contextx.WithSubject(ctx, contextx.Subject{UserID: 2, ResourceKeys: map[string]struct{}{"uns.source.credentials": {}}})
	_, err = s.OAuthComplete(other, id, start["state"].(string), "granted")
	require.ErrorIs(t, err, ErrOAuthState)
	_, err = s.OAuthComplete(ctx, id, strings.Repeat("x", 43), "granted")
	require.ErrorIs(t, err, ErrOAuthState)
	view, err = s.OAuthComplete(ctx, id, start["state"].(string), "granted")
	require.NoError(t, err)
	require.Equal(t, "authorized", view["oauthStatus"])
	require.Equal(t, revision, view["revision"], "authorization must not invalidate Stream configuration")
	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	for _, value := range []string{"merchant-access", "merchant-refresh", "_vendorToken", "_vendorPending", "_vendorExchange"} {
		require.NotContains(t, string(encoded), value)
	}
	row, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(row.Document), &d))
	raw, err := s.HTTPProgramCall(ctx, row, d, streamexec.ProgramCall{Kind: "source.secrets"})
	require.NoError(t, err)
	require.JSONEq(t, `{"appKey":"app","appSecret":"secret"}`, string(raw))
	_, err = s.OAuthComplete(ctx, id, start["state"].(string), "granted")
	require.ErrorIs(t, err, ErrOAuthState)
	require.Equal(t, int32(1), exchanges.Load())
}

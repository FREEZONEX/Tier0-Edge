package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/contextx"
	"backend/internal/permission"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestOAuthAuthorizationAndScriptRead(t *testing.T) {
	t.Setenv("TIER0_RUNTIME_MODE", "native")
	testkit.NewTestDB(t)
	evaluator := permission.New(context.Background())
	for _, suffix := range []string{"start", "complete"} {
		path := "/api/core/v2/uns/sources/documents/oauth/" + suffix
		for _, credentialPermission := range []bool{false, true} {
			keys := map[string]struct{}{"uns.source": {}}
			if credentialPermission {
				keys["uns.source.credentials"] = struct{}{}
			}
			allowed, _, err := evaluator.Allow(context.Background(), contextx.Subject{UserID: 9, ResourceKeys: keys}, "api", "POST", path)
			require.NoError(t, err)
			require.Equal(t, credentialPermission, allowed)
		}
	}

	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.source": {}, "uns.source.credentials": {}}})
	var challenge string
	var refreshes atomic.Int32
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			require.NoError(t, r.ParseForm())
			user, secret, ok := r.BasicAuth()
			require.True(t, ok)
			require.Equal(t, "client", user)
			require.Equal(t, "test-secret", secret)
			if r.Form.Get("grant_type") == "authorization_code" {
				hash := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
				require.Equal(t, challenge, base64.RawURLEncoding.EncodeToString(hash[:]))
				require.Equal(t, "http://127.0.0.1:5178/uns", r.Form.Get("redirect_uri"))
				if r.Form.Get("code") == "rejected" {
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"error":"invalid_grant","secret":"never-return-this"}`))
					return
				}
				_, _ = w.Write([]byte(`{"access_token":"test-access","refresh_token":"test-refresh","token_type":"Bearer","expires_in":1}`))
			} else {
				require.Equal(t, "test-refresh", r.Form.Get("refresh_token"))
				refreshes.Add(1)
				_, _ = w.Write([]byte(`{"access_token":"test-new-access","refresh_token":"test-new-refresh","token_type":"Bearer","expires_in":3600}`))
			}
			return
		}
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "Bearer test-new-access", r.Header.Get("Authorization"))
		require.Equal(t, "/api/items", r.URL.Path)
		require.Equal(t, "open", r.URL.Query().Get("status"))
		_, _ = w.Write([]byte(`{"items":[{"id":7}]}`))
	}))
	defer provider.Close()
	s := New(nil, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	// Only this isolated test trusts the local provider certificate/address.
	s.httpClient = provider.Client()
	s.httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	def := SourceDefinition{Name: "OAuth API", Protocol: OAuth, RuntimeLocation: "local", Endpoint: provider.URL + "/api", HTTP: &HTTPConnection{OAuth: &OAuthConfig{AuthorizationURL: provider.URL + "/authorize", TokenURL: provider.URL + "/token", ClientID: "client", ClientAuthentication: "client_secret_basic", Scopes: "read"}}}
	view, err := s.CreateSource(ctx, def, 0)
	require.NoError(t, err)
	id := view["id"].(int64)
	view, err = s.SaveCredential(ctx, id, view["revision"].(int64), Credential{Kind: "oauth2", Secret: "test-secret", OAuth: &OAuthCredential{}}, "")
	require.NoError(t, err)
	view, err = testkit.SetLegacySourceState(ctx, s, id, view["revision"].(int64), "enabled")
	require.NoError(t, err)
	revision := view["revision"]
	configRevision := view["configRevision"]
	start, err := s.OAuthStart(ctx, id, "http://127.0.0.1:5178/uns")
	require.NoError(t, err)
	authorization, err := url.Parse(start["authorizationUrl"].(string))
	require.NoError(t, err)
	require.Equal(t, "S256", authorization.Query().Get("code_challenge_method"))
	challenge = authorization.Query().Get("code_challenge")
	other := contextx.WithSubject(ctx, contextx.Subject{UserID: 2, ResourceKeys: map[string]struct{}{"uns.source.credentials": {}}})
	_, err = s.OAuthComplete(other, id, start["state"].(string), "valid")
	require.ErrorIs(t, err, ErrOAuthState)
	_, err = s.OAuthComplete(ctx, id, strings.Repeat("x", 43), "valid")
	require.ErrorIs(t, err, ErrOAuthState)
	view, err = s.OAuthComplete(ctx, id, start["state"].(string), "valid")
	require.NoError(t, err)
	require.Equal(t, "authorized", view["oauthStatus"])
	require.Equal(t, revision, view["revision"])
	require.Equal(t, configRevision, view["configRevision"])
	encoded, _ := json.Marshal(view)
	require.NotContains(t, string(encoded), "test-secret")
	require.NotContains(t, string(encoded), "test-access")
	require.NotContains(t, string(encoded), "test-refresh")
	_, err = s.OAuthComplete(ctx, id, start["state"].(string), "valid")
	require.ErrorIs(t, err, ErrOAuthState)
	// Concurrent refreshes share the persisted Source lock: one rotates the token.
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.readScriptSource(ctx, SourceBinding{SourceID: id, ConfigRevision: configRevision.(int64)}, json.RawMessage(`{"path":"items","query":{"status":"open"}}`))
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	for e := range results {
		require.NoError(t, e)
	}
	require.Equal(t, int32(1), refreshes.Load())
	result, err := streamexec.ExecuteWithLegacySources(ctx, streamexec.Request{Code: `const api = sources.get("api"); const result = await api.read({path:"items",query:{status:"open"}}); return {data:result.items,drop:false};`, Input: json.RawMessage(`{}`), Context: json.RawMessage(`{}`), TimeoutMs: 5000, LegacySources: []string{"api"}}, func(ctx context.Context, alias string, raw json.RawMessage) (json.RawMessage, error) {
		require.Equal(t, "api", alias)
		return s.readScriptSource(ctx, SourceBinding{SourceID: id, ConfigRevision: configRevision.(int64)}, raw)
	})
	require.NoError(t, err)
	require.JSONEq(t, `[{"id":7}]`, string(result.Data))
	view, err = s.Source(ctx, id)
	require.NoError(t, err)
	require.Equal(t, revision, view["revision"])
	require.Equal(t, configRevision, view["configRevision"])
	// Provider errors consume the valid state and never echo response secrets.
	start, err = s.OAuthStart(ctx, id, "http://127.0.0.1:5178/uns")
	require.NoError(t, err)
	u, _ := url.Parse(start["authorizationUrl"].(string))
	challenge = u.Query().Get("code_challenge")
	_, err = s.OAuthComplete(ctx, id, start["state"].(string), "rejected")
	require.ErrorIs(t, err, ErrOAuth)
	require.NotContains(t, err.Error(), "never-return-this")
	_, err = s.OAuthComplete(ctx, id, start["state"].(string), "valid")
	require.ErrorIs(t, err, ErrOAuthState)
	// Expired state and changed endpoints cannot reuse a saved client secret.
	start, err = s.OAuthStart(ctx, id, "http://127.0.0.1:5178/uns")
	require.NoError(t, err)
	require.NoError(t, s.changeOAuth(ctx, id, func(_ SourceDefinition, c *Credential) error {
		c.OAuth.Pending.ExpiresAt = time.Now().Add(-time.Minute).Unix()
		return nil
	}))
	_, err = s.OAuthComplete(ctx, id, start["state"].(string), "valid")
	require.ErrorIs(t, err, ErrOAuthState)
	row, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal([]byte(row.Document), &def))
	def.HTTP.OAuth.TokenURL = provider.URL + "/different-token"
	_, err = s.UpdateSource(ctx, id, row.Revision, def, "")
	require.NoError(t, err)
	_, err = s.OAuthStart(ctx, id, "http://127.0.0.1:5178/uns")
	require.ErrorIs(t, err, ErrOAuthRequired)
	_, err = s.OAuthStart(context.Background(), id, "http://127.0.0.1:5178/uns")
	require.ErrorIs(t, err, ErrForbidden)
	_, err = s.OAuthStart(ctx, id, "http://evil.example/callback")
	require.Error(t, err)
}

func TestOAuthTokenClientAuthenticationAndCancellation(t *testing.T) {
	for _, mode := range []string{"none", "client_secret_post"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, r.ParseForm())
				require.Empty(t, r.Header.Get("Authorization"))
				require.Equal(t, "client", r.Form.Get("client_id"))
				if mode == "none" {
					require.Empty(t, r.Form.Get("client_secret"))
				} else {
					require.Equal(t, "test-only-secret", r.Form.Get("client_secret"))
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`))
			}))
			defer server.Close()
			s := New(nil, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
			s.httpClient = server.Client()
			d := SourceDefinition{Endpoint: server.URL, HTTP: &HTTPConnection{OAuth: &OAuthConfig{TokenURL: server.URL, ClientID: "client", ClientAuthentication: mode}}}
			token, err := s.oauthToken(context.Background(), d, "test-only-secret", url.Values{"grant_type": {"authorization_code"}, "code": {"test-code"}})
			require.NoError(t, err)
			require.Equal(t, "test-token", token.AccessToken)
			require.Greater(t, token.ExpiresAt, time.Now().Unix())
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = s.oauthToken(ctx, d, "test-only-secret", url.Values{})
			require.ErrorIs(t, err, context.Canceled)
			require.NotErrorIs(t, err, ErrOAuth)
		})
	}
}

func TestOAuthClientCredentials(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.source": {}, "uns.source.credentials": {}}})
	var tokens atomic.Int32
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/token" {
			require.NoError(t, r.ParseForm())
			require.Equal(t, "client_credentials", r.Form.Get("grant_type"))
			require.Equal(t, "client", r.Form.Get("client_id"))
			require.Equal(t, "reader-secret", r.Form.Get("client_secret"))
			require.Equal(t, "read", r.Form.Get("scope"))
			require.Empty(t, r.Form.Get("redirect_uri"))
			tokens.Add(1)
			_, _ = w.Write([]byte(`{"access_token":"reader-token","token_type":"Bearer","expires_in":3600}`))
			return
		}
		require.Equal(t, "Bearer reader-token", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"value":42}`))
	}))
	defer provider.Close()
	s := New(nil, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
	s.httpClient = provider.Client()
	d := SourceDefinition{Name: "Service account", Protocol: OAuth, RuntimeLocation: "local", Endpoint: provider.URL + "/api", HTTP: &HTTPConnection{OAuth: &OAuthConfig{GrantType: "client_credentials", TokenURL: provider.URL + "/token", ClientID: "client", ClientAuthentication: "client_secret_post", Scopes: "read"}}}
	view, err := s.CreateSource(ctx, d, 0)
	require.NoError(t, err)
	id := view["id"].(int64)
	view, err = s.SaveCredential(ctx, id, view["revision"].(int64), Credential{Kind: "oauth2", Secret: "reader-secret", OAuth: &OAuthCredential{}}, "")
	require.NoError(t, err)
	_, err = s.OAuthStart(ctx, id, "http://localhost/uns")
	require.Error(t, err)
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.readHTTPSource(ctx, id, d, json.RawMessage(`{"path":"items"}`))
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	for e := range results {
		require.NoError(t, e)
	}
	require.EqualValues(t, 1, tokens.Load())
	require.NoError(t, s.changeOAuth(ctx, id, func(_ SourceDefinition, c *Credential) error {
		c.OAuth.ExpiresAt = time.Now().Add(-time.Minute).Unix()
		return nil
	}))
	result, err := s.readHTTPSource(ctx, id, d, json.RawMessage(`{"path":"items"}`))
	require.NoError(t, err)
	require.Equal(t, float64(42), result.(map[string]any)["value"])
	require.EqualValues(t, 2, tokens.Load())
	current, err := s.Source(ctx, id)
	require.NoError(t, err)
	require.Equal(t, view["revision"], current["revision"])
	raw, _ := json.Marshal(current)
	require.NotContains(t, string(raw), "reader-secret")
	require.NotContains(t, string(raw), "reader-token")
	d.HTTP.OAuth.ClientAuthentication = "none"
	require.Error(t, d.Validate())
	d.HTTP.OAuth.ClientAuthentication = "client_secret_basic"
	d.HTTP.OAuth.AuthorizationURL = provider.URL + "/authorize"
	require.Error(t, d.Validate())
}

func TestOAuthRejectedTokenRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, grant                                     string
		tokenFailure, rejectAgain, noRefresh, forbidden bool
	}{
		{name: "client credentials without expiry", grant: "client_credentials"},
		{name: "authorization code refresh", grant: "authorization_code"},
		{name: "exchange failure persists rejection", grant: "client_credentials", tokenFailure: true},
		{name: "second unauthorized stops", grant: "client_credentials", rejectAgain: true},
		{name: "authorization needs consent", grant: "authorization_code", noRefresh: true},
		{name: "forbidden does not refresh", grant: "client_credentials", forbidden: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testkit.NewTestDB(t)
			ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.source": {}, "uns.source.credentials": {}}})
			var exchanges, reads atomic.Int32
			provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					exchanges.Add(1)
					require.NoError(t, r.ParseForm())
					if tc.grant == "authorization_code" {
						require.Equal(t, "refresh_token", r.Form.Get("grant_type"))
						require.Equal(t, "refresh", r.Form.Get("refresh_token"))
					} else {
						require.Equal(t, tc.grant, r.Form.Get("grant_type"))
					}
					if tc.tokenFailure {
						w.WriteHeader(400)
						return
					}
					_, _ = w.Write([]byte("{\"access_token\":\"new\",\"token_type\":\"Bearer\"}"))
					return
				}
				reads.Add(1)
				require.Equal(t, http.MethodGet, r.Method)
				if tc.forbidden {
					w.WriteHeader(403)
					return
				}
				if r.Header.Get("Authorization") == "Bearer old" || tc.rejectAgain {
					w.WriteHeader(401)
					return
				}
				require.Equal(t, "Bearer new", r.Header.Get("Authorization"))
				_, _ = w.Write([]byte("{\"items\":[1]}"))
			}))
			defer provider.Close()
			s := New(nil, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
			s.httpClient = provider.Client()
			oauth := &OAuthConfig{GrantType: tc.grant, TokenURL: provider.URL + "/token", ClientID: "client", ClientAuthentication: "client_secret_post"}
			if tc.grant == "authorization_code" {
				oauth.AuthorizationURL = provider.URL + "/authorize"
			}
			def := SourceDefinition{Name: "OAuth retry", Protocol: OAuth, RuntimeLocation: "local", Endpoint: provider.URL + "/api", HTTP: &HTTPConnection{OAuth: oauth}}
			view, err := s.CreateSource(ctx, def, 0)
			require.NoError(t, err)
			id := view["id"].(int64)
			view, err = s.SaveCredential(ctx, id, view["revision"].(int64), Credential{Kind: "oauth2", Secret: "secret", OAuth: &OAuthCredential{}}, "")
			require.NoError(t, err)
			rev, configRev := view["revision"], view["configRevision"]
			require.NoError(t, s.changeOAuth(ctx, id, func(d SourceDefinition, c *Credential) error {
				c.OAuth.AccessToken = "old"
				c.OAuth.Configuration = oauthConfiguration(d)
				if !tc.noRefresh {
					c.OAuth.RefreshToken = "refresh"
				}
				return nil
			}))
			_, err = s.readHTTPSource(ctx, id, def, json.RawMessage("{\"path\":\"items\"}"))
			failed := tc.tokenFailure || tc.rejectAgain || tc.noRefresh || tc.forbidden
			if failed {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			expectedExchanges := int32(1)
			if tc.noRefresh || tc.forbidden {
				expectedExchanges = 0
			}
			require.Equal(t, expectedExchanges, exchanges.Load())
			expectedReads := int32(2)
			if tc.noRefresh || tc.forbidden || tc.tokenFailure {
				expectedReads = 1
			}
			require.Equal(t, expectedReads, reads.Load())
			view, err = s.Source(ctx, id)
			require.NoError(t, err)
			require.Equal(t, rev, view["revision"])
			require.Equal(t, configRev, view["configRevision"])
			if failed && !tc.forbidden {
				require.Equal(t, "required", view["oauthStatus"])
			} else {
				require.Equal(t, "authorized", view["oauthStatus"])
			}
			if !failed {
				// A delayed 401 for the old token must not erase the newly persisted token.
				require.NoError(t, s.rejectOAuthToken(ctx, id, def, "old"))
				var wg sync.WaitGroup
				errs := make(chan error, 2)
				for i := 0; i < 2; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						_, e := s.readHTTPSource(ctx, id, def, json.RawMessage("{\"path\":\"items\"}"))
						errs <- e
					}()
				}
				wg.Wait()
				close(errs)
				for e := range errs {
					require.NoError(t, e)
				}
				require.Equal(t, int32(1), exchanges.Load())
			}
		})
	}
}

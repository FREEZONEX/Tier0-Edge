package sourcestream

import (
	"backend/internal/contextx"
	"backend/internal/testkit"
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestHTTPSourceRequestBoundary(t *testing.T) {
	for _, path := range []string{"https://evil.example/", "//evil.example/", "../token", "%2e%2e/token", "/root", "items?access_token=x", "items#token", "..\\token"} {
		_, err := sourceReadURL("https://api.example.com/v1", httpReadOptions{Path: path})
		require.Error(t, err, path)
	}
	u, err := sourceReadURL("https://api.example.com/v1", httpReadOptions{Path: "items", Query: map[string]string{"page": "2"}})
	require.NoError(t, err)
	require.Equal(t, "https://api.example.com/v1/items?page=2", u)
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.100.100.200", "::1", "::ffff:127.0.0.1", "fe80::1", "fd00::1", "2002:7f00:1::"} {
		require.False(t, publicSourceIP(netip.MustParseAddr(ip)), ip)
	}
	require.True(t, publicSourceIP(netip.MustParseAddr("1.1.1.1")))
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	_, err = sourceHTTPClient().Do(req)
	require.Error(t, err)
	require.False(t, called)
}

func TestHTTPSourceAuthentication(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.source": {}, "uns.source.credentials": {}}})
	for _, mode := range []string{"none", "basic", "bearer"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, "/api/items", r.URL.Path)
				switch mode {
				case "none":
					require.Empty(t, r.Header.Get("Authorization"))
				case "basic":
					user, secret, ok := r.BasicAuth()
					require.True(t, ok)
					require.Equal(t, "reader", user)
					require.Equal(t, "test-secret", secret)
				case "bearer":
					require.Equal(t, "Bearer test-secret", r.Header.Get("Authorization"))
				}
				_, _ = w.Write([]byte(`{"value":42}`))
			}))
			defer server.Close()
			s := New(nil, nil, base64.StdEncoding.EncodeToString(make([]byte, 32)))
			s.httpClient = server.Client()
			d := SourceDefinition{Name: "HTTP " + mode, Protocol: HTTP, RuntimeLocation: "local", Endpoint: server.URL + "/api", HTTP: &HTTPConnection{Authentication: mode}}
			view, err := s.CreateSource(ctx, d, 0)
			require.NoError(t, err)
			id := view["id"].(int64)
			_, err = s.SaveCredential(ctx, id, view["revision"].(int64), Credential{Kind: "oauth2", OAuth: &OAuthCredential{}}, "")
			require.ErrorIs(t, err, ErrCredential)
			if mode != "none" {
				view, err = s.SaveCredential(ctx, id, view["revision"].(int64), Credential{Kind: mode, Username: "reader", Secret: "test-secret"}, "")
				require.NoError(t, err)
				d, _, _, err = s.connection(ctx, id, nil)
				require.NoError(t, err)
			}
			result, err := s.readHTTPSource(ctx, id, d, json.RawMessage(`{"path":"items"}`))
			require.NoError(t, err)
			require.Equal(t, float64(42), result.(map[string]any)["value"])
			require.Equal(t, 1, calls)
			view, err = s.Source(ctx, id)
			require.NoError(t, err)
			require.NotContains(t, view, "oauthStatus")
			raw, err := json.Marshal(view)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "test-secret")
			_, err = s.OAuthStart(ctx, id, "http://localhost/uns")
			require.Error(t, err)
		})
	}
	d := SourceDefinition{Name: "HTTP", Protocol: HTTP, RuntimeLocation: "local", Endpoint: "https://api.example.com", HTTP: &HTTPConnection{Authentication: "none", OAuth: &OAuthConfig{}}}
	require.Error(t, d.Validate())
	d.Protocol = OAuth
	require.Error(t, d.Validate())
}

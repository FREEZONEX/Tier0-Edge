package sourcestream

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/repo"
	"github.com/stretchr/testify/require"
)

func TestVendorLosslessNumbersAndDigest(t *testing.T) {
	value, err := vendorJSON([]byte(`{"id":9007199254740999,"amount":12345.678901234567890,"safe":42,"nested":[9223372036854775807]}`))
	require.NoError(t, err)
	obj := vendorObject(value)
	require.Equal(t, "9007199254740999", obj["id"])
	require.Equal(t, "12345.678901234567890", obj["amount"])
	require.Equal(t, int64(42), obj["safe"])
	require.Equal(t, "9223372036854775807", obj["nested"].([]any)[0])
	req, err := http.NewRequest("POST", "https://camera.test/ISAPI/AccessControl/AcsEvent?format=json", nil)
	require.NoError(t, err)
	credential := Credential{Kind: "basic", Username: "test-user", Secret: "test-password"}
	header, err := vendorDigest(`Digest realm="camera", nonce="fresh", opaque="opaque", qop="auth", algorithm=MD5`, req, credential)
	require.NoError(t, err)
	require.Contains(t, header, `uri="/ISAPI/AccessControl/AcsEvent?format=json"`)
	require.Contains(t, header, `qop=auth`)
	require.Contains(t, header, `nc=00000001`)
	require.NotContains(t, header, credential.Secret)
	for _, challenge := range []string{"Basic realm=x", `Digest realm="x", nonce="x", qop="auth-int"`, `Digest realm="x", nonce="x", algorithm=SHA-256`} {
		_, err = vendorDigest(challenge, req, credential)
		require.Error(t, err)
	}
}

// Authorize the real request object without I/O; only the exact native metadata
// GET/HEAD switches media type, while entities retain lossless JSON negotiation.
func TestVendorSAPMetadataAcceptBoundary(t *testing.T) {
	s := New(nil, nil, "")
	t.Cleanup(s.httpManager.Close)
	for _, test := range []struct {
		name, method, path, accept, expected string
	}{
		{"metadata-default", http.MethodGet, "/odata/v1/$metadata", "", "application/xml"},
		{"metadata-json-overridden", http.MethodGet, "/odata/v1/$metadata", "application/json", "application/xml"},
		{"metadata-xml-preserved", http.MethodGet, "/odata/v1/$metadata", "application/xml", "application/xml"},
		{"metadata-head", http.MethodHead, "/odata/v1/$metadata", "application/json", "application/xml"},
		{"entity-json-precision", http.MethodGet, "/odata/v1/Materials", "", "application/json;IEEE754Compatible=true"},
		{"entity-cannot-select-xml", http.MethodGet, "/odata/v1/Materials", "application/xml", "application/json;IEEE754Compatible=true"},
		{"entity-head-json", http.MethodHead, "/odata/v1/Materials", "application/xml", "application/json;IEEE754Compatible=true"},
		{"metadata-nested-not-native", http.MethodGet, "/odata/v1/Materials/$metadata", "application/xml", "application/json;IEEE754Compatible=true"},
		{"metadata-other-prefix", http.MethodGet, "/other/v1/$metadata", "application/xml", "application/json;IEEE754Compatible=true"},
		{"metadata-suffix-not-native", http.MethodGet, "/odata/v1/$metadata/extra", "application/xml", "application/json;IEEE754Compatible=true"},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, endpoint := range []string{"https://fixture.invalid/odata/v1", "https://fixture.invalid/odata/v1/"} {
				req, err := http.NewRequest(test.method, "https://fixture.invalid"+test.path, nil)
				require.NoError(t, err)
				req.Header.Set("Accept", test.accept)
				d := SourceDefinition{SourceType: "sap-odata", Endpoint: endpoint}
				require.NoError(t, s.vendorAuthorize(context.Background(), repo.Source{}, d, Credential{}, req, nil))
				require.Equal(t, test.expected, req.Header.Get("Accept"))
			}
		})
	}
}

func TestVendorReservedCredentialKeys(t *testing.T) {
	s := New(nil, nil, "")
	for _, key := range []string{"_vendorToken", "_vendorPending", "_vendorExchange", "_vendorInjected"} {
		_, err := s.SaveCredential(context.Background(), 1, 1, Credential{Kind: "custom", Values: map[string]string{key: "injected"}}, "")
		require.ErrorIs(t, err, ErrCredential, "public input cannot inject an adapter-managed cache")
	}
	values := httpCredentialValues(Credential{Kind: "custom", Values: map[string]string{"appKey": "configured", "_vendorToken": "cached-ticket", "_vendorPending": "state"}})
	require.Equal(t, map[string]string{"appKey": "configured"}, values)
}

func TestVendorSessionJarIsSourceScoped(t *testing.T) {
	var logins atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/login") {
			var document struct {
				Body struct {
					Credentials map[string]string `json:"credentials"`
				} `json:"body"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&document))
			logins.Add(1)
			http.SetCookie(w, &http.Cookie{Name: "session", Value: document.Body.Credentials["user"], Path: "/", HttpOnly: true})
			_, _ = w.Write([]byte(`{"ServiceData":{"partialErrors":[]}}`))
			return
		}
		cookie, err := r.Cookie("session")
		if err != nil {
			w.WriteHeader(401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"seen": cookie.Value})
	}))
	defer server.Close()
	s := New(nil, nil, "")
	httpTestManager(t, s.httpManager, server)
	d := SourceDefinition{Name: "Team fixture", Endpoint: "http://fixture.internal/tc", SourceType: "teamcenter", HTTP: &HTTPConnection{}}
	for _, example := range []struct {
		id   int64
		user string
	}{{1, "alice"}, {2, "bob"}, {1, "alice"}} {
		row := repo.Source{ID: example.id, ConfigRevision: 1}
		credential := Credential{Kind: "basic", Username: example.user, Secret: "password"}
		require.NoError(t, s.vendorLogin(context.Background(), row, d, credential))
		res, err := s.vendorRaw(context.Background(), row, d, Credential{}, "POST", "/tc/read", nil, []byte(`{}`), nil)
		require.NoError(t, err)
		require.Equal(t, example.user, vendorObject(res.Data)["seen"])
	}
	require.Equal(t, int32(2), logins.Load(), "sessions must be reused but never shared across Sources")
	s.httpManager.CloseSource(1)
	require.NoError(t, s.vendorLogin(context.Background(), repo.Source{ID: 1, ConfigRevision: 2}, d, Credential{Kind: "basic", Username: "alice", Secret: "password"}))
	require.Equal(t, int32(3), logins.Load())
}

func TestVendorTokenExchangeContracts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/open-auth/selfAppAuth/getAccessToken":
			q := r.URL.Query()
			h := hmac.New(sha256.New, []byte("app-secret"))
			_, _ = h.Write([]byte("appKey" + q.Get("appKey") + "timestamp" + q.Get("timestamp")))
			require.Equal(t, base64.StdEncoding.EncodeToString(h.Sum(nil)), q.Get("signature"))
			_, _ = w.Write([]byte(`{"code":"00000","data":{"access_token":"yon-token","expire":3600}}`))
		case "/jdyconnector/app_management/kingdee_auth_token":
			require.Equal(t, "client", r.Header.Get("X-Api-ClientID"))
			require.Equal(t, "2.0", r.Header.Get("X-Api-Auth-Version"))
			require.NotEmpty(t, r.Header.Get("X-Api-Signature"))
			require.Equal(t, base64.StdEncoding.EncodeToString([]byte(vendorMAC("app-secret", "app", false))), r.URL.Query().Get("app_signature"))
			_, _ = w.Write([]byte(`{"errcode":0,"data":{"app-token":"jdy-token","expires_in":86400}}`))
		case "/oauth2/token":
			require.NoError(t, r.ParseForm())
			require.Equal(t, "refresh_token", r.PostForm.Get("grant_type"))
			require.Equal(t, "app", r.PostForm.Get("app_key"))
			require.Equal(t, "refresh", r.PostForm.Get("refresh_token"))
			_, _ = w.Write([]byte(`{"access_token":"ngp-token","refresh_token":"rotated","expires_in":3600}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	s := New(nil, nil, "")
	httpTestManager(t, s.httpManager, server)
	credential := Credential{Kind: "custom", Values: map[string]string{"appKey": "app", "appSecret": "app-secret", "clientId": "client", "clientSecret": "client-secret", "refreshToken": "refresh"}}
	for _, profile := range []string{"yonyou-yonbip", "kingdee-jdy", "ngp"} {
		d := SourceDefinition{Endpoint: "http://fixture.internal/", SourceType: profile, HTTP: &HTTPConnection{}}
		token, err := s.vendorExchange(context.Background(), repo.Source{ID: 1, ConfigRevision: 1}, d, credential, vendorToken{})
		require.NoError(t, err)
		require.NotEmpty(t, token.Token)
		require.Greater(t, token.Expires, time.Now().Unix())
		if profile == "ngp" {
			require.Equal(t, "rotated", token.Refresh)
		}
	}
	_, err := vendorOriginURL(SourceDefinition{Endpoint: "https://business.test/api"}, "/../secret")
	require.Error(t, err)
	_, err = vendorOriginURL(SourceDefinition{Endpoint: "http://user:secret@business.test/api"}, "/token")
	require.Error(t, err)
	query := url.Values{"page": {"1"}}
	require.NoError(t, jdySignature(http.Header{}, "GET", "/business", query, credential))
}

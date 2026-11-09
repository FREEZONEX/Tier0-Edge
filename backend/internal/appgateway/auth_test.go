package appgateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"backend/internal/config"
	"backend/internal/repo"
)

func TestExtractToken(t *testing.T) {
	a := NewAuth(config.Config{})

	tests := []struct {
		name        string
		setup       func(*http.Request)
		wantToken   string
		wantFromURL bool
	}{
		{
			name: "app_auth_token cookie",
			setup: func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: "app_auth_token", Value: "app-token"})
			},
			wantToken:   "app-token",
			wantFromURL: false,
		},
		{
			name: "url _token",
			setup: func(r *http.Request) {
				r.URL.RawQuery = "_token=url-token"
			},
			wantToken:   "url-token",
			wantFromURL: true,
		},
		{
			name:        "no token",
			setup:       func(r *http.Request) {},
			wantToken:   "",
			wantFromURL: false,
		},
		{
			name: "url token priority over cookie",
			setup: func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: "app_auth_token", Value: "cookie-token"})
				r.URL.RawQuery = "_token=url-token"
			},
			wantToken:   "url-token",
			wantFromURL: true,
		},
		{
			name: "platform session cookie priority over stale app auth token",
			setup: func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: "edge_session", Value: "platform-token"})
				r.AddCookie(&http.Cookie{Name: "app_auth_token", Value: "stale-app-token"})
			},
			wantToken:   "platform-token",
			wantFromURL: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{
				URL:    mustParseURL("http://example.com/"),
				Header: http.Header{},
			}
			tt.setup(r)
			gotToken, gotFromURL := a.extractToken(r)
			if gotToken != tt.wantToken {
				t.Errorf("extractToken() token = %q, want %q", gotToken, tt.wantToken)
			}
			if gotFromURL != tt.wantFromURL {
				t.Errorf("extractToken() fromURL = %v, want %v", gotFromURL, tt.wantFromURL)
			}
		})
	}
}

func TestValidateToken_IgnoresPlatformRole(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/core/oauth/userinfo" {
			t.Errorf("path = %q, want /api/core/oauth/userinfo", r.URL.Path)
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token" {
			t.Errorf("Authorization = %q, want Bearer token", got)
			http.Error(w, "unexpected authorization", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"sub":1,"preferred_username":"tier0","email":"tier0@example.com","roleList":[{"roleName":"Admin","roleCode":"admin"}]}`))
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	port, err := strconv.Atoi(serverURL.Port())
	if err != nil {
		t.Fatalf("parse test server port: %v", err)
	}

	a := NewAuth(config.Config{})
	a.conf.Port = port
	userInfo, err := a.validateToken("token")
	if err != nil {
		t.Fatalf("validateToken() error = %v", err)
	}
	if got := userInfo["role"]; got != "" {
		t.Fatalf("userInfo[role] = %q, want empty platform role", got)
	}
}

func mustParseURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

func TestAuthenticate_SkipAuthForLoginAndAPI(t *testing.T) {
	a := NewAuth(config.Config{})

	tests := []struct {
		path        string
		wantAllowed bool
	}{
		{"/login", true},
		{"/api/health", true},
		{"/api/v1/users", true},
		{"/dashboard", false},
		{"/", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			r := &http.Request{
				URL:    mustParseURL("http://example.com" + tt.path),
				Header: http.Header{},
			}
			w := &recordingResponseWriter{}
			got := a.Authenticate(w, r, nil)
			if got != tt.wantAllowed {
				t.Errorf("Authenticate(%q) = %v, want %v", tt.path, got, tt.wantAllowed)
			}
		})
	}
}

func TestRedirectToLoginFollowsGatewayScheme(t *testing.T) {
	tests := []struct {
		name   string
		env    string
		scheme string
		want   string
	}{
		{name: "http gateway", scheme: "http", want: "http://example.com/login"},
		{name: "https gateway", scheme: "https", want: "https://example.com/login"},
		{name: "empty follows http entrance", env: "http", scheme: "", want: "http://example.com/login"},
		{name: "empty follows https entrance", env: "https", scheme: "", want: "https://example.com/login"},
		{name: "empty defaults to http", scheme: "", want: "http://example.com/login"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ENTRANCE_PROTOCOL", tt.env)
			a := NewAuth(config.Config{
				AppGateway: config.AppGatewayConfig{Scheme: tt.scheme},
			})
			r := &http.Request{
				Method: http.MethodGet,
				URL:    mustParseURL("http://example.com/dashboard"),
				Host:   "example.com",
				Header: http.Header{},
			}
			w := &recordingResponseWriter{}

			a.redirectToLogin(w, r)

			if w.code != http.StatusFound {
				t.Fatalf("redirect status = %d, want %d", w.code, http.StatusFound)
			}
			if got := w.Header().Get("Location"); got != tt.want {
				t.Fatalf("Location = %q, want %q", got, tt.want)
			}
		})
	}
}

type recordingResponseWriter struct {
	header http.Header
	code   int
}

func (w *recordingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *recordingResponseWriter) Write(b []byte) (int, error) {
	return len(b), nil
}

func (w *recordingResponseWriter) WriteHeader(code int) {
	w.code = code
}

func TestRedirectToLaunchpadApp(t *testing.T) {
	newAuthWithExternal := func(externalHost, scheme string) *Auth {
		a := NewAuth(config.Config{
			AppGateway: config.AppGatewayConfig{
				Scheme:            scheme,
				ExternalHost:      externalHost,
				DefaultAccessMode: "port",
			},
		})
		// 注入 fake store：跳转时解析 project_app 数字 id 走 resolver.newStore
		a.resolver.newStore = func(context.Context) projectRoleStore {
			return &fakeProjectRoleStore{app: repo.ProjectApp{ID: 39, ProjectID: 10}}
		}
		return a
	}

	docNav := func(path string) *http.Request {
		r := &http.Request{
			Method: http.MethodGet,
			URL:    mustParseURL("http://app.example.com:30017" + path),
			Host:   "app.example.com:30017",
			Header: http.Header{},
		}
		r.Header.Set("Accept", "text/html,application/xhtml+xml")
		return r
	}

	t.Run("redirects with external host and resolved app id", func(t *testing.T) {
		a := newAuthWithExternal("app.example.com", "https")
		route := &repo.AppGatewayRoute{AppID: "deploy-uuid-1", ProjectID: 10}
		r := docNav("/")
		w := &recordingResponseWriter{}

		if !a.redirectToLaunchpadApp(w, r, route, "access-required", true) {
			t.Fatal("redirectToLaunchpadApp() = false, want true")
		}
		if w.code != http.StatusFound {
			t.Fatalf("status = %d, want %d", w.code, http.StatusFound)
		}
		if got := w.Header().Get("Location"); got != "https://app.example.com/launchpad/10/39" {
			t.Fatalf("Location = %q, want %q", got, "https://app.example.com/launchpad/10/39")
		}
	})

	t.Run("external port appended when non-standard", func(t *testing.T) {
		a := newAuthWithExternal("enterprise-new-dev.tier0.dev", "https")
		a.conf.AppGateway.ExternalPort = 8443
		route := &repo.AppGatewayRoute{AppID: "deploy-uuid-1", ProjectID: 10}
		r := docNav("/")
		w := &recordingResponseWriter{}

		if !a.redirectToLaunchpadApp(w, r, route, "access-required", true) {
			t.Fatal("redirectToLaunchpadApp() = false, want true")
		}
		if got := w.Header().Get("Location"); got != "https://app.example.com:8443/launchpad/10/39" {
			t.Fatalf("Location = %q, want %q", got, "https://app.example.com:8443/launchpad/10/39")
		}
	})

	t.Run("member without role redirects with real app id", func(t *testing.T) {
		a := newAuthWithExternal("enterprise-new-dev.tier0.dev", "https")
		route := &repo.AppGatewayRoute{AppID: "deploy-uuid-1", ProjectID: 10}
		r := docNav("/")
		w := &recordingResponseWriter{}

		if !a.redirectToLaunchpadApp(w, r, route, "access-required", true) {
			t.Fatal("redirectToLaunchpadApp() = false, want true")
		}
		if got := w.Header().Get("Location"); got != "https://app.example.com/launchpad/10/39" {
			t.Fatalf("Location = %q, want %q", got, "https://app.example.com/launchpad/10/39")
		}
	})

	t.Run("non-member gets id 0 (do not disclose existence)", func(t *testing.T) {
		a := newAuthWithExternal("enterprise-new-dev.tier0.dev", "https")
		route := &repo.AppGatewayRoute{AppID: "deploy-uuid-1", ProjectID: 10}
		r := docNav("/")
		w := &recordingResponseWriter{}
		// discloseAppID=false：即使 store 能返回真实 appId 也强制置 0
		if !a.redirectToLaunchpadApp(w, r, route, "not-found", false) {
			t.Fatal("redirectToLaunchpadApp() = false, want true")
		}
		if got := w.Header().Get("Location"); got != "https://app.example.com/launchpad/10/0" {
			t.Fatalf("Location = %q, want %q", got, "https://app.example.com/launchpad/10/0")
		}
	})

	t.Run("app not found resolves to id 0 (do not disclose existence)", func(t *testing.T) {
		a := NewAuth(config.Config{
			AppGateway: config.AppGatewayConfig{
				Scheme:            "https",
				ExternalHost:      "enterprise-new-dev.tier0.dev",
				DefaultAccessMode: "port",
			},
		})
		a.resolver.newStore = func(context.Context) projectRoleStore {
			return &fakeProjectRoleStore{appErr: repo.ErrNotFound}
		}
		route := &repo.AppGatewayRoute{AppID: "deploy-uuid-deleted", ProjectID: 10}
		r := docNav("/")
		w := &recordingResponseWriter{}

		if !a.redirectToLaunchpadApp(w, r, route, "not-found", true) {
			t.Fatal("redirectToLaunchpadApp() = false, want true")
		}
		if got := w.Header().Get("Location"); got != "https://app.example.com/launchpad/10/0" {
			t.Fatalf("Location = %q, want %q", got, "https://app.example.com/launchpad/10/0")
		}
	})

	t.Run("xhr request keeps JSON (no redirect)", func(t *testing.T) {
		a := newAuthWithExternal("app.example.com", "https")
		route := &repo.AppGatewayRoute{AppID: "deploy-uuid-1", ProjectID: 10}
		// XHR: Sec-Fetch-Dest=empty，Accept 无 text/html
		r := &http.Request{
			Method: http.MethodGet,
			URL:    mustParseURL("http://app.example.com:30017/graphql"),
			Host:   "app.example.com:30017",
			Header: http.Header{},
		}
		r.Header.Set("Sec-Fetch-Dest", "empty")
		r.Header.Set("Accept", "application/json")
		w := &recordingResponseWriter{}

		if a.redirectToLaunchpadApp(w, r, route, "access-required", true) {
			t.Fatal("redirectToLaunchpadApp() on XHR = true, want false")
		}
		if w.code != 0 {
			t.Fatalf("status on XHR = %d, want 0 (no write)", w.code)
		}
	})

	t.Run("api path keeps JSON response (no redirect)", func(t *testing.T) {
		a := newAuthWithExternal("app.example.com", "https")
		route := &repo.AppGatewayRoute{AppID: "deploy-uuid-1", ProjectID: 10}
		r := &http.Request{
			Method: http.MethodGet,
			URL:    mustParseURL("http://app.example.com:30017/api/v1/data"),
			Host:   "app.example.com:30017",
			Header: http.Header{},
		}
		w := &recordingResponseWriter{}

		if a.redirectToLaunchpadApp(w, r, route, "access-required", true) {
			t.Fatal("redirectToLaunchpadApp() on /api/ = true, want false")
		}
		if w.code != 0 {
			t.Fatalf("status on /api/ = %d, want 0 (no write)", w.code)
		}
	})

	t.Run("empty launchpad base returns false", func(t *testing.T) {
		a := NewAuth(config.Config{AppGateway: config.AppGatewayConfig{}})
		route := &repo.AppGatewayRoute{AppID: "deploy-uuid-1", ProjectID: 10}
		// 请求未携带 Host 且 ExternalHost 未配置：解析不出入口，回退 JSON
		r := docNav("/")
		r.Host = ""
		w := &recordingResponseWriter{}

		if a.redirectToLaunchpadApp(w, r, route, "access-required", true) {
			t.Fatal("redirectToLaunchpadApp() with empty base = true, want false")
		}
		if w.code != 0 {
			t.Fatalf("status with empty base = %d, want 0 (caller falls back to JSON)", w.code)
		}
	})

	t.Run("port mode follows request host when external host missing", func(t *testing.T) {
		// 服务器换 IP：ExternalHost 未配置/失效，按请求 host 动态推导
		a := NewAuth(config.Config{
			AppGateway: config.AppGatewayConfig{Scheme: "http", DefaultAccessMode: "port"},
		})
		a.resolver.newStore = func(context.Context) projectRoleStore {
			return &fakeProjectRoleStore{app: repo.ProjectApp{ID: 39, ProjectID: 10}}
		}
		route := &repo.AppGatewayRoute{AppID: "deploy-uuid-1", ProjectID: 10}
		r := docNav("/")
		r.Host = "192.168.7.9:30017"
		w := &recordingResponseWriter{}

		if !a.redirectToLaunchpadApp(w, r, route, "access-required", true) {
			t.Fatal("redirectToLaunchpadApp() = false, want true")
		}
		if got := w.Header().Get("Location"); got != "http://192.168.7.9/launchpad/10/39" {
			t.Fatalf("Location = %q, want %q", got, "http://192.168.7.9/launchpad/10/39")
		}
	})

	t.Run("subdomain mode redirects to request parent domain", func(t *testing.T) {
		a := NewAuth(config.Config{
			AppGateway: config.AppGatewayConfig{
				Scheme:            "https",
				ExternalHost:      "10.0.0.1", // 安装期烘入的旧地址，应被请求推导覆盖
				DefaultAccessMode: "subdomain",
				SubdomainSuffix:   "apps.example.com",
			},
		})
		a.resolver.newStore = func(context.Context) projectRoleStore {
			return &fakeProjectRoleStore{app: repo.ProjectApp{ID: 39, ProjectID: 10}}
		}
		route := &repo.AppGatewayRoute{AppID: "deploy-uuid-1", ProjectID: 10}
		r := docNav("/")
		r.Host = "a1b2c3d4e5f6.apps.example.com"
		w := &recordingResponseWriter{}

		if !a.redirectToLaunchpadApp(w, r, route, "access-required", true) {
			t.Fatal("redirectToLaunchpadApp() = false, want true")
		}
		if got := w.Header().Get("Location"); got != "https://apps.example.com/launchpad/10/39" {
			t.Fatalf("Location = %q, want %q", got, "https://apps.example.com/launchpad/10/39")
		}
	})

	t.Run("subdomain mode keeps explicit request port", func(t *testing.T) {
		a := NewAuth(config.Config{
			AppGateway: config.AppGatewayConfig{
				Scheme:            "https",
				DefaultAccessMode: "subdomain",
				SubdomainSuffix:   "apps.example.com",
			},
		})
		a.resolver.newStore = func(context.Context) projectRoleStore {
			return &fakeProjectRoleStore{app: repo.ProjectApp{ID: 39, ProjectID: 10}}
		}
		route := &repo.AppGatewayRoute{AppID: "deploy-uuid-1", ProjectID: 10}
		r := docNav("/")
		r.Host = "app-a1b2c3d4e5f6.apps.example.com:8443"
		w := &recordingResponseWriter{}

		if !a.redirectToLaunchpadApp(w, r, route, "access-required", true) {
			t.Fatal("redirectToLaunchpadApp() = false, want true")
		}
		if got := w.Header().Get("Location"); got != "https://apps.example.com:8443/launchpad/10/39" {
			t.Fatalf("Location = %q, want %q", got, "https://apps.example.com:8443/launchpad/10/39")
		}
	})
}

func TestLaunchpadAuthority(t *testing.T) {
	cases := []struct {
		name     string
		conf     config.AppGatewayConfig
		host     string
		trustXFF bool
		xffHost  string
		want     string
	}{
		{
			name: "port mode strips app port and normalizes via external port",
			conf: config.AppGatewayConfig{ExternalPort: 8443, Scheme: "https"},
			host: "192.168.7.9:30017",
			want: "192.168.7.9:8443",
		},
		{
			name: "port mode keeps hostname only without external port",
			conf: config.AppGatewayConfig{},
			host: "192.168.7.9:30017",
			want: "192.168.7.9",
		},
		{
			name: "subdomain host returns parent domain",
			conf: config.AppGatewayConfig{SubdomainSuffix: "apps.example.com"},
			host: "a1b2.apps.example.com",
			want: "apps.example.com",
		},
		{
			name: "subdomain legacy label returns parent domain",
			conf: config.AppGatewayConfig{SubdomainSuffix: "apps.example.com"},
			host: "app-a1b2.apps.example.com",
			want: "apps.example.com",
		},
		{
			name:     "forwarded host honored when trusted",
			conf:     config.AppGatewayConfig{ExternalPort: 8443, Scheme: "https"},
			host:     "10.0.0.2:8080",
			trustXFF: true,
			xffHost:  "entrance.example.com, inner:80",
			want:     "entrance.example.com:8443",
		},
		{
			name:     "forwarded host ignored when untrusted",
			conf:     config.AppGatewayConfig{SubdomainSuffix: "apps.example.com"},
			host:     "a1b2.apps.example.com",
			trustXFF: false,
			xffHost:  "evil.example.com",
			want:     "apps.example.com",
		},
		{
			name: "empty host falls back to external host",
			conf: config.AppGatewayConfig{ExternalHost: "legacy.example.com"},
			host: "",
			want: "legacy.example.com",
		},
		{
			name: "empty host and empty external host returns empty",
			conf: config.AppGatewayConfig{},
			host: "",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := NewAuth(config.Config{AppGateway: tc.conf})
			a.conf.Gateway.TrustProxyHeaders = tc.trustXFF
			r := &http.Request{Host: tc.host, Header: http.Header{}}
			if tc.xffHost != "" {
				r.Header.Set("X-Forwarded-Host", tc.xffHost)
			}

			if got := a.launchpadAuthority(r); got != tc.want {
				t.Fatalf("launchpadAuthority(%q) = %q, want %q", tc.host, got, tc.want)
			}
		})
	}
}

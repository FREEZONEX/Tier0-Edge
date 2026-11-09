// 测试目标：requestauthority 中间件的 ctx 注入（requestauthority.go）。
// 覆盖的正常路径：请求携带 Host 时 authority 写入 ctx、下游 handler 可提取；
// 信任代理头开关分别生效。
// 覆盖的异常情况：请求无 Host 时不注入。

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/httpx"
)

func TestRequestAuthorityMiddleware_InjectsAuthority(t *testing.T) {
	newRequest := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "http://backend-internal/api/core/projects", nil)
		r.Host = "192.168.7.9:8080"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "entrance.example.com")
		return r
	}

	cases := []struct {
		name          string
		trustProxy    bool
		wantScheme    string
		wantHost      string
		wantExtracted bool
	}{
		{name: "trust proxy headers", trustProxy: true, wantScheme: "https", wantHost: "entrance.example.com", wantExtracted: true},
		{name: "ignore proxy headers", trustProxy: false, wantScheme: "http", wantHost: "192.168.7.9:8080", wantExtracted: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var (
				gotScheme, gotHost string
				gotOK              bool
			)
			handler := NewRequestAuthorityMiddleware(tc.trustProxy).Handle(func(w http.ResponseWriter, r *http.Request) {
				gotScheme, gotHost, gotOK = httpx.AuthorityFrom(r.Context())
			})

			handler(httptest.NewRecorder(), newRequest())

			if !gotOK {
				t.Fatalf("authority not injected into ctx")
			}
			if gotScheme != tc.wantScheme || gotHost != tc.wantHost {
				t.Fatalf("authority = %q/%q, want %q/%q", gotScheme, gotHost, tc.wantScheme, tc.wantHost)
			}
		})
	}
}

func TestRequestAuthorityMiddleware_EmptyHostSkipsInjection(t *testing.T) {
	var gotOK bool
	handler := NewRequestAuthorityMiddleware(true).Handle(func(w http.ResponseWriter, r *http.Request) {
		_, _, gotOK = httpx.AuthorityFrom(r.Context())
	})

	r := httptest.NewRequest(http.MethodGet, "http://backend-internal/", nil)
	r.Host = ""
	handler(httptest.NewRecorder(), r)

	if gotOK {
		t.Fatal("empty host request should not inject authority")
	}
}

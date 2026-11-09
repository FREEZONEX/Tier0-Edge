package appgateway

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"backend/internal/repo"
)

func TestRewriteLocation(t *testing.T) {
	route := &repo.AppGatewayRoute{
		ContainerHost: "172.18.0.1",
		ContainerPort: 30001,
	}

	tests := []struct {
		name         string
		location     string
		originalHost string
		want         string
	}{
		{
			name:         "http 内部 IP 重定向",
			location:     "http://172.18.0.1:30001/path",
			originalHost: "52.77.190.126:30001",
			want:         "https://52.77.190.126:30001/path",
		},
		{
			name:         "https 内部 IP 根路径",
			location:     "https://172.18.0.1:30001/",
			originalHost: "52.77.190.126:30001",
			want:         "https://52.77.190.126:30001/",
		},
		{
			name:         "子域名模式",
			location:     "http://172.18.0.1:30001/dashboard",
			originalHost: "app-001.localhost",
			want:         "https://app-001.localhost/dashboard",
		},
		{
			name:         "清除 _token 参数",
			location:     "http://172.18.0.1:30001/?_token=abc123",
			originalHost: "52.77.190.126:30001",
			want:         "https://52.77.190.126:30001/",
		},
		{
			name:         "清除 _token 带其他参数",
			location:     "http://172.18.0.1:30001/?foo=bar&_token=abc123",
			originalHost: "52.77.190.126:30001",
			want:         "https://52.77.190.126:30001/?foo=bar",
		},
		{
			name:         "不匹配的内部 IP",
			location:     "http://other-host:8080/path",
			originalHost: "52.77.190.126:30001",
			want:         "http://other-host:8080/path",
		},
		{
			name:         "容器其他 172.18.x.x 地址",
			location:     "http://172.18.0.22:5173/dashboard",
			originalHost: "52.77.190.126:30001",
			want:         "https://52.77.190.126:30001/dashboard",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteLocation(tt.location, tt.originalHost, route)
			if got != tt.want {
				t.Errorf("rewriteLocation(%q, %q) = %q, want %q", tt.location, tt.originalHost, got, tt.want)
			}
		})
	}
}

func TestModifyResponse(t *testing.T) {
	route := &repo.AppGatewayRoute{
		ContainerHost: "172.18.0.1",
		ContainerPort: 30001,
	}

	resp := &http.Response{
		Header: http.Header{
			"Location": []string{"http://172.18.0.1:30001/"},
		},
	}

	err := modifyResponse(resp, "52.77.190.126:30001", route)
	if err != nil {
		t.Fatalf("modifyResponse returned error: %v", err)
	}
	if got := resp.Header.Get("Location"); got != "https://52.77.190.126:30001/" {
		t.Errorf("Location = %q, want %q", got, "https://52.77.190.126:30001/")
	}
}

func TestRewriteLocationHTTPMode(t *testing.T) {
	route := &repo.AppGatewayRoute{
		ContainerHost: "172.18.0.1",
		ContainerPort: 30001,
	}

	tests := []struct {
		name         string
		location     string
		originalHost string
		want         string
	}{
		{
			name:         "internal redirect uses public http",
			location:     "http://172.18.0.1:30001/path",
			originalHost: "192.168.31.136:30004",
			want:         "http://192.168.31.136:30004/path",
		},
		{
			name:         "same public host https redirect is downgraded",
			location:     "https://192.168.31.136:30004/",
			originalHost: "192.168.31.136:30004",
			want:         "http://192.168.31.136:30004/",
		},
		{
			name:         "token is removed",
			location:     "http://172.18.0.1:30001/?foo=bar&_token=abc123",
			originalHost: "192.168.31.136:30004",
			want:         "http://192.168.31.136:30004/?foo=bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteLocation(tt.location, tt.originalHost, route, "http")
			if got != tt.want {
				t.Errorf("rewriteLocation(%q, %q, http) = %q, want %q", tt.location, tt.originalHost, got, tt.want)
			}
		})
	}
}

func TestModifyResponseHTTPModeCookie(t *testing.T) {
	route := &repo.AppGatewayRoute{
		ContainerHost: "172.18.0.1",
		ContainerPort: 30001,
	}
	req, err := http.NewRequestWithContext(context.WithValue(context.Background(), "token_from_url", "platform-token"), http.MethodGet, "http://192.168.31.136:30004/?_token=platform-token", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext returned error: %v", err)
	}
	resp := &http.Response{
		Request: req,
		Header: http.Header{
			"Location":   []string{"https://192.168.31.136:30004/"},
			"Set-Cookie": []string{"mes-session=abc; Path=/; HttpOnly; Secure; SameSite=None"},
		},
	}

	err = modifyResponse(resp, "192.168.31.136:30004", route, "http")
	if err != nil {
		t.Fatalf("modifyResponse returned error: %v", err)
	}
	if got := resp.Header.Get("Location"); got != "http://192.168.31.136:30004/" {
		t.Errorf("Location = %q, want %q", got, "http://192.168.31.136:30004/")
	}
	cookies := resp.Header.Values("Set-Cookie")
	if len(cookies) != 2 {
		t.Fatalf("Set-Cookie count = %d, want 2: %v", len(cookies), cookies)
	}
	for _, cookie := range cookies {
		if strings.Contains(cookie, "Secure") {
			t.Errorf("HTTP Set-Cookie should not contain Secure: %q", cookie)
		}
		if strings.Contains(cookie, "SameSite=None") {
			t.Errorf("HTTP Set-Cookie should not contain SameSite=None: %q", cookie)
		}
	}
}

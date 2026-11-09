// 测试目标：httpx.Authority 请求入口解析与 ctx 注入/提取（authority.go）。
// 覆盖的正常路径：代理头开启时 X-Forwarded-Proto/X-Forwarded-Host 生效、TLS 直连
// 判定 https、多级 X-Forwarded-Host 取第一个、IPv6 host 解析、middleware 链路
// WithAuthority/AuthorityFrom 往返。
// 覆盖的异常情况：代理头关闭时回退 r.Host、非法转发 proto 回退 http、空 Host 不注入、
// 未注入 ctx 时 AuthorityFrom 返回 ok=false。

package httpx

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
)

func requestFor(host string, header http.Header) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "http://backend-internal/dashboard", nil)
	r.Host = host
	r.Header = header
	return r
}

func TestAuthority_ProxyHeadersTrusted(t *testing.T) {
	header := http.Header{}
	header.Set("X-Forwarded-Host", "entrance.example.com:8443, inner.proxy:80")
	header.Set("X-Forwarded-Proto", "https")

	scheme, host := Authority(requestFor("10.0.0.2:8080", header), true)
	if scheme != "https" {
		t.Fatalf("scheme = %q, want https", scheme)
	}
	if host != "entrance.example.com:8443" {
		t.Fatalf("host = %q, want first forwarded host", host)
	}
}

func TestAuthority_ProxyHeadersUntrusted(t *testing.T) {
	header := http.Header{}
	header.Set("X-Forwarded-Host", "evil.example.com")
	header.Set("X-Forwarded-Proto", "https")

	// 信任关闭：伪造头被忽略，回退 r.Host
	scheme, host := Authority(requestFor("10.0.0.2:8080", header), false)
	if scheme != "http" {
		t.Fatalf("scheme = %q, want http", scheme)
	}
	if host != "10.0.0.2:8080" {
		t.Fatalf("host = %q, want r.Host", host)
	}
}

func TestAuthority_TLSRequestIsHTTPS(t *testing.T) {
	r := requestFor("entrance.example.com", nil)
	r.TLS = &tls.ConnectionState{}

	scheme, host := Authority(r, true)
	if scheme != "https" {
		t.Fatalf("scheme = %q, want https for TLS request", scheme)
	}
	if host != "entrance.example.com" {
		t.Fatalf("host = %q, want r.Host", host)
	}
}

func TestAuthority_InvalidForwardedProtoFallsBack(t *testing.T) {
	header := http.Header{}
	header.Set("X-Forwarded-Proto", "ftp")

	scheme, _ := Authority(requestFor("h.example.com", header), true)
	if scheme != "http" {
		t.Fatalf("scheme = %q, want http for invalid forwarded proto", scheme)
	}
}

func TestAuthority_IPv6Host(t *testing.T) {
	scheme, host := Authority(requestFor("[::1]:8080", nil), false)
	if scheme != "http" || host != "[::1]:8080" {
		t.Fatalf("scheme/host = %q/%q, want http/[::1]:8080", scheme, host)
	}
}

func TestAuthority_EmptyHost(t *testing.T) {
	scheme, host := Authority(requestFor("", nil), false)
	if scheme != "http" {
		t.Fatalf("scheme = %q, want http", scheme)
	}
	if host != "" {
		t.Fatalf("host = %q, want empty", host)
	}
}

func TestWithAuthorityAndAuthorityFrom(t *testing.T) {
	ctx := WithAuthority(context.Background(), "https", "10.0.0.5:8080")
	scheme, host, ok := AuthorityFrom(ctx)
	if !ok || scheme != "https" || host != "10.0.0.5:8080" {
		t.Fatalf("AuthorityFrom = %q/%q/%v, want https/10.0.0.5:8080/true", scheme, host, ok)
	}

	// scheme 或 host 为空时不注入
	if _, _, ok := AuthorityFrom(WithAuthority(context.Background(), "", "h")); ok {
		t.Fatal("empty scheme should not be stored")
	}
	if _, _, ok := AuthorityFrom(WithAuthority(context.Background(), "http", "")); ok {
		t.Fatal("empty host should not be stored")
	}
	// 未注入的 ctx
	if _, _, ok := AuthorityFrom(context.Background()); ok {
		t.Fatal("missing authority should report ok=false")
	}
}

func TestResolvePublicMQTTWebsocketURL(t *testing.T) {
	// 显式配置优先：外部 WSS 组网原样下发
	t.Setenv("MQTT_WEBSOCKET_PUBLIC_URL", "wss://mqtt.example.com:8084/mqtt")
	if got := ResolvePublicMQTTWebsocketURL(WithAuthority(context.Background(), "http", "10.0.0.5:8088")); got != "wss://mqtt.example.com:8084/mqtt" {
		t.Fatalf("explicit env = %q, want configured url", got)
	}

	// 按当前请求入口动态生成，https 页面用 wss、http 页面用 ws
	t.Setenv("MQTT_WEBSOCKET_PUBLIC_URL", "")
	if got := ResolvePublicMQTTWebsocketURL(WithAuthority(context.Background(), "https", "tier0.example.com")); got != "wss://tier0.example.com/mqtt" {
		t.Fatalf("https authority = %q, want wss://tier0.example.com/mqtt", got)
	}
	if got := ResolvePublicMQTTWebsocketURL(WithAuthority(context.Background(), "http", "10.0.0.5:8088")); got != "ws://10.0.0.5:8088/mqtt" {
		t.Fatalf("http authority = %q, want ws://10.0.0.5:8088/mqtt", got)
	}

	// 无请求入口信息时回退空串，由前端走旧端口拼接
	if got := ResolvePublicMQTTWebsocketURL(context.Background()); got != "" {
		t.Fatalf("missing authority = %q, want empty", got)
	}
}

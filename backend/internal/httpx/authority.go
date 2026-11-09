package httpx

import (
	"context"
	"net/http"
	"os"
	"strings"
)

// ctxKey 沿用 audit 领域的 ctxKey 模式，避免跨包键冲突。
type ctxKey string

const authorityKey ctxKey = "httpx.authority"
const proxyPrefixKey ctxKey = "httpx.proxyPrefix"

// authority 是按请求解析出的对外访问入口：scheme 为实际对外协议，host 含端口。
type authority struct {
	scheme string
	host   string
}

// ForwardedProto 返回请求的实际对外协议：TLS 直连优先；开启代理头信任时采纳
// 首个 X-Forwarded-Proto；其余按 http 处理。从 gateway/http.go 提取复用。
func ForwardedProto(r *http.Request, trustProxyHeaders bool) string {
	if r == nil {
		return "http"
	}
	if r.TLS != nil {
		return "https"
	}
	if trustProxyHeaders {
		switch strings.ToLower(FirstForwardedValue(r.Header.Get("X-Forwarded-Proto"))) {
		case "https":
			return "https"
		case "http":
			return "http"
		}
	}
	return "http"
}

// ForwardedHost 返回请求的实际对外 host（含端口）：开启代理头信任时优先取首个
// X-Forwarded-Host，否则回退 r.Host。从 gateway/http.go 提取复用。
func ForwardedHost(r *http.Request, trustProxyHeaders bool) string {
	if r == nil {
		return ""
	}
	if trustProxyHeaders {
		if host := FirstForwardedValue(r.Header.Get("X-Forwarded-Host")); host != "" {
			return host
		}
	}
	return r.Host
}

// FirstForwardedValue 取逗号分隔转发头的第一段（多级代理链只信任最外层）。
func FirstForwardedValue(value string) string {
	value, _, _ = strings.Cut(value, ",")
	return strings.TrimSpace(value)
}

// Authority 返回请求的实际对外 scheme+host（代理头受 trustProxyHeaders 开关控制）。
func Authority(r *http.Request, trustProxyHeaders bool) (scheme, host string) {
	return ForwardedProto(r, trustProxyHeaders), ForwardedHost(r, trustProxyHeaders)
}

// ResolvePublicMQTTWebsocketURL 返回浏览器应连接的 MQTT over WebSocket 地址：
//  1. MQTT_WEBSOCKET_PUBLIC_URL 显式配置（外部 WSS 组网）时原样返回；
//  2. 否则按当前请求的对外入口生成 <ws|wss>://<host>/mqtt，由 backend 网关 /mqtt
//     固定前缀代理到 EMQX，http/https、IP 更换、外层反代均自适应；
//  3. 无请求入口信息时返回空串，由前端回退到端口拼接的旧行为。
func ResolvePublicMQTTWebsocketURL(ctx context.Context) string {
	if u := strings.TrimSpace(os.Getenv("MQTT_WEBSOCKET_PUBLIC_URL")); u != "" {
		return u
	}
	scheme, host, ok := AuthorityFrom(ctx)
	if !ok {
		return ""
	}
	if scheme == "https" {
		scheme = "wss"
	} else {
		scheme = "ws"
	}
	return scheme + "://" + host + "/mqtt"
}

// WithAuthority 把请求派生的对外入口存入 ctx（由 requestauthority 中间件注入）；
// scheme/host 任一为空时不注入。
func WithAuthority(ctx context.Context, scheme, host string) context.Context {
	if strings.TrimSpace(scheme) == "" || strings.TrimSpace(host) == "" {
		return ctx
	}
	return context.WithValue(ctx, authorityKey, authority{scheme: scheme, host: host})
}

// AuthorityFrom 读取 ctx 中注入的请求派生入口；未注入时 ok=false。
func AuthorityFrom(ctx context.Context) (scheme, host string, ok bool) {
	if ctx == nil {
		return "", "", false
	}
	authority, ok := ctx.Value(authorityKey).(authority)
	if !ok || authority.scheme == "" || authority.host == "" {
		return "", "", false
	}
	return authority.scheme, authority.host, true
}

// ProxyPrefixFrom 仅使用已由可信代理中间件校验的对外路径前缀。
func ProxyPrefixFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(proxyPrefixKey).(string)
	return value
}

func WithProxyPrefix(ctx context.Context, value string) context.Context {
	value = strings.TrimRight(FirstForwardedValue(value), "/")
	if value == "" || !strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\?#\r\n\"'`$") || strings.Contains(value, "//") {
		return ctx
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." || part == "." {
			return ctx
		}
	}
	return context.WithValue(ctx, proxyPrefixKey, value)
}

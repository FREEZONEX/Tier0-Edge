package gateway

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"backend/internal/httpx"

	"github.com/gorilla/websocket"
	"github.com/zeromicro/go-zero/core/logx"
)

// mqttWebSocketPath 是浏览器 MQTT over WebSocket 的固定入口路径，与 EMQX WS listener 的
// endpoint 一致。TLS 由 backend HTTPS listener 或外层反代终止，浏览器 ws/wss 跟随页面协议，
// 因此 EMQX 的 8083/8084 不再需要直接暴露给浏览器。
const mqttWebSocketPath = "/mqtt"

// tryMQTTWebSocket 把 GET /mqtt 的 WebSocket 升级请求字节透传代理到 EMQX 内部明文 WS
// listener（Gateway.MqttWsUpstreamUrl）。CONNECT 鉴权仍由 EMQX HTTP 回调 backend
// /api/core/mqtt/auth 完成，代理层只转发连接，与浏览器直连 8083 的安全语义一致。
func (g *HTTPGateway) tryMQTTWebSocket(w http.ResponseWriter, r *http.Request) bool {
	if g == nil || r == nil || r.URL.Path != mqttWebSocketPath {
		return false
	}
	if r.Method != http.MethodGet || !websocket.IsWebSocketUpgrade(r) {
		return false
	}
	upstreamValue := strings.TrimSpace(g.config.MqttWsUpstreamUrl)
	if upstreamValue == "" {
		return false
	}
	upstreamURL, err := url.Parse(upstreamValue)
	if err != nil || (upstreamURL.Scheme != "ws" && upstreamURL.Scheme != "wss") || upstreamURL.Host == "" {
		logx.WithContext(r.Context()).Errorf("[mqtt-ws] invalid upstream url %q", upstreamValue)
		http.Error(w, "mqtt websocket upstream unavailable", http.StatusBadGateway)
		return true
	}
	upstreamURL.RawQuery = r.URL.RawQuery

	// 先校验同源再拨上游：跨站请求必须在消耗 EMQX 连接资源之前被拒绝。
	if !webSocketSameOrigin(r, g.config.TrustProxyHeaders) {
		http.Error(w, "cross-origin websocket denied", http.StatusForbidden)
		return true
	}

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second
	dialer.Subprotocols = websocket.Subprotocols(r)
	header := http.Header{}
	if origin := strings.TrimSpace(r.Header.Get("Origin")); origin != "" {
		header.Set("Origin", origin)
	}
	upstream, response, err := dialer.DialContext(r.Context(), upstreamURL.String(), header)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		logx.WithContext(r.Context()).Errorf("[mqtt-ws] dial upstream %s failed: %v", upstreamURL.Redacted(), err)
		http.Error(w, "mqtt websocket upstream unavailable", http.StatusBadGateway)
		return true
	}
	defer upstream.Close()

	upgrader := websocket.Upgrader{
		HandshakeTimeout: 10 * time.Second,
		CheckOrigin: func(request *http.Request) bool {
			return webSocketSameOrigin(request, g.config.TrustProxyHeaders)
		},
	}
	if protocol := upstream.Subprotocol(); protocol != "" {
		upgrader.Subprotocols = []string{protocol}
	}
	client, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return true
	}
	defer client.Close()
	client.SetReadLimit(8 << 20)
	upstream.SetReadLimit(8 << 20)

	done := make(chan struct{}, 2)
	go relayWebSocketMessages(upstream, client, done)
	go relayWebSocketMessages(client, upstream, done)
	<-done
	return true
}

// relayWebSocketMessages 把 src 的消息原样写入 dst，任一方向出错即结束；
// 两侧连接的 defer Close 会解除另一方向的阻塞。
func relayWebSocketMessages(dst, src *websocket.Conn, done chan<- struct{}) {
	defer func() { done <- struct{}{} }()
	for {
		messageType, data, err := src.ReadMessage()
		if err != nil {
			return
		}
		if err := dst.WriteMessage(messageType, data); err != nil {
			return
		}
	}
}

// webSocketSameOrigin 要求 WS 握手的 Origin 与当前请求的对外 scheme+host 一致
// （代理头受 trustProxyHeaders 开关控制），防止跨站劫持平台入口的 WebSocket。
func webSocketSameOrigin(r *http.Request, trustProxyHeaders bool) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	return strings.EqualFold(parsed.Scheme, httpx.ForwardedProto(r, trustProxyHeaders)) &&
		strings.EqualFold(parsed.Host, httpx.ForwardedHost(r, trustProxyHeaders))
}

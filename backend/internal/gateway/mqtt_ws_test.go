package gateway

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/config"

	"github.com/gorilla/websocket"
)

// newMQTTEchoUpstream 启动一个模拟 EMQX WS listener 的 echo 上游：接受 mqtt 子协议，
// 原样回显消息，并记录收到的 query。
func newMQTTEchoUpstream(t *testing.T, gotQuery *string) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{
		Subprotocols: []string{"mqtt"},
		CheckOrigin:  func(r *http.Request) bool { return true },
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*gotQuery = r.URL.RawQuery
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(messageType, data); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func mqttGatewayUpstream(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http") + "/mqtt"
}

func TestTryMQTTWebSocketPassthrough(t *testing.T) {
	var gotQuery string
	upstream := newMQTTEchoUpstream(t, &gotQuery)

	g := &HTTPGateway{config: config.GatewayConf{MqttWsUpstreamUrl: mqttGatewayUpstream(upstream.URL) + "?ignored=1"}}
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.tryMQTTWebSocket(w, r) {
			http.Error(w, "not handled", http.StatusNotFound)
		}
	}))
	defer front.Close()

	header := http.Header{}
	header.Set("Origin", front.URL)
	header.Set("Sec-WebSocket-Protocol", "mqtt")
	conn, resp, err := websocket.DefaultDialer.Dial(strings.Replace(front.URL, "http", "ws", 1)+"/mqtt?keepalive=30", header)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		t.Fatalf("dial via gateway: %v", err)
	}
	defer conn.Close()

	if got := conn.Subprotocol(); got != "mqtt" {
		t.Fatalf("subprotocol = %q, want mqtt", got)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	messageType, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if messageType != websocket.BinaryMessage || string(data) != "ping" {
		t.Fatalf("echo = (%d, %q), want binary ping", messageType, data)
	}
	if gotQuery != "keepalive=30" {
		t.Fatalf("upstream query = %q, want keepalive=30", gotQuery)
	}
}

func TestTryMQTTWebSocketSkipsNonUpgrade(t *testing.T) {
	g := &HTTPGateway{config: config.GatewayConf{MqttWsUpstreamUrl: "ws://emqx:8083/mqtt"}}

	plain := httptest.NewRequest(http.MethodGet, "/mqtt", nil)
	if g.tryMQTTWebSocket(httptest.NewRecorder(), plain) {
		t.Fatal("plain GET /mqtt should not be handled")
	}

	other := httptest.NewRequest(http.MethodGet, "/mqtt/extra", nil)
	other.Header.Set("Connection", "Upgrade")
	other.Header.Set("Upgrade", "websocket")
	if g.tryMQTTWebSocket(httptest.NewRecorder(), other) {
		t.Fatal("non-/mqtt path should not be handled")
	}
}

func TestTryMQTTWebSocketDisabledWithoutUpstream(t *testing.T) {
	g := &HTTPGateway{config: config.GatewayConf{}}
	r := httptest.NewRequest(http.MethodGet, "/mqtt", nil)
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	r.Header.Set("Sec-WebSocket-Version", "13")
	if g.tryMQTTWebSocket(httptest.NewRecorder(), r) {
		t.Fatal("empty upstream should fall through")
	}
}

func TestTryMQTTWebSocketRejectsCrossOrigin(t *testing.T) {
	var gotQuery string
	upstream := newMQTTEchoUpstream(t, &gotQuery)
	g := &HTTPGateway{config: config.GatewayConf{MqttWsUpstreamUrl: mqttGatewayUpstream(upstream.URL)}}
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.tryMQTTWebSocket(w, r)
	}))
	defer front.Close()

	header := http.Header{}
	header.Set("Origin", "http://evil.example.com")
	_, resp, err := websocket.DefaultDialer.Dial(strings.Replace(front.URL, "http", "ws", 1)+"/mqtt", header)
	if err == nil {
		t.Fatal("cross-origin handshake should fail")
	}
	if resp != nil {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
	}
	if gotQuery != "" {
		t.Fatal("cross-origin request must not dial upstream")
	}
}

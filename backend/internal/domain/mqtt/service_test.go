package mqtt

// 测试目标：MQTT 服务（service.go）的纯逻辑函数。
// 覆盖的正常路径：allowResult、内部用户/客户端 ID 识别、edge 节点主题权限、
// clientTypeValue/creatableClientTypeValue/clientTypeName、clientResp 字段。
// 覆盖的异常情况：非法 action/主题、未知类型回退 device。
// DB 路径（Auth/ACL/ListClients/GenerateClient 等）依赖 repo，见 service_db_test.go。

import (
	"testing"
	"time"

	"backend/internal/repo"
)

func TestAllowResult(t *testing.T) {
	if allowResult(true) != "allow" || allowResult(false) != "deny" {
		t.Fatal("allowResult mismatch")
	}
}

func TestAllowInternalMqttUser(t *testing.T) {
	// secrets.AllowInternalMQTTUser 由 secrets 包实现，这里仅验证用户名快捷判断
	for _, name := range []string{"backend", "sourceflow", "eventflow"} {
		if !allowInternalMqttUsername(name) {
			t.Fatalf("internal user %q not recognized", name)
		}
	}
	if allowInternalMqttUsername("external") {
		t.Fatal("external user recognized as internal")
	}
	// TrimSpace 后再比较，因此带空白也识别
	if !allowInternalMqttUsername(" backend ") {
		t.Fatal("whitespace username should be trimmed to match")
	}
	if allowInternalMqttUsername(" backend-extra ") {
		t.Fatal("non-exact username recognized as internal")
	}
}

func TestAllowInternalMqttClientID(t *testing.T) {
	for _, id := range []string{"node-red-abc", "eventflow-1", "nodered-x", "edge_server_1"} {
		if !allowInternalMqttClientID(id) {
			t.Fatalf("internal clientID %q not recognized", id)
		}
	}
	if allowInternalMqttClientID("my-client") {
		t.Fatal("external clientID recognized as internal")
	}
}

func TestEdgeNodeTopicAllowed(t *testing.T) {
	authID := int64(42)

	// Retired business synchronization topics must no longer be authorized.
	for _, topic := range []string{"cloudsync/42/meta", "cloudsync/42/data", "cloudsync/42/uns-scope", "cloudsync/42/user-snapshot/request", "cloudsync/42/user-sync/request", "cloudsync/42/user-sync/result", "cloudsync/42/user-snapshot/response/1"} {
		for _, action := range []string{"publish", "subscribe"} {
			if edgeNodeTopicAllowed(authID, topic, action) {
				t.Fatalf("retired topic %q authorized for %s", topic, action)
			}
		}
	}

	// fleet/v1/{id}/ 协议
	for _, kind := range []string{"hello", "heartbeat", "snapshot", "commandAck", "commandResult", "disconnect"} {
		if !edgeNodeTopicAllowed(authID, "fleet/v1/42/"+kind, "publish") {
			t.Fatalf("fleet publish %q should be allowed", kind)
		}
	}
	for _, kind := range []string{"helloAck", "error", "snapshotRequest", "command", "disconnect"} {
		if !edgeNodeTopicAllowed(authID, "fleet/v1/42/"+kind, "subscribe") {
			t.Fatalf("fleet subscribe %q should be allowed", kind)
		}
	}
	if edgeNodeTopicAllowed(authID, "fleet/v1/42/command", "publish") {
		t.Fatal("fleet command publish should be denied")
	}
	if edgeNodeTopicAllowed(authID, "fleet/v1/42/heartbeat", "subscribe") {
		t.Fatal("fleet heartbeat subscribe should be denied")
	}
	if edgeNodeTopicAllowed(authID, "fleet/v1/other/hello", "publish") {
		t.Fatal("other authID topic should be denied")
	}
	if edgeNodeTopicAllowed(authID, "unrelated/topic", "publish") {
		t.Fatal("unrelated topic should be denied")
	}
	// authID 0 拒绝
	if edgeNodeTopicAllowed(0, "cloudsync/0/meta", "publish") {
		t.Fatal("zero authID should be denied")
	}
	// 空白 topic 安全
	if edgeNodeTopicAllowed(authID, "", "publish") {
		t.Fatal("empty topic should be denied")
	}
}

func TestClientTypeValue(t *testing.T) {
	if clientTypeValue("", true) != repo.ConnectClientTypeDevice {
		t.Fatal("empty with allowEmpty should default device")
	}
	if clientTypeValue("all", true) != 0 {
		t.Fatal("all should map to 0 when allowEmpty")
	}
	if clientTypeValue("all", false) != repo.ConnectClientTypeDevice {
		t.Fatal("all without allowEmpty should default device")
	}
	if clientTypeValue("device", false) != repo.ConnectClientTypeDevice ||
		clientTypeValue("1", false) != repo.ConnectClientTypeDevice {
		t.Fatal("device mapping")
	}
	if clientTypeValue("flow", false) != repo.ConnectClientTypeFlow ||
		clientTypeValue("2", false) != repo.ConnectClientTypeFlow {
		t.Fatal("flow mapping")
	}
	if clientTypeValue("edge", false) != repo.ConnectClientTypeEdge ||
		clientTypeValue("edgeNode", false) != repo.ConnectClientTypeEdge ||
		clientTypeValue("3", false) != repo.ConnectClientTypeEdge {
		t.Fatal("edge mapping")
	}
	if clientTypeValue("system", false) != repo.ConnectClientTypeSystem ||
		clientTypeValue("4", false) != repo.ConnectClientTypeSystem {
		t.Fatal("system mapping")
	}
	if clientTypeValue("bogus", false) != repo.ConnectClientTypeDevice {
		t.Fatal("unknown should default device")
	}
}

func TestCreatableClientTypeValue(t *testing.T) {
	if creatableClientTypeValue("system") != repo.ConnectClientTypeSystem {
		t.Fatal("system should stay system")
	}
	if creatableClientTypeValue("flow") != repo.ConnectClientTypeDevice {
		t.Fatal("flow should collapse to device for creatable clients")
	}
	if creatableClientTypeValue("") != repo.ConnectClientTypeDevice {
		t.Fatal("empty should be device")
	}
}

func TestClientTypeName(t *testing.T) {
	if clientTypeName(repo.ConnectClientTypeFlow) != "flow" ||
		clientTypeName(repo.ConnectClientTypeEdge) != "edgeNode" ||
		clientTypeName(repo.ConnectClientTypeSystem) != "system" ||
		clientTypeName(repo.ConnectClientTypeDevice) != "device" ||
		clientTypeName(99) != "device" {
		t.Fatal("clientTypeName mismatch")
	}
}

func TestClientResp(t *testing.T) {
	item := repo.ConnectClient{
		ID:                          7,
		Name:                        "c1",
		Type:                        repo.ConnectClientTypeEdge,
		Password:                    "pw",
		ClientID:                    "cid",
		Username:                    "user",
		IP:                          "1.2.3.4",
		ConnectedTime:               123,
		ClientIDRandomSuffixEnabled: true,
	}
	resp := clientResp(item)
	if resp["id"] != int64(7) || resp["type"] != "edgeNode" || resp["typeCode"] != repo.ConnectClientTypeEdge {
		t.Fatalf("clientResp = %v", resp)
	}
	if resp["clientIdRandomSuffixEnabled"] != true {
		t.Fatal("random suffix flag missing")
	}
	_ = time.Now()
}

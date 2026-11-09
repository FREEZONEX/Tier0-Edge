package mqtt

// 测试目标：open api key 会话解析（open_api_key.go）的纯函数部分。
// 覆盖的正常路径：open 用户名/客户端 ID 解析、legacy 用户名与客户端 ID 解析、
// 会话 key 拼接、action→资源映射、资源键列表转换。
// 覆盖的异常情况：格式非法返回零值/空串；未知 action 返回空资源。

import (
	"testing"
)

func TestParseOpenUsername(t *testing.T) {
	workspaceID, isOpen := parseOpenUsername("1&open")
	if !isOpen || workspaceID != 1 {
		t.Fatalf("parseOpenUsername(1&open) = %d, %v", workspaceID, isOpen)
	}
	workspaceID, isOpen = parseOpenUsername("enterprise&open")
	if !isOpen || workspaceID != 0 {
		t.Fatalf("parseOpenUsername(enterprise&open) = %d, %v", workspaceID, isOpen)
	}
	if _, isOpen := parseOpenUsername("1&client"); isOpen {
		t.Fatal("non-open suffix should not be open")
	}
	if _, isOpen := parseOpenUsername("1"); isOpen {
		t.Fatal("single part should not be open")
	}
	if _, isOpen := parseOpenUsername("1&open&extra"); isOpen {
		t.Fatal("three parts should not be open")
	}
	if _, isOpen := parseOpenUsername(""); isOpen {
		t.Fatal("empty should not be open")
	}
}

func TestParseOpenClientID(t *testing.T) {
	workspaceID, random := parseOpenClientID("1&client-abc")
	if workspaceID != 1 || random != "client-abc" {
		t.Fatalf("parseOpenClientID = %d, %q", workspaceID, random)
	}
	workspaceID, random = parseOpenClientID(" 1 & client-abc ")
	if workspaceID != 1 || random != "client-abc" {
		t.Fatalf("parseOpenClientID trimmed = %d, %q", workspaceID, random)
	}
	if workspaceID, random := parseOpenClientID("bad"); workspaceID != 0 || random != "" {
		t.Fatalf("parseOpenClientID(bad) = %d, %q", workspaceID, random)
	}
}

func TestIsLegacyOpenUsername(t *testing.T) {
	if !isLegacyOpenUsername("enterprise&open") {
		t.Fatal("enterprise&open should be legacy")
	}
	if !isLegacyOpenUsername("ENTERPRISE&OPEN") {
		t.Fatal("case-insensitive legacy check")
	}
	if isLegacyOpenUsername("1&open") {
		t.Fatal("workspace username should not be legacy")
	}
}

func TestParseLegacyOpenClientID(t *testing.T) {
	if parseLegacyOpenClientID("enterprise&client-1") != "client-1" {
		t.Fatal("legacy client id parse failed")
	}
	if parseLegacyOpenClientID("1&client-1") != "" {
		t.Fatal("non-legacy client id should be empty")
	}
	if parseLegacyOpenClientID("bad") != "" {
		t.Fatal("malformed client id should be empty")
	}
}

func TestOpenSessionKey(t *testing.T) {
	if key := openSessionKey(1, "client-1"); key != "cache:uns:emqx:open:1:client-1" {
		t.Fatalf("openSessionKey = %q", key)
	}
}

func TestOpenAPIRequiredResource(t *testing.T) {
	if openAPIRequiredResource("publish") != resourceUNSWrite ||
		openAPIRequiredResource("pub") != resourceUNSWrite {
		t.Fatal("publish mapping")
	}
	if openAPIRequiredResource("subscribe") != resourceUNSRead ||
		openAPIRequiredResource("sub") != resourceUNSRead {
		t.Fatal("subscribe mapping")
	}
	if openAPIRequiredResource("") != "" || openAPIRequiredResource("other") != "" {
		t.Fatal("unknown action should map to empty")
	}
}

func TestHasResourceKey(t *testing.T) {
	resources := []string{"uns.read", "uns.write"}
	if !hasResourceKey(resources, "uns.read") {
		t.Fatal("matching key not found")
	}
	if hasResourceKey(resources, "uns.manage") {
		t.Fatal("non-matching key found")
	}
	if hasResourceKey(nil, "uns.read") {
		t.Fatal("nil resources should not match")
	}
}

func TestResourceKeyList(t *testing.T) {
	out := resourceKeyList(map[string]struct{}{"uns.read": {}, "  ": {}, "uns.write": {}})
	if len(out) != 2 {
		t.Fatalf("resourceKeyList = %v", out)
	}
	seen := map[string]bool{}
	for _, key := range out {
		seen[key] = true
	}
	if !seen["uns.read"] || !seen["uns.write"] {
		t.Fatalf("resourceKeyList = %v", out)
	}
	if len(resourceKeyList(nil)) != 0 {
		t.Fatal("nil resources should produce empty list")
	}
}

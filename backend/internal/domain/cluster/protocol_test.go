// protocol_test.go 覆盖 cluster 协议层纯函数：
// 同步令牌编解码（parseSyncToken/splitTokenMetadata/appendTokenMetadata）、
// MQTT 主题构造、URL/MQTT broker 归一化、路径与枚举归一化等。
// 正常路径：令牌各分段格式（裸 authID / id&pwd / workspace&authID&pwd /
//           带元数据后缀）与元数据往返、标准 URL/broker/路径输入。
// 异常情况：空令牌、非法 workspace/authID、非法元数据、含通配符主题段、
//           非法 scheme 的 URL、未知枚举名、超长 eventID 等。
package cluster

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSyncToken_BareAuthID(t *testing.T) {
	parsed, err := parseSyncToken("  mqtt-9  ")
	require.NoError(t, err)
	assert.Equal(t, "mqtt-9", parsed.ClientID)
	assert.Equal(t, "mqtt-9", parsed.MqttAuthID)
	assert.Equal(t, "mqtt-9&edgeNode", parsed.Username)
	assert.Equal(t, "", parsed.SignedPassword)
	assert.Equal(t, syncModeEdgeEdge, parsed.SyncMode)
	assert.Equal(t, int64(0), parsed.WorkspaceID)
	assert.Equal(t, defaultTopicRoot, parsed.TopicRoot)
}

func TestParseSyncToken_AuthIDWithPassword(t *testing.T) {
	parsed, err := parseSyncToken("mqtt-9&secret")
	require.NoError(t, err)
	assert.Equal(t, "mqtt-9", parsed.ClientID)
	assert.Equal(t, "mqtt-9", parsed.MqttAuthID)
	assert.Equal(t, "secret", parsed.SignedPassword)
	assert.Equal(t, syncModeEdgeEdge, parsed.SyncMode)
}

func TestParseSyncToken_CloudEdgeWorkspaceAuthPassword(t *testing.T) {
	parsed, err := parseSyncToken("42&mqtt-9&p@ss")
	require.NoError(t, err)
	assert.Equal(t, int64(42), parsed.WorkspaceID)
	assert.Equal(t, "mqtt-9", parsed.MqttAuthID)
	assert.Equal(t, "42&mqtt-9", parsed.ClientID)
	assert.Equal(t, "42&mqtt-9&edgeNode", parsed.Username)
	assert.Equal(t, "p@ss", parsed.SignedPassword)
	assert.Equal(t, syncModeCloudEdge, parsed.SyncMode)
}

func TestParseSyncToken_PasswordContainsAmpersand(t *testing.T) {
	parsed, err := parseSyncToken("42&mqtt-9&a&b&c")
	require.NoError(t, err)
	assert.Equal(t, int64(42), parsed.WorkspaceID)
	assert.Equal(t, "mqtt-9", parsed.MqttAuthID)
	assert.Equal(t, "a&b&c", parsed.SignedPassword)
}

func TestParseSyncToken_Invalid(t *testing.T) {
	cases := []string{
		"",
		"   ",
		"42&",             // workspace 无 authID
		"0&mqtt&pwd",      // workspace <= 0
		"-3&mqtt&pwd",     // workspace <= 0
		"abc&mqtt&pwd",    // workspace 非数字
		"&pwd",            // authID 段为空
		"mqtt&  ",         // 密码段为空
		"mqtt/id&pwd",     // authID 含非法字符
		"mqtt+id&pwd",     // authID 含 +
		// 注意：# 是 metadata 分隔符（splitTokenMetadata），不是 authID 非法字符；
		// authID 含 # 的语义由 metadata 用例覆盖，此处不重复。
	}
	for _, token := range cases {
		_, err := parseSyncToken(token)
		require.ErrorIs(t, err, ErrInvalid, "token %q should be invalid", token)
	}
}

func TestParseSyncToken_MetadataDriven(t *testing.T) {
	meta := tokenMetadata{
		Version:   1,
		SyncMode:  "cloudEdge",
		NodeID:    "edge.fn_abc",
		NodeName:  "edge-1",
		ClientID:  "42&mqtt-9",
		Username:  "u",
		Password:  "p",
		HTTPBaseURL: "https://c.example.com",
		MqttURL:   "broker.example.com:8883",
		TopicRoot: "/root/",
	}
	token := appendTokenMetadata("anything", meta)
	parsed, err := parseSyncToken(token)
	require.NoError(t, err)
	assert.Equal(t, int64(42), parsed.WorkspaceID)
	assert.Equal(t, "mqtt-9", parsed.MqttAuthID)
	assert.Equal(t, "fn_abc", parsed.EdgeNodeID)
	assert.Equal(t, "edge-1", parsed.EdgeNodeName)
	assert.Equal(t, "u", parsed.Username)
	assert.Equal(t, "p", parsed.SignedPassword)
	assert.Equal(t, syncModeCloudEdge, parsed.SyncMode)
	assert.Equal(t, "https://c.example.com", parsed.HTTPBaseURL)
	assert.Equal(t, "ssl://broker.example.com:8883", parsed.MqttURL)
	assert.Equal(t, "root", parsed.TopicRoot)
}

func TestParseSyncToken_MetadataClientIDMismatch(t *testing.T) {
	// metadata 的 ClientID 非法（workspace 非数字）→ parseTokenClientID 失败 → ErrInvalid。
	meta := tokenMetadata{ClientID: "abc&mqtt"}
	token := appendTokenMetadata("raw", meta)
	_, err := parseSyncToken(token)
	require.ErrorIs(t, err, ErrInvalid)
}

func TestSplitTokenMetadata_NoSeparator(t *testing.T) {
	authToken, meta := splitTokenMetadata("42&mqtt&pwd")
	assert.Equal(t, "42&mqtt&pwd", authToken)
	assert.Zero(t, meta)
}

func TestSplitTokenMetadata_InvalidPayloads(t *testing.T) {
	for _, payload := range []string{"!!not-base64!!", base64.RawURLEncoding.EncodeToString([]byte("{broken"))} {
		raw := "42&mqtt&pwd#" + payload
		authToken, meta := splitTokenMetadata(raw)
		assert.Equal(t, "42&mqtt&pwd", authToken)
		assert.Zero(t, meta)
	}
	authToken, meta := splitTokenMetadata("42&mqtt&pwd#")
	assert.Equal(t, "42&mqtt&pwd", authToken)
	assert.Zero(t, meta)
}

func TestSplitTokenMetadata_ValidMetadata(t *testing.T) {
	meta := tokenMetadata{SyncMode: "edge-edge", NodeID: "edge.x", TopicRoot: "/t/"}
	body, err := json.Marshal(meta)
	require.NoError(t, err)
	raw := "42&mqtt&pwd#" + base64.RawURLEncoding.EncodeToString(body)
	authToken, got := splitTokenMetadata(raw)
	assert.Equal(t, "42&mqtt&pwd", authToken)
	assert.Equal(t, syncModeEdgeEdge, got.SyncMode)
	assert.Equal(t, "edge.x", got.NodeID)
	assert.Equal(t, "t", got.TopicRoot)
}

func TestAppendTokenMetadata_EmptyInput(t *testing.T) {
	assert.Equal(t, "", appendTokenMetadata("", tokenMetadata{NodeName: "x"}))
	assert.Equal(t, "raw", appendTokenMetadata("raw", tokenMetadata{}))
}

func TestAppendTokenMetadata_RoundTrip(t *testing.T) {
	raw := "42&mqtt-9&pwd"
	token := appendTokenMetadata(raw, tokenMetadata{
		SyncMode:    syncModeEdgeEdge,
		NodeID:      "fn_x",
		HTTPBaseURL: "c.example.com",
		TopicRoot:   "/",
	})
	parsed, err := parseSyncToken(token)
	require.NoError(t, err)
	assert.Equal(t, int64(42), parsed.WorkspaceID)
	assert.Equal(t, "mqtt-9", parsed.MqttAuthID)
	assert.Equal(t, "pwd", parsed.SignedPassword)
	assert.Equal(t, "fn_x", parsed.EdgeNodeID)
	assert.Equal(t, "https://c.example.com", parsed.HTTPBaseURL)
	assert.Equal(t, syncModeEdgeEdge, parsed.SyncMode)
	assert.Equal(t, defaultTopicRoot, parsed.TopicRoot)
}

func TestNormalizeSyncMode(t *testing.T) {
	assert.Equal(t, syncModeCloudEdge, normalizeSyncMode("cloudEdge", ""))
	assert.Equal(t, syncModeCloudEdge, normalizeSyncMode("cloud-edge", "x"))
	assert.Equal(t, syncModeCloudEdge, normalizeSyncMode("CLOUD_EDGE", "x"))
	assert.Equal(t, syncModeEdgeEdge, normalizeSyncMode("edgeEdge", ""))
	assert.Equal(t, syncModeEdgeEdge, normalizeSyncMode("edge-edge", "x"))
	assert.Equal(t, syncModeEdgeEdge, normalizeSyncMode("edge_edge", "x"))
	assert.Equal(t, "fallback", normalizeSyncMode("", "fallback"))
	assert.Equal(t, "fallback", normalizeSyncMode("garbage", "fallback"))
}

func TestParsedSyncToken_IsCloudEdge(t *testing.T) {
	var nilToken *parsedSyncToken
	assert.False(t, nilToken.isCloudEdge())
	assert.True(t, (&parsedSyncToken{SyncMode: syncModeCloudEdge}).isCloudEdge())
	assert.False(t, (&parsedSyncToken{SyncMode: syncModeEdgeEdge}).isCloudEdge())
}

func TestParseTokenClientID(t *testing.T) {
	workspaceID, authID, ok := parseTokenClientID("42&mqtt-9")
	assert.True(t, ok)
	assert.Equal(t, int64(42), workspaceID)
	assert.Equal(t, "mqtt-9", authID)

	workspaceID, authID, ok = parseTokenClientID("mqtt-9")
	assert.True(t, ok)
	assert.Equal(t, int64(0), workspaceID)
	assert.Equal(t, "mqtt-9", authID)

	_, _, ok = parseTokenClientID("")
	assert.False(t, ok)
	_, _, ok = parseTokenClientID("abc&mqtt")
	assert.False(t, ok)
	_, _, ok = parseTokenClientID("mqtt/id")
	assert.False(t, ok)
}

func TestBuildEdgeNodeUsername(t *testing.T) {
	assert.Equal(t, "abc&edgeNode", buildEdgeNodeUsername("abc"))
	assert.Equal(t, "", buildEdgeNodeUsername(""))
	assert.Equal(t, "42&mqtt&edgeNode", buildEdgeNodeUsername("42&mqtt"))
}

func TestDefaultSyncModeByClientID(t *testing.T) {
	assert.Equal(t, syncModeCloudEdge, defaultSyncModeByClientID("42&mqtt-9"))
	assert.Equal(t, syncModeEdgeEdge, defaultSyncModeByClientID("mqtt-9"))
	assert.Equal(t, syncModeEdgeEdge, defaultSyncModeByClientID(""))
}

func TestTokenPasswordFromRaw(t *testing.T) {
	assert.Equal(t, "pwd", tokenPasswordFromRaw("42&mqtt&pwd", "42&mqtt"))
	assert.Equal(t, "", tokenPasswordFromRaw("42&mqtt&pwd", "42&other"))
	assert.Equal(t, "", tokenPasswordFromRaw("42&mqtt", "42&mqtt"))
	assert.Equal(t, "", tokenPasswordFromRaw("", "42&mqtt"))
	assert.Equal(t, "", tokenPasswordFromRaw("42&mqtt&pwd", ""))
}

func TestNormalizeEdgeNodeID(t *testing.T) {
	assert.Equal(t, "fn_abc", normalizeEdgeNodeID("edge.fn_abc"))
	// normalizeEdgeNodeID 只去 "edge." 前缀，不做大小写归一化。
	assert.Equal(t, "FN_abc", normalizeEdgeNodeID("Edge.FN_abc"))
	assert.Equal(t, "edge.", normalizeEdgeNodeID("edge."))
	assert.Equal(t, "fn_abc", normalizeEdgeNodeID("fn_abc"))
	assert.Equal(t, "", normalizeEdgeNodeID(""))
}

func TestBuildTopic(t *testing.T) {
	assert.Equal(t, "cloudsync/abc/meta", buildTopic("", "abc", "meta"))
	assert.Equal(t, "cloudsync/abc/meta", buildTopic("/", "abc", "meta"))
	assert.Equal(t, "root/abc/meta", buildTopic("/root/", "abc", "meta"))
}

func TestBuildUserSnapshotResponseTopic(t *testing.T) {
	assert.Equal(t, "root/abc/user-snapshot/response/req-1", buildUserSnapshotResponseTopic("root", "abc", "req-1"))
	assert.Equal(t, "", buildUserSnapshotResponseTopic("root", "abc", "req/1"))
	assert.Equal(t, "", buildUserSnapshotResponseTopic("root", "abc", "req+1"))
}

func TestCleanTopicSegment(t *testing.T) {
	assert.Equal(t, "abc", cleanTopicSegment("abc"))
	assert.Equal(t, "", cleanTopicSegment(""))
	assert.Equal(t, "", cleanTopicSegment("a/b"))
	assert.Equal(t, "", cleanTopicSegment("a+b"))
	assert.Equal(t, "", cleanTopicSegment("a#b"))
}

func TestNormalizeBaseURL(t *testing.T) {
	assert.Equal(t, "", normalizeBaseURL(""))
	assert.Equal(t, "https://example.com", normalizeBaseURL("example.com"))
	assert.Equal(t, "https://example.com", normalizeBaseURL("https://example.com/"))
	assert.Equal(t, "http://a.com/path", normalizeBaseURL("http://a.com/path/"))
}

func TestEndpointFromBaseURL(t *testing.T) {
	assert.Equal(t, "", endpointFromBaseURL("", "api/x"))
	assert.Equal(t, "", endpointFromBaseURL("ftp://x", "api/x"))
	assert.Equal(t, "", endpointFromBaseURL("https://", "api/x"))
	got := endpointFromBaseURL("https://c.example.com", "api/core/uns/cloud-edge-sync/meta")
	assert.Equal(t, "https://c.example.com/api/core/uns/cloud-edge-sync/meta", got)
	// 清理原始查询串与 fragment，且保留 base path（拼接）。
	got = endpointFromBaseURL("https://c.example.com/base?token=1#frag", "api/x")
	assert.Equal(t, "https://c.example.com/base/api/x", got)
}

func TestMetadataEndpointFromBaseURL(t *testing.T) {
	assert.Equal(t, "https://c.example.com/api/core/uns/cloud-edge-sync/meta",
		metadataEndpointFromBaseURL("https://c.example.com"))
}

func TestUserSnapshotEndpointFromBaseURL(t *testing.T) {
	assert.Equal(t, "https://c.example.com/api/core/uns/cloud-edge-sync/user-snapshot",
		userSnapshotEndpointFromBaseURL("https://c.example.com", syncModeCloudEdge))
	assert.Equal(t, "https://c.example.com/api/core/cluster/user-snapshot",
		userSnapshotEndpointFromBaseURL("https://c.example.com", syncModeEdgeEdge))
	assert.Equal(t, "https://c.example.com/api/core/cluster/user-snapshot",
		userSnapshotEndpointFromBaseURL("https://c.example.com", "garbage"))
}

func TestNormalizeMqttBrokers(t *testing.T) {
	assert.Equal(t, "", normalizeMqttBrokers(""))
	assert.Equal(t, "", normalizeMqttBrokers(" ,; "))
	assert.Equal(t, "tcp://a:1883,ssl://b:8883,ssl://c.example.com:8883",
		normalizeMqttBrokers("a:1883;b:8883,c.example.com"))
	assert.Equal(t, "tcp://a:1883", normalizeMqttBroker("a:1883"))
	assert.Equal(t, "ssl://b:8883", normalizeMqttBroker("b:8883"))
	assert.Equal(t, "ssl://c:8883", normalizeMqttBroker("c"))
	assert.Equal(t, "tcp://a:1883", normalizeMqttBroker("tcp://a:1883"))
	assert.Equal(t, "", normalizeMqttBroker(""))
}

func TestParsedSyncToken_ResolveEndpoints(t *testing.T) {
	var nilToken *parsedSyncToken
	assert.Equal(t, "", nilToken.resolveHTTPMetadataEndpoint())
	assert.Equal(t, "", nilToken.resolveHTTPUNSScopeEndpoint())
	assert.Equal(t, "", nilToken.resolveHTTPUserSnapshotEndpoint())

	cloud := &parsedSyncToken{SyncMode: syncModeCloudEdge, HTTPBaseURL: "https://c.example.com"}
	assert.Equal(t, "https://c.example.com/api/core/uns/cloud-edge-sync/meta", cloud.resolveHTTPMetadataEndpoint())
	assert.Equal(t, "", cloud.resolveHTTPUNSScopeEndpoint())
	assert.Equal(t, "https://c.example.com/api/core/uns/cloud-edge-sync/user-snapshot", cloud.resolveHTTPUserSnapshotEndpoint())

	edge := &parsedSyncToken{SyncMode: syncModeEdgeEdge, HTTPBaseURL: "https://e.example.com"}
	assert.Equal(t, "https://e.example.com/api/core/cluster/user-snapshot", edge.resolveHTTPMetadataEndpoint())
	assert.Equal(t, "https://e.example.com/api/core/cluster/user-snapshot", edge.resolveHTTPUNSScopeEndpoint())
}

func TestParsedSyncToken_ResolveMqttBrokers(t *testing.T) {
	assert.Equal(t, "tcp://a:1883", (&parsedSyncToken{}).resolveMqttBrokers("a:1883"))
	assert.Equal(t, "ssl://m:8883", (&parsedSyncToken{MqttURL: "m:8883"}).resolveMqttBrokers("a:1883"))
	var nilToken *parsedSyncToken
	assert.Equal(t, "tcp://a:1883", nilToken.resolveMqttBrokers("a:1883"))
}

func TestCleanPathAndJoinPath(t *testing.T) {
	assert.Equal(t, "a/b/c", cleanPath("/a//b\\c/ "))
	assert.Equal(t, "", cleanPath(""))
	assert.Equal(t, "a/b/c", joinPath("a/", "/b", "", "c"))
	assert.Equal(t, "", joinPath("", " ", "/"))
}

func TestEnumNameAndTopicTypeByEnumName(t *testing.T) {
	assert.Equal(t, "State", enumName(topicTypeState))
	assert.Equal(t, "Action", enumName(topicTypeAction))
	assert.Equal(t, "Metric", enumName(topicTypeMetric))
	assert.Equal(t, "", enumName(0))
	assert.Equal(t, topicTypeState, topicTypeByEnumName("state"))
	assert.Equal(t, topicTypeAction, topicTypeByEnumName(" Action "))
	assert.Equal(t, topicTypeMetric, topicTypeByEnumName("metric"))
	assert.Equal(t, int32(0), topicTypeByEnumName("unknown"))
}

func TestNormalizeEnumSegments(t *testing.T) {
	assert.Equal(t, "a/State/b", normalizeEnumSegments("a/state/state/b"))
	assert.Equal(t, "Action/Metric", normalizeEnumSegments("action/metric"))
	assert.Equal(t, "plain/path", normalizeEnumSegments("plain/path"))
	assert.Equal(t, "", normalizeEnumSegments(""))
}

func TestInferTopicTypeFromPath(t *testing.T) {
	assert.Equal(t, topicTypeAction, inferTopicTypeFromPath("x/action"))
	assert.Equal(t, topicTypeMetric, inferTopicTypeFromPath("metric"))
	assert.Equal(t, int32(0), inferTopicTypeFromPath("a/b"))
}

func TestEnsureFileEnumParent(t *testing.T) {
	assert.Equal(t, "a/b/Action/c", ensureFileEnumParent("a/b/c", topicTypeAction))
	assert.Equal(t, "a/State/c", ensureFileEnumParent("a/state/c", topicTypeState))
	assert.Equal(t, "Metric/x", ensureFileEnumParent("x", topicTypeMetric))
	assert.Equal(t, "", ensureFileEnumParent("", topicTypeAction))
	// 未知枚举：原样返回。
	assert.Equal(t, "a/b/c", ensureFileEnumParent("a/b/c", 0))
}

func TestTrimLeadingDuplicateEnum(t *testing.T) {
	assert.Equal(t, "b", trimLeadingDuplicateEnum("a/State", "/state/b"))
	assert.Equal(t, "b", trimLeadingDuplicateEnum("a/State", "/State/b"))
	assert.Equal(t, "/b", trimLeadingDuplicateEnum("a/State", "/b"))
	assert.Equal(t, "", trimLeadingDuplicateEnum("a/State", ""))
}

func TestMetaSnapshot_AuthID(t *testing.T) {
	snapshot := metaSnapshot{MqttAuthID: " mqtt-9 ", DeprecatedClientKey: "old"}
	assert.Equal(t, "mqtt-9", snapshot.AuthID())
	snapshot.MqttAuthID = ""
	assert.Equal(t, "old", snapshot.AuthID())
	snapshot.DeprecatedClientKey = " "
	assert.Equal(t, "", snapshot.AuthID())
}

func TestDataMessage_AuthID(t *testing.T) {
	msg := dataMessage{MqttAuthID: "mqtt-9", DeprecatedClientKey: "old"}
	assert.Equal(t, "mqtt-9", msg.AuthID())
	msg.MqttAuthID = ""
	assert.Equal(t, "old", msg.AuthID())
}

// helper_test.go — uns OpenAPI 助手纯函数测试。
//
// 测试目标：helper.go 中无 DB/外部依赖的转换与归一化函数。
// 正常路径：SaveCommand 构建、schema 字段 JSON 序列化、node data 归一化、
//
//	topic 归一化、bool 值解析。
//
// 异常情况：非法 bool 值、缺失必填字段、nil 值解析。
package uns

import (
	"context"
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestBuildOpenapiUnsNodeSaveCommand(t *testing.T) {
	ctx := context.Background()
	req := &types.OpenapiUnsNodeSaveReq{
		NodeId:           42,
		ParentId:         7,
		Name:             "temp",
		DisplayName:      "Temp",
		Description:      "desc",
		Alias:            "temp-alias",
		Namespace:        "plant/line/State/temp",
		NodeType:         "file",
		TopicType:        "State",
		Fields:           []types.SchemaField{{Name: "v", Type: "float"}},
		ExtendProperties: `{"k":"v"}`,
		LabelIds:         []int64{1, 2},
		AssetFileIds:     []int64{3},
		EnableHistory:    boolPtr(true),
		WithFlow:         true,
		MockData:         true,
	}
	cmd := buildOpenapiUnsNodeSaveCommand(ctx, req)
	require.Equal(t, int64(42), cmd.ID)
	require.Equal(t, int64(7), cmd.ParentID)
	require.Equal(t, "temp", cmd.Name)
	require.Equal(t, "plant/line/State/temp", cmd.Namespace)
	require.Equal(t, "file", cmd.NodeType)
	require.Equal(t, `[{"name":"v","type":"float","unit":""}]`, cmd.Schema)
	require.Equal(t, `{"k":"v"}`, cmd.ExtendProperties)
	require.Equal(t, []int64{1, 2}, cmd.LabelIDs)
	require.Equal(t, []int64{3}, cmd.AssetFileIDs)
	require.True(t, *cmd.EnableHistory)
	// AddFlow = WithFlow || AddFlow || MockData
	require.True(t, cmd.AddFlow)
	require.True(t, cmd.MockData)
	// UserID 来自 ctx Subject。
	require.Zero(t, cmd.UserID)
}

func TestBuildOpenapiUnsNodeSaveCommand_UserIDFromCtx(t *testing.T) {
	req := &types.OpenapiUnsNodeSaveReq{Name: "x", NodeType: "file", TopicType: "State"}
	cmd := buildOpenapiUnsNodeSaveCommand(withSubjectUser(t, 99), req)
	require.Equal(t, int64(99), cmd.UserID)
}

func TestBuildOpenapiUnsNodeSaveCommand_AddFlowVariants(t *testing.T) {
	// MockData=false, WithFlow=false, AddFlow=true → AddFlow=true
	cmd := buildOpenapiUnsNodeSaveCommand(context.Background(), &types.OpenapiUnsNodeSaveReq{
		Name: "x", NodeType: "file", AddFlow: true,
	})
	require.True(t, cmd.AddFlow)

	// 全 false → AddFlow=false
	cmd = buildOpenapiUnsNodeSaveCommand(context.Background(), &types.OpenapiUnsNodeSaveReq{
		Name: "x", NodeType: "file",
	})
	require.False(t, cmd.AddFlow)
	require.False(t, cmd.MockData)
}

func TestBuildOpenapiUnsNodeSaveCommand_EnableHistoryNil(t *testing.T) {
	cmd := buildOpenapiUnsNodeSaveCommand(context.Background(), &types.OpenapiUnsNodeSaveReq{
		Name: "x", NodeType: "file",
	})
	require.Nil(t, cmd.EnableHistory)
}

func TestNormalizeOpenapiTopic(t *testing.T) {
	require.Equal(t, "", normalizeOpenapiTopic(""))
	require.Equal(t, "", normalizeOpenapiTopic("  "))
	require.Equal(t, "a/b", normalizeOpenapiTopic("/a/b/"))
	require.Equal(t, "a/b", normalizeOpenapiTopic(" a/b "))
}

func TestHasOpenapiWildcard(t *testing.T) {
	require.False(t, hasOpenapiWildcard("a/b"))
	require.True(t, hasOpenapiWildcard("a/+/b"))
	require.True(t, hasOpenapiWildcard("a/#"))
}

func TestCompactOpenapiTopicPatterns(t *testing.T) {
	patterns := []string{
		"+/+/+/+/+/+/Metric/Telemetry",
		"SmartCity/+/+/+/+/+/+/Metric/Telemetry",
		"SmartCity/+/+/+/+/+/Metric/Telemetry",
		"SmartCity/+/+/+/+/Metric/Telemetry",
		"+/+/+/+/+/+/Metric/Daily",
		"SmartCity/+/+/+/+/+/+/Metric/Daily",
		"SmartCity/+/+/+/+/+/Metric/Daily",
		"SmartCity/+/+/+/+/Metric/Daily",
		"+/+/+/+/+/+/Metric/Monthly",
		"SmartCity/+/+/+/+/+/+/Metric/Monthly",
		"SmartCity/+/+/+/+/+/Metric/Monthly",
		"SmartCity/+/+/+/+/Metric/Monthly",
	}
	compacted := compactOpenapiTopicPatterns(patterns)
	require.Len(t, compacted, 9)
	require.NotContains(t, compacted, "SmartCity/+/+/+/+/+/Metric/Telemetry")
	require.NotContains(t, compacted, "SmartCity/+/+/+/+/+/Metric/Daily")
	require.NotContains(t, compacted, "SmartCity/+/+/+/+/+/Metric/Monthly")
	require.True(t, mqttTopicPatternCovers("+/+/+/+/+/+/Metric/Telemetry", "SmartCity/+/+/+/+/+/Metric/Telemetry"))
	require.False(t, mqttTopicPatternCovers("SmartCity/+/+/+/+/+/Metric/Telemetry", "+/+/+/+/+/+/Metric/Telemetry"))
}

func TestNormalizeOpenapiUnsData(t *testing.T) {
	// 叶子标量原样返回。
	require.Equal(t, "abc", normalizeOpenapiUnsData("abc"))
	require.Equal(t, int64(3), normalizeOpenapiUnsData(int64(3)))

	// map：包含节点时归一化 type/topicType/enableHistory，并删除 persistence。
	input := map[string]any{
		"id":             int64(1),
		"type":           "topic",
		"name":           "t",
		"topicType":      "state",
		"enableHistory":  "1",
		"persistence":    true,
		"nested":         map[string]any{"id": int64(2), "type": "folder", "name": "f"},
		"nonNode":        "keep-me",
		"existingTime":   int64(100),
		"persistenceStr": "x",
	}
	out := normalizeOpenapiUnsData(input).(map[string]any)
	require.Equal(t, "TOPIC", out["type"])
	require.Equal(t, "STATE", out["topicType"])
	require.Equal(t, true, out["enableHistory"])
	_, hasPersistence := out["persistence"]
	require.False(t, hasPersistence)

	nested := out["nested"].(map[string]any)
	require.Equal(t, "PATH", nested["type"])

	// []map[string]any 递归。
	list := []map[string]any{{"id": int64(1), "type": "file", "name": "f"}}
	outList := normalizeOpenapiUnsData(list).([]map[string]any)
	require.Equal(t, "TOPIC", outList[0]["type"])

	// []any 递归。
	anyList := []any{map[string]any{"id": int64(1), "type": "topic", "name": "f"}}
	outAnyList := normalizeOpenapiUnsData(anyList).([]any)
	require.Equal(t, "TOPIC", outAnyList[0].(map[string]any)["type"])
}

func TestNormalizeOpenapiUnsNode_TypeMappings(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"folder", "PATH"}, {"path", "PATH"}, {"1", "PATH"},
		{"file", "TOPIC"}, {"topic", "TOPIC"}, {"2", "TOPIC"},
		{"Folder", "PATH"}, {" Topic ", "TOPIC"}, {"", ""},
	}
	for _, tc := range cases {
		item := map[string]any{"id": 1, "type": tc.in, "name": "n"}
		normalizeOpenapiUnsNode(item)
		require.Equal(t, tc.want, item["type"], "type=%q", tc.in)
	}
}

func TestNormalizeOpenapiUnsNode_TopicTypeAndHistory(t *testing.T) {
	// topicType 转大写；enableHistory 来自 bool 解析；persistence 被删除。
	item := map[string]any{
		"id": 1, "type": "file", "name": "n",
		"topicType": "action", "enableHistory": int64(0), "persistence": true,
	}
	normalizeOpenapiUnsNode(item)
	require.Equal(t, "ACTION", item["topicType"])
	require.Equal(t, false, item["enableHistory"])
	_, has := item["persistence"]
	require.False(t, has)

	// 无 enableHistory 时回退 persistence 值。
	item = map[string]any{"id": 1, "type": "file", "name": "n", "persistence": "true"}
	normalizeOpenapiUnsNode(item)
	require.Equal(t, true, item["enableHistory"])

	// 非节点 map（缺 id/type/name）不改动。
	plain := map[string]any{"a": 1}
	normalizeOpenapiUnsNode(plain)
	require.Equal(t, map[string]any{"a": 1}, plain)
}

func TestIsOpenapiUnsNode(t *testing.T) {
	require.True(t, isOpenapiUnsNode(map[string]any{"id": 1, "type": "file", "name": "n"}))
	require.False(t, isOpenapiUnsNode(map[string]any{"id": 1, "type": "file"}))
	require.False(t, isOpenapiUnsNode(map[string]any{"id": 1, "name": "n"}))
	require.False(t, isOpenapiUnsNode(nil))
}

func TestBoolFromOpenapiValue(t *testing.T) {
	require.True(t, mustBool(t, true))
	require.False(t, mustBool(t, false))
	require.True(t, mustBool(t, 1))
	require.False(t, mustBool(t, 0))
	require.True(t, mustBool(t, int8(1)))
	require.True(t, mustBool(t, int16(1)))
	require.True(t, mustBool(t, int32(1)))
	require.True(t, mustBool(t, int64(1)))
	require.True(t, mustBool(t, uint(1)))
	require.True(t, mustBool(t, uint8(1)))
	require.True(t, mustBool(t, uint16(1)))
	require.True(t, mustBool(t, uint32(1)))
	require.True(t, mustBool(t, uint64(1)))
	require.True(t, mustBool(t, float32(1)))
	require.True(t, mustBool(t, float64(1)))
	require.True(t, mustBool(t, "1"))
	require.True(t, mustBool(t, "true"))
	require.True(t, mustBool(t, "YES"))
	require.True(t, mustBool(t, " enabled "))
	require.False(t, mustBool(t, "0"))
	require.False(t, mustBool(t, "false"))
	require.False(t, mustBool(t, "n"))
	require.False(t, mustBool(t, "disabled"))

	// 无法解析的字符串。
	_, ok := boolFromOpenapiValue("maybe")
	require.False(t, ok)
	_, ok = boolFromOpenapiValue(nil)
	require.False(t, ok)

	// 未知类型走 fmt.Sprint 再解析（[]byte 会打印数字序列，解析失败属预期）。
	_, ok = boolFromOpenapiValue([]byte("true"))
	require.False(t, ok)
}

func TestNormalizeOpenapiUnsNode_NoTypeFieldKeepsValue(t *testing.T) {
	// type 不是 string（如数字）时不归一化。
	item := map[string]any{"id": 1, "type": 2, "name": "n"}
	normalizeOpenapiUnsNode(item)
	require.Equal(t, 2, item["type"])
}

func TestOpenapiHistoryQueryControls(t *testing.T) {
	exact, err := parseOpenapiHistoryCountMode("")
	require.NoError(t, err)
	require.True(t, exact)

	exact, err = parseOpenapiHistoryCountMode("exact")
	require.NoError(t, err)
	require.True(t, exact)

	exact, err = parseOpenapiHistoryCountMode("none")
	require.NoError(t, err)
	require.False(t, exact)

	_, err = parseOpenapiHistoryCountMode("estimated")
	require.Error(t, err)
}

func mustBool(t *testing.T, v any) bool {
	t.Helper()
	out, ok := boolFromOpenapiValue(v)
	require.True(t, ok, "value %v should parse", v)
	return out
}

func boolPtr(v bool) *bool { return &v }

// withSubjectUser 构造带登录用户的 context。
func withSubjectUser(t *testing.T, userID int64) context.Context {
	t.Helper()
	return withUserSubject(userID)
}

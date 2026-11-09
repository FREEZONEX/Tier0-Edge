// openapi_contract_test.go — openapi_contract.go 纯函数测试。
//
// 测试目标：openapi_contract.go 中不依赖 DB/外部服务的纯函数：
//
//	topic 匹配/归一化、节点类型映射、schema/extend 解析、历史时间与聚合参数解析、
//	分页、VQT 载荷归一化、MQTT topic 通配匹配等。
//
// 正常路径：各类映射与解析的常规输入。
// 异常情况：非法聚合函数、非法时间串、非法 interval、越界分页等。
package uns

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"backend/internal/domain/dataingest"
	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestMqttTopicMatches(t *testing.T) {
	require.True(t, mqttTopicMatches("a/b/c", "a/b/c"))
	require.False(t, mqttTopicMatches("a/b/c", "a/b/d"))
	require.True(t, mqttTopicMatches("a/+/c", "a/x/c"))
	require.False(t, mqttTopicMatches("a/+/c", "a/x/y"))
	require.True(t, mqttTopicMatches("a/#", "a/b/c"))
	require.True(t, mqttTopicMatches("a/#", "a"))
	require.False(t, mqttTopicMatches("a/+/c", "a/x/c/d"))
	// 注意生产实现：# 只出现在模式最后一段时命中；"#/x" 形态不属于标准 MQTT 通配。
	require.True(t, mqttTopicMatches("#/x", "a/x"))
	require.True(t, mqttTopicMatches("#", "a/x"))
}

func TestNormalizeOpenapiTopicTrimmed(t *testing.T) {
	require.Equal(t, "x/y", normalizeOpenapiTopic("/x/y/"))
	require.Equal(t, "", normalizeOpenapiTopic("///"))
}

func TestOpenapiItemError(t *testing.T) {
	item := openapiItemError(404, "nope")
	require.Equal(t, int64(404), item["code"])
	require.Equal(t, "nope", item["message"])
}

func TestOpenapiNodeTypeMappings(t *testing.T) {
	require.Equal(t, "TOPIC", openapiExternalNodeType(2))
	require.Equal(t, "PATH", openapiExternalNodeType(1))
	require.Equal(t, "file", openapiInternalNodeType(2))
	require.Equal(t, "folder", openapiInternalNodeType(1))

	require.Equal(t, "STATE", openapiExternalTopicType(1))
	require.Equal(t, "ACTION", openapiExternalTopicType(2))
	require.Equal(t, "METRIC", openapiExternalTopicType(3))
	require.Equal(t, "", openapiExternalTopicType(9))

	require.Equal(t, "State", openapiInternalTopicType(1))
	require.Equal(t, "Action", openapiInternalTopicType(2))
	require.Equal(t, "Metric", openapiInternalTopicType(3))
	require.Equal(t, "", openapiInternalTopicType(9))
}

func TestOpenapiTopicTypeValue(t *testing.T) {
	v, set, err := openapiTopicTypeValue("")
	require.NoError(t, err)
	require.False(t, set)
	require.Zero(t, v)

	for _, in := range []string{"state", "STATE", "1"} {
		v, set, err = openapiTopicTypeValue(in)
		require.NoError(t, err)
		require.True(t, set)
		require.Equal(t, int16(1), v)
	}
	_, _, err = openapiTopicTypeValue("bogus")
	require.Error(t, err)
}

func TestOpenapiTopicCategoryByName(t *testing.T) {
	require.Equal(t, "State", openapiTopicCategoryByName("state"))
	require.Equal(t, "Action", openapiTopicCategoryByName("Action"))
	require.Equal(t, "Metric", openapiTopicCategoryByName(" metric "))
	require.Equal(t, "", openapiTopicCategoryByName("other"))
}

func TestNormalizeNamespaceNodeType(t *testing.T) {
	require.Equal(t, "folder", normalizeNamespaceNodeType("path", false))
	require.Equal(t, "folder", normalizeNamespaceNodeType("dir", false))
	require.Equal(t, "folder", normalizeNamespaceNodeType("1", false))
	require.Equal(t, "file", normalizeNamespaceNodeType("topic", false))
	require.Equal(t, "file", normalizeNamespaceNodeType("object", false))
	require.Equal(t, "folder", normalizeNamespaceNodeType("", true))
	require.Equal(t, "file", normalizeNamespaceNodeType("", false))
	require.Equal(t, "whatever", normalizeNamespaceNodeType("Whatever", false))
}

func TestOpenapiSchemaFields(t *testing.T) {
	// 空 schema。
	require.Empty(t, openapiSchemaFields(nil))

	// 数组形态。
	raw := json.RawMessage(`[{"name":"v","type":"float","unit":"m"},{"name":"x","type":"string"},{"type":"no-name"}]`)
	fields := openapiSchemaFields(raw)
	require.Len(t, fields, 3)
	require.Equal(t, "v", fields[0]["name"])
	require.Equal(t, "float", fields[0]["type"])
	require.Equal(t, "m", fields[0]["unit"])
	require.Equal(t, "x", fields[1]["name"])
	require.Equal(t, "string", fields[1]["type"])
	// 无 unit 时字段 unit 为 nil（fmt.Sprint(nil) 为 "<nil>"）。
	require.Equal(t, "<nil>", fmt.Sprint(fields[1]["unit"]))
	// name 缺失的条目被保留（name 为 "<nil>" 字符串形态）。
	require.Equal(t, "<nil>", fmt.Sprint(fields[2]["name"]))
	require.Equal(t, "no-name", fields[2]["type"])

	// 对象形态 {"fields":[...]}。
	raw = json.RawMessage(`{"fields":[{"name":"a","type":"int"}]}`)
	fields = openapiSchemaFields(raw)
	require.Len(t, fields, 1)
	require.Equal(t, "a", fields[0]["name"])

	// 非法 JSON 不 panic。
	require.Empty(t, openapiSchemaFields(json.RawMessage(`{invalid`)))
}

func TestOpenapiExtendProperties(t *testing.T) {
	require.Empty(t, openapiExtendProperties(nil))
	raw := json.RawMessage(`{"k":123,"s":"v"}`)
	out := openapiExtendProperties(raw)
	require.Equal(t, "123", out["k"])
	require.Equal(t, "v", out["s"])
	require.Empty(t, openapiExtendProperties(json.RawMessage(`bad`)))
}

func TestOpenapiSchemaFieldsJSON(t *testing.T) {
	require.Equal(t, "[]", openapiSchemaFieldsJSON(nil))
	require.Equal(t, `[{"name":"v","type":"float","unit":""}]`,
		openapiSchemaFieldsJSON([]types.SchemaField{{Name: "v", Type: "float"}}))
}

func TestOpenapiStringMapJSON(t *testing.T) {
	require.Equal(t, "{}", openapiStringMapJSON(nil))
	require.Equal(t, `{"a":"1"}`, openapiStringMapJSON(map[string]string{"a": "1"}))
}

func TestOpenapiPayloadPreservation(t *testing.T) {
	cases := []struct {
		name  string
		value any
	}{
		{"allFields", map[string]any{
			"value": float64(42), "timestamp": int64(100), "timeStamp": int64(101),
			"quality": "payload-quality", "_timestamp": int64(102), "_quality": "raw",
			"_ct": int64(103), "_qos": int64(1), "created_time": "original",
			"nested": map[string]any{"timestamp": int64(104)},
		}},
		{"valueWithTimestamp", map[string]any{"value": float64(42), "timeStamp": int64(100)}},
		{"singleValue", map[string]any{"value": float64(1)}},
		{"singleNullValue", map[string]any{"value": nil}},
		{"singleObjectValue", map[string]any{"value": map[string]any{"xxx": float64(2)}}},
		{"scalar", "abc"},
		{"array", []any{map[string]any{"timestamp": int64(100)}}},
		{"emptyObject", map[string]any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, field := range []string{"payload", "data"} {
				out := openapiLatestVQTFromPayload(map[string]any{
					field: tc.value, "timeStamp": int64(200), "quality": "Good",
				})
				require.Equal(t, tc.value, out["value"])
				require.Equal(t, int64(200), out["timeStamp"])
				require.Equal(t, "Good", out["quality"])
			}
			rows := openapiHistoryValuesFromData(map[string]any{
				"list": []map[string]any{{"payload": tc.value, "timestamp": int64(200)}},
			}, openapiHistoryAggregation{})
			require.Len(t, rows, 1)
			require.Equal(t, tc.value, rows[0]["value"])
			require.Equal(t, int64(200), rows[0]["timeStamp"])
		})
	}
}

func TestOpenapiNormalizedScalarPayloadPreservation(t *testing.T) {
	for _, raw := range []string{"opcua-raw-string", `"json-string"`, "42", "true"} {
		t.Run(raw, func(t *testing.T) {
			records, err := dataingest.NormalizePayload(context.Background(), &dataingest.Definition{
				TopicType: 1,
			}, []byte(raw))
			require.NoError(t, err)
			require.Len(t, records, 1)
			payload := records[0].EventPayload
			require.Len(t, payload, 1)
			expected := payload
			require.NotNil(t, expected)
			latest := openapiLatestVQTFromPayload(map[string]any{"payload": payload, "timeStamp": int64(200)})
			require.Equal(t, expected, latest["value"])
			rows := openapiHistoryValuesFromData(map[string]any{
				"list": []map[string]any{{"payload": payload, "timestamp": int64(200)}},
			}, openapiHistoryAggregation{})
			require.Equal(t, expected, rows[0]["value"])
		})
	}
}

func TestOpenapiPayloadQuality(t *testing.T) {
	require.Equal(t, "Bad", openapiPayloadQuality("bad"))
	require.Equal(t, "Bad", openapiPayloadQuality("BAD"))
	require.Equal(t, "Uncertain", openapiPayloadQuality("uncertain"))
	require.Equal(t, "Good", openapiPayloadQuality(""))
	require.Equal(t, "Good", openapiPayloadQuality("<nil>"))
	require.Equal(t, "Good", openapiPayloadQuality("good"))
	require.Equal(t, "Good", openapiPayloadQuality("whatever"))
}

func TestOpenapiLatestTimestamp(t *testing.T) {
	require.Zero(t, openapiLatestTimestamp(nil))
	require.Equal(t, int64(5), openapiLatestTimestamp(map[string]any{"timeStamp": int64(5)}))
	require.Equal(t, int64(6), openapiLatestTimestamp(map[string]any{"updateTime": int64(6)}))
	require.Equal(t, int64(7), openapiLatestTimestamp(map[string]any{"timestamp": int64(7)}))
	// dt map[string]int64。
	require.Equal(t, int64(9), openapiLatestTimestamp(map[string]any{
		"dt": map[string]int64{"a": 3, "b": 9},
	}))
	// dt map[string]any。
	require.Equal(t, int64(8), openapiLatestTimestamp(map[string]any{
		"dt": map[string]any{"a": int64(2), "b": int64(8)},
	}))
}

func TestParseOpenapiHistoryTime(t *testing.T) {
	ts, err := parseOpenapiHistoryTime("start_time", "2024-01-01T00:00:00Z")
	require.NoError(t, err)
	require.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), ts)

	_, err = parseOpenapiHistoryTime("start_time", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "start_time is required")

	_, err = parseOpenapiHistoryTime("end_time", "2024-01-01")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be RFC3339")
}

func TestParseOpenapiHistoryInterval(t *testing.T) {
	dur, err := parseOpenapiHistoryInterval("1m")
	require.NoError(t, err)
	require.Equal(t, time.Minute, dur)

	dur, err = parseOpenapiHistoryInterval("2d")
	require.NoError(t, err)
	require.Equal(t, 48*time.Hour, dur)

	_, err = parseOpenapiHistoryInterval("")
	require.Error(t, err)

	_, err = parseOpenapiHistoryInterval("0d")
	require.Error(t, err)

	_, err = parseOpenapiHistoryInterval("xyz")
	require.Error(t, err)

	_, err = parseOpenapiHistoryInterval("-5s")
	require.Error(t, err)

	_, err = parseOpenapiHistoryInterval("1.5ms")
	require.Error(t, err)
}

func TestNormalizeOpenapiPage(t *testing.T) {
	page, size := normalizeOpenapiPage(0, 0, 100)
	require.Equal(t, int64(1), page)
	require.Equal(t, int64(100), size)

	page, size = normalizeOpenapiPage(2, 5000, 100)
	require.Equal(t, int64(2), page)
	require.Equal(t, openapiHistoryMaxSize, size)
}

func TestPageOpenapiNodes(t *testing.T) {
	nodes := []repo.UnsNode{{ID: 1}, {ID: 2}, {ID: 3}, {ID: 4}}
	require.Len(t, pageOpenapiNodes(nodes, 1, 2), 2)
	require.Len(t, pageOpenapiNodes(nodes, 2, 2), 2)
	require.Len(t, pageOpenapiNodes(nodes, 3, 2), 0)
	require.Len(t, pageOpenapiNodes(nodes, 1, 100), 4)
	require.Empty(t, pageOpenapiNodes(nil, 1, 10))
}

func TestPageOpenapiHistoryValues(t *testing.T) {
	values := []map[string]any{{"v": 1}, {"v": 2}, {"v": 3}}
	require.Len(t, pageOpenapiHistoryValues(values, 1, 2), 2)
	require.Len(t, pageOpenapiHistoryValues(values, 2, 2), 1)
	require.Empty(t, pageOpenapiHistoryValues(values, 9, 2))
	require.Empty(t, pageOpenapiHistoryValues(nil, 1, 2))
}

func TestOpenapiAnyInt64(t *testing.T) {
	require.Equal(t, int64(1), openapiAnyInt64(int64(1)))
	require.Equal(t, int64(1), openapiAnyInt64(1))
	require.Equal(t, int64(1), openapiAnyInt64(int32(1)))
	require.Equal(t, int64(1), openapiAnyInt64(float64(1.9)))
	require.Equal(t, int64(2), openapiAnyInt64(json.Number("2")))
	require.Equal(t, int64(3), openapiAnyInt64("3"))
	require.Equal(t, int64(4), openapiAnyInt64(" 4 "))
	require.Zero(t, openapiAnyInt64("abc"))
	require.Zero(t, openapiAnyInt64(nil))
	require.Zero(t, openapiAnyInt64(map[string]any{}))
}

func TestOpenapiFloat(t *testing.T) {
	f, ok := openapiFloat(float64(1.5))
	require.True(t, ok)
	require.Equal(t, 1.5, f)

	f, ok = openapiFloat(float32(1.5))
	require.True(t, ok)

	f, ok = openapiFloat(2)
	require.True(t, ok)
	require.Equal(t, 2.0, f)

	f, ok = openapiFloat(int64(3))
	require.True(t, ok)

	f, ok = openapiFloat(json.Number("1.25"))
	require.True(t, ok)
	require.Equal(t, 1.25, f)

	f, ok = openapiFloat("2.5")
	require.True(t, ok)
	require.Equal(t, 2.5, f)

	_, ok = openapiFloat("abc")
	require.False(t, ok)
	_, ok = openapiFloat(map[string]any{})
	require.False(t, ok)
}

func TestFirstNonBlank(t *testing.T) {
	require.Equal(t, "a", firstNonBlank("a", "b"))
	require.Equal(t, "a", firstNonBlank(" a ", "b")) // 生产实现会 TrimSpace 后返回
	require.Equal(t, "b", firstNonBlank("  ", "b"))
	require.Equal(t, "b", firstNonBlank("", "b"))
}

func TestParseOpenapiHistoryAggregation(t *testing.T) {
	// nil 输入。
	agg, err := parseOpenapiHistoryAggregation(nil)
	require.NoError(t, err)
	require.False(t, agg.enabled)

	// interval 缺失。
	_, err = parseOpenapiHistoryAggregation(&types.HistoryAggregation{})
	require.Error(t, err)

	// field 与 fields 互斥。
	_, err = parseOpenapiHistoryAggregation(&types.HistoryAggregation{
		Interval: "1m", Field: "v", Fields: []types.HistoryAggregationField{{Name: "v"}},
	})
	require.Error(t, err)

	// field 需要 function。
	_, err = parseOpenapiHistoryAggregation(&types.HistoryAggregation{Interval: "1m", Field: "v"})
	require.Error(t, err)

	// field 带点号非法。
	_, err = parseOpenapiHistoryAggregation(&types.HistoryAggregation{Interval: "1m", Field: "a.b", Function: "avg"})
	require.Error(t, err)

	// 非法 function。
	_, err = parseOpenapiHistoryAggregation(&types.HistoryAggregation{Interval: "1m", Field: "v", Function: "bogus"})
	require.Error(t, err)

	// function 无 field。
	_, err = parseOpenapiHistoryAggregation(&types.HistoryAggregation{Interval: "1m", Function: "avg"})
	require.Error(t, err)

	// 合法 legacy。
	agg, err = parseOpenapiHistoryAggregation(&types.HistoryAggregation{Interval: "5m", Field: "v", Function: "avg"})
	require.NoError(t, err)
	require.True(t, agg.enabled)
	require.True(t, agg.legacy)
	require.Equal(t, int64(5*60*1000), agg.intervalMs)
	require.Equal(t, "v", agg.field)
	require.Equal(t, "avg", agg.function)

	// fields 模式：字段名非法 / 重复。
	_, err = parseOpenapiHistoryAggregation(&types.HistoryAggregation{
		Interval: "1m", Fields: []types.HistoryAggregationField{{Name: "a.b"}},
	})
	require.Error(t, err)
	_, err = parseOpenapiHistoryAggregation(&types.HistoryAggregation{
		Interval: "1m",
		Fields:   []types.HistoryAggregationField{{Name: "v"}, {Name: "V"}},
	})
	require.Error(t, err)

	// fields 模式合法。
	agg, err = parseOpenapiHistoryAggregation(&types.HistoryAggregation{
		Interval: "1m",
		Fields:   []types.HistoryAggregationField{{Name: "v", Function: "avg"}, {Name: "w"}},
	})
	require.NoError(t, err)
	require.True(t, agg.enabled)
	require.False(t, agg.legacy)
	require.Len(t, agg.fields, 2)
	require.Equal(t, "avg", agg.fields[0].function)
	require.Empty(t, agg.fields[1].function)
}

func TestValidateOpenapiHistoryFunction(t *testing.T) {
	for _, fn := range []string{"avg", "min", "max", "sum", "count", "first", "last"} {
		require.NoError(t, validateOpenapiHistoryFunction(fn))
	}
	require.Error(t, validateOpenapiHistoryFunction("median"))
}

func TestAggregateOpenapiHistory(t *testing.T) {
	values := []map[string]any{
		{"value": float64(1), "quality": "Good", "timeStamp": int64(1000)},
		{"value": float64(3), "quality": "Good", "timeStamp": int64(1500)},
		{"value": float64(2), "quality": "Bad", "timeStamp": int64(3000)},
	}
	agg := openapiHistoryAggregation{intervalMs: 2000, function: "avg", field: "value", legacy: true}
	out, err := aggregateOpenapiHistory(values, agg)
	require.NoError(t, err)
	require.Len(t, out, 2)
	// 0 桶：两个 Good → Good；2000 桶：Bad → Bad。
	require.Equal(t, int64(0), out[0]["timeStamp"])
	require.Equal(t, 2.0, out[0]["value"])
	require.Equal(t, "Good", out[0]["quality"])
	require.Equal(t, int64(2000), out[1]["timeStamp"])
	require.Equal(t, 2.0, out[1]["value"])
	require.Equal(t, "Bad", out[1]["quality"])

	// 非数值 field → 错误。
	bad := []map[string]any{{"value": "not-num", "quality": "Good", "timeStamp": int64(1)}}
	_, err = aggregateOpenapiHistory(bad, agg)
	require.Error(t, err)

	// first/last。
	agg.function = "first"
	out, err = aggregateOpenapiHistory(values, agg)
	require.NoError(t, err)
	require.Equal(t, float64(1), out[0]["value"])

	// count 忽略非数值。
	aggCount := openapiHistoryAggregation{intervalMs: 2000, function: "count", field: "value", legacy: true}
	out, err = aggregateOpenapiHistory(
		[]map[string]any{{"value": "x", "quality": "Good", "timeStamp": int64(1)}}, aggCount)
	require.NoError(t, err)
	require.Equal(t, 0, out[0]["value"])

	// 空桶。
	empty, err := aggregateOpenapiHistory(nil, agg)
	require.NoError(t, err)
	require.Empty(t, empty)
}

func TestAggregateOpenapiQuality(t *testing.T) {
	require.Equal(t, "Good", aggregateOpenapiQuality([]map[string]any{{"quality": "Good"}}))
	require.Equal(t, "Bad", aggregateOpenapiQuality([]map[string]any{{"quality": "Good"}, {"quality": "Bad"}}))
	require.Equal(t, "Uncertain", aggregateOpenapiQuality([]map[string]any{{"quality": "Good"}, {"quality": "Uncertain"}}))
	require.Equal(t, "Good", aggregateOpenapiQuality(nil))
}

func TestOpenapiHistoryAggregateField(t *testing.T) {
	require.Equal(t, float64(1), openapiHistoryAggregateField(map[string]any{"v": float64(1)}, "v"))
	require.Nil(t, openapiHistoryAggregateField(map[string]any{"v": float64(1)}, "w"))
	require.Equal(t, "raw", openapiHistoryAggregateField("raw", "value"))
	require.Nil(t, openapiHistoryAggregateField("raw", "other"))
}

func TestOpenapiHistoryValuesFromData(t *testing.T) {
	data := map[string]any{
		"list": []map[string]any{
			{"payload": map[string]any{"value": float64(1)}, "timestamp": int64(100)},
		},
		"total": json.Number("7"),
	}
	values, total := openapiHistoryValuesFromData(data, openapiHistoryAggregation{}), openapiAnyInt64(data["total"])
	require.Len(t, values, 1)
	require.Equal(t, float64(1), values[0]["value"])
	require.Equal(t, "Good", values[0]["quality"])
	require.Equal(t, int64(100), values[0]["timeStamp"])
	require.Equal(t, int64(7), total)

	// legacy 聚合字段抽取。
	agg := openapiHistoryAggregation{legacy: true, field: "v"}
	data["list"] = []map[string]any{
		{"payload": map[string]any{"v": float64(5)}, "timestamp": int64(100)},
	}
	values = openapiHistoryValuesFromData(data, agg)
	require.Equal(t, float64(5), values[0]["value"])
}

package dataingest

// message_test.go
// 测试目标：MQTT/OpenAPI 载荷解析与 Record 构造逻辑（NormalizePayload 及纯函数助手）。
// 正常路径：对象/数组/标量/裸文本 payload 解析；metric 走 schema 驱动、事件保留完整 JSON；
//   时间戳/质量字段提取；类型归一化；上下文取消提前返回。
// 异常情况：nil 定义、非法 JSON、单值回退、空值记录跳过、datatime 空值兜底。

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizePayload_MetricRowsBehavior(t *testing.T) {
	def := &Definition{
		ID:        1,
		Namespace: "Metric/line1",
		Alias:     "metric1",
		TopicType: metricTopicType,
		Fields: []Field{
			{Name: "temp", Type: "double"},
			{Name: metricTimeColumn, Type: "datetime"},
		},
		TimestampField: metricTimeColumn,
	}
	payload := []byte(`[{"temp":25.5,"_timestamp":1732223334512},{"temp":26.0,"_timestamp":1732223334513}]`)
	records, err := NormalizePayload(context.Background(), def, payload)
	require.NoError(t, err)
	require.Len(t, records, 2)
	for _, record := range records {
		assert.Equal(t, int64(1), record.UnsID)
		assert.Equal(t, metricTopicType, record.TopicType)
		assert.Equal(t, "Metric/line1", record.Namespace)
		assert.Equal(t, "metric1", record.Alias)
		// metric 记录不保留 EventPayload，值走 schema 驱动
		assert.Nil(t, record.EventPayload)
		require.Contains(t, record.Values, "temp")
	}
	assert.Equal(t, "25.5", records[0].Values["temp"])
	assert.Equal(t, "26", records[1].Values["temp"])
	// RawValues 保留完整原始行
	assert.Equal(t, "1732223334512", records[0].RawValues["_timestamp"])
}

func TestNormalizePayload_StateEventPayloadBehavior(t *testing.T) {
	def := &Definition{
		ID:             2,
		Namespace:      "State/action1",
		Alias:          "action1",
		TopicType:      1,
		Fields:         []Field{{Name: "cmd", Type: "string"}},
		TimestampField: "timeStamp",
	}
	payload := []byte(`{"cmd":"start","extra":123,"timeStamp":1732223334512}`)
	records, err := NormalizePayload(context.Background(), def, payload)
	require.NoError(t, err)
	require.Len(t, records, 1)
	// 非 metric 记录保留完整 JSON 事件负载
	require.NotNil(t, records[0].EventPayload)
	assert.Equal(t, json.Number("123"), records[0].EventPayload["extra"])
	assert.Equal(t, "start", records[0].EventPayload["cmd"])
}

func TestNormalizePayload_SingleValueFallbackBehavior(t *testing.T) {
	// 单字段场景：row 只有 value 键时回退到 value
	def := &Definition{ID: 3, Namespace: "Metric/scalar", TopicType: metricTopicType}
	records, err := NormalizePayload(context.Background(), def, []byte(`42`))
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "42", records[0].Values["value"])
}

func TestNormalizePayload_EmptyDataFieldsBehavior(t *testing.T) {
	// 无数据字段时使用默认 value:string 字段
	def := &Definition{
		ID:        4,
		Namespace: "Metric/emptyfields",
		TopicType: metricTopicType,
		Fields:    []Field{{Name: "_timestamp", Type: "datetime"}},
	}
	records, err := NormalizePayload(context.Background(), def, []byte(`{"value":"x"}`))
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "x", records[0].Values["value"])
}

func TestNormalizePayload_DatetimeEmptyValueBehavior(t *testing.T) {
	// 无匹配值时，datetime 类型字段写空串兜底（触发记录生成）
	def := &Definition{
		ID:        5,
		Namespace: "Metric/dt",
		TopicType: metricTopicType,
		Fields:    []Field{{Name: "ts", Type: "datetime"}},
	}
	records, err := NormalizePayload(context.Background(), def, []byte(`{"other":"x"}`))
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "", records[0].Values["ts"])
}

func TestNormalizePayload_NilDefAndEmptyPayloadBehavior(t *testing.T) {
	_, err := NormalizePayload(context.Background(), nil, []byte(`{}`))
	require.ErrorIs(t, err, ErrDefinitionNotFound)
	def := &Definition{ID: 6, Namespace: "Metric/nodata", TopicType: metricTopicType}
	// 空白 payload 不产生记录
	records, err := NormalizePayload(context.Background(), def, nil)
	require.NoError(t, err)
	assert.Empty(t, records)
	// 记录为空（无值且无事件负载）被跳过
	records, err = NormalizePayload(context.Background(), def, []byte(`{}`))
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestNormalizePayload_CanceledContextBehavior(t *testing.T) {
	def := &Definition{ID: 7, Namespace: "Metric/cancel", TopicType: metricTopicType}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	records, err := NormalizePayload(ctx, def, []byte(`[{"value":1},{"value":2}]`))
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, records)
}

func TestNormalizePayload_QualityExtractionBehavior(t *testing.T) {
	def := &Definition{
		ID:           8,
		Namespace:    "Metric/q",
		TopicType:    metricTopicType,
		Fields:       []Field{{Name: "value", Type: "double"}, {Name: "quality", Type: "string"}},
		QualityField: metricQualityColumn,
	}
	records, err := NormalizePayload(context.Background(), def, []byte(`{"value":1,"_quality":2}`))
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, int64(2), records[0].Quality)
	// quality 缺失时 intValue("") → 0
	records, err = NormalizePayload(context.Background(), def, []byte(`{"value":1}`))
	require.NoError(t, err)
	assert.Equal(t, int64(0), records[0].Quality)

	// 平台质量输入优先级固定为 _quality > quality > _qos；payload
	// 内的 quality 仍作为原始业务字段保留，不被平台判定回写。
	records, err = NormalizePayload(context.Background(), def, []byte(`{"value":1,"_quality":"Good","quality":"Bad","_qos":2}`))
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, int64(1), records[0].Quality)
	assert.Equal(t, "Bad", records[0].Values["quality"])
	assert.Equal(t, "Bad", records[0].RawValues["quality"])

	records, err = NormalizePayload(context.Background(), def, []byte(`{"value":1,"quality":"Uncertain","_qos":3}`))
	require.NoError(t, err)
	assert.Equal(t, int64(2), records[0].Quality)

	records, err = NormalizePayload(context.Background(), def, []byte(`{"value":1,"_qos":"Bad"}`))
	require.NoError(t, err)
	assert.Equal(t, int64(3), records[0].Quality)
}

func TestNormalizePayload_PreservesReceiveTimeAndObservesSourceTime(t *testing.T) {
	def := &Definition{
		ID:             42,
		TopicType:      metricTopicType,
		TimestampField: "timestamp",
		Fields: []Field{
			{Name: "timestamp", Type: "datetime", SystemField: true},
			{Name: "value", Type: "double"},
		},
	}
	sourceMillis := int64(1_735_689_600_123)
	records, err := NormalizePayload(context.Background(), def, []byte(`{"timestamp":1735689600123,"value":12.5}`))
	if err != nil {
		t.Fatalf("NormalizePayload() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(records))
	}
	record := records[0]
	if !record.SourceTimeSet || record.SourceTime.UnixMilli() != sourceMillis {
		t.Fatalf("source time = %v set=%v, want %d", record.SourceTime, record.SourceTimeSet, sourceMillis)
	}
	if record.IngestedAt.IsZero() || !record.Time.Equal(record.IngestedAt) {
		t.Fatalf("receive time semantics changed: time=%v ingestedAt=%v", record.Time, record.IngestedAt)
	}
	if record.Time.Equal(record.SourceTime) {
		t.Fatal("Record.Time unexpectedly switched to source time")
	}
}

func TestNormalizePayload_InvalidSourceTimeIsNotInvented(t *testing.T) {
	def := &Definition{
		ID:             43,
		TopicType:      metricTopicType,
		TimestampField: "timestamp",
		Fields: []Field{
			{Name: "timestamp", Type: "datetime", SystemField: true},
			{Name: "value", Type: "double"},
		},
	}
	records, err := NormalizePayload(context.Background(), def, []byte(`{"timestamp":"not-a-time","value":1}`))
	if err != nil || len(records) != 1 {
		t.Fatalf("NormalizePayload() records=%d error=%v", len(records), err)
	}
	if records[0].SourceTimeSet || !records[0].SourceTime.IsZero() {
		t.Fatalf("invalid source time was accepted: %+v", records[0])
	}
}

func TestParsePayload_Behavior(t *testing.T) {
	rows, err := ParsePayload([]byte(`{"a":1,"b":2.5}`))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "1", rows[0]["a"])
	assert.Equal(t, "2.5", rows[0]["b"])

	rows, err = ParsePayload([]byte(`[{"a":1},{"a":2}]`))
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "2", rows[1]["a"])

	// 非 JSON 文本 → 单行 value
	rows, err = ParsePayload([]byte(`"hello"`))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "hello", rows[0]["value"])

	rows, err = ParsePayload([]byte(``))
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestParsePayloadValues_InvalidJSONBehavior(t *testing.T) {
	rows, err := parsePayloadValues([]byte(`not-json`))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "not-json", rows[0]["value"])
}

func TestDecodeJSONValue_Behavior(t *testing.T) {
	value, ok := decodeJSONValue([]byte(`{"a":1}`))
	assert.True(t, ok)
	obj, ok := value.(map[string]any)
	require.True(t, ok)
	// UseNumber：数字保留精度
	assert.Equal(t, json.Number("1"), obj["a"])

	// 尾随垃圾 → 解析失败
	_, ok = decodeJSONValue([]byte(`{"a":1} trailing`))
	assert.False(t, ok)
	_, ok = decodeJSONValue([]byte(`{invalid`))
	assert.False(t, ok)
}

func TestFlattenRawJSONValue_Behavior(t *testing.T) {
	// 标量 → 单行 value
	rows := flattenRawJSONValue(42)
	require.Len(t, rows, 1)
	assert.Equal(t, 42, rows[0]["value"])
	// 对象 → 单行
	rows = flattenRawJSONValue(map[string]any{"a": 1})
	require.Len(t, rows, 1)
	// 数组 → 逐元素展开（含嵌套数组递归）
	rows = flattenRawJSONValue([]any{map[string]any{"a": 1}, []any{map[string]any{"b": 2}}})
	require.Len(t, rows, 2)
	assert.Equal(t, 1, rows[0]["a"])
	assert.Equal(t, 2, rows[1]["b"])
}

func TestFlattenJSONValue_Behavior(t *testing.T) {
	rows := flattenJSONValue([]any{map[string]any{"a": 1.5}, "x"})
	require.Len(t, rows, 2)
	assert.Equal(t, "1.5", rows[0]["a"])
	assert.Equal(t, "x", rows[1]["value"])
}

func TestClonePayloadMap_Behavior(t *testing.T) {
	assert.Nil(t, clonePayloadMap(nil))
	source := map[string]any{"a": 1}
	clone := clonePayloadMap(source)
	require.NotNil(t, clone)
	clone["b"] = 2
	assert.NotContains(t, source, "b")
}

func TestValueString_Behavior(t *testing.T) {
	assert.Equal(t, "", valueString(nil))
	assert.Equal(t, "abc", valueString("abc"))
	assert.Equal(t, "3", valueString(3.0))     // 整型浮点去小数点
	assert.Equal(t, "3.14", valueString(3.14)) // 非整型保留
	assert.Equal(t, "true", valueString(true))
	assert.Equal(t, `{"a":1}`, valueString(map[string]any{"a": 1}))
	assert.Equal(t, `[1,2]`, valueString([]any{1, 2}))
	assert.Equal(t, "42", valueString(json.Number("42"))) // default 分支 fmt.Sprint
}

func TestDataFields_Behavior(t *testing.T) {
	fields := []Field{
		{Name: "temp", Type: "double"},
		{Name: "_id", Type: "long"},
		{Name: "timeStamp", Type: "long"},
		{Name: "quality", Type: "string"},
		{Name: "data", Type: "string", SystemField: true},
	}
	// metric 类型：timeStamp/quality 不是系统字段（不剔除）
	out := dataFields(fields, metricTopicType)
	names := make([]string, 0, len(out))
	for _, field := range out {
		names = append(names, field.Name)
	}
	assert.ElementsMatch(t, []string{"temp", "timeStamp", "quality"}, names)

	// 事件类型：timeStamp/quality 被剔除
	out = dataFields(fields, 1)
	names = names[:0]
	for _, field := range out {
		names = append(names, field.Name)
	}
	assert.ElementsMatch(t, []string{"temp"}, names)
}

func TestIsSystemField_Behavior(t *testing.T) {
	assert.True(t, isSystemField(Field{Name: "_id"}, 3))
	assert.True(t, isSystemField(Field{Name: "_timestamp"}, 3))
	assert.True(t, isSystemField(Field{Name: "created_time"}, 1))
	assert.True(t, isSystemField(Field{Name: "x", SystemField: true}, 3))
	assert.False(t, isSystemField(Field{Name: "x"}, 3))
	assert.True(t, isSystemField(Field{Name: "timeStamp"}, 1))
	assert.False(t, isSystemField(Field{Name: "timeStamp"}, 3))
}

func TestSingleFieldValue_Behavior(t *testing.T) {
	row := map[string]string{"a": "1", "value": "2"}
	value, ok := singleFieldValue(row, "a")
	assert.True(t, ok)
	assert.Equal(t, "1", value)
	// 字段不存在且 row 只有 value 键 → 回退 value
	value, ok = singleFieldValue(map[string]string{"value": "9"}, "a")
	assert.True(t, ok)
	assert.Equal(t, "9", value)
	// 多键行且字段不存在 → 失败
	_, ok = singleFieldValue(row, "missing")
	assert.False(t, ok)
}

func TestParseTime_Behavior(t *testing.T) {
	// 微秒（>1e15）
	tm, ok := parseTime("1732223334512000")
	assert.True(t, ok)
	assert.Equal(t, int64(1732223334512), tm.UnixMilli())
	// 毫秒
	tm, ok = parseTime("1732223334512")
	assert.True(t, ok)
	assert.Equal(t, int64(1732223334512), tm.UnixMilli())
	// 秒
	tm, ok = parseTime("1732223334")
	assert.True(t, ok)
	assert.Equal(t, int64(1732223334000), tm.UnixMilli())
	// RFC3339Nano
	tm, ok = parseTime("2024-11-22T03:48:54.512Z")
	assert.True(t, ok)
	// 常见布局
	_, ok = parseTime("2024-11-22 03:48:54.512")
	assert.True(t, ok)
	_, ok = parseTime("2024-11-22 03:48:54")
	assert.True(t, ok)
	// 非法
	_, ok = parseTime("not-a-time")
	assert.False(t, ok)
	_, ok = parseTime("")
	assert.False(t, ok)
}

func TestIntValue_Behavior(t *testing.T) {
	assert.Equal(t, int64(0), intValue(""))
	assert.Equal(t, int64(-5), intValue("-5"))
	assert.Equal(t, int64(2), intValue("Uncertain"))
	assert.Equal(t, int64(3), intValue("bad"))
	assert.Equal(t, int64(1), intValue("other"))
	assert.Equal(t, int64(1), intValue("Good"))
}

func TestNormalizeFieldType_Behavior(t *testing.T) {
	assert.Equal(t, "integer", normalizeFieldType("int"))
	assert.Equal(t, "integer", normalizeFieldType("INTEGER"))
	assert.Equal(t, "long", normalizeFieldType("bigint"))
	assert.Equal(t, "float", normalizeFieldType("float"))
	assert.Equal(t, "double", normalizeFieldType("decimal"))
	assert.Equal(t, "double", normalizeFieldType("number"))
	assert.Equal(t, "boolean", normalizeFieldType("bool"))
	assert.Equal(t, "datetime", normalizeFieldType("timestamp"))
	assert.Equal(t, "datetime", normalizeFieldType("date"))
	assert.Equal(t, "blob", normalizeFieldType("lblob"))
	assert.Equal(t, "string", normalizeFieldType("json"))
	assert.Equal(t, "string", normalizeFieldType(""))
	assert.Equal(t, "string", normalizeFieldType("whatever"))
}

func TestContextDone_Behavior(t *testing.T) {
	assert.False(t, contextDone(context.Background()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.True(t, contextDone(ctx))
}

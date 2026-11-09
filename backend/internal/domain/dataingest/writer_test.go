package dataingest

// writer_test.go
// 测试目标：SinkWriter 落库（metric 物理表/视图、事件别名表、冲突合并）与全部纯函数助手。
// 正常路径：metric 记录写入 uns_timeserial 并建视图；事件记录写入 _json 别名表；
//   同 (id,时间) 冲突按 COALESCE 合并、quality 以新值覆盖；批量分片；列/表名引用安全转义；
//   视图定义解析、字段映射、去重与合并。
// 异常情况：空输入安全；非法 DSN 报错；typedValueByType 非法值报错；nil writer 安全。
// 未覆盖：真实 TimescaleDB 压缩/保留策略行为（EnsureSchema 由 NewSinkWriter 在 DB 测试中执行）。

import (
	"context"
	"fmt"
	"testing"
	"time"

	"backend/internal/testkit"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuoteIdent_Behavior(t *testing.T) {
	assert.Equal(t, `"abc"`, quoteIdent("abc"))
	assert.Equal(t, `"a""b"`, quoteIdent(`a"b`))
	assert.Equal(t, `""`, quoteIdent(""))
}

func TestQuoteLiteral_Behavior(t *testing.T) {
	assert.Equal(t, `'abc'`, quoteLiteral("abc"))
	assert.Equal(t, `'it''s'`, quoteLiteral("it's"))
}

func TestSinkQualifiedName_Behavior(t *testing.T) {
	assert.Equal(t, `"public"."tbl"`, sinkQualifiedName("tbl"))
	assert.Equal(t, sinkQualifiedName("tbl"), sinkRegclassName("tbl"))
}

func TestQuotedList_Behavior(t *testing.T) {
	assert.Equal(t, `"a", "b"`, quotedList([]string{"a", "b"}))
	assert.Equal(t, "", quotedList(nil))
}

func TestShortHash_Behavior(t *testing.T) {
	hash := shortHash("hello")
	assert.Len(t, hash, 12)
	assert.Equal(t, hash, shortHash("hello"))
	assert.NotEqual(t, hash, shortHash("world"))
}

func TestSortedStringSet_Behavior(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, sortedStringSet(map[string]struct{}{"b": {}, "a": {}}))
	assert.Empty(t, sortedStringSet(nil))
}

func TestShouldResetAliasEventTable_Behavior(t *testing.T) {
	// 空列 → 不重置
	assert.False(t, shouldResetAliasEventTable(nil))
	// 恰好三列（含大小写/空白）→ 不重置
	assert.False(t, shouldResetAliasEventTable([]string{"_id", "_timestamp", "_json"}))
	assert.False(t, shouldResetAliasEventTable([]string{"_id", "_json", " _TIMESTAMP "}))
	// 缺列 / 多列 → 重置
	assert.True(t, shouldResetAliasEventTable([]string{"_id", "_timestamp"}))
	assert.True(t, shouldResetAliasEventTable([]string{"_id", "_timestamp", "_json", "extra"}))
	assert.True(t, shouldResetAliasEventTable([]string{"_id", "_timestamp", "_json", "_json"}))
}

func TestEventRecordJSON_Behavior(t *testing.T) {
	record := Record{EventPayload: map[string]any{"a": 1, "b": "x"}}
	raw, err := eventRecordJSON(record)
	require.NoError(t, err)
	assert.JSONEq(t, `{"a":1,"b":"x"}`, string(raw))
	// 无 EventPayload → 从 RawValues/Values 组装
	record = Record{
		Fields:    []Field{{Name: "temp", Type: "double"}, {Name: "name", Type: "string"}},
		RawValues: map[string]string{"temp": "1.5", "name": "x", "extra": "y"},
	}
	raw, err = eventRecordJSON(record)
	require.NoError(t, err)
	assert.JSONEq(t, `{"temp":1.5,"name":"x","extra":"y"}`, string(raw))
}

func TestEventPayloadValues_Behavior(t *testing.T) {
	// EventPayload 优先（浅拷贝）
	record := Record{EventPayload: map[string]any{"a": 1}}
	values := eventPayloadValues(record)
	values["b"] = 2
	assert.NotContains(t, record.EventPayload, "b")
	// 回退 RawValues（按字段类型转换）
	record = Record{
		Fields:    []Field{{Name: "temp", Type: "double"}},
		RawValues: map[string]string{"temp": "2.5"},
	}
	values = eventPayloadValues(record)
	assert.Equal(t, 2.5, values["temp"])
	// 再回退 Values
	record = Record{
		Fields: []Field{{Name: "name", Type: "string"}},
		Values: map[string]string{"name": "x"},
	}
	values = eventPayloadValues(record)
	assert.Equal(t, "x", values["name"])
	// 全空 → 空 map
	assert.Empty(t, eventPayloadValues(Record{}))
}

func TestEventPayloadValue_Behavior(t *testing.T) {
	assert.Equal(t, "raw", eventPayloadValue("", "raw"))
	assert.Equal(t, 3.5, eventPayloadValue("double", "3.5"))
	assert.Equal(t, "oops", eventPayloadValue("double", "oops"))
}

func TestRecordFields_Behavior(t *testing.T) {
	record := Record{Fields: []Field{{Name: "a", Type: "double"}, {Name: "_timestamp", Type: "datetime"}}}
	fields := recordFields(record)
	require.Len(t, fields, 1)
	assert.Equal(t, "a", fields[0].Name)
	// 空字段 → value 兜底
	fields = recordFields(Record{})
	require.Len(t, fields, 1)
	assert.Equal(t, "value", fields[0].Name)
}

func TestSortedFields_Behavior(t *testing.T) {
	fields := sortedFields([]Field{{Name: "b"}, {Name: "A"}, {Name: "a"}})
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.Name)
	}
	assert.Equal(t, []string{"A", "a", "b"}, names)
}

func TestSortedColumnNames_Behavior(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, sortedColumnNames(map[string]Field{"b": {}, "a": {}}))
}

func TestAliasColumns_Behavior(t *testing.T) {
	columns := aliasColumns([]Record{
		{Fields: []Field{{Name: "a", Type: "double"}}},
		{Fields: []Field{{Name: "b", Type: "string"}}},
	})
	assert.Contains(t, columns, "a")
	assert.Contains(t, columns, "b")
}

func TestMetricColumns_Behavior(t *testing.T) {
	mappings := map[int64][]metricFieldMapping{
		1: {{Field: Field{Name: "a"}, SourceColumn: "double_1"}},
		2: {{Field: Field{Name: "b"}, SourceColumn: "str_1"}},
	}
	columns := metricColumns(mappings)
	assert.Equal(t, Field{Name: "a"}, columns["double_1"])
	assert.Equal(t, Field{Name: "b"}, columns["str_1"])
}

func TestMetricRecordValues_Behavior(t *testing.T) {
	record := Record{Values: map[string]string{"a": "1", "b": "2"}}
	mappings := []metricFieldMapping{
		{Field: Field{Name: "a"}, SourceColumn: "double_1"},
		{Field: Field{Name: "missing"}, SourceColumn: "double_2"},
	}
	values := metricRecordValues(record, mappings)
	assert.Equal(t, map[string]string{"double_1": "1"}, values)
}

func TestDedupeMetricRecords_Behavior(t *testing.T) {
	base := time.UnixMilli(1000)
	records := []Record{
		{UnsID: 1, Time: base, Values: map[string]string{"a": "1"}},
		{UnsID: 1, Time: base, Values: map[string]string{"a": "2"}, Quality: 3},
		{UnsID: 2, Time: base, Values: map[string]string{"a": "3"}},
	}
	out := dedupeMetricRecords(records)
	require.Len(t, out, 2)
	assert.Equal(t, "2", out[0].Values["a"])
	assert.Equal(t, int64(3), out[0].Quality)
}

func TestDedupeAliasRecords_Behavior(t *testing.T) {
	base := time.UnixMilli(1000)
	records := []Record{
		{Time: base, Values: map[string]string{"a": "1"}},
		{Time: base, Values: map[string]string{"b": "2"}},
		{Time: base.Add(time.Second), Values: map[string]string{"c": "3"}},
	}
	out := dedupeAliasRecords(records)
	require.Len(t, out, 2)
	assert.Contains(t, out[0].Values, "a")
	assert.Contains(t, out[0].Values, "b")
}

func TestMergeRecord_Behavior(t *testing.T) {
	base := time.UnixMilli(100)
	merged := mergeRecord(
		Record{Time: base, Quality: 1, Values: map[string]string{"a": "1"}, Fields: []Field{{Name: "a", Type: "string"}}},
		Record{Time: base.Add(time.Second), Quality: 3, Values: map[string]string{"b": "2"}, Fields: []Field{{Name: "b", Type: "long"}, {Name: "a", Type: "string"}}},
	)
	assert.Equal(t, base.Add(time.Second), merged.Time)
	assert.Equal(t, int64(3), merged.Quality)
	assert.Equal(t, map[string]string{"a": "1", "b": "2"}, merged.Values)
	require.Len(t, merged.Fields, 2) // 字段去重
}

func TestRecordAlias_Behavior(t *testing.T) {
	assert.Equal(t, "alias", recordAlias(Record{Alias: "/alias/", Namespace: "ns"}))
	assert.Equal(t, "ns", recordAlias(Record{Namespace: "/ns/"}))
	assert.Equal(t, "42", recordAlias(Record{UnsID: 42}))
	assert.Equal(t, "", recordAlias(Record{}))
}

func TestRepresentativeMetricRecords_Behavior(t *testing.T) {
	records := []Record{
		{UnsID: 1, Values: map[string]string{"a": "1"}},
		{UnsID: 1, Values: map[string]string{"a": "2"}},
		{UnsID: 2, Values: map[string]string{"b": "3"}},
	}
	reps := representativeMetricRecords(records)
	require.Len(t, reps, 2)
	assert.Equal(t, "2", reps[1].Values["a"])
}

func TestMetricSelectExpression_Behavior(t *testing.T) {
	// 无表达式 → 引用源列 + 类型 cast
	expr := metricSelectExpression(metricFieldMapping{Field: Field{Name: "temp", Type: "integer"}, SourceColumn: "long_1"})
	assert.Equal(t, `"long_1"::int4 AS "temp"`, expr)
	expr = metricSelectExpression(metricFieldMapping{Field: Field{Name: "f", Type: "float"}, SourceColumn: "double_1"})
	assert.Equal(t, `"double_1"::real AS "f"`, expr)
	// 已有 AS → 原样
	expr = metricSelectExpression(metricFieldMapping{Field: Field{Name: "f"}, SourceColumn: "double_1", Expression: `CASE WHEN x THEN NULL ELSE "double_1" END`})
	assert.Equal(t, `CASE WHEN x THEN NULL ELSE "double_1" END AS "f"`, expr)
}

func TestParseMetricViewColumns_Behavior(t *testing.T) {
	definition := ` SELECT uns_timeserial."_id",
    uns_timeserial."_timestamp",
    uns_timeserial."_quality",
    uns_timeserial."double_1" AS "temp",
    (uns_timeserial."str_1")::text AS "name"
   FROM public.uns_timeserial
  WHERE (uns_timeserial."_id" = 701);`
	columns := parseMetricViewColumns(definition)
	require.Len(t, columns, 2)
	assert.Equal(t, "temp", columns[0].ColumnName)
	assert.Equal(t, "double_1", columns[0].SourceColumn)
	assert.Equal(t, "name", columns[1].ColumnName)
	assert.Equal(t, "str_1", columns[1].SourceColumn)
	// 无 AS 的裸列 → 取最后一个标识符
	columns = parseMetricViewColumns(`SELECT "double_1" FROM public.uns_timeserial`)
	require.Len(t, columns, 1)
	assert.Equal(t, "double_1", columns[0].ColumnName)
	assert.Equal(t, "double_1", columns[0].SourceColumn)
	// 非法定义 → nil
	assert.Nil(t, parseMetricViewColumns("not a view"))
}

func TestSelectListFromView_Behavior(t *testing.T) {
	definition := "SELECT a, b FROM public.x WHERE id = 1"
	assert.Equal(t, "a, b", selectListFromView(definition))
	assert.Equal(t, "", selectListFromView("FROM x"))
	assert.Equal(t, "", selectListFromView("SELECT x"))
	// SELECT 在 FROM 之后 → 空
	assert.Equal(t, "", selectListFromView("FROM x SELECT y"))
}

func TestSplitSQLList_Behavior(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, splitSQLList("a, b"))
	// 函数调用内的逗号不拆分（括号深度保护）
	assert.Equal(t, []string{"concat(a,b)", "c"}, splitSQLList("concat(a,b), c"))
	assert.Equal(t, []string{`'x,y'`, "z"}, splitSQLList(`'x,y', z`))
	assert.Equal(t, []string{`"a,b"`, "c"}, splitSQLList(`"a,b", c`))
	assert.Empty(t, splitSQLList("  "))
}

func TestSplitSelectAlias_Behavior(t *testing.T) {
	alias, expr := splitSelectAlias(`"double_1" AS "temp"`)
	assert.Equal(t, "temp", alias)
	assert.Equal(t, `"double_1"`, expr)
	// 无 AS → 空 alias
	alias, expr = splitSelectAlias(`"double_1"`)
	assert.Equal(t, "", alias)
	assert.Equal(t, `"double_1"`, expr)
}

func TestMetricSourceColumn_Behavior(t *testing.T) {
	assert.Equal(t, "double_1", metricSourceColumn(`"double_1"`))
	// CASE ELSE 分支取源列
	assert.Equal(t, "double_1", metricSourceColumn(`CASE WHEN "_timestamp" < to_timestamp(1) THEN NULL ELSE "double_1" END`))
	// 带表限定的引用
	assert.Equal(t, "double_1", metricSourceColumn(`uns_timeserial."double_1"`))
}

func TestLastIdentifier_Behavior(t *testing.T) {
	assert.Equal(t, "double_1", lastIdentifier(`uns_timeserial."double_1"`))
	assert.Equal(t, "temp", lastIdentifier(`uns_timeserial.double_1 AS temp`))
	assert.Equal(t, "b", lastIdentifier("a.b"))
	assert.Equal(t, "", lastIdentifier(""))
	assert.Equal(t, "", lastIdentifier("  *  "))
}

func TestCleanIdentifier_Behavior(t *testing.T) {
	assert.Equal(t, "temp", cleanIdentifier(`"temp"`))
	assert.Equal(t, `a"b`, cleanIdentifier(`"a""b"`))
	assert.Equal(t, "temp", cleanIdentifier("temp"))
}

func TestLegacyMetricColumnPrefix_Behavior(t *testing.T) {
	assert.Equal(t, "long", legacyMetricColumnPrefix("int"))
	assert.Equal(t, "long", legacyMetricColumnPrefix("long"))
	assert.Equal(t, "double", legacyMetricColumnPrefix("float"))
	assert.Equal(t, "double", legacyMetricColumnPrefix("double"))
	assert.Equal(t, "bool", legacyMetricColumnPrefix("boolean"))
	assert.Equal(t, "date", legacyMetricColumnPrefix("datetime"))
	assert.Equal(t, "str", legacyMetricColumnPrefix("string"))
	assert.Equal(t, "str", legacyMetricColumnPrefix("unknown"))
}

func TestMetricSQLType_Behavior(t *testing.T) {
	assert.Equal(t, "int8", metricSQLType("long_1", "long"))
	assert.Equal(t, "float8", metricSQLType("double_1", "double"))
	assert.Equal(t, "boolean", metricSQLType("bool_1", "boolean"))
	assert.Equal(t, "timestamptz", metricSQLType("date_1", "datetime"))
	assert.Equal(t, "text", metricSQLType("str_1", "string"))
	// 非前缀列 → 按类型映射
	assert.Equal(t, "int4", metricSQLType("custom", "integer"))
	assert.Equal(t, "text", metricSQLType("custom", "string"))
}

func TestSQLType_Behavior(t *testing.T) {
	assert.Equal(t, "int4", sqlType("integer"))
	assert.Equal(t, "int8", sqlType("long"))
	assert.Equal(t, "float4", sqlType("float"))
	assert.Equal(t, "float8", sqlType("double"))
	assert.Equal(t, "boolean", sqlType("boolean"))
	assert.Equal(t, "timestamptz(3)", sqlType("datetime"))
	assert.Equal(t, "varchar(512)", sqlType("blob"))
	assert.Equal(t, "text", sqlType("string"))
	assert.Equal(t, "text", sqlType("unknown"))
}

func TestTypedValueByType_Behavior(t *testing.T) {
	value, err := typedValueByType("integer", "42")
	require.NoError(t, err)
	assert.Equal(t, int32(42), value)
	value, err = typedValueByType("long", "42")
	require.NoError(t, err)
	assert.Equal(t, int64(42), value)
	value, err = typedValueByType("float", "1.5")
	require.NoError(t, err)
	assert.Equal(t, float32(1.5), value)
	value, err = typedValueByType("double", "1.5")
	require.NoError(t, err)
	assert.Equal(t, 1.5, value)
	value, err = typedValueByType("boolean", "true")
	require.NoError(t, err)
	assert.Equal(t, true, value)
	value, err = typedValueByType("datetime", "")
	require.NoError(t, err)
	assert.Nil(t, value)
	value, err = typedValueByType("datetime", "1732223334512")
	require.NoError(t, err)
	assert.Equal(t, time.UnixMilli(1732223334512).UTC(), value)
	value, err = typedValueByType("string", "x")
	require.NoError(t, err)
	assert.Equal(t, "x", value)
	// 非法值报错
	_, err = typedValueByType("integer", "abc")
	require.Error(t, err)
	_, err = typedValueByType("datetime", "not-a-time")
	require.Error(t, err)
}

func TestNextMetricColumn_Behavior(t *testing.T) {
	used := map[string]map[int]bool{"double": {1: true, 3: true}}
	assert.Equal(t, "double_2", nextMetricColumn(used, "double"))
	assert.Equal(t, "double_4", nextMetricColumn(used, "double"))
	assert.Equal(t, "str_1", nextMetricColumn(used, "str"))
}

func TestMetricColumnNumber_Behavior(t *testing.T) {
	assert.Equal(t, 3, metricColumnNumber("double", "double_3"))
	assert.Equal(t, 0, metricColumnNumber("double", "str_3"))
	assert.Equal(t, 0, metricColumnNumber("double", "double_x"))
	assert.Equal(t, 0, metricColumnNumber("double", "double"))
}

func TestMarkMetricColumnUsed_Behavior(t *testing.T) {
	used := map[string]map[int]bool{}
	markMetricColumnUsed(used, "double", "double_2")
	assert.True(t, used["double"][2])
	// 非前缀列不标记
	markMetricColumnUsed(used, "double", "custom")
	assert.NotContains(t, used["double"], 0)
}

func TestBuildMetricMappings_Behavior(t *testing.T) {
	fields := []Field{{Name: "temp", Type: "double"}, {Name: "flag", Type: "boolean"}, {Name: "note", Type: "string"}}
	// 空视图列 → 全部新映射，源列按前缀递增
	mappings := buildMetricMappings(fields, nil)
	require.Len(t, mappings, 3)
	assert.Equal(t, "double_1", mappings[0].SourceColumn)
	assert.Equal(t, "bool_1", mappings[1].SourceColumn)
	assert.Equal(t, "str_1", mappings[2].SourceColumn)
	assert.Empty(t, mappings[0].Expression)

	// 已有兼容列 → 复用且不重复分配
	existing := []metricViewColumn{
		{ColumnName: "temp", SourceColumn: "double_7", Expression: `"double_7"`},
		{ColumnName: "old", SourceColumn: "double_1"},
	}
	mappings = buildMetricMappings(fields, existing)
	require.Len(t, mappings, 3)
	assert.Equal(t, "double_7", mappings[0].SourceColumn)
	assert.Equal(t, `"double_7"`, mappings[0].Expression)
	// flag/note 为新列：bool_1/str_1 未被旧列占用（旧列是 double_1/double_7），
	// 无需生成 CASE WHEN 防读旧值，Expression 为空。
	assert.Equal(t, "bool_1", mappings[1].SourceColumn)
	assert.Equal(t, "str_1", mappings[2].SourceColumn)
	assert.Empty(t, mappings[1].Expression)
	assert.Empty(t, mappings[2].Expression)
}

func TestMetricMappings_Behavior(t *testing.T) {
	records := []Record{
		{UnsID: 1, Alias: "m1", TopicType: metricTopicType, Fields: []Field{{Name: "temp", Type: "double"}}},
		{UnsID: 2, Alias: "m2", TopicType: metricTopicType, Fields: []Field{{Name: "temp", Type: "double"}}},
	}
	// m1 有视图定义 → 复用 double_7；m2 无 → 新列
	catalog := map[string]sinkRelationInfo{
		"m1": {Kind: "v", ViewDefinition: `SELECT "_id", "double_7" AS "temp" FROM public.uns_timeserial WHERE "_id" = 1`},
		"m2": {},
	}
	mappings := metricMappings(records, catalog)
	require.Len(t, mappings, 2)
	assert.Equal(t, "double_7", mappings[1][0].SourceColumn)
	assert.Equal(t, "double_1", mappings[2][0].SourceColumn)
}

func TestExecutePhysicalDDLBatch_EmptyBehavior(t *testing.T) {
	require.NoError(t, executePhysicalDDLBatch(context.Background(), nil, &pgx.Batch{}))
}

func TestSinkWriterNilReceiverBehavior(t *testing.T) {
	var w *SinkWriter
	w.Close()
	require.NoError(t, w.WriteRecords(context.Background(), []Record{{UnsID: 1}}))
	require.NoError(t, w.EnsureSchema(context.Background()))
	_, err := w.ensurePhysicalForRecords(context.Background(), []Record{{UnsID: 1}})
	require.NoError(t, err)
	require.NoError(t, w.writeAliasRecords(context.Background(), "a", nil))
}

func TestNewSinkWriter_InvalidURLBehavior(t *testing.T) {
	_, err := NewSinkWriter(context.Background(), "://bad", 100)
	require.Error(t, err)
	_, err = NewSinkWriter(context.Background(), "postgres://user:pass@127.0.0.1:1/db?sslmode=disable", 0)
	require.Error(t, err)
}

func TestWriteRecords_MetricBehavior(t *testing.T) {
	_, dsn := testkit.NewTestDB(t)
	w, err := NewSinkWriter(context.Background(), dsn, 100)
	require.NoError(t, err)
	defer w.Close()
	ctx := context.Background()

	base := time.Now().UTC().Add(-time.Minute)
	records := []Record{
		{UnsID: 1, Alias: "w_metric1", Namespace: "W/ns/m1", TopicType: metricTopicType,
			Time: base, Quality: 1, Fields: []Field{{Name: "temp", Type: "double"}}, Values: map[string]string{"temp": "1.5"}},
		{UnsID: 1, Alias: "w_metric1", Namespace: "W/ns/m1", TopicType: metricTopicType,
			Time: base.Add(time.Second), Quality: 2, Fields: []Field{{Name: "temp", Type: "double"}}, Values: map[string]string{"temp": "2.5"}},
	}
	require.NoError(t, w.WriteRecords(ctx, records))

	// 物理表落库
	var count int64
	require.NoError(t, w.pool.QueryRow(ctx, `SELECT count(*) FROM public."w_metric1"`).Scan(&count))
	assert.Equal(t, int64(2), count)
	// 视图列名 = 字段名
	var temp float64
	require.NoError(t, w.pool.QueryRow(ctx, `SELECT "temp" FROM public."w_metric1" ORDER BY "_timestamp" DESC LIMIT 1`).Scan(&temp))
	assert.Equal(t, 2.5, temp)
	// 物理列 double_1 已加列
	var columnExists bool
	require.NoError(t, w.pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='uns_timeserial' AND column_name='double_1')`).Scan(&columnExists))
	assert.True(t, columnExists)
}

func TestWriteRecords_MetricBatchedBehavior(t *testing.T) {
	_, dsn := testkit.NewTestDB(t)
	w, err := NewSinkWriter(context.Background(), dsn, 2) // 每批 2 条
	require.NoError(t, err)
	defer w.Close()
	ctx := context.Background()

	base := time.Now().UTC().Add(-time.Minute)
	records := make([]Record, 0, 5)
	for i := 0; i < 5; i++ {
		records = append(records, Record{
			UnsID: 2, Alias: "w_metric2", Namespace: "W/ns/m2", TopicType: metricTopicType,
			Time: base.Add(time.Duration(i) * time.Second), Quality: 1,
			Fields: []Field{{Name: "temp", Type: "double"}}, Values: map[string]string{"temp": fmt.Sprintf("%d.5", i)},
		})
	}
	require.NoError(t, w.WriteRecords(ctx, records))
	var count int64
	require.NoError(t, w.pool.QueryRow(ctx, `SELECT count(*) FROM public."w_metric2"`).Scan(&count))
	assert.Equal(t, int64(5), count)
}

func TestWriteRecords_EventBehavior(t *testing.T) {
	_, dsn := testkit.NewTestDB(t)
	w, err := NewSinkWriter(context.Background(), dsn, 100)
	require.NoError(t, err)
	defer w.Close()
	ctx := context.Background()

	records := []Record{
		{UnsID: 3, Alias: "w_event1", Namespace: "W/ns/e1", TopicType: 1,
			Time: time.Now().UTC(), EventPayload: map[string]any{"a": 1, "b": "x"}},
	}
	require.NoError(t, w.WriteRecords(ctx, records))
	var count int64
	require.NoError(t, w.pool.QueryRow(ctx, `SELECT count(*) FROM public."w_event1"`).Scan(&count))
	assert.Equal(t, int64(1), count)
	var rawJSON []byte
	require.NoError(t, w.pool.QueryRow(ctx, `SELECT "_json" FROM public."w_event1"`).Scan(&rawJSON))
	assert.JSONEq(t, `{"a":1,"b":"x"}`, string(rawJSON))
	// 无字段记录走 value 兜底
	require.NoError(t, w.WriteRecords(ctx, []Record{{
		UnsID: 3, Alias: "w_event1", Namespace: "W/ns/e1", TopicType: 1,
		Time: time.Now().UTC(), Values: map[string]string{"value": "raw"},
	}}))
	var count2 int64
	require.NoError(t, w.pool.QueryRow(ctx, `SELECT count(*) FROM public."w_event1"`).Scan(&count2))
	assert.Equal(t, int64(2), count2)
}

func TestWriteRecords_MergeBehavior(t *testing.T) {
	_, dsn := testkit.NewTestDB(t)
	w, err := NewSinkWriter(context.Background(), dsn, 100)
	require.NoError(t, err)
	defer w.Close()
	ctx := context.Background()

	base := time.Now().UTC().Add(-time.Minute)
	// 同一 (id, 时间)：首次写入 temp=1.5 quality=1；再次写入 temp=2.5 quality=3
	first := Record{UnsID: 4, Alias: "w_merge1", Namespace: "W/ns/mg", TopicType: metricTopicType,
		Time: base, Quality: 1, Fields: []Field{{Name: "temp", Type: "double"}}, Values: map[string]string{"temp": "1.5"}}
	second := Record{UnsID: 4, Alias: "w_merge1", Namespace: "W/ns/mg", TopicType: metricTopicType,
		Time: base, Quality: 3, Fields: []Field{{Name: "temp", Type: "double"}}, Values: map[string]string{"temp": "2.5"}}
	require.NoError(t, w.WriteRecords(ctx, []Record{first}))
	require.NoError(t, w.WriteRecords(ctx, []Record{second}))

	var temp float64
	var quality int64
	require.NoError(t, w.pool.QueryRow(ctx, `SELECT "temp", "_quality" FROM public."w_merge1"`).Scan(&temp, &quality))
	assert.Equal(t, 2.5, temp)
	assert.Equal(t, int64(3), quality)
}

func TestSinkWriterCloseIdempotent(t *testing.T) {
	_, dsn := testkit.NewTestDB(t)
	w, err := NewSinkWriter(context.Background(), dsn, 100)
	require.NoError(t, err)
	w.Close()
	w.Close()
}

func TestSplitSQLListQuotedBehavior(t *testing.T) {
	// 嵌套括号 + 引号混合
	items := splitSQLList(`func(a, b), "x,y", 'z,w', last`)
	require.Len(t, items, 4)
	assert.Equal(t, "func(a, b)", items[0])
	assert.Equal(t, `"x,y"`, items[1])
	assert.Equal(t, `'z,w'`, items[2])
	assert.Equal(t, "last", items[3])
}

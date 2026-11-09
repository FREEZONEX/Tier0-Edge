package dataingest

// history_query_test.go
// 测试目标：HistoryQuery 历史查询的响应组装、采样/分页/聚合参数计算与 SQL 表达式构建。
// 正常路径：自动采样/手动分页/聚合三种查询策略；字段级聚合解析与函数校验；
//   jsonb 对象表达式分片；metric/event 取值表达式；DB 端写入后可查询回读。
// 异常情况：无数据返回空响应；缺失关系表静默降级为空；非法聚合字段/函数报错；
//   非 type2 节点与未开启历史返回空响应。
// 未覆盖：queryAutoSampledHistoryRows 的真实采样 SQL（由 QueryHistory 自动路径覆盖）；
//   聚合查询依赖 IntervalMs>0（OpenAPI 契约强制），IntervalMs=0 会触发 SQL 除零（见报告）。

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHistoryQueryResponse_Behavior(t *testing.T) {
	fields := []map[string]any{{"name": "a", "type": "double"}}
	// nil list → 空数组
	resp := historyQueryResponse(fields, nil, 0, historyQueryMeta{})
	list, ok := resp["list"].([]map[string]any)
	require.True(t, ok)
	assert.Empty(t, list)
	assert.Equal(t, int64(0), resp["total"])
	meta := resp["meta"].(map[string]any)
	assert.Equal(t, false, meta["aggregated"])
	assert.Equal(t, int64(0), meta["rawTotal"])
	// 有数据 + meta
	resp = historyQueryResponse(fields, []map[string]any{{"timestamp": int64(1)}}, 5, historyQueryMeta{
		Aggregated:       true,
		Sampled:          true,
		RawTotal:         100,
		ReturnedTotal:    2,
		ActualIntervalMs: 1000,
		Strategy:         "timeBucket",
	})
	meta = resp["meta"].(map[string]any)
	assert.Equal(t, true, meta["aggregated"])
	assert.Equal(t, true, meta["sampled"])
	assert.Equal(t, int64(100), meta["rawTotal"])
	assert.Equal(t, int64(1000), meta["actualIntervalMs"])
	assert.Equal(t, "timeBucket", meta["strategy"])
}

func TestEffectiveHistoryPointLimit_Behavior(t *testing.T) {
	assert.Equal(t, historySamplingPointLimit, effectiveHistoryPointLimit(1, 0))
	// 请求更小 limit 生效
	assert.Equal(t, 100, effectiveHistoryPointLimit(1, 100))
	// 请求超过上限取上限
	assert.Equal(t, historySamplingPointLimit, effectiveHistoryPointLimit(1, 99999))
	// 字段多 → 单元格上限收缩
	assert.Equal(t, 1, effectiveHistoryPointLimit(50000, 0))
	assert.Equal(t, 10, effectiveHistoryPointLimit(5000, 100))
	// fieldCount<=0 按 1 处理
	assert.Equal(t, historySamplingPointLimit, effectiveHistoryPointLimit(0, 0))
}

func TestHistorySamplingInterval_Behavior(t *testing.T) {
	assert.Equal(t, int64(10), historySamplingInterval(0, 99, 10))
	assert.Equal(t, int64(1), historySamplingInterval(0, 9, 10))
	assert.Equal(t, int64(1), historySamplingInterval(100, 50, 10)) // 反向区间
	assert.Equal(t, int64(1), historySamplingInterval(0, 99, 0))
}

func TestEffectiveHistoryInterval_Behavior(t *testing.T) {
	// 请求间隔大于自动间隔 → 保留请求值
	assert.Equal(t, int64(1000), effectiveHistoryInterval(0, 9999, 1000, 10))
	// 否则取自动间隔
	assert.Equal(t, int64(100), effectiveHistoryInterval(0, 9999, 50, 100))
}

func TestNormalizeHistoryPage_Behavior(t *testing.T) {
	page, size := normalizeHistoryPage(0, 0, 100, false)
	assert.Equal(t, 1, page)
	assert.Equal(t, 100, size)
	page, size = normalizeHistoryPage(-1, -1, 100, false)
	assert.Equal(t, 1, page)
	assert.Equal(t, 100, size)
	page, size = normalizeHistoryPage(3, 5000, 100, false)
	assert.Equal(t, 3, page)
	assert.Equal(t, maxHistoryLimit, size)
	page, size = normalizeHistoryPage(9, 0, 100, true)
	assert.Equal(t, 1, page)
	assert.Equal(t, 100, size)
}

func TestResolveHistoryAggregationFields_Behavior(t *testing.T) {
	fields := []Field{
		{Name: "temp", Type: "double"},
		{Name: "name", Type: "string"},
	}
	// 未指定 → 全部字段，数值字段默认 avg、文本默认 last
	resolved, err := resolveHistoryAggregationFields(fields, nil)
	require.NoError(t, err)
	require.Len(t, resolved, 2)
	assert.Equal(t, "avg", resolved[0].Function)
	assert.Equal(t, "last", resolved[1].Function)
	// 显式指定
	resolved, err = resolveHistoryAggregationFields(fields, []HistoryAggregationField{
		{Name: "temp", Function: "max"},
		{Name: "name", Function: "first"},
	})
	require.NoError(t, err)
	assert.Equal(t, "max", resolved[0].Function)
	// 不存在的字段
	_, err = resolveHistoryAggregationFields(fields, []HistoryAggregationField{{Name: "missing"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	// 重复字段
	_, err = resolveHistoryAggregationFields(fields, []HistoryAggregationField{
		{Name: "temp"}, {Name: "TEMP"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicated")
	// 非法函数
	_, err = resolveHistoryAggregationFields(fields, []HistoryAggregationField{{Name: "temp", Function: "median"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
	// 数值函数作用于非数值字段
	_, err = resolveHistoryAggregationFields(fields, []HistoryAggregationField{{Name: "name", Function: "sum"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires numeric")
}

func TestValidateHistoryAggregationFunction_Behavior(t *testing.T) {
	for _, fn := range []string{"avg", "min", "max", "sum", "count", "first", "last"} {
		assert.NoError(t, validateHistoryAggregationFunction(fn))
	}
	err := validateHistoryAggregationFunction("median")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "median")
}

func TestHistoryFunctionRequiresNumeric_Behavior(t *testing.T) {
	for _, fn := range []string{"avg", "min", "max", "sum"} {
		assert.True(t, historyFunctionRequiresNumeric(fn))
	}
	for _, fn := range []string{"count", "first", "last"} {
		assert.False(t, historyFunctionRequiresNumeric(fn))
	}
}

func TestHistoryFieldTypeNumeric_Behavior(t *testing.T) {
	for _, kind := range []string{"int", "integer", "long", "float", "double", "number", "decimal", "numeric", "smallint", "bigint", "real"} {
		assert.True(t, historyFieldTypeNumeric(kind), "type %s should be numeric", kind)
	}
	for _, kind := range []string{"string", "boolean", "datetime", "blob", ""} {
		assert.False(t, historyFieldTypeNumeric(kind), "type %s should not be numeric", kind)
	}
}

func TestIsMissingHistoryRelation_Behavior(t *testing.T) {
	assert.False(t, isMissingHistoryRelation(nil))
	assert.True(t, isMissingHistoryRelation(fmt.Errorf(`relation "uns.xxx" does not exist`)))
	assert.True(t, isMissingHistoryRelation(fmt.Errorf("undefined table: xxx")))
	assert.False(t, isMissingHistoryRelation(fmt.Errorf("syntax error")))
}

func TestHistoryJSONBuildObjectExpr_Behavior(t *testing.T) {
	assert.Equal(t, `'{}'::jsonb`, historyJSONBuildObjectExpr(nil))
	one := historyJSONBuildObjectExpr([]string{"'a'", "count(x)"})
	assert.Equal(t, "jsonb_build_object('a', count(x))", one)
	// 超过 100 参数 → 分片拼接
	args := make([]string, 0, 250)
	for i := 0; i < 250; i++ {
		args = append(args, fmt.Sprintf("'k%d'", i), "1")
	}
	multi := historyJSONBuildObjectExpr(args)
	assert.Contains(t, multi, " || ")
	assert.Contains(t, multi, "jsonb_build_object(")
}

func TestEnterpriseHistoryValueExpr_Behavior(t *testing.T) {
	field := HistoryAggregationField{Name: "temp", Function: "avg", Type: "double"}
	valueExpr, numericExpr := enterpriseHistoryValueExpr(metricTopicType, field)
	assert.Equal(t, `"temp"`, valueExpr)
	assert.Equal(t, `("temp")::double precision`, numericExpr)
	// 非数值字段 → numeric NULL
	field.Type = "string"
	_, numericExpr = enterpriseHistoryValueExpr(metricTopicType, field)
	assert.Equal(t, "NULL::double precision", numericExpr)
	// 事件类型 → jsonb 取值 + 数值正则过滤
	valueExpr, numericExpr = enterpriseHistoryValueExpr(1, field)
	assert.Contains(t, valueExpr, "jsonb_extract_path")
	assert.Contains(t, numericExpr, "jsonb_extract_path_text")
	assert.Contains(t, numericExpr, historyNumericRegexp)
}

func TestHistoryAggregateSQL_Behavior(t *testing.T) {
	assert.Equal(t, "count(v)", historyAggregateSQL("count", "v", "n", "ts"))
	assert.Equal(t, "avg(n)", historyAggregateSQL("avg", "v", "n", "ts"))
	assert.Equal(t, "sum(n)", historyAggregateSQL("sum", "v", "n", "ts"))
	first := historyAggregateSQL("first", "v", "n", "ts")
	assert.Contains(t, first, "array_agg(v ORDER BY ts ASC)")
	last := historyAggregateSQL("last", "v", "n", "ts")
	assert.Contains(t, last, "array_agg(v ORDER BY ts DESC)")
}

func TestCanUseFullWindowBoundaryQuery_Behavior(t *testing.T) {
	startAt := time.UnixMilli(1000).UTC()
	endAt := time.UnixMilli(1999).UTC()
	query := HistoryQuery{Auto: true, SkipCount: true, IntervalMs: 1000}
	fields := []HistoryAggregationField{
		{Name: "a", Function: "first"},
		{Name: "b", Function: "last"},
	}
	assert.True(t, canUseFullWindowBoundaryQuery(query, startAt, endAt, 1000, fields))
	assert.False(t, canUseFullWindowBoundaryQuery(query, startAt, endAt, 999, fields))

	query.Auto = false
	assert.False(t, canUseFullWindowBoundaryQuery(query, startAt, endAt, 1000, fields))
	query.Auto = true
	query.SkipCount = false
	assert.False(t, canUseFullWindowBoundaryQuery(query, startAt, endAt, 1000, fields))
	query.SkipCount = true
	query.IntervalMs = 0
	assert.False(t, canUseFullWindowBoundaryQuery(query, startAt, endAt, 1000, fields))
	query.IntervalMs = 1000
	assert.False(t, canUseFullWindowBoundaryQuery(query, startAt, endAt, 1000, []HistoryAggregationField{{Name: "a", Function: "avg"}}))
	assert.False(t, canUseFullWindowBoundaryQuery(query, startAt, endAt, 1000, nil))
}

func TestFullWindowBoundaryHistorySQL_Behavior(t *testing.T) {
	query := fullWindowBoundaryHistorySQL(`public."metric"`, metricTimeColumn, metricTopicType, []HistoryAggregationField{
		{Name: "a", Function: "first", Type: "double"},
		{Name: "b", Function: "last", Type: "string"},
	})
	assert.Contains(t, query, `SELECT "a"`)
	assert.Contains(t, query, `ORDER BY "_timestamp" ASC`)
	assert.Contains(t, query, `ORDER BY "_timestamp" DESC`)
	assert.NotContains(t, query, "array_agg")
	assert.NotContains(t, query, "GROUP BY")

	eventQuery := fullWindowBoundaryHistorySQL(`public."event"`, metricTimeColumn, 1, []HistoryAggregationField{
		{Name: "state", Function: "last", Type: "string"},
	})
	assert.Contains(t, eventQuery, `jsonb_extract_path("_json", 'state')`)
}

func TestQueryHistory_EmptyResponsesBehavior(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	s, err := New(context.Background(), config.DataIngestConf{}, dsn)
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()

	node := repo.UnsNode{
		ID:            801,
		Namespace:     "Test/ns/hist1",
		Alias:         "hist1",
		Name:          "hist1",
		Type:          2,
		TopicType:     metricTopicType,
		EnableHistory: 1,
		Schema:        json.RawMessage(`[{"name":"temp","type":"double"}]`),
	}
	testkit.Insert(t, store.CommonDB(), &node)

	// 有定义但无数据 → 空响应
	resp, err := s.QueryHistory(ctx, node, HistoryQuery{StartMs: 0, EndMs: 0, Auto: true})
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp["total"])
	assert.Empty(t, resp["list"])

	// 非 type2 节点 → 空响应
	resp, err = s.QueryHistory(ctx, repo.UnsNode{ID: 1, Type: 1}, HistoryQuery{Auto: true})
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp["total"])

	// 未开启历史（SaveToDB=false）→ 空响应
	noSave := repo.UnsNode{ID: 802, Namespace: "Test/ns/nosave", Type: 2, TopicType: 3, EnableHistory: 2}
	testkit.Insert(t, store.CommonDB(), &noSave)
	resp, err = s.QueryHistory(ctx, noSave, HistoryQuery{Auto: true})
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp["total"])

	// 缺失关系表 → 静默降级空响应
	ghost := repo.UnsNode{ID: 803, Namespace: "Test/ns/ghost", Alias: "ghost_table_never_created", Type: 2, TopicType: 3, EnableHistory: 1}
	testkit.Insert(t, store.CommonDB(), &ghost)
	resp, err = s.QueryHistory(ctx, ghost, HistoryQuery{Auto: true})
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp["total"])
}

func TestQueryHistory_AutoAndManualBehavior(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	s, err := New(context.Background(), config.DataIngestConf{}, dsn)
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()
	node := repo.UnsNode{
		ID:            804,
		Namespace:     "Test/ns/hist2",
		Alias:         "hist2",
		Name:          "hist2",
		Type:          2,
		TopicType:     metricTopicType,
		EnableHistory: 1,
		Schema:        json.RawMessage(`[{"name":"temp","type":"double"}]`),
	}
	testkit.Insert(t, store.CommonDB(), &node)

	base := time.Now().UTC().Add(-time.Minute)
	records := []Record{
		{UnsID: 804, Alias: "hist2", Namespace: "Test/ns/hist2", TopicType: metricTopicType,
			Time: base, Quality: 1, Fields: []Field{{Name: "temp", Type: "double"}}, Values: map[string]string{"temp": "1.5"}},
		{UnsID: 804, Alias: "hist2", Namespace: "Test/ns/hist2", TopicType: metricTopicType,
			Time: base.Add(time.Second), Quality: 1, Fields: []Field{{Name: "temp", Type: "double"}}, Values: map[string]string{"temp": "2.5"}},
		{UnsID: 804, Alias: "hist2", Namespace: "Test/ns/hist2", TopicType: metricTopicType,
			Time: base.Add(2 * time.Second), Quality: 1, Fields: []Field{{Name: "temp", Type: "double"}}, Values: map[string]string{"temp": "3.5"}},
	}
	require.NoError(t, s.writer.WriteRecords(ctx, records))

	// 自动模式：数据量小 → 全量返回不采样
	resp, err := s.QueryHistory(ctx, node, HistoryQuery{
		StartMs: base.Add(-time.Second).UnixMilli(),
		EndMs:   base.Add(5 * time.Second).UnixMilli(),
		Auto:    true,
	})
	require.NoError(t, err)
	list := resp["list"].([]map[string]any)
	require.Len(t, list, 3)
	assert.Equal(t, int64(3), resp["total"])
	meta := resp["meta"].(map[string]any)
	assert.Equal(t, false, meta["sampled"])
	row := list[0]
	payload := row["payload"].(map[string]any)
	assert.Equal(t, 1.5, payload["temp"])
	// 时间升序
	assert.LessOrEqual(t, list[0]["timestamp"].(int64), list[1]["timestamp"].(int64))

	// 手动分页：page=2 size=2 → 只返回第 2 页 1 条，total=rawTotal
	resp, err = s.QueryHistory(ctx, node, HistoryQuery{
		StartMs: base.Add(-time.Second).UnixMilli(),
		EndMs:   base.Add(5 * time.Second).UnixMilli(),
		Auto:    false,
		Page:    2,
		Size:    2,
	})
	require.NoError(t, err)
	list = resp["list"].([]map[string]any)
	require.Len(t, list, 1)
	assert.Equal(t, int64(3), resp["total"])

	// 显式免 COUNT：不再返回精确 total，通过 hasMore 驱动翻页。
	resp, err = s.QueryHistory(ctx, node, HistoryQuery{
		StartMs:   base.Add(-time.Second).UnixMilli(),
		EndMs:     base.Add(5 * time.Second).UnixMilli(),
		Auto:      false,
		Page:      1,
		Size:      2,
		SkipCount: true,
	})
	require.NoError(t, err)
	list = resp["list"].([]map[string]any)
	require.Len(t, list, 2)
	assert.Equal(t, int64(-1), resp["total"])
	meta = resp["meta"].(map[string]any)
	assert.Equal(t, false, meta["totalExact"])
	assert.Equal(t, true, meta["hasMore"])
}

func TestQueryHistory_AggregateBehavior(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	s, err := New(context.Background(), config.DataIngestConf{}, dsn)
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()
	node := repo.UnsNode{
		ID:            805,
		Namespace:     "Test/ns/hist3",
		Alias:         "hist3",
		Name:          "hist3",
		Type:          2,
		TopicType:     metricTopicType,
		EnableHistory: 1,
		Schema:        json.RawMessage(`[{"name":"temp","type":"double"}]`),
	}
	testkit.Insert(t, store.CommonDB(), &node)

	base := time.Now().UTC().Add(-time.Minute)
	records := []Record{
		{UnsID: 805, Alias: "hist3", Namespace: "Test/ns/hist3", TopicType: metricTopicType,
			Time: base, Quality: 1, Fields: []Field{{Name: "temp", Type: "double"}}, Values: map[string]string{"temp": "10"}},
		{UnsID: 805, Alias: "hist3", Namespace: "Test/ns/hist3", TopicType: metricTopicType,
			Time: base.Add(time.Second), Quality: 1, Fields: []Field{{Name: "temp", Type: "double"}}, Values: map[string]string{"temp": "30"}},
	}
	require.NoError(t, s.writer.WriteRecords(ctx, records))

	resp, err := s.QueryHistory(ctx, node, HistoryQuery{
		StartMs:    base.Add(-time.Second).UnixMilli(),
		EndMs:      base.Add(3 * time.Second).UnixMilli(),
		Auto:       true,
		Aggregate:  true,
		IntervalMs: 2000,
		Fields:     []HistoryAggregationField{{Name: "temp", Function: "avg"}},
	})
	require.NoError(t, err)
	meta := resp["meta"].(map[string]any)
	assert.Equal(t, true, meta["aggregated"])
	list := resp["list"].([]map[string]any)
	require.NotEmpty(t, list)
	payload := list[0]["payload"].(map[string]any)
	// 两个桶（10 与 30 落在不同 2s 桶）avg 分别为 10 / 30
	assert.Contains(t, []any{10.0, 30.0}, payload["temp"])
}

func TestQueryHistory_FullWindowBoundaryBehavior(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	s, err := New(context.Background(), config.DataIngestConf{}, dsn)
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()
	node := repo.UnsNode{
		ID:               807,
		Namespace:        "Test/ns/hist-boundary",
		Alias:            "hist_boundary",
		Name:             "hist-boundary",
		Type:             2,
		TopicType:        metricTopicType,
		EnableHistory:    1,
		Schema:           json.RawMessage(`[{"name":"a","type":"double"},{"name":"b","type":"double"},{"name":"c","type":"double"},{"name":"d","type":"double"}]`),
		ExtendProperties: json.RawMessage(`{}`),
	}
	testkit.Insert(t, store.CommonDB(), &node)

	base := time.Now().UTC().Truncate(time.Millisecond).Add(-time.Minute)
	fields := []Field{{Name: "a", Type: "double"}, {Name: "b", Type: "double"}, {Name: "c", Type: "double"}, {Name: "d", Type: "double"}}
	records := []Record{
		{UnsID: 807, Alias: "hist_boundary", Namespace: "Test/ns/hist-boundary", TopicType: metricTopicType,
			Time: base, Quality: 1, Fields: fields, Values: map[string]string{"a": "1", "c": "100"}},
		{UnsID: 807, Alias: "hist_boundary", Namespace: "Test/ns/hist-boundary", TopicType: metricTopicType,
			Time: base.Add(time.Second), Quality: 2, Fields: fields, Values: map[string]string{"a": "2", "b": "10", "c": "101", "d": "199"}},
		{UnsID: 807, Alias: "hist_boundary", Namespace: "Test/ns/hist-boundary", TopicType: metricTopicType,
			Time: base.Add(2 * time.Second), Quality: 3, Fields: fields, Values: map[string]string{"b": "20", "d": "200"}},
	}
	for _, record := range records {
		require.NoError(t, s.writer.WriteRecords(ctx, []Record{record}))
	}
	var firstB, firstC float64
	require.NoError(t, s.writer.pool.QueryRow(ctx, `SELECT "b" FROM public."hist_boundary" WHERE "b" IS NOT NULL ORDER BY "_timestamp" ASC LIMIT 1`).Scan(&firstB))
	require.NoError(t, s.writer.pool.QueryRow(ctx, `SELECT "c" FROM public."hist_boundary" WHERE "c" IS NOT NULL ORDER BY "_timestamp" ASC LIMIT 1`).Scan(&firstC))
	assert.Equal(t, 10.0, firstB)
	assert.Equal(t, 100.0, firstC)

	aggregationFields := []HistoryAggregationField{
		{Name: "a", Function: "last"},
		{Name: "b", Function: "first"},
		{Name: "c", Function: "first"},
		{Name: "d", Function: "last"},
	}
	legacyResp, err := s.QueryHistory(ctx, node, HistoryQuery{
		StartMs:    base.UnixMilli(),
		EndMs:      base.Add(2 * time.Second).UnixMilli(),
		Auto:       true,
		Aggregate:  true,
		IntervalMs: 2001,
		Fields:     aggregationFields,
	})
	require.NoError(t, err)
	legacyList := legacyResp["list"].([]map[string]any)
	require.Len(t, legacyList, 1)
	legacyPayload := legacyList[0]["payload"].(map[string]any)

	resp, err := s.QueryHistory(ctx, node, HistoryQuery{
		StartMs:    base.UnixMilli(),
		EndMs:      base.Add(2 * time.Second).UnixMilli(),
		Auto:       true,
		Aggregate:  true,
		IntervalMs: 2001,
		SkipCount:  true,
		Fields:     aggregationFields,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(-1), resp["total"])
	list := resp["list"].([]map[string]any)
	require.Len(t, list, 1)
	assert.Equal(t, base.UnixMilli(), list[0]["timestamp"])
	payload := list[0]["payload"].(map[string]any)
	assert.Equal(t, legacyPayload, payload)
	assert.Equal(t, 2.0, payload["a"])
	assert.Equal(t, 10.0, payload["b"])
	assert.Equal(t, 100.0, payload["c"])
	assert.Equal(t, 200.0, payload["d"])
	meta := resp["meta"].(map[string]any)
	assert.Equal(t, true, meta["aggregated"])
	assert.Equal(t, false, meta["totalExact"])
	assert.Equal(t, false, meta["hasMore"])
	assert.Equal(t, int64(-1), meta["rawTotal"])
	assert.Equal(t, int64(1), meta["returnedTotal"])
}

func TestHistory_EntrypointBehavior(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	s, err := New(context.Background(), config.DataIngestConf{}, dsn)
	require.NoError(t, err)
	defer s.Close()
	ctx := context.Background()
	node := repo.UnsNode{ID: 806, Namespace: "Test/ns/entry", Alias: "entry", Type: 2, TopicType: 3, EnableHistory: 1}
	testkit.Insert(t, store.CommonDB(), &node)
	// History 入口（Auto 模式）无数据 → 空响应
	resp, err := s.History(ctx, node, 0, 0, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp["total"])
}

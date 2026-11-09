package dataingest

// definition_test.go
// 测试目标：DefinitionResolver 定义解析与 Definition 构建逻辑。
// 正常路径：按 topic（namespace/ID）解析出 Definition；schema 列表/对象两种形态解析；
//   DataType 回退规则；时间戳/质量系统字段识别；缓存命中与失效；节点构造。
// 异常情况：空 topic、未知 namespace/ID 返回 ErrDefinitionNotFound；非法 schema
//   （空字段名/重复字段）校验报错；nil 接收者调用不 panic。

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefinitionFromNode_MetricBehavior(t *testing.T) {
	node := repo.UnsNode{
		ID:            101,
		Namespace:     "/Metric/line1/temp/",
		Alias:         "metric-temp",
		Type:          2,
		TopicType:     metricTopicType,
		EnableHistory: 1,
		Schema:        json.RawMessage(`[{"name":"temp","type":"double","lowerLimit":0,"upperLimit":100}]`),
	}
	def := definitionFromNode(node)
	require.NotNil(t, def)
	assert.Equal(t, int64(101), def.ID)
	assert.Equal(t, "Metric/line1/temp", def.Namespace)
	assert.Equal(t, "metric-temp", def.Alias)
	assert.Equal(t, metricTopicType, def.TopicType)
	assert.Equal(t, 1, def.DataType) // schema 无 dataType，回退 dataTypeFromTopicType(3)
	assert.True(t, def.SaveToDB)     // EnableHistory == 1
	assert.Equal(t, metricTimeColumn, def.TimestampField)
	assert.Equal(t, metricQualityColumn, def.QualityField)
	require.Len(t, def.Fields, 1)
	assert.Equal(t, "temp", def.Fields[0].Name)
	assert.Equal(t, "double", def.Fields[0].Type)
	require.NotNil(t, def.Fields[0].LowerLimit)
	require.NotNil(t, def.Fields[0].UpperLimit)
	assert.Equal(t, float64(0), *def.Fields[0].LowerLimit)
	assert.Equal(t, float64(100), *def.Fields[0].UpperLimit)
}

func TestDefinitionFromNode_EventTimestampQualityFields(t *testing.T) {
	node := repo.UnsNode{
		ID:        102,
		Namespace: "State/action1",
		Alias:     "",
		Type:      2,
		TopicType: 1,
		Schema:    json.RawMessage(`[{"name":"timeStamp","type":"long"},{"name":"quality","type":"int"},{"name":"data","type":"string"}]`),
	}
	def := definitionFromNode(node)
	require.NotNil(t, def)
	// Alias 为空时回退 Namespace
	assert.Equal(t, "State/action1", def.Alias)
	assert.Equal(t, "timeStamp", def.TimestampField)
	assert.Equal(t, "quality", def.QualityField)
	assert.Equal(t, 8, def.DataType) // topicType 1 → 8
	// SaveToDB 默认关闭（EnableHistory 默认 0）
	assert.False(t, def.SaveToDB)
}

func TestDefinitionFromNode_SchemaObjectForm(t *testing.T) {
	node := repo.UnsNode{
		ID:        103,
		Namespace: "Metric/obj",
		Type:      2,
		TopicType: 3,
		Schema:    json.RawMessage(`{"dataType":8,"fields":[{"name":"v","type":"float"}]}`),
	}
	def := definitionFromNode(node)
	require.NotNil(t, def)
	assert.Equal(t, 8, def.DataType) // schema 显式 dataType 优先
	require.Len(t, def.Fields, 1)
	assert.Equal(t, "v", def.Fields[0].Name)
}

func TestIsTimestampSystemField_Behavior(t *testing.T) {
	// metric 类型：_timestamp/_ct/created_time 命中，timeStamp 不命中
	assert.True(t, isTimestampSystemField("_timestamp", metricTopicType))
	assert.True(t, isTimestampSystemField("_CT", metricTopicType))
	assert.True(t, isTimestampSystemField("created_time", metricTopicType))
	assert.False(t, isTimestampSystemField("timeStamp", metricTopicType))
	// 事件类型：timeStamp/timestamp/_ct/created_time 命中
	assert.True(t, isTimestampSystemField("timeStamp", 1))
	assert.True(t, isTimestampSystemField("TIMESTAMP", 1))
	assert.False(t, isTimestampSystemField("_timestamp", 1))
}

func TestIsQualitySystemField_Behavior(t *testing.T) {
	assert.True(t, isQualitySystemField("_quality", metricTopicType))
	assert.True(t, isQualitySystemField("_qos", metricTopicType))
	assert.False(t, isQualitySystemField("quality", metricTopicType))
	assert.True(t, isQualitySystemField("quality", 1))
	assert.True(t, isQualitySystemField("QUALITY", 1))
	assert.False(t, isQualitySystemField("_quality", 1))
}

func TestParseSchema_Behavior(t *testing.T) {
	// 空 schema
	doc := parseSchema(nil)
	assert.Equal(t, 0, doc.DataType)
	assert.Empty(t, doc.Fields)
	// 列表形态
	doc = parseSchema(json.RawMessage(`[{"name":"a","type":"double"}]`))
	assert.Equal(t, 0, doc.DataType)
	require.Len(t, doc.Fields, 1)
	assert.Equal(t, "a", doc.Fields[0].Name)
	// 对象形态
	doc = parseSchema(json.RawMessage(`{"dataType":8,"fields":[{"name":"a","type":"double"}]}`))
	assert.Equal(t, 8, doc.DataType)
	require.Len(t, doc.Fields, 1)
	// 非法 JSON → 空结构
	doc = parseSchema(json.RawMessage(`{invalid`))
	assert.Equal(t, 0, doc.DataType)
	assert.Empty(t, doc.Fields)
}

func TestDataTypeFromTopicType_Behavior(t *testing.T) {
	assert.Equal(t, 1, dataTypeFromTopicType(3))
	assert.Equal(t, 8, dataTypeFromTopicType(1))
	assert.Equal(t, 8, dataTypeFromTopicType(2))
	assert.Equal(t, 0, dataTypeFromTopicType(0))
	assert.Equal(t, 0, dataTypeFromTopicType(9))
}

func TestNormalizeFields_Behavior(t *testing.T) {
	fields := normalizeFields([]Field{
		{Name: "  A  ", Type: "INT"},
		{Name: "a", Type: "double"},  // 与 A 大小写重复，去重
		{Name: "", Type: "string"},   // 空名跳过
		{Name: "b", Type: "BOOL"},    // 类型归一化
		{Name: "c", Type: "unknown"}, // 未知类型 → string
	})
	require.Len(t, fields, 3)
	assert.Equal(t, "A", fields[0].Name)
	assert.Equal(t, "integer", fields[0].Type)
	assert.Equal(t, "b", fields[1].Name)
	assert.Equal(t, "boolean", fields[1].Type)
	assert.Equal(t, "c", fields[2].Name)
	assert.Equal(t, "string", fields[2].Type)
}

func TestValidateSchema_Behavior(t *testing.T) {
	// 合法
	assert.NoError(t, ValidateSchema(json.RawMessage(`[{"name":"a","type":"double"},{"name":"b","type":"string"}]`)))
	assert.NoError(t, ValidateSchema(json.RawMessage(`[]`)))
	// 空字段名
	err := ValidateSchema(json.RawMessage(`[{"name":" ","type":"double"}]`))
	require.Error(t, err)
	assert.Equal(t, "schema field name is empty", err.Error())
	// 重复字段（大小写不敏感）
	err = ValidateSchema(json.RawMessage(`[{"name":"a","type":"double"},{"name":"A","type":"string"}]`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate schema field")
}

func TestDefinitionResolver_ResolveNotFoundBehavior(t *testing.T) {
	r := NewDefinitionResolver(nil)
	_, err := r.Resolve(context.Background(), "")
	require.ErrorIs(t, err, ErrDefinitionNotFound)
	_, err = r.Resolve(context.Background(), "   /  ")
	require.ErrorIs(t, err, ErrDefinitionNotFound)
	// 未知 namespace（走 DB 查询）
	store, _ := testkit.NewTestDB(t)
	_ = store
	dbResolver := NewDefinitionResolver(repo.NewUnsRepo(context.Background()))
	_, err = dbResolver.Resolve(context.Background(), "not/exist/ns")
	require.ErrorIs(t, err, ErrDefinitionNotFound)
	// 未知数字 ID → 回落 namespace 查询 → not found
	_, err = dbResolver.Resolve(context.Background(), "999999")
	require.ErrorIs(t, err, ErrDefinitionNotFound)
}

func TestDefinitionResolver_ResolveAndInvalidateBehavior(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	db := store.CommonDB()
	node := repo.UnsNode{
		ID:            901,
		Namespace:     "Test/ns/resolve1",
		Alias:         "resolve1",
		Name:          "resolve1",
		Type:          2,
		TopicType:     3,
		EnableHistory: 1,
		Schema:        json.RawMessage(`[{"name":"temp","type":"double"}]`),
	}
	testkit.Insert(t, db, &node)

	r := NewDefinitionResolver(repo.NewUnsRepo(context.Background()))
	ctx := context.Background()

	// 按 namespace 解析（带首尾斜杠会被裁剪）
	def, err := r.Resolve(ctx, "/Test/ns/resolve1/")
	require.NoError(t, err)
	require.NotNil(t, def)
	assert.Equal(t, int64(901), def.ID)
	assert.Equal(t, metricTopicType, def.TopicType)

	// 按数字 ID 解析
	defByID, err := r.Resolve(ctx, "901")
	require.NoError(t, err)
	require.NotNil(t, defByID)
	assert.Equal(t, "Test/ns/resolve1", defByID.Namespace)

	// 缓存命中：删除 DB 记录后仍可解析
	require.NoError(t, db.Exec("DELETE FROM uns_namespace_node_info WHERE id = ?", node.ID).Error)
	defCached, err := r.Resolve(ctx, "Test/ns/resolve1")
	require.NoError(t, err)
	require.NotNil(t, defCached)
	assert.Equal(t, int64(901), defCached.ID)

	// InvalidateNode 后缓存失效 → 再解析报 not found
	r.InvalidateNode(node)
	_, err = r.Resolve(ctx, "Test/ns/resolve1")
	require.ErrorIs(t, err, ErrDefinitionNotFound)

	// 重新插入并批量失效
	testkit.Insert(t, db, &node)
	_, err = r.Resolve(ctx, "Test/ns/resolve1")
	require.NoError(t, err)
	r.InvalidateNodes([]repo.UnsNode{node})
	_, err = r.Resolve(ctx, "Test/ns/resolve1")
	require.ErrorIs(t, err, ErrDefinitionNotFound)
}

func TestDefinitionResolver_ResolveWrongTypeBehavior(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	db := store.CommonDB()
	// type != 2 的节点不能解析为定义
	node := repo.UnsNode{
		ID:        902,
		Namespace: "Test/ns/folder1",
		Name:      "folder1",
		Type:      1,
		TopicType: 0,
	}
	testkit.Insert(t, db, &node)
	r := NewDefinitionResolver(repo.NewUnsRepo(context.Background()))
	_, err := r.Resolve(context.Background(), "Test/ns/folder1")
	require.ErrorIs(t, err, ErrDefinitionNotFound)
	_, err = r.Resolve(context.Background(), "902")
	require.ErrorIs(t, err, ErrDefinitionNotFound)
}

func TestDefinitionResolver_NilReceiverBehavior(t *testing.T) {
	var r *DefinitionResolver
	require.NotPanics(t, func() { r.InvalidateNode(repo.UnsNode{ID: 1}) })
	require.NotPanics(t, func() { r.InvalidateNodes([]repo.UnsNode{{ID: 1}}) })
}

func TestDefinitionResolver_RememberCachesByTopic(t *testing.T) {
	r := NewDefinitionResolver(nil)
	def := r.remember(&Definition{ID: 7, Namespace: "Cached/ns"})
	assert.Same(t, def, r.cachedTopic("Cached/ns"))
	// 空 namespace 不写 byTopic 缓存
	def2 := r.remember(&Definition{ID: 8, Namespace: ""})
	assert.Same(t, def2, r.cachedTopic(""))
	assert.Nil(t, r.cachedTopic("missing"))
}

func TestErrDefinitionNotFoundDefined(t *testing.T) {
	assert.True(t, errors.Is(ErrDefinitionNotFound, ErrDefinitionNotFound))
	assert.Equal(t, "uns definition not found", ErrDefinitionNotFound.Error())
}

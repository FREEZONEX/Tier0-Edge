// service.go 纯函数/纯逻辑单元测试（无 DB 依赖）。
// 覆盖：别名生成与校验、命名空间解析、导入导出解析、schema 清洗、
// 类型枚举转换、响应结构组装等正常路径与异常输入路径。
package uns

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"backend/internal/repo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stableSuffixFor(t *testing.T, seed string) string {
	t.Helper()
	sum := md5.Sum([]byte(seed))
	return hex.EncodeToString(sum[:])[:aliasSuffixHexLength]
}

// ---------- 别名基础工具 ----------

func TestNormalizeAliasCandidate_Behavior(t *testing.T) {
	require.Equal(t, "foo/bar", normalizeAliasCandidate("/foo/bar/"))
	require.Equal(t, "foo", normalizeAliasCandidate("  foo  "))
	require.Equal(t, "", normalizeAliasCandidate(""))
	require.Equal(t, "", normalizeAliasCandidate("///"))
}

func TestIsAliasFormatOK_Behavior(t *testing.T) {
	require.True(t, isAliasFormatOK("abc_123"))
	require.True(t, isAliasFormatOK("Abc$d"))
	require.True(t, isAliasFormatOK("_x"))
	require.False(t, isAliasFormatOK(""))
	require.False(t, isAliasFormatOK("1abc"))
	require.False(t, isAliasFormatOK("abc def"))
	require.False(t, isAliasFormatOK("abc-"))
	require.False(t, isAliasFormatOK("中文"))
}

func TestSanitizeAndPinyin_Behavior(t *testing.T) {
	require.Equal(t, "a_b_c_d", sanitizeAndPinyin("a/b-c d!"))
	require.Equal(t, "zhongwen", sanitizeAndPinyin("中文"))
	require.Equal(t, "abc", sanitizeAndPinyin(" abc "))
	require.Equal(t, "", sanitizeAndPinyin("!!! "))
	require.Equal(t, "a$1", sanitizeAndPinyin("a$1"))
}

func TestAliasPartRune_Behavior(t *testing.T) {
	require.True(t, isAliasPartRune('a'))
	require.True(t, isAliasPartRune('Z'))
	require.True(t, isAliasPartRune('9'))
	require.True(t, isAliasPartRune('_'))
	require.True(t, isAliasPartRune('$'))
	require.False(t, isAliasPartRune('-'))
	require.False(t, isAliasPartRune('/'))
	require.False(t, isAliasPartRune('中'))
}

func TestStartsWithAliasLetter_Behavior(t *testing.T) {
	require.True(t, startsWithAliasLetter("abc"))
	require.True(t, startsWithAliasLetter("Abc"))
	require.False(t, startsWithAliasLetter("1abc"))
	require.False(t, startsWithAliasLetter("_abc"))
	require.False(t, startsWithAliasLetter(""))
}

func TestFirstNRunes_Behavior(t *testing.T) {
	require.Equal(t, "你好", firstNRunes("你好world", 2))
	require.Equal(t, "", firstNRunes("你好world", 0))
	require.Equal(t, "你好world", firstNRunes("你好world", 20))
	require.Equal(t, "", firstNRunes("你好world", -1))
}

func TestRuneLen_Behavior(t *testing.T) {
	require.Equal(t, 2, runeLen("你好"))
	require.Equal(t, 7, runeLen("你好world"))
	require.Equal(t, 0, runeLen(""))
}

func TestLastPathSegment_Behavior(t *testing.T) {
	require.Equal(t, "c", lastPathSegment("a/b/c"))
	require.Equal(t, "b", lastPathSegment("/a/b/"))
	require.Equal(t, "", lastPathSegment(""))
	require.Equal(t, "a", lastPathSegment("a"))
}

func TestLastOrdinalIndexOf_Behavior(t *testing.T) {
	require.Equal(t, 5, lastOrdinalIndexOf("a/b/c/d", "/", 1))
	require.Equal(t, 3, lastOrdinalIndexOf("a/b/c/d", "/", 2))
	require.Equal(t, 1, lastOrdinalIndexOf("a/b/c/d", "/", 3))
	require.Equal(t, -1, lastOrdinalIndexOf("a/b/c/d", "/", 4))
	require.Equal(t, -1, lastOrdinalIndexOf("a/b/c/d", "/", 0))
	require.Equal(t, -1, lastOrdinalIndexOf("a/b/c/d", "", 1))
}

func TestRandomAliasSuffix_Behavior(t *testing.T) {
	require.Len(t, randomAliasSuffix(), aliasSuffixHexLength)
	require.NotEqual(t, randomAliasSuffix(), randomAliasSuffix())
	for _, r := range randomAliasSuffix() {
		require.Contains(t, "0123456789abcdef", string(r))
	}
}

func TestStableAliasSuffix_Behavior(t *testing.T) {
	require.Equal(t, stableSuffixFor(t, "a/b"), stableAliasSuffix("a/b"))
	require.NotEqual(t, stableAliasSuffix("a/b"), stableAliasSuffix("a/c"))
	require.Len(t, stableAliasSuffix("a/b"), aliasSuffixHexLength)
}

func TestGenerateUnsAlias_Behavior(t *testing.T) {
	// 文件节点：取路径最后一段
	alias := generateUnsAlias("Common/TAG/Metric/TAG_001", 2)
	require.True(t, strings.HasPrefix(alias, "TAG_001_"), "file alias prefix = %s", alias)
	require.True(t, isAliasFormatOK(alias))

	// 系统主题文件夹名：小写主题名 + 随机后缀
	alias = generateUnsAlias("Common/Metric", 1)
	require.True(t, strings.HasPrefix(alias, "metric_"), "topic folder alias = %s", alias)
	require.True(t, isAliasFormatOK(alias))

	// 普通文件夹：整段路径清洗后加后缀
	alias = generateUnsAlias("Common/TAG", 1)
	require.True(t, strings.HasPrefix(alias, "Common_TAG_"), "folder alias = %s", alias)
	require.True(t, isAliasFormatOK(alias))

	// 数字开头：补下划线保证首字符合法
	alias = generateUnsAlias("123", 1)
	require.True(t, strings.HasPrefix(alias, "_"), "digit folder alias = %s", alias)
	require.True(t, isAliasFormatOK(alias))
}

func TestGenerateFileAlias_Behavior(t *testing.T) {
	alias := generateFileAlias("path/to/温度")
	require.True(t, strings.HasPrefix(alias, "wendu_"), "pinyin file alias = %s", alias)
	require.True(t, isAliasFormatOK(alias))

	alias = generateFileAlias("")
	require.True(t, strings.HasPrefix(alias, "file_"), "empty file alias = %s", alias)
	require.True(t, isAliasFormatOK(alias))

	alias = generateFileAlias("a/b/中文名太长需要截断为前十二个字符_")
	require.True(t, strings.HasPrefix(alias, "zhongwenming_"), "truncated file alias = %s", alias)
}

func TestStableAliasFromNode_Behavior(t *testing.T) {
	seed := "Common/Metric/temp"
	alias := stableAliasFromNode(repo.UnsNode{Type: 2, Namespace: seed})
	require.Equal(t, "temp_"+stableSuffixFor(t, seed), alias)

	// 文件夹：末段为系统主题名时用其小写形式
	alias = stableAliasFromNode(repo.UnsNode{Type: 1, Namespace: "Common/Metric"})
	require.Equal(t, "metric_"+stableSuffixFor(t, "Common/Metric"), alias)

	// 深层文件夹：取最后两级
	alias = stableAliasFromNode(repo.UnsNode{Type: 1, Namespace: "a/b/c"})
	require.Equal(t, "b_c_"+stableSuffixFor(t, "a/b/c"), alias)

	// 无 namespace 时用 name
	alias = stableAliasFromNode(repo.UnsNode{Type: 2, Namespace: "", Name: "device"})
	require.Equal(t, "device_"+stableSuffixFor(t, "device"), alias)

	// 全空：返回空串
	require.Equal(t, "", stableAliasFromNode(repo.UnsNode{}))
}

func TestUnsAlias_Behavior(t *testing.T) {
	// 合法 alias 原样返回
	node := repo.UnsNode{Alias: "good_alias", Namespace: "a/b", Type: 2}
	require.Equal(t, "good_alias", unsAlias(node))

	// 非法 alias（含空格）回退到稳定别名
	node.Alias = "bad alias"
	require.Equal(t, stableAliasFromNode(node), unsAlias(node))
}

func TestCleanSaveNamespace_Behavior(t *testing.T) {
	require.Equal(t, "a/b", cleanSaveNamespace(SaveCommand{Namespace: "/a/b/", ParentID: 0}))
	require.Equal(t, "", cleanSaveNamespace(SaveCommand{Namespace: "/a/b/", ParentID: 7}))
	require.Equal(t, "", cleanSaveNamespace(SaveCommand{}))
}

// ---------- 类型枚举转换 ----------

func TestNodeTypeValueName_Behavior(t *testing.T) {
	for _, value := range []string{"", "folder", "path", "1"} {
		v, err := nodeTypeValue(value)
		require.NoError(t, err)
		require.Equal(t, int16(1), v)
	}
	for _, value := range []string{"file", "topic", "2"} {
		v, err := nodeTypeValue(value)
		require.NoError(t, err)
		require.Equal(t, int16(2), v)
	}
	_, err := nodeTypeValue("bogus")
	require.ErrorIs(t, err, ErrInvalid)

	require.Equal(t, "file", nodeTypeName(2))
	require.Equal(t, "folder", nodeTypeName(1))
	require.Equal(t, "folder", nodeTypeName(0))
}

func TestTopicTypeValueName_Behavior(t *testing.T) {
	require.Equal(t, int16(1), mustTopicType(t, "state"))
	require.Equal(t, int16(2), mustTopicType(t, "Action"))
	require.Equal(t, int16(3), mustTopicType(t, "metric"))
	require.Equal(t, int16(0), mustTopicType(t, ""))
	require.Equal(t, int16(0), mustTopicType(t, "0"))
	_, err := topicTypeValue("bogus")
	require.ErrorIs(t, err, ErrInvalid)

	require.Equal(t, "State", topicTypeName(1))
	require.Equal(t, "Action", topicTypeName(2))
	require.Equal(t, "Metric", topicTypeName(3))
	require.Equal(t, "", topicTypeName(0))
	require.Equal(t, "", topicTypeName(9))
}

func mustTopicType(t *testing.T, value string) int16 {
	t.Helper()
	v, err := topicTypeValue(value)
	require.NoError(t, err)
	return v
}

func TestTopicTypeFromNamespace_Behavior(t *testing.T) {
	require.Equal(t, int16(1), topicTypeFromNamespace("Common/State/x"))
	require.Equal(t, int16(3), topicTypeFromNamespace("Common/Metric/x"))
	// 主题文件夹作为最后一段时不算父级，返回 0
	require.Equal(t, int16(0), topicTypeFromNamespace("Common/Metric"))
	require.Equal(t, int16(0), topicTypeFromNamespace("x"))
	require.Equal(t, int16(0), topicTypeFromNamespace("a/b"))
}

func TestTopicTypeByPart_Behavior(t *testing.T) {
	require.Equal(t, int16(1), topicTypeByPart("State"))
	require.Equal(t, int16(3), topicTypeByPart("metric"))
	require.Equal(t, int16(0), topicTypeByPart("x"))
}

func TestSystemTopicTypeByName_Behavior(t *testing.T) {
	require.Equal(t, int16(1), systemTopicTypeByName("State"))
	require.Equal(t, int16(2), systemTopicTypeByName("Action"))
	require.Equal(t, int16(3), systemTopicTypeByName("Metric"))
	require.Equal(t, int16(0), systemTopicTypeByName("state"))
	require.Equal(t, int16(0), systemTopicTypeByName(""))
	require.Equal(t, int16(1), importSystemTopicTypeByName("State"))
}

// ---------- JSON 工具 ----------

func TestJSONValue_Behavior(t *testing.T) {
	raw, err := jsonValue("")
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(raw))

	raw, err = jsonValue(`{"a":1}`)
	require.NoError(t, err)
	require.JSONEq(t, `{"a":1}`, string(raw))

	_, err = jsonValue("{bad")
	require.ErrorIs(t, err, ErrInvalidJSON)
}

func TestRawJSON_Behavior(t *testing.T) {
	require.Equal(t, map[string]any{}, rawJSON(nil))
	require.Equal(t, map[string]any{}, rawJSON(json.RawMessage{}))
	require.Equal(t, map[string]any{"a": float64(1)}, rawJSON(json.RawMessage(`{"a":1}`)))
	require.Equal(t, "not-json", rawJSON(json.RawMessage(`not-json`)))
	require.Equal(t, []any{float64(1), float64(2)}, rawJSON(json.RawMessage(`[1,2]`)))
}

func TestNormalizeNotFound_Behavior(t *testing.T) {
	require.ErrorIs(t, normalizeNotFound(repo.ErrNotFound), ErrNotFound)
	require.ErrorIs(t, normalizeNotFound(ErrInvalid), ErrInvalid)
	require.NoError(t, normalizeNotFound(nil))
}

func TestToInt64_Behavior(t *testing.T) {
	require.Equal(t, int64(7), toInt64(int64(7)))
	require.Equal(t, int64(7), toInt64(7))
	require.Equal(t, int64(7), toInt64(7.9))
	require.Equal(t, int64(42), toInt64(json.Number("42")))
	require.Equal(t, int64(0), toInt64(json.Number("abc")))
	require.Equal(t, int64(42), toInt64(" 42 "))
	require.Equal(t, int64(0), toInt64("abc"))
	require.Equal(t, int64(0), toInt64(nil))
	require.Equal(t, int64(0), toInt64(struct{}{}))
}

func TestSplitNamespaceParts_Behavior(t *testing.T) {
	require.Equal(t, []string{"a", "b", "c"}, splitNamespaceParts("/a/ b /c/"))
	require.Equal(t, []string{"a"}, splitNamespaceParts("a"))
	require.Empty(t, splitNamespaceParts(""))
	require.Empty(t, splitNamespaceParts("///"))
}

func TestParentNamespace_Behavior(t *testing.T) {
	require.Equal(t, "a/b", parentNamespace("a/b/c"))
	require.Equal(t, "", parentNamespace("a"))
	require.Equal(t, "a", parentNamespace("/a/b/"))
	require.Equal(t, "", parentNamespace(""))
}

func TestLastNamespacePart_Behavior(t *testing.T) {
	require.Equal(t, "c", lastNamespacePart("a/b/c"))
	require.Equal(t, "b", lastNamespacePart("/a/b/"))
	require.Equal(t, "", lastNamespacePart(""))
}

func TestValidateCreatePathParts_Behavior(t *testing.T) {
	// 空/前后斜杠/连续斜杠均非法
	require.ErrorIs(t, validateCreatePathParts("", []string{"x"}, 1), ErrInvalid)
	require.ErrorIs(t, validateCreatePathParts("/a/b", []string{"a", "b"}, 1), ErrInvalid)
	require.ErrorIs(t, validateCreatePathParts("a/b/", []string{"a", "b"}, 1), ErrInvalid)
	require.ErrorIs(t, validateCreatePathParts("a//b", []string{"a", "b"}, 1), ErrInvalid)

	// 文件夹：路径任意段不得为保留名 label
	require.ErrorIs(t, validateCreatePathParts("a/label", []string{"a", "label"}, 1), ErrInvalid)
	require.ErrorIs(t, validateCreatePathParts("label/x", []string{"label", "x"}, 1), ErrInvalid)

	// 文件夹：中间段不得为系统主题名
	require.ErrorIs(t, validateCreatePathParts("State/a", []string{"State", "a"}, 1), ErrInvalid)
	require.NoError(t, validateCreatePathParts("a/State", []string{"a", "State"}, 1))

	// 名称超长
	long := strings.Repeat("x", 64)
	require.ErrorIs(t, validateCreatePathParts(long, []string{long}, 1), ErrInvalid)

	// 文件：父级必须是系统主题文件夹
	require.ErrorIs(t, validateCreatePathParts("a/b", []string{"a", "b"}, 2), ErrInvalid)
	require.NoError(t, validateCreatePathParts("a/State/b", []string{"a", "State", "b"}, 2))
	require.ErrorIs(t, validateCreatePathParts("State/Metric/b", []string{"State", "Metric", "b"}, 2), ErrInvalid)
	require.ErrorIs(t, validateCreatePathParts("a/Metric", []string{"a", "Metric"}, 2), ErrInvalid)
}

// ---------- Schema 工具 ----------

func TestSchemaBool_Behavior(t *testing.T) {
	m := map[string]any{"a": true, "b": "true", "c": float64(1), "d": 1, "e": false}
	require.True(t, schemaBool(m, "a"))
	require.True(t, schemaBool(m, "b"))
	require.True(t, schemaBool(m, "c"))
	require.True(t, schemaBool(m, "d"))
	require.False(t, schemaBool(m, "e"))
	require.False(t, schemaBool(m, "missing"))
	require.False(t, schemaBool("not-map", "a"))
	require.False(t, schemaBool(nil, "a"))
	// 多 key 时按顺序返回第一个存在的 key 的值
	require.False(t, schemaBool(map[string]any{"a": false, "b": true}, "a", "b"))
	require.True(t, schemaBool(map[string]any{"a": false, "b": true}, "b"))
}

func TestSchemaFields_Behavior(t *testing.T) {
	fields := []any{map[string]any{"name": "v", "type": "INTEGER"}}
	require.Equal(t, fields, schemaFieldsFromAny(fields))

	require.Equal(t, fields, schemaFieldsFromAny(map[string]any{"fields": fields}))
	require.Empty(t, schemaFieldsFromAny(map[string]any{}))
	require.Empty(t, schemaFieldsFromAny("raw"))
	require.Empty(t, schemaFieldsFromAny(nil))

	require.Empty(t, normalizeSchemaFields(nil))
	require.Empty(t, normalizeSchemaFields([]any{}))
	require.Equal(t, []any{map[string]any{"name": "v"}}, normalizeSchemaFields([]any{nil, map[string]any{}, map[string]any{"name": "v"}}))

	raw := json.RawMessage(`[{"name":"v"}]`)
	require.Equal(t, []any{map[string]any{"name": "v"}}, schemaFieldsFromRaw(raw))
	require.Empty(t, schemaFieldsFromRaw(nil))
}

func TestIsBlankSchemaField_Behavior(t *testing.T) {
	require.True(t, isBlankSchemaField(map[string]any{}))
	require.True(t, isBlankSchemaField(map[string]any{"name": ""}))
	require.False(t, isBlankSchemaField(map[string]any{"name": "v"}))
	require.False(t, isBlankSchemaField("x"))
	require.False(t, isBlankSchemaField(nil))
}

func TestSchemaValueHasContent_Behavior(t *testing.T) {
	require.False(t, schemaValueHasContent(nil))
	require.False(t, schemaValueHasContent(""))
	require.True(t, schemaValueHasContent("x"))
	require.False(t, schemaValueHasContent(false))
	require.True(t, schemaValueHasContent(true))
	require.False(t, schemaValueHasContent(0))
	require.True(t, schemaValueHasContent(1))
	require.False(t, schemaValueHasContent(int64(0)))
	require.False(t, schemaValueHasContent(0.0))
	require.True(t, schemaValueHasContent(1.5))
	require.False(t, schemaValueHasContent(json.Number("0")))
	require.False(t, schemaValueHasContent(json.Number("")))
	require.True(t, schemaValueHasContent(json.Number("3")))
	require.False(t, schemaValueHasContent([]any{}))
	require.False(t, schemaValueHasContent([]any{0}))
	require.True(t, schemaValueHasContent([]any{1}))
	require.False(t, schemaValueHasContent(map[string]any{}))
	require.True(t, schemaValueHasContent(map[string]any{"a": 1}))
	require.True(t, schemaValueHasContent(struct{}{}))
}

func TestDataTypeForNode_Behavior(t *testing.T) {
	schemaWithType := map[string]any{"dataType": 5}
	require.Equal(t, 5, dataTypeForNode(repo.UnsNode{Type: 2, TopicType: 3}, schemaWithType))
	require.Equal(t, 5, dataTypeForNode(repo.UnsNode{Type: 1}, schemaWithType))

	require.Equal(t, 2, dataTypeForNode(repo.UnsNode{Type: 1, TopicType: 2}, map[string]any{}))
	require.Equal(t, 1, dataTypeForNode(repo.UnsNode{Type: 2, TopicType: 3}, nil))
	require.Equal(t, 8, dataTypeForNode(repo.UnsNode{Type: 2, TopicType: 1}, nil))
	require.Equal(t, 8, dataTypeForNode(repo.UnsNode{Type: 2, TopicType: 2}, nil))
	require.Equal(t, 0, dataTypeForNode(repo.UnsNode{Type: 2, TopicType: 0}, nil))
}

func TestIntFromAny_Behavior(t *testing.T) {
	require.Equal(t, 5, intFromAny(5))
	require.Equal(t, 5, intFromAny(int64(5)))
	require.Equal(t, 5, intFromAny(5.9))
	require.Equal(t, 5, intFromAny(json.Number("5")))
	require.Equal(t, 0, intFromAny(json.Number("abc")))
	require.Equal(t, 5, intFromAny("5"))
	require.Equal(t, 0, intFromAny("abc"))
	require.Equal(t, 0, intFromAny(nil))
}

func TestSanitizeNodeSchema_Behavior(t *testing.T) {
	out, err := sanitizeNodeSchema(json.RawMessage(`{}`))
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(out))

	out, err = sanitizeNodeSchema(nil)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, string(out))

	out, err = sanitizeNodeSchema(json.RawMessage(`[{"name":"v","type":"INTEGER"},{"name":""}]`))
	require.NoError(t, err)
	require.JSONEq(t, `[{"name":"v","type":"INTEGER"}]`, string(out))

	_, err = sanitizeNodeSchema(json.RawMessage(`{bad`))
	require.ErrorIs(t, err, ErrInvalidJSON)

	long := strings.Repeat("x", 64)
	_, err = sanitizeNodeSchema(json.RawMessage(`[{"name":"` + long + `"}]`))
	require.ErrorIs(t, err, ErrInvalid)
}

func TestValidateSchemaFieldName_Behavior(t *testing.T) {
	require.NoError(t, validateSchemaFieldName(map[string]any{}))
	require.NoError(t, validateSchemaFieldName(map[string]any{"name": "v"}))
	require.NoError(t, validateSchemaFieldName(map[string]any{"name": ""}))
	require.ErrorIs(t, validateSchemaFieldName(map[string]any{"name": strings.Repeat("x", 64)}), ErrInvalid)
}

func TestPersistenceFlag_Behavior(t *testing.T) {
	require.Equal(t, int16(1), persistenceFlag(boolPtr(true)))
	require.Equal(t, int16(2), persistenceFlag(boolPtr(false)))
	require.Equal(t, int16(2), persistenceFlag(nil))
	require.True(t, *boolPtr(true))
	require.False(t, *boolPtr(false))
}

func TestYesNoFlag_Behavior(t *testing.T) {
	require.Equal(t, int16(1), yesNoFlag(true))
	require.Equal(t, int16(2), yesNoFlag(false))
}

func TestIsSystemFieldName_Behavior(t *testing.T) {
	require.True(t, isSystemFieldName("_id", 1))
	require.True(t, isSystemFieldName("_timestamp", 3))
	require.True(t, isSystemFieldName("_quality", 1))
	require.True(t, isSystemFieldName("_ct", 2))
	require.True(t, isSystemFieldName("_qos", 3))
	require.True(t, isSystemFieldName("created_time", 1))
	require.True(t, isSystemFieldName("timeStamp", 1))
	require.True(t, isSystemFieldName("timestamp", 2))
	require.True(t, isSystemFieldName("quality", 1))
	// Metric 主题下 timeStamp/quality 视为业务字段
	require.False(t, isSystemFieldName("timeStamp", 3))
	require.False(t, isSystemFieldName("quality", 3))
	require.False(t, isSystemFieldName("value", 1))
}

// 空 schema 回退默认字段

// 缺失字段经 fmt.Sprint(nil) 变成 "<nil>"，仍按非空名处理

// ---------- 响应组装 ----------

func TestNodeResp_Behavior(t *testing.T) {
	node := repo.UnsNode{
		ID: 7, ParentID: 3, Name: "t1", DisplayName: "T1", Description: "d",
		Namespace: "Common/Metric/t1", Alias: "t1_abc", Type: 2, TopicType: 3,
		Schema:           json.RawMessage(`[{"name":"v","type":"INTEGER"}]`),
		ExtendProperties: json.RawMessage(`{"a":1}`),
		EnableHistory:    1, MockData: 1, Status: "active",
	}
	resp := nodeResp(node)
	require.Equal(t, int64(7), resp["id"])
	require.Equal(t, "t1", resp["name"])
	require.Equal(t, "file", resp["type"])
	require.Equal(t, "Metric", resp["topicType"])
	require.Equal(t, "t1_abc", resp["alias"])
	require.Equal(t, 1, resp["dataType"])
	require.Equal(t, 3, resp["parentDataType"])
	require.Equal(t, true, resp["persistence"])
	require.Equal(t, true, resp["withFlow"])
	require.Equal(t, int16(1), resp["enableHistory"])
	_, hasJSONFields := resp["jsonFields"]
	require.False(t, hasJSONFields, "metric 文件 dataType=1 不应有 jsonFields")

	// State/Action 文件 dataType=8 → 带 jsonFields
	node.TopicType = 1
	resp = nodeResp(node)
	require.Equal(t, 8, resp["dataType"])
	require.Contains(t, resp, "jsonFields")

	// 文件夹
	folder := repo.UnsNode{ID: 1, Name: "Common", Type: 1}
	resp = nodeResp(folder)
	require.Equal(t, "folder", resp["type"])
	require.Equal(t, 0, resp["dataType"])
	require.Equal(t, "", resp["topicType"])
	require.NotEmpty(t, resp["alias"])
}

func TestAddRootNodeResp_Behavior(t *testing.T) {
	nodes := []repo.UnsNode{{ID: 5, Name: "other"}, {ID: 7, Name: "root", Type: 1}}
	resp := map[string]any{"deleted": 2}
	addRootNodeResp(resp, 7, nodes)
	require.Equal(t, 2, resp["deleted"])
	require.Equal(t, "root", resp["name"])
	require.Equal(t, "folder", resp["type"])

	// 未命中 rootID 不改变响应
	resp = map[string]any{"deleted": 1}
	addRootNodeResp(resp, 99, nodes)
	require.Len(t, resp, 1)

	// nil / 非法 rootID 安全
	addRootNodeResp(nil, 7, nodes)
	addRootNodeResp(map[string]any{}, 0, nodes)
}

func TestDeletedEventString_Behavior(t *testing.T) {
	require.Equal(t, "deleted 2 uns nodes", DeletedEvent{Nodes: []repo.UnsNode{{}, {}}}.String())
}

func TestTemplateResp_Behavior(t *testing.T) {
	item := repo.UnsTemplate{
		ID: 5, Name: "t", TopicType: 3,
		Schema:           json.RawMessage(`[{"name":"v"}]`),
		ExtendProperties: json.RawMessage(`{"description":"d"}`),
	}
	item.CreatedTime = time.UnixMilli(100)
	item.UpdatedTime = time.UnixMilli(200)
	resp := templateResp(item)
	require.Equal(t, int64(5), resp["id"])
	require.Equal(t, "t", resp["name"])
	require.Equal(t, "Metric", resp["topicType"])
	require.Equal(t, "d", resp["description"])
	require.Len(t, resp["fields"], 1)
	require.Equal(t, time.UnixMilli(100), resp["createTime"])
	require.Equal(t, time.UnixMilli(200), resp["updatedTime"])
}

func TestLabelResp_Behavior(t *testing.T) {
	item := repo.UnsLabel{ID: 3, LabelKey: "k", Name: "l", Color: "#fff", Description: "d"}
	item.CreatedTime = time.UnixMilli(100)
	resp := labelResp(item)
	require.Equal(t, int64(3), resp["id"])
	require.Equal(t, "k", resp["labelKey"])
	require.Equal(t, "l", resp["name"])
	require.Equal(t, "#fff", resp["color"])
	require.Equal(t, "d", resp["description"])
	require.Equal(t, time.UnixMilli(100), resp["createdTime"])
}

func TestNodesRespAndListResp_Behavior(t *testing.T) {
	nodes := []repo.UnsNode{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}}
	out := nodesResp(nodes)
	require.Len(t, out, 2)
	require.Equal(t, "a", out[0]["name"])

	resp := listResp(nodes)
	require.Equal(t, 2, resp["total"])
	require.Len(t, resp["list"], 2)
	require.Empty(t, nodesResp(nil))
}

// ---------- 导出 ----------

func TestFilterExportNodes_Behavior(t *testing.T) {
	nodes := []repo.UnsNode{
		{ID: 1, Namespace: "Common", Name: "Common", Type: 1},
		{ID: 2, Namespace: "Common/Metric", Name: "Metric", Type: 1},
		{ID: 3, Namespace: "Other", Name: "Other", Type: 1},
	}
	ids := func(out []repo.UnsNode) []int64 {
		res := make([]int64, 0, len(out))
		for _, n := range out {
			res = append(res, n.ID)
		}
		return res
	}
	require.Equal(t, []int64{1, 2, 3}, ids(filterExportNodes(nodes, ExportCommand{ExportType: "ALL"})))
	require.Equal(t, []int64{1, 2, 3}, ids(filterExportNodes(nodes, ExportCommand{})))
	require.Equal(t, []int64{1, 2}, ids(filterExportNodes(nodes, ExportCommand{Folders: []int64{1}})))
	require.Equal(t, []int64{3}, ids(filterExportNodes(nodes, ExportCommand{Files: []int64{3}})))
	require.Equal(t, []int64{1, 2, 3}, ids(filterExportNodes(nodes, ExportCommand{Folders: []int64{1}, Files: []int64{3}})))
	require.Empty(t, filterExportNodes(nodes, ExportCommand{Folders: []int64{99}}))
	require.Empty(t, filterExportNodes(nodes, ExportCommand{Files: []int64{-1, 0}}))
}

func TestExportNamespaceNode_Behavior(t *testing.T) {
	qualityPolicy := &repo.DataQualityPolicy{
		Enabled: true, ExpectedIntervalMs: 1000, Rules: []string{"valueRange", "missingTelemetry"},
		SeverityOverrides: map[string]string{},
	}
	file := repo.UnsNode{
		Name: "t", DisplayName: "T", Description: "d", Namespace: "x/t",
		Type: 2, TopicType: 3, Alias: "t_abc",
		Schema:           json.RawMessage(`[{"name":"v","type":"INTEGER"}]`),
		ExtendProperties: json.RawMessage(`{"a":1}`),
		EnableHistory:    1, MockData: 2, DataQualityPolicy: qualityPolicy,
	}
	item := exportNamespaceNode(file)
	require.Equal(t, "TOPIC", item["type"])
	require.Equal(t, "METRIC", item["topicType"])
	require.Equal(t, "t_abc", item["alias"])
	require.Len(t, item["fields"], 1)
	require.Equal(t, "TRUE", item["enableHistory"])
	require.NotContains(t, item, "mockData")
	require.Equal(t, map[string]any{"a": float64(1)}, item["extendProperties"])
	require.Same(t, qualityPolicy, item["dataQualityPolicy"])

	folder := exportNamespaceNode(repo.UnsNode{Name: "Common", Type: 1})
	require.Equal(t, "PATH", folder["type"])
	require.NotContains(t, folder, "topicType")

	// 空 extend 不输出
	noExtend := exportNamespaceNode(repo.UnsNode{Name: "x", Type: 1})
	require.NotContains(t, noExtend, "extendProperties")
}

func TestExportNamespaceTree_Behavior(t *testing.T) {
	nodes := []repo.UnsNode{
		{ID: 1, ParentID: 0, Name: "Common", Namespace: "Common", Type: 1},
		{ID: 2, ParentID: 1, Name: "Metric", Namespace: "Common/Metric", Type: 1, TopicType: 3},
		{ID: 3, ParentID: 2, Name: "t1", Namespace: "Common/Metric/t1", Type: 2, TopicType: 3},
		{ID: 4, ParentID: 99, Name: "Orphan", Namespace: "Orphan", Type: 1},
	}
	tree := exportNamespaceTree(nodes)
	require.Len(t, tree, 2)
	require.Equal(t, "Common", tree[0]["name"])
	require.Equal(t, "Orphan", tree[1]["name"])

	common := tree[0]["children"].([]map[string]any)
	require.Len(t, common, 1)
	require.Equal(t, "Metric", common[0]["name"])
	metricChildren := common[0]["children"].([]map[string]any)
	require.Len(t, metricChildren, 1)
	require.Equal(t, "TOPIC", metricChildren[0]["type"])

	// 叶子节点无 children 键
	require.NotContains(t, metricChildren[0], "children")
}

// ---------- 导入解析 ----------

func TestHasImportNamespacePayload_Behavior(t *testing.T) {
	require.True(t, hasImportNamespacePayload(map[string]any{"nodes": []any{map[string]any{"namespace": "a"}}}))
	require.True(t, hasImportNamespacePayload(map[string]any{"namespace": map[string]any{"nodes": []any{map[string]any{"type": "PATH"}}}}))
	require.True(t, hasImportNamespacePayload(map[string]any{"uns": map[string]any{"nodes": []any{map[string]any{"namespace": "a"}}}}))
	require.False(t, hasImportNamespacePayload(map[string]any{}))
}

func TestExtractImportNodes_Behavior(t *testing.T) {
	payload := map[string]any{
		"nodes": []any{
			map[string]any{
				"namespace":     "/Common/Metric/t1/",
				"type":          "file",
				"topicType":     "3",
				"displayName":   "T1",
				"alias":         "t1_x",
				"enableHistory": "true",
				"mockData":      "yes",
				"fields":        []any{map[string]any{"name": "v", "type": "INTEGER"}},
				"dataQualityPolicy": map[string]any{
					"enabled": true, "expectedIntervalMs": float64(1000),
					"rules": []any{"valueRange", "missingTelemetry"},
				},
			},
		},
	}
	nodes := extractImportNodes(payload)
	require.Len(t, nodes, 1)
	node := nodes[0]
	require.Equal(t, "Common/Metric/t1", node.Namespace)
	require.Equal(t, int16(2), node.Type)
	require.Equal(t, int16(3), node.TopicType)
	require.Equal(t, "T1", node.DisplayName)
	require.Equal(t, "t1_x", node.Alias)
	require.Equal(t, int16(1), node.EnableHistory)
	require.Equal(t, int16(1), node.MockData)
	require.JSONEq(t, `[{"name":"v","type":"INTEGER"}]`, string(node.Schema))
	require.JSONEq(t, `{}`, string(node.ExtendProperties))
	require.NotNil(t, node.DataQualityPolicy)
	require.True(t, node.DataQualityPolicy.Enabled)
	require.Equal(t, int64(1000), node.DataQualityPolicy.ExpectedIntervalMs)
	require.Equal(t, []string{"valueRange", "missingTelemetry"}, node.DataQualityPolicy.Rules)

	// uns.nodes 优先于 nodes
	payload = map[string]any{
		"nodes": []any{map[string]any{"namespace": "a"}},
		"uns":   map[string]any{"nodes": []any{map[string]any{"namespace": "b"}}},
	}
	nodes = extractImportNodes(payload)
	require.Len(t, nodes, 1)
	require.Equal(t, "b", nodes[0].Namespace)

	require.Nil(t, extractImportNodes(map[string]any{}))
	require.Nil(t, extractImportNodes(map[string]any{"nodes": "not-array"}))
}

func TestImportArrayAndString_Behavior(t *testing.T) {
	payload := map[string]any{"labels": []any{map[string]any{"name": "l1"}, "junk"}}
	items := importArray(payload, "labels", "label")
	require.Len(t, items, 1)
	require.Equal(t, "l1", items[0]["name"])

	require.Nil(t, importArray(payload, "label"))
	require.Nil(t, importArray(map[string]any{"labels": "x"}, "labels"))
	require.Nil(t, importArray(map[string]any{}, "labels"))

	require.Equal(t, "abc", importString(map[string]any{"k": " abc "}, "k"))
	require.Equal(t, "", importString(map[string]any{"k": nil}, "k"))
	require.Equal(t, "", importString(map[string]any{}, "k"))
	require.Equal(t, "3", importString(map[string]any{"k": json.Number("3")}, "k"))
}

func TestImportTree_Behavior(t *testing.T) {
	payload := map[string]any{
		"namespace": map[string]any{
			"nodes": []any{map[string]any{"type": "PATH", "name": "a", "children": []any{map[string]any{"type": "TOPIC", "name": "b"}}}},
		},
	}
	tree := importTree(payload)
	require.Len(t, tree, 1)
	require.Equal(t, "a", tree[0].Name)
	require.Len(t, tree[0].Children, 1)

	// uns 下原生 namespace 数组（非树）应被跳过
	payload = map[string]any{"uns": map[string]any{"nodes": []any{map[string]any{"namespace": "a/b"}}}}
	require.Nil(t, importTree(payload))

	// 非法 JSON / 空数组
	require.Nil(t, importTree(map[string]any{"namespace": map[string]any{"nodes": "x"}}))
	require.Nil(t, importTree(map[string]any{}))
}

func TestIsRawNamespaceNodeArray_Behavior(t *testing.T) {
	require.True(t, isRawNamespaceNodeArray([]any{map[string]any{"namespace": "a"}}))
	require.False(t, isRawNamespaceNodeArray([]any{map[string]any{"type": "PATH"}}))
	require.False(t, isRawNamespaceNodeArray("x"))
	require.False(t, isRawNamespaceNodeArray(nil))
}

func TestImportFields_Behavior(t *testing.T) {
	fields := importFields([]any{
		map[string]any{"name": "v", "type": "integer", "displayName": "V", "maxLen": float64(10)},
		map[string]any{"type": "STRING"},
		"junk",
		map[string]any{"name": "x"},
	})
	require.Len(t, fields, 2)
	require.Equal(t, "v", fields[0]["name"])
	require.Equal(t, "INTEGER", fields[0]["type"])
	require.Equal(t, "V", fields[0]["displayName"])
	require.Equal(t, int64(10), fields[0]["maxLen"])
	require.Equal(t, "x", fields[1]["name"])
	require.Equal(t, "", fields[1]["type"])

	require.Nil(t, importFields("x"))
	require.Empty(t, importFields([]any{"junk"}))
}

func TestImportNodeLabelIDs_Behavior(t *testing.T) {
	labels := map[string]int64{"a": 1, "b": 2}
	require.Equal(t, []int64{1, 2}, importNodeLabelIDs(importFileData{Label: "a, b"}, labels))
	require.Equal(t, []int64{1}, importNodeLabelIDs(importFileData{Labels: []any{"a"}}, labels))
	require.Equal(t, []int64{2}, importNodeLabelIDs(importFileData{Labels: "b"}, labels))
	require.Equal(t, []int64{1}, importNodeLabelIDs(importFileData{Label: "a,zzz"}, labels))
	require.Nil(t, importNodeLabelIDs(importFileData{}, labels))
	require.Nil(t, importNodeLabelIDs(importFileData{Label: " "}, labels))
}

func TestCountImportTree_Behavior(t *testing.T) {
	items := []importFileData{{}, {Children: []importFileData{{}, {Children: []importFileData{{}}}}}}
	require.Equal(t, 5, countImportTree(items))
	require.Equal(t, 5, countImportTreeNodes(items))
	require.Equal(t, 0, countImportTree(nil))
}

func TestImportNodeTypeName_Behavior(t *testing.T) {
	require.Equal(t, "file", importNodeTypeName("topic"))
	require.Equal(t, "file", importNodeTypeName("file"))
	require.Equal(t, "file", importNodeTypeName("2"))
	require.Equal(t, "folder", importNodeTypeName("PATH"))
	require.Equal(t, "folder", importNodeTypeName(""))
	require.Equal(t, "folder", importNodeTypeName("0"))
	require.Equal(t, "folder", importNodeTypeName("x"))
	require.Equal(t, "file", importNodeTypeNameForParent("", 3))
	require.Equal(t, "file", importNodeTypeNameForParent("0", 1))
	require.Equal(t, "folder", importNodeTypeNameForParent("", 0))
	require.Equal(t, "folder", importNodeTypeNameForParent("folder", 3))
}

func TestImportTopicTypeName_Behavior(t *testing.T) {
	require.Equal(t, "Metric", importTopicTypeName(map[string]any{"topicType": "metric"}))
	require.Equal(t, "Action", importTopicTypeName(map[string]any{"parentDataType": "2"}))
	require.Equal(t, "State", importTopicTypeName(map[string]any{"dataType": "state"}))
	require.Equal(t, "Metric", importTopicTypeName(map[string]any{"name": "Metric"}))
	require.Equal(t, "Action", importTopicTypeName(map[string]any{"alias": "Action"}))
	require.Equal(t, "State", importTopicTypeName(map[string]any{}))
}

func TestImportTopicTypeNameFromFile_Behavior(t *testing.T) {
	require.Equal(t, "Action", importTopicTypeNameFromFile(importFileData{TopicType: "action"}, ""))
	require.Equal(t, "Metric", importTopicTypeNameFromFile(importFileData{ParentDataType: "3"}, ""))
	require.Equal(t, "State", importTopicTypeNameFromFile(importFileData{DataType: "state"}, ""))
	require.Equal(t, "Action", importTopicTypeNameFromFile(importFileData{DataType: json.Number("2")}, ""))
	require.Equal(t, "Metric", importTopicTypeNameFromFile(importFileData{}, "Metric"))
	require.Equal(t, "Metric", importTopicTypeNameFromFile(importFileData{Name: "Metric"}, ""))
	require.Equal(t, "", importTopicTypeNameFromFile(importFileData{}, ""))
}

func TestImportExplicitTopicType_Behavior(t *testing.T) {
	require.Equal(t, int16(3), importExplicitTopicType(importFileData{TopicType: "metric"}))
	require.Equal(t, int16(2), importExplicitTopicType(importFileData{DataType: json.Number("2")}))
	require.Equal(t, int16(0), importExplicitTopicType(importFileData{ParentDataType: "bogus"}))
	require.Equal(t, int16(0), importExplicitTopicType(importFileData{}))
}

func TestNormalizeImportTopicType_Behavior(t *testing.T) {
	require.Equal(t, "State", normalizeImportTopicType("STATE"))
	require.Equal(t, "State", normalizeImportTopicType("1"))
	require.Equal(t, "Action", normalizeImportTopicType("action"))
	require.Equal(t, "Metric", normalizeImportTopicType("3"))
	require.Equal(t, "", normalizeImportTopicType("bogus"))
	require.Equal(t, "", normalizeImportTopicType(""))
}

func TestImportSchemaJSONAndItemFields_Behavior(t *testing.T) {
	fields := []map[string]any{{"name": "v"}}
	require.JSONEq(t, `[{"name":"v"}]`, importSchemaJSON(importFileData{}, fields, ""))
	require.Equal(t, fields, importItemFields(importFileData{Fields: fields, LegacySchema: []map[string]any{{"name": "old"}}}))
	require.Equal(t, []map[string]any{{"name": "old"}}, importItemFields(importFileData{LegacySchema: []map[string]any{{"name": "old"}}}))
	require.Nil(t, importItemFields(importFileData{}))
}

func TestFirstNonNilAndNonEmpty_Behavior(t *testing.T) {
	require.Equal(t, "x", firstNonNil(nil, "x", 1))
	require.Equal(t, 1, firstNonNil(nil, nil, 1))
	require.Nil(t, firstNonNil(nil, nil))
	require.Equal(t, "b", firstNonEmpty(" ", "b", "c"))
	require.Equal(t, "c", firstNonEmpty("", "c"))
	require.Equal(t, "", firstNonEmpty("", " "))
}

func TestParseImportBool_Behavior(t *testing.T) {
	for _, v := range []string{"true", "1", "yes", "y", "TRUE"} {
		require.True(t, parseImportBool(v), v)
	}
	for _, v := range []string{"false", "0", "no", "n", "", "x"} {
		require.False(t, parseImportBool(v), v)
	}
}

func TestImportExtendJSON_Behavior(t *testing.T) {
	require.JSONEq(t, `{"a":1}`, importExtendJSON(importFileData{ExtendProperties: map[string]any{"a": 1}}))
	require.JSONEq(t, `{"b":2}`, importExtendJSON(importFileData{Extend: map[string]any{"b": 2}}))
	require.JSONEq(t, `{}`, importExtendJSON(importFileData{}))
}

func TestDefaultMapAndImportNodeType_Behavior(t *testing.T) {
	require.Equal(t, map[string]any{}, defaultMap(nil))
	require.Equal(t, "x", defaultMap("x"))

	require.Equal(t, int16(0), importNodeType(nil))
	require.Equal(t, int16(0), importNodeType(""))
	require.Equal(t, int16(0), importNodeType("0"))
	require.Equal(t, int16(2), importNodeType("file"))
	require.Equal(t, int16(2), importNodeType("2"))
	require.Equal(t, int16(1), importNodeType("topic"))
	require.Equal(t, int16(1), importNodeType("folder"))

	require.Equal(t, int16(3), importTopicType("metric"))
	require.Equal(t, int16(1), importTopicType("1"))
	require.Equal(t, int16(0), importTopicType("bogus"))
	require.Equal(t, int16(0), importTopicType(""))

	require.Equal(t, int16(3), importTopicTypeFromNamespace("Common/Metric/x"))
	require.Equal(t, int16(0), importTopicTypeFromNamespace("x"))
}

func TestValidateImportAliasCandidate_Behavior(t *testing.T) {
	alias, err := validateImportAliasCandidate("", map[string]repo.UnsNode{})
	require.NoError(t, err)
	require.Equal(t, "", alias)

	_, err = validateImportAliasCandidate("bad alias", map[string]repo.UnsNode{})
	require.ErrorContains(t, err, "invalid alias")

	_, err = validateImportAliasCandidate("good_1", map[string]repo.UnsNode{"good_1": {ID: 5}})
	require.ErrorContains(t, err, "alias already exists")

	alias, err = validateImportAliasCandidate("/good_1/", map[string]repo.UnsNode{})
	require.NoError(t, err)
	require.Equal(t, "good_1", alias)

	// 已存在但 ID 为 0（占位）不视为冲突
	alias, err = validateImportAliasCandidate("good_1", map[string]repo.UnsNode{"good_1": {ID: 0}})
	require.NoError(t, err)
	require.Equal(t, "good_1", alias)
}

func TestIndexImportNode_Behavior(t *testing.T) {
	m := map[string]repo.UnsNode{}
	indexImportNode(m, repo.UnsNode{Namespace: "/a/b/", ID: 1})
	require.Equal(t, int64(1), m["a/b"].ID)
	indexImportNode(m, repo.UnsNode{Namespace: "", ID: 2})
	require.Len(t, m, 1)

	m2 := map[string]repo.UnsNode{}
	indexImportAliasNode(m2, repo.UnsNode{Alias: "/x/", ID: 1})
	require.Equal(t, int64(1), m2["x"].ID)
	indexImportAliasNode(m2, repo.UnsNode{Alias: "", ID: 2})
	require.Len(t, m2, 1)
}

func TestValidateImportExistingNode_Behavior(t *testing.T) {
	require.NoError(t, validateImportExistingNode(repo.UnsNode{Type: 1}, "folder", false, 0))
	require.NoError(t, validateImportExistingNode(repo.UnsNode{Type: 2}, "file", false, 0))
	require.ErrorIs(t, validateImportExistingNode(repo.UnsNode{Type: 1}, "file", false, 0), ErrInvalid)
	require.ErrorIs(t, validateImportExistingNode(repo.UnsNode{Type: 2}, "folder", false, 0), ErrInvalid)
	require.ErrorIs(t, validateImportExistingNode(repo.UnsNode{Type: 1, TopicType: 1}, "folder", true, 3), ErrInvalid)
	require.NoError(t, validateImportExistingNode(repo.UnsNode{Type: 1, TopicType: 0}, "folder", true, 3))
	require.ErrorIs(t, validateImportExistingNode(repo.UnsNode{Type: 1}, "bogus", false, 0), ErrInvalid)
}

func TestImportExistingNodeTopicContext_Behavior(t *testing.T) {
	node := repo.UnsNode{TopicType: 2}
	topic, parentType := importExistingNodeTopicContext(node, "x", "y", false, 0)
	require.Equal(t, "Action", topic)
	require.Equal(t, int16(2), parentType)

	topic, parentType = importExistingNodeTopicContext(repo.UnsNode{TopicType: 0}, "x", "y", true, 3)
	require.Equal(t, "Metric", topic)
	require.Equal(t, int16(3), parentType)

	topic, parentType = importExistingNodeTopicContext(repo.UnsNode{TopicType: 0}, "x", "State", false, 0)
	require.Equal(t, "State", topic)
	require.Equal(t, int16(0), parentType)

	topic, parentType = importExistingNodeTopicContext(repo.UnsNode{TopicType: 0}, "inherited", "", false, 0)
	require.Equal(t, "inherited", topic)
	require.Equal(t, int16(0), parentType)
}

// ---------- 模板命令 ----------

func TestTemplateCommandToRepo_Behavior(t *testing.T) {
	svc := &Service{}
	item, err := svc.templateCommandToRepo(TemplateCommand{
		Name: "t", TopicType: "metric",
		Fields: []map[string]any{{"name": "v", "type": "INTEGER"}},
	})
	require.NoError(t, err)
	require.Equal(t, "t", item.Name)
	require.Equal(t, int16(3), item.TopicType)
	require.JSONEq(t, `[{"name":"v","type":"INTEGER"}]`, string(item.Schema))
	require.JSONEq(t, `{"description":""}`, string(item.ExtendProperties))

	// 空主题类型默认 State
	item, err = svc.templateCommandToRepo(TemplateCommand{Name: "t"})
	require.NoError(t, err)
	require.Equal(t, int16(1), item.TopicType)

	_, err = svc.templateCommandToRepo(TemplateCommand{Name: ""})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = svc.templateCommandToRepo(TemplateCommand{Name: strings.Repeat("x", 64)})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = svc.templateCommandToRepo(TemplateCommand{Name: "t", Description: strings.Repeat("x", 513)})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = svc.templateCommandToRepo(TemplateCommand{Name: "t", TopicType: "bogus"})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = svc.templateCommandToRepo(TemplateCommand{Name: "t", Fields: []map[string]any{{"name": strings.Repeat("x", 64)}}})
	require.ErrorIs(t, err, ErrInvalid)
}

// ---------- 无 DB 的 Service 方法 ----------

func TestPublishWithNilService_Behavior(t *testing.T) {
	var svc *Service
	svc.publishPostCommit(context.Background(), struct{}{})
	require.NoError(t, svc.publishInTx(context.Background(), struct{}{}))

	svc = &Service{}
	svc.publishPostCommit(context.Background(), struct{}{})
	require.NoError(t, svc.publishInTx(context.Background(), struct{}{}))
	assert.NotPanics(t, func() { svc.publishPostCommit(context.Background(), nil) })
}

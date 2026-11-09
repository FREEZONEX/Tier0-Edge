package unsquery

// 共享字段校验不接受任意 SQL；整数游标和过滤值保留 JSON 数字精度。
import (
	"encoding/json"
	"testing"

	"backend/internal/domain/unsmeta"
	"github.com/stretchr/testify/require"
)

func TestRecordQueryFieldAndValueValidation(t *testing.T) {
	schema := &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"count": {Type: "integer"}, "value": {Type: "number"}, "name": {Type: "string"}, "enabled": {Type: "boolean"}, "nested": {Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}}}}}
	for _, field := range []string{"id", "createdAt", "updatedAt", "revision", "data.count", "data.value", "data.name", "data.enabled", "data.nested.value"} {
		_, _, _, err := recordField(schema, field)
		require.NoError(t, err, field)
	}
	for _, field := range []string{"raw_sql", "data.unknown", "data.nested", "data.value OR true", "data.nested.value.more"} {
		_, _, _, err := recordField(schema, field)
		require.Error(t, err, field)
	}
	require.True(t, filterValue("integer", json.Number("9007199254740993")))
	require.True(t, filterValue("number", json.Number("0.00000000000000000001")))
	require.False(t, filterValue("integer", json.Number("1.5")))
	require.False(t, filterValue("string", json.Number("1")))
	require.False(t, filterValue("number", json.Number("NaN")))
	require.True(t, filterValue("boolean", true))
	require.True(t, filterValue("string", "stable-id"))
	require.False(t, filterValue("number", "number-as-text"))
}

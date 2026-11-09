package system

// 公开文档只保留公开路由及其可达类型，包含循环引用和 Swagger 2 / OpenAPI 3。
import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPrunePublicSchemas(t *testing.T) {
	for _, prefix := range []string{"#/definitions/", "#/components/schemas/"} {
		schemas := map[string]any{"Public": map[string]any{"properties": map[string]any{"child": map[string]any{"$ref": prefix + "Nested"}}}, "Nested": map[string]any{"$ref": prefix + "Public"}, "InternalSecret": map[string]any{"type": "object"}, "OpenapiUnused": map[string]any{"type": "object"}}
		paths := map[string]any{"/uns/sources/create": map[string]any{"post": map[string]any{"schema": map[string]any{"$ref": prefix + "Public"}}}}
		spec := map[string]any{}
		if prefix == "#/definitions/" {
			spec["definitions"] = schemas
		} else {
			spec["components"] = map[string]any{"schemas": schemas}
		}
		pruneOpenAPIV1Schemas(spec, paths)
		var got map[string]any
		if prefix == "#/definitions/" {
			got = spec["definitions"].(map[string]any)
		} else {
			got = spec["components"].(map[string]any)["schemas"].(map[string]any)
		}
		require.Len(t, got, 2)
		require.Contains(t, got, "Public")
		require.Contains(t, got, "Nested")
		require.NotContains(t, got, "InternalSecret")
		require.NotContains(t, got, "OpenapiUnused")
	}
}
func TestPublicPathsExcludeInternal(t *testing.T) {
	paths := normalizeOpenAPIV1Paths(map[string]any{"/api/core/uns/sources": map[string]any{}, "/gateway/private": map[string]any{}, "/openapi/v1/uns/sources/create": map[string]any{"post": map[string]any{}}})
	require.Len(t, paths, 1)
	require.Contains(t, paths, "/uns/sources/create")
}

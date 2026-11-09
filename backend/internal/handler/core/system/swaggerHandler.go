// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package system

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	openapiV1Prefix          = "/openapi/v1"
	openapiV1SchemaRefPrefix = "#/components/schemas/"
)

var (
	openapiV1SwaggerOnce sync.Once
	openapiV1SwaggerJSON []byte
	openapiV1SwaggerErr  error
)

func OpenapiV1SwaggerUIHandler() http.HandlerFunc {
	return swaggerUIHandler(openapiV1Prefix, "Tier0 Enterprise OpenAPI")
}

func swaggerUIHandler(prefix, title string) http.HandlerFunc {
	tagsSorter := "'alpha'"
	versionStyle := ""
	if prefix == "/openapi/v2" {
		// The v2 contract provides its own topic and category order.
		tagsSorter = "undefined"
		versionStyle = ".swagger-ui .info .title small:not(.version-stamp) { display: none; }"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(strings.NewReplacer("{{title}}", title, "{{prefix}}", prefix, "{{tagsSorter}}", tagsSorter, "{{versionStyle}}", versionStyle).Replace(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{title}}</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    html, body, #swagger-ui { height: 100%; margin: 0; }
    body { background: #fff; }
    .swagger-ui .topbar { display: none; }
    {{versionStyle}}
    .swagger-ui .api-topic { margin: 28px 0 36px; }
    .swagger-ui .api-topic > h2 { margin: 0 0 8px; font-size: 24px; }
    .swagger-ui .api-topic > p { margin: 0 0 16px; color: #555; font-size: 14px; }
    .swagger-ui .api-topic > div { padding-left: 16px; border-left: 3px solid #e5e7eb; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    // Keep Swagger's operation components, authorization and Try it out intact.
    // Only partition the tag list according to the contract's topic metadata.
    function TopicGroupsPlugin(system) {
      return { wrapComponents: { operations: function (Original, system) {
        return function (props) {
          var groups = props.specSelectors.specJson().get('x-tagGroups');
          if (!groups || !groups.size) return system.React.createElement(Original, props);
          var all = props.specSelectors.taggedOperations();
          var filter = props.layoutSelectors.currentFilter();
          if (typeof filter !== 'string') filter = '';
          return system.React.createElement('div', null, groups.toJS().map(function (group) {
            var selected = all.filter(function (_, tag) { return group.tags.indexOf(tag) !== -1; });
            if (!selected.size || (filter && !selected.keySeq().some(function (tag) { return tag.indexOf(filter) !== -1; }))) return null;
            return system.React.createElement('section', {className: 'api-topic', key: group.name},
              system.React.createElement('h2', null, group.name),
              system.React.createElement('p', null, group.description),
              system.React.createElement(Original, Object.assign({}, props, {
                specSelectors: Object.assign({}, props.specSelectors, {taggedOperations: function () {return selected;}})
              })));
          }));
        };
      }}};
    }
    window.addEventListener('load', function () {
      window.ui = SwaggerUIBundle({
        url: '{{prefix}}/swagger.json',
        dom_id: '#swagger-ui',
        deepLinking: true,
        filter: true,
        docExpansion: 'none',
        tagsSorter: {{tagsSorter}},
        plugins: '{{prefix}}' === '/openapi/v2' ? [TopicGroupsPlugin] : [],
        defaultModelsExpandDepth: -1,
        persistAuthorization: true,
        displayRequestDuration: true
      });
    });
  </script>
</body>
</html>`)))
	}
}

func OpenapiV1SwaggerJSONHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := openapiV1NormalizedSwaggerJSON()
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(data)
	}
}

func SwaggerHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := readSwaggerJSON()
		if err != nil {
			http.Error(w, "swagger.json not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(data)
	}
}

func readSwaggerJSON() ([]byte, error) {
	candidates := []string{
		filepath.Join("http", "swagger.json"),
		filepath.Join("backend", "http", "swagger.json"),
		filepath.Join("/app", "http", "swagger.json"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err == nil {
			return data, nil
		}
	}
	return nil, os.ErrNotExist
}

func openapiV1NormalizedSwaggerJSON() ([]byte, error) {
	openapiV1SwaggerOnce.Do(func() {
		raw, err := readSwaggerJSON()
		if err != nil {
			openapiV1SwaggerErr = err
			return
		}
		var spec map[string]any
		if err := json.Unmarshal(raw, &spec); err != nil {
			openapiV1SwaggerErr = err
			return
		}

		spec["info"] = map[string]any{
			"title":       "Tier0 Enterprise OpenAPI v1",
			"description": "Tier0 Enterprise OpenAPI",
			"version":     "v1",
		}
		// The generated document is Swagger 2.0, where servers is ignored.
		// Keep host and schemes unset so Swagger UI uses the current page origin,
		// including the external scheme provided by the reverse proxy.
		spec["basePath"] = openapiV1Prefix
		delete(spec, "host")
		delete(spec, "schemes")
		delete(spec, "servers")
		spec["tags"] = []any{
			map[string]any{
				"name":        "openapi",
				"description": "OpenAPI v1",
			},
		}
		paths := normalizeOpenAPIV1Paths(spec["paths"])
		spec["paths"] = paths
		pruneOpenAPIV1Schemas(spec, paths)

		scheme := map[string]any{"type": "apiKey", "in": "header", "name": "x-api-key", "description": "Explicitly scoped Agent or service API Key"}
		if spec["swagger"] == "2.0" {
			spec["securityDefinitions"] = map[string]any{"ApiKeyAuth": scheme}
		} else {
			components, ok := spec["components"].(map[string]any)
			if !ok {
				components = map[string]any{}
				spec["components"] = components
			}
			components["securitySchemes"] = map[string]any{"ApiKeyAuth": scheme}
		}

		spec["security"] = []any{
			map[string]any{"ApiKeyAuth": []any{}},
		}
		applyOpenAPIRequestDescriptions(spec)

		data, err := json.MarshalIndent(spec, "", "  ")
		if err != nil {
			openapiV1SwaggerErr = err
			return
		}
		openapiV1SwaggerJSON = data
	})
	return openapiV1SwaggerJSON, openapiV1SwaggerErr
}

func normalizeOpenAPIV1Paths(raw any) map[string]any {
	paths, ok := raw.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	out := make(map[string]any)
	for path, item := range paths {
		if !strings.HasPrefix(path, openapiV1Prefix+"/") {
			continue
		}
		normalizedPath := strings.TrimPrefix(path, openapiV1Prefix)
		if normalizedPath == "" {
			normalizedPath = "/"
		}
		if !isOpenAPIV1DocPath(normalizedPath) {
			continue
		}
		normalizeOpenAPIV1PathItem(normalizedPath, item)
		out[normalizedPath] = item
	}
	return out
}

func isOpenAPIV1DocPath(path string) bool {
	return path == "/info" ||
		strings.HasPrefix(path, "/notifications") ||
		strings.HasPrefix(path, "/assets") ||
		strings.HasPrefix(path, "/auth/") ||
		strings.HasPrefix(path, "/flow/") ||
		strings.HasPrefix(path, "/flows") ||
		strings.HasPrefix(path, "/launchpad/") ||
		strings.HasPrefix(path, "/platform/") ||
		strings.HasPrefix(path, "/uns/")
}

func normalizeOpenAPIV1PathItem(path string, item any) {
	pathItem, ok := item.(map[string]any)
	if !ok {
		return
	}
	for _, operation := range pathItem {
		operationMap, ok := operation.(map[string]any)
		if !ok {
			continue
		}
		operationMap["tags"] = []any{"openapi"}
		if path == "/info" {
			operationMap["security"] = []any{}
		}
	}
}

// Retain only schemas transitively referenced by public paths. Swagger 2 uses
// definitions; OpenAPI 3 uses components/schemas. Neither may leak internal DTOs.
func pruneOpenAPIV1Schemas(spec map[string]any, paths map[string]any) {
	if definitions, ok := spec["definitions"].(map[string]any); ok {
		spec["definitions"] = reachableSchemas(paths, definitions, "#/definitions/")
	}
	if components, ok := spec["components"].(map[string]any); ok {
		if schemas, ok := components["schemas"].(map[string]any); ok {
			components["schemas"] = reachableSchemas(paths, schemas, openapiV1SchemaRefPrefix)
		}
	}
}
func reachableSchemas(paths, schemas map[string]any, prefix string) map[string]any {
	used := map[string]any{}
	var visit func(any)
	visit = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok && strings.HasPrefix(ref, prefix) {
				name := strings.TrimPrefix(ref, prefix)
				if _, found := used[name]; !found {
					if schema, exists := schemas[name]; exists {
						used[name] = schema
						visit(schema)
					}
				}
			}
			for _, child := range v {
				visit(child)
			}
		case []any:
			for _, child := range v {
				visit(child)
			}
		}
	}
	visit(paths)
	return used
}

package system

import (
	openapiv2 "backend/http/openapi/v2"
	"net/http"
)

func OpenapiV2SwaggerUIHandler() http.HandlerFunc {
	return swaggerUIHandler("/openapi/v2", "Tier0 Enterprise OpenAPI v2")
}

func OpenapiV2SwaggerJSONHandler() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = writer.Write(openapiv2.Specification)
	}
}

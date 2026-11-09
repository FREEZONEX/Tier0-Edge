package handler

import (
	"net/http"
	"time"

	"backend/internal/domain/mcpserver"
	"backend/internal/domain/unsv2"
	coresystem "backend/internal/handler/core/system"
	"backend/internal/svc"

	"github.com/zeromicro/go-zero/rest"
)

func RegisterExtraHandlers(server *rest.Server, ctx *svc.ServiceContext) {
	RegisterOpenAPIDocumentationHandlers(server)
	registerMCPHandlers(server, ctx)
}

func RegisterOpenAPIDocumentationHandlers(server *rest.Server) {
	server.AddRoutes(
		[]rest.Route{
			{
				Method:  http.MethodGet,
				Path:    "/swagger",
				Handler: coresystem.OpenapiV1SwaggerUIHandler(),
			},
			{
				Method:  http.MethodGet,
				Path:    "/swagger.json",
				Handler: coresystem.OpenapiV1SwaggerJSONHandler(),
			},
		},
		rest.WithPrefix("/openapi/v1"),
		rest.WithTimeout(0),
	)
	server.AddRoutes([]rest.Route{
		{Method: http.MethodGet, Path: "/swagger", Handler: coresystem.OpenapiV2SwaggerUIHandler()},
		{Method: http.MethodGet, Path: "/swagger.json", Handler: coresystem.OpenapiV2SwaggerJSONHandler()},
	}, rest.WithPrefix("/openapi/v2"), rest.WithTimeout(0))
}

func registerMCPHandlers(server *rest.Server, ctx *svc.ServiceContext) {
	// OAuth 与 API Key 均复用当前用户权限匹配；标准 JSON-RPC 不套业务 Envelope。
	mcpHandler := ctx.App.MCPOAuth.Protect(ctx.App.Permission, ctx.App.MCP.ServeHTTP)
	server.AddRoutes([]rest.Route{
		{Method: http.MethodPost, Path: mcpserver.EndpointPath, Handler: mcpHandler},
		{Method: http.MethodGet, Path: mcpserver.EndpointPath, Handler: mcpHandler},
		{Method: http.MethodDelete, Path: mcpserver.EndpointPath, Handler: mcpHandler},
	}, rest.WithMaxBytes(unsv2.MaxBodyBytes), rest.WithTimeout(35*time.Second))
}

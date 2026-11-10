package middleware

import (
	"github.com/zeromicro/go-zero/core/logx"
	"net/http"
	"strings"
	"time"
)

func sensitiveSourceRequest(r *http.Request) bool {
	if r.Method == http.MethodPut && r.URL.Path == "/api/core/fleet/current/connection" {
		return true
	}
	// MCP 的认证头与模型查询参数不进入详细请求日志或 5xx body dump。
	if r.URL.Path == "/mcp" || strings.HasPrefix(r.URL.Path, "/mcp/oauth/") || strings.HasPrefix(r.URL.Path, "/api/core/mcp/oauth/") {
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/openapi/v2/uns/sources/") || strings.HasPrefix(r.URL.Path, "/api/core/v2/uns/sources/") {
		return true
	}
	return (strings.HasPrefix(r.URL.Path, "/api/core/uns/sources/") || strings.HasPrefix(r.URL.Path, "/openapi/v1/uns/sources/")) && (strings.HasSuffix(r.URL.Path, "/credentials") || strings.Contains(r.URL.Path, "/oauth/"))
}

// go-zero's request logger dumps headers and bodies on 5xx (all requests in
// verbose mode). Every route may carry credentials; log only a safe summary.
func SourceSafeLog(_ bool) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := &auditResponseWriter{ResponseWriter: w}
			next(recorder, r)
			logx.WithContext(r.Context()).Infof("[HTTP] %s %s status=%d duration=%s", r.Method, r.URL.Path, recorder.StatusCode(), time.Since(started))
		}
	}
}

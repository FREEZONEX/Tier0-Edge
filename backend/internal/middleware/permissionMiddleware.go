// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package middleware

import (
	"net/http"
	"strings"

	"backend/internal/contextx"
	apphttpx "backend/internal/httpx"
	"backend/internal/permission"
)

type PermissionMiddleware struct {
	evaluator *permission.Evaluator
}

func NewPermissionMiddleware(evaluator *permission.Evaluator) *PermissionMiddleware {
	return &PermissionMiddleware{evaluator: evaluator}
}

func (m *PermissionMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subject, ok := contextx.SubjectFrom(r.Context())
		if !ok {
			if apphttpx.WriteStreamRequestAuthError(w, r, "UNAUTHENTICATED") || apphttpx.WriteUnsV2AuthError(w, r, "UNAUTHENTICATED") {
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if isAdminOnlyInfrastructureAPI(r.URL.Path) {
			isAdmin, err := m.evaluator.IsAdmin(r.Context(), subject)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !isAdmin {
				apphttpx.WriteError(w, apphttpx.NewHTTPError(http.StatusForbidden, "forbidden"))
				return
			}
		}
		if isClusterSelfServiceAPI(r.Method, r.URL.Path) {
			next(w, r)
			return
		}
		allowed, _, err := m.evaluator.Allow(r.Context(), subject, "api", r.Method, r.URL.Path)
		if err != nil {
			if apphttpx.WriteStreamRequestAuthError(w, r, "INTERNAL_ERROR") || apphttpx.WriteUnsV2AuthError(w, r, "INTERNAL_ERROR") {
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !allowed {
			if apphttpx.WriteStreamRequestAuthError(w, r, "FORBIDDEN") || apphttpx.WriteUnsV2AuthError(w, r, "FORBIDDEN") {
				return
			}
			apphttpx.WriteError(w, apphttpx.NewHTTPError(http.StatusForbidden, "forbidden"))
			return
		}
		next(w, r)
	}
}

func isAdminOnlyInfrastructureAPI(path string) bool {
	switch path {
	case "/api/core/system/runtime-metrics",
		"/api/core/system/login-session-duration",
		"/api/core/cluster/nodes",
		"/api/core/cluster/tokens",
		"/api/core/cluster/scopes",
		"/api/core/cluster/outbox":
		return true
	default:
		return path == "/api/core/fleet" ||
			strings.HasPrefix(path, "/api/core/fleet/")
	}
}

func isClusterSelfServiceAPI(method, path string) bool {
	switch path {
	case "/api/core/cluster/config":
		return method == http.MethodGet
	case "/api/core/cluster/connect",
		"/api/core/cluster/user-sync",
		"/api/core/cluster/disconnect",
		"/api/core/cluster/test-connect":
		return method == http.MethodPost
	case "/api/core/cluster/tokens":
		return method == http.MethodPost
	case "/api/core/cluster/nodes",
		"/api/core/cluster/scopes",
		"/api/core/cluster/outbox",
		"/api/core/cluster/sync-logs":
		return method == http.MethodGet
	default:
		return false
	}
}

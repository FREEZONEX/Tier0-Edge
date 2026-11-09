// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package middleware

import (
	"net/http"
	"strings"

	"backend/internal/contextx"
	"backend/internal/domain/audit"
	authdomain "backend/internal/domain/auth"
	"backend/internal/domain/notify/openapiresp"
	"backend/internal/domain/unsv2"
	respx "backend/internal/httpx"
	"backend/internal/operationcontract"
	"backend/internal/permission"
	"backend/internal/repo"
)

type ApiKeyAuthMiddleware struct {
	auth       *authdomain.Service
	permission *permission.Evaluator
	audit      *audit.Service
}

func NewApiKeyAuthMiddleware(auth *authdomain.Service, evaluator *permission.Evaluator, auditSvc *audit.Service) *ApiKeyAuthMiddleware {
	return &ApiKeyAuthMiddleware{auth: auth, permission: evaluator, audit: auditSvc}
}

func (m *ApiKeyAuthMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 消息通知 OpenAPI（TASK-030）：错误契约按 {errorCode,message} 输出（对齐云端 notifyresp），
		// 与其他 OpenAPI 共用 action 匹配，错误体保留通知接口兼容格式。
		isNotify := strings.HasPrefix(r.URL.Path, "/openapi/v1/notifications/")
		subject, err := m.auth.AuthenticateAPIKey(r.Context(), authdomain.APIKeyFromRequest(r))
		if err != nil {
			if respx.WriteStreamRequestAuthError(w, r, "UNAUTHENTICATED") || respx.WriteUnsV2AuthError(w, r, "UNAUTHENTICATED") {
				return
			}
			if isNotify {
				openapiresp.WriteError(w, http.StatusUnauthorized, "INVALID_CREDENTIAL", "invalid credential")
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// App data keys use the explicit operation registry before Collection grants.
		// Platform and Control routes retain their own capability allowlists.
		appDataRoute := false
		if subject.APIKeyUsageType == repo.APIKeyUsageAppData && (r.Method == http.MethodPost || r.Method == http.MethodGet && r.URL.Path == "/openapi/v2/uns/records:subscribe") && strings.HasPrefix(r.URL.Path, "/openapi/v2/uns/") {
			respx.PrepareUnsV2(r)
			operation, _ := respx.UnsV2Operation(r.Context())
			appDataRoute = unsv2.IsAppOperationAllowed(operation)
		}
		if subject.APIKeyUsageType == repo.APIKeyUsageAppData && !appDataRoute && !repo.IsPlatformV2Route(r.Method, r.URL.Path) && !operationcontract.IsAppRoute(r.Method, r.URL.Path) && !operationcontract.IsStreamRequestRoute(r.Method, r.URL.Path) {
			if respx.WriteStreamRequestAuthError(w, r, "FORBIDDEN") || respx.WriteUnsV2AuthError(w, r, "FORBIDDEN") {
				return
			}
			http.Error(w, "App data keys require OpenAPI v2", http.StatusForbidden)
			return
		}
		if isNotify {
			allowed, _, err := m.permission.Allow(r.Context(), subject, "openapi", r.Method, r.URL.Path)
			if err != nil {
				openapiresp.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "permission lookup failed")
				return
			}
			if !allowed {
				openapiresp.WriteError(w, http.StatusForbidden, "NOTIFICATION_NOT_ALLOWED",
					"notification capability not allowed for this API key")
				return
			}
			ctx := contextx.WithSubject(r.Context(), subject)
			if m.audit != nil {
				m.audit.BindSubject(ctx, subject)
			}
			next(w, r.WithContext(ctx))
			return
		}
		allowed, _, err := m.permission.Allow(r.Context(), subject, "openapi", r.Method, r.URL.Path)
		if err != nil {
			if respx.WriteStreamRequestAuthError(w, r, "INTERNAL_ERROR") || respx.WriteUnsV2AuthError(w, r, "INTERNAL_ERROR") {
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !allowed {
			if respx.WriteStreamRequestAuthError(w, r, "FORBIDDEN") || respx.WriteUnsV2AuthError(w, r, "FORBIDDEN") {
				return
			}
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ctx := contextx.WithSubject(r.Context(), subject)
		if m.audit != nil {
			m.audit.BindSubject(ctx, subject)
		}
		next(w, r.WithContext(ctx))
	}
}

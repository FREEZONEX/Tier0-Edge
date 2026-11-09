// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package middleware

import (
	"net/http"

	"backend/internal/contextx"
	"backend/internal/domain/audit"
	authdomain "backend/internal/domain/auth"
	respx "backend/internal/httpx"
)

type SessionAuthMiddleware struct {
	auth  *authdomain.Service
	audit *audit.Service
}

func NewSessionAuthMiddleware(auth *authdomain.Service, auditSvc *audit.Service) *SessionAuthMiddleware {
	return &SessionAuthMiddleware{auth: auth, audit: auditSvc}
}

func (m *SessionAuthMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subject, err := m.auth.AuthenticateRequest(r)
		if err != nil {
			if respx.WriteStreamRequestAuthError(w, r, "UNAUTHENTICATED") || respx.WriteUnsV2AuthError(w, r, "UNAUTHENTICATED") {
				return
			}
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := contextx.WithSubject(r.Context(), subject)
		if m.audit != nil {
			m.audit.BindSubject(ctx, subject)
		}
		next(w, r.WithContext(ctx))
	}
}

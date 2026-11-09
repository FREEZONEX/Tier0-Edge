package middleware

import (
	"backend/internal/contextx"
	"backend/internal/domain/cluster"
	"backend/internal/unsref"
	"encoding/json"
	"net/http"
	"strings"
)

type FleetAuthMiddleware struct {
	cluster *cluster.Service
	apiKey  func(http.HandlerFunc) http.HandlerFunc
}

func NewFleetAuthMiddleware(service *cluster.Service, apiKey func(http.HandlerFunc) http.HandlerFunc) *FleetAuthMiddleware {
	return &FleetAuthMiddleware{cluster: service, apiKey: apiKey}
}

func (m *FleetAuthMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := strings.Fields(r.Header.Get("Authorization"))
		// JWT task credentials have three segments. Opaque API keys retain their
		// existing validator. A selected task credential never falls back to APIKey.
		if len(header) != 2 || !strings.EqualFold(header[0], "Bearer") || strings.Count(header[1], ".") != 2 {
			m.apiKey(next)(w, r)
			return
		}
		reject := func() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 403, "msg": "Fleet task not authorized", "data": nil, "error": map[string]string{"code": "FORBIDDEN"}})
		}
		if r.Header.Get("X-API-Key") != "" || r.URL.Query().Get("apikey") != "" || r.URL.Query().Get("apiKey") != "" {
			reject()
			return
		}
		if suffix, ok := strings.CutPrefix(r.URL.Path, "/openapi/v1/controls/"); ok {
			code, action, valid := strings.Cut(suffix, "/")
			scope := map[string]string{"describe": "controls.describe", "invoke": "controls.invoke", "invocations/get": "controls.get"}[action]
			if suffix == "discover" {
				code, scope, valid = "node", "controls.discover", true
			}
			if !valid || r.Method != http.MethodPost || !unsref.ValidMetadataID(code) || scope == "" {
				reject()
				return
			}
			claims, err := m.cluster.VerifyFleetServiceToken(r.Context(), header[1], scope, code)
			if err != nil {
				reject()
				return
			}
			subject := contextx.Subject{AuthType: "fleetTask", FleetTask: &contextx.FleetTaskSubject{Automatic: claims.Automatic, NodeID: claims.Subject, Scope: claims.Scope, ResourceID: claims.ResourceID, GrantID: claims.GrantID, GrantRevision: claims.GrantRevision}}
			next(w, r.WithContext(contextx.WithSubject(r.Context(), subject)))
			return
		}
		reject()
	}
}

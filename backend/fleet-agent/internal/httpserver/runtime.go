package httpserver

import (
	"backend/internal/fleetprotocol"
	"net/http"
	"regexp"
)

func (s *Server) BindRuntime(provider fleetprotocol.RuntimeProvider) { s.runtime = provider }
func (s *Server) runtimeOperation(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	var req fleetprotocol.RuntimeRequest
	if decodeJSONBody(w, r, &req) != nil {
		s.writeError(w, 400, "RUNTIME_INVALID_REQUEST", "invalid runtime request")
		return
	}
	result, err := fleetprotocol.ExecuteRuntime(r.Context(), s.runtime, req)
	if err != nil {
		code := err.Error()
		if !runtimeErrorCode.MatchString(code) {
			code = "RUNTIME_OPERATION_FAILED"
		}
		s.writeError(w, 400, code, "runtime operation rejected")
		return
	}
	s.writeJSON(w, 200, result)
}

var runtimeErrorCode = regexp.MustCompile(`^RUNTIME_[A-Z_]{1,80}$`)

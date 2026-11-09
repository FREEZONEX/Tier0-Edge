package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSourceStreamModeRejectsLegacyFlowEntryPoints(t *testing.T) {
	g := &HTTPGateway{}
	g.DisableLegacyFlows()
	for _, path := range []string{"/flow", "/nodered/flows", "/NodeRed/Flows", "/sourceflow/x", "/eventflow/x", "/api/core/flows/1", "/openapi/v1/flow/list", "/openapi/v1/flows/1", "/openapi/v1/uns/unsBindFlow"} {
		w := httptest.NewRecorder()
		if !g.Handle(w, httptest.NewRequest(http.MethodPost, path, nil)) || w.Code != http.StatusGone {
			t.Fatalf("legacy route %q was not rejected: %d", path, w.Code)
		}
	}
	for _, path := range []string{"/api/core/uns/sources", "/api/core/uns/streams/1/invoke", "/openapi/v1/uns/streams/1/invoke", "/uns", "/workflow"} {
		if legacyFlowPath(path) {
			t.Fatalf("new or unrelated route %q was blocked", path)
		}
	}
}

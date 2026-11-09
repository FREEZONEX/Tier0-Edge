package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/internal/bootstrap"
	"backend/internal/config"
	domainauth "backend/internal/domain/auth"
	"backend/internal/infra/cache"
	"backend/internal/svc"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

// 即使提供方已配置，关闭总开关也必须阻断所有入口；未知提供方不得落入默认适配器。
func TestExternalSSOGatesAllEndpoints(t *testing.T) {
	service := &domainauth.Service{}
	service.ConfigureMicrosoft(&cache.Client{}, domainauth.MicrosoftConfig{
		Enabled: true, ClientID: "test-client", ClientSecret: "test-secret",
		RedirectURL: "https://example.com/api/core/auth/sso/microsoft/callback",
	})
	c := &svc.ServiceContext{App: &bootstrap.App{Auth: service}}
	for _, enabled := range []bool{false, true} {
		c.Config.ExternalLogin = config.ExternalLoginConf{Enabled: enabled}
		provider := "microsoft"
		if enabled {
			provider = "unregistered"
		}
		for _, action := range []string{"start", "callback", "pending", "username", "complete", "link"} {
			req := pathvar.WithVars(httptest.NewRequest(http.MethodGet, "/api/core/auth/sso/"+provider+"/"+action, nil), map[string]string{"provider": provider})
			recorder := httptest.NewRecorder()
			externalSSOHandler(c, action)(recorder, req)
			if recorder.Code != http.StatusNotFound || recorder.Header().Get("Location") != "" || len(recorder.Result().Cookies()) != 0 {
				t.Fatalf("enabled=%v provider=%s action=%s: expected inert 404, got %d", enabled, provider, action, recorder.Code)
			}
		}
	}
}

func TestMiniEntryOriginAndCacheHeaders(t *testing.T) {
	for _, origin := range []string{"", "http://customer.example", "https://other.example", "https://customer.example/path", "https://customer.example#fragment"} {
		request := httptest.NewRequest(http.MethodPost, "https://customer.example/api/core/auth/mini-entry-exchange", strings.NewReader(`{"ticket":"test"}`))
		request.Header.Set("Origin", origin)
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		MiniEntryExchangeHandler(nil)(recorder, request)
		if recorder.Code != 403 || len(recorder.Result().Cookies()) != 0 {
			t.Fatalf("origin %q: status %d", origin, recorder.Code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "https://customer.example/api/core/auth/mini-entry", nil)
	recorder := httptest.NewRecorder()
	MiniEntryHandler(nil)(recorder, request)
	if recorder.Code != 200 || recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("Referrer-Policy") != "no-referrer" || !strings.Contains(recorder.Header().Get("Content-Security-Policy"), "script-src 'nonce-") {
		t.Fatal("bridge response must not be cached or allow arbitrary script")
	}
	if !strings.Contains(recorder.Body.String(), "history.replaceState") {
		t.Fatal("bridge must clear fragment before exchange")
	}
}

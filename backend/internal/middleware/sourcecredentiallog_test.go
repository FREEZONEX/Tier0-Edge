package middleware

import (
	"bytes"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/core/logx"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrdinaryRequestLogsExcludeCredentials(t *testing.T) {
	previous := logx.Reset()
	t.Cleanup(func() {
		logx.Reset()
		if previous != nil {
			logx.SetWriter(previous)
		}
	})
	var captured bytes.Buffer
	logx.SetWriter(logx.NewWriter(&captured))
	for _, verbose := range []bool{false, true} {
		captured.Reset()
		r := httptest.NewRequest(http.MethodPost, "/openapi/v2/uns/records/search?token=query-private", strings.NewReader(`{"secret":"body-private"}`))
		r.Header.Set("X-API-Key", "key-private")
		r.Header.Set("Authorization", "Bearer auth-private")
		r.Header.Set("Cookie", "session=cookie-private")
		w := httptest.NewRecorder()
		SourceSafeLog(verbose)(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "key-private", r.Header.Get("X-API-Key"))
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.Contains(t, string(body), "body-private")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("response-private"))
		})(w, r)
		require.Equal(t, http.StatusInternalServerError, w.Code)
		logged := captured.String()
		require.Contains(t, logged, "POST /openapi/v2/uns/records/search status=500")
		for _, secret := range []string{"query-private", "key-private", "auth-private", "cookie-private", "body-private", "response-private", "X-Api-Key", "Authorization", "Cookie"} {
			require.NotContains(t, logged, secret)
		}
	}
}

func TestSourceCredentialRequestsDoNotCaptureAuditPayload(t *testing.T) {
	for _, path := range []string{"/api/core/uns/sources/42/credentials", "/api/core/uns/sources/42/oauth/start", "/api/core/uns/sources/42/oauth/complete", "/openapi/v1/uns/sources/42/credentials", "/openapi/v1/uns/sources/42/oauth/start", "/openapi/v1/uns/sources/42/oauth/complete"} {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"code":"secret-code","secret":"private-value"}`))
		r.Header.Set("Content-Type", "application/json")
		require.True(t, sensitiveSourceRequest(r))
		require.Nil(t, captureJSONRequestPayload(r))
		w := httptest.NewRecorder()
		SourceSafeLog(true)(func(w http.ResponseWriter, r *http.Request) {
			b, e := io.ReadAll(r.Body)
			require.NoError(t, e)
			require.Contains(t, string(b), "secret-code")
			w.WriteHeader(http.StatusBadGateway)
		})(w, r)
		require.Equal(t, http.StatusBadGateway, w.Code)
	}
	require.False(t, sensitiveSourceRequest(httptest.NewRequest(http.MethodGet, "/api/core/uns/sources/42", nil)))
}

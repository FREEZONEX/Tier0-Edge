package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/handler"
	"backend/internal/repo"
	"backend/internal/svc/svctest"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"github.com/zeromicro/go-zero/rest"
)

func TestFleetTokenSubmissionRoute(t *testing.T) {
	service := svctest.NewTestServiceContext(t)
	server, err := rest.NewServer(service.Config.RestConf)
	require.NoError(t, err)
	defer server.Stop()
	handler.RegisterHandlers(server, service)
	engine, err := rest.NewServerless(server)
	require.NoError(t, err)

	operator, err := repo.NewIAMRepo(service.App.Store).CreateUser(context.Background(), repo.UserCreate{
		UserName: "fleet-route-operator", Password: "test-password", Status: 1, RoleID: repo.SystemRoleOperatorID,
	})
	require.NoError(t, err)
	session := func(uid any) string {
		t.Helper()
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"uid": uid, "exp": time.Now().Add(time.Hour).Unix(),
		}).SignedString([]byte(service.Config.Security.JwtSecret))
		require.NoError(t, err)
		return token
	}
	const invalidToken = "invalid-test-token"
	call := func(token string, expected int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPut, "/api/core/fleet/current/connection",
			strings.NewReader(`{"accessToken":"`+invalidToken+`"}`))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		engine.Serve(w, r)
		require.Equal(t, expected, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), invalidToken)
		return w
	}
	call("", http.StatusUnauthorized)
	call(session(operator["userId"]), http.StatusForbidden)
	w := call(session(1), http.StatusBadRequest)
	require.Contains(t, w.Body.String(), "invalid")
}

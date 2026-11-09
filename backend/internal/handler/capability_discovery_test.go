package handler_test

// 已生成真实路由 + 正常session/APIKey中间件；只读请求在独立测试库上执行。
import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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

func TestCapabilityDiscoveryRegisteredPublicAndSession(t *testing.T) {
	service := svctest.NewTestServiceContext(t)
	service.Config.RestConf.Middlewares.MaxBytes = true
	server, err := rest.NewServer(service.Config.RestConf)
	require.NoError(t, err)
	defer server.Stop()
	handler.RegisterHandlers(server, service)
	engine, err := rest.NewServerless(server)
	require.NoError(t, err)
	require.Len(t, service.PublicRoutes, len(server.Routes()), "all generators must snapshot routes only after the final registration")
	require.True(t, service.PublicRoutes["POST /openapi/v2/uns/streams/program/invoke"])
	require.True(t, service.PublicRoutes["POST /openapi/v2/capabilities/discover"])
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"uid": 1, "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte(service.Config.Security.JwtSecret))
	require.NoError(t, err)
	_, key, err := repo.NewAPIKeyRepo(service.App.Store).CreateAPIKey(context.Background(), "discovery test", 1, 1, "full_access", "", "", []string{"openapi.base", "uns.read", "uns.source", "uns.source.manage", "uns.stream", "uns.stream.manage", "uns.stream.deploy", "uns.stream.execute"})
	require.NoError(t, err)
	_, limitedKey, err := repo.NewAPIKeyRepo(service.App.Store).CreateAPIKey(context.Background(), "limited discovery test", 1, 1, "read_only", "", "", []string{"asset.file.read"})
	require.NoError(t, err)
	call := func(path, header, value, body, content string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Content-Type", content)
		if header != "" {
			r.Header.Set(header, value)
		}
		w := httptest.NewRecorder()
		engine.Serve(w, r)
		return w
	}
	live := httptest.NewServer(http.HandlerFunc(engine.Serve))
	defer live.Close()
	callHTTP := func(path, header, value, body string) *httptest.ResponseRecorder {
		t.Helper()
		r, err := http.NewRequest(http.MethodPost, live.URL+path, strings.NewReader(body))
		require.NoError(t, err)
		r.Header.Set("Content-Type", "application/json")
		if header != "" {
			r.Header.Set(header, value)
		}
		response, err := live.Client().Do(r)
		require.NoError(t, err)
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		w := httptest.NewRecorder()
		w.Code = response.StatusCode
		_, err = w.Body.Write(data)
		require.NoError(t, err)
		return w
	}
	assertError := func(w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		require.Equal(t, status, w.Code, w.Body.String())
		var fault map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fault))
		require.Len(t, fault, 2)
		require.Equal(t, code, fault["errorCode"])
		require.IsType(t, "", fault["message"])
	}
	assertError(call("/openapi/v2/capabilities/discover", "X-API-Key", limitedKey, "{}", "application/json"), 403, "FORBIDDEN")
	assertError(callHTTP("/openapi/v2/capabilities/discover", "X-API-Key", limitedKey, strings.Repeat(" ", 1048577)+"{}"), 403, "FORBIDDEN")
	for _, c := range []struct{ path, header, value string }{{"/openapi/v2/capabilities/discover", "X-API-Key", key}, {"/api/core/v2/capabilities/discover", "Authorization", "Bearer " + token}} {
		w := call(c.path, c.header, c.value, "{}", "application/json")
		require.Equal(t, 200, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.Equal(t, "enterprise", body["product"])
		require.NotContains(t, body, "data")
		require.NotContains(t, body, "code")
		assertError(call(c.path+"?scope=all", c.header, c.value, "{}", "application/json"), 400, "INVALID_REQUEST")
		require.Equal(t, 400, call(c.path, c.header, c.value, `{"scope":"all"}`, "application/json").Code)
		require.Equal(t, 415, call(c.path, c.header, c.value, "{}", "text/plain").Code)
		for _, size := range []int{4097, 1048577} {
			assertError(callHTTP(c.path, c.header, c.value, strings.Repeat(" ", size)+"{}"), 413, "REQUEST_TOO_LARGE")
		}
		assertError(callHTTP(c.path, "", "", strings.Repeat(" ", 1048577)+"{}"), 401, "UNAUTHENTICATED")
	}
	assertError(call("/openapi/v2/capabilities/discover", "", "", "{}", "application/json"), 401, "UNAUTHENTICATED")
	// A session token cannot enter the APIKey public route, or vice versa.
	require.Equal(t, 401, call("/openapi/v2/capabilities/discover", "Authorization", "Bearer "+token, "{}", "application/json").Code)
	require.Equal(t, 401, call("/api/core/v2/capabilities/discover", "X-API-Key", key, "{}", "application/json").Code)
	// Preserve the existing platform error envelope outside discovery.
	unrelated := call("/openapi/v2/platform/info/get", "", "", "{}", "application/json")
	require.Equal(t, 401, unrelated.Code)
	var original map[string]any
	require.NoError(t, json.Unmarshal(unrelated.Body.Bytes(), &original))
	require.Contains(t, original, "code")
	require.NotContains(t, original, "errorCode")
	// The unrelated route retains its existing engine size guard and empty 413.
	unrelatedLarge := call("/openapi/v2/platform/info/get", "X-API-Key", key, strings.Repeat(" ", 1048577)+"{}", "application/json")
	require.Equal(t, 413, unrelatedLarge.Code)
	require.Empty(t, unrelatedLarge.Body.String())
	store := service.App.Store
	service.App.Store = nil
	assertError(call("/openapi/v2/capabilities/discover", "X-API-Key", key, "{}", "application/json"), 503, "DISCOVERY_UNAVAILABLE")
	service.App.Store = store
	if module := os.Getenv("CAPABILITY_SDK_MODULE"); module != "" {
		script, err := filepath.Abs("../../../packages/app-data-client/capabilities-sdk-errors.mjs")
		require.NoError(t, err)
		for _, test := range []struct{ status, code, key string }{{"413", "REQUEST_TOO_LARGE", key}, {"401", "UNAUTHENTICATED", ""}, {"403", "FORBIDDEN", limitedKey}, {"400", "INVALID_REQUEST", key}, {"503", "DISCOVERY_UNAVAILABLE", key}} {
			if test.status == "503" {
				service.App.Store = nil
			}
			command := exec.Command("node", script)
			command.Env = append(os.Environ(), "CAPABILITY_TEST_BASE="+live.URL, "CAPABILITY_TEST_KEY="+test.key, "CAPABILITY_EXPECTED_STATUS="+test.status, "CAPABILITY_EXPECTED_CODE="+test.code)
			output, err := command.CombinedOutput()
			service.App.Store = store
			require.NoError(t, err, "SDK HTTP %s: %s", test.status, output)
			t.Logf("real middleware/handler -> SDK: HTTP %s %s", test.status, test.code)
		}
	}

}

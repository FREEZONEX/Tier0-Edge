package audit

import (
	"backend/internal/repo"
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
)

func req(t *testing.T, method, path string) *http.Request {
	t.Helper()
	return httptest.NewRequest(method, path, nil)
}

// BindRequestInfo 应把中间件采集的 client IP 与 User-Agent 存入 audit state，
// 并可通过 RequestInfoFrom 从同一 ctx 读回。

// 默认不信任转发头：未认证调用方伪造 X-Forwarded-For 不得污染登录日志的来源 IP。

// 部署在可信反向代理之后（TrustProxyHeaders=true）时才采信转发头。

// 无 audit state 的 ctx 读不到请求信息；nil 请求不 panic。
func TestRequestInfoFromWithoutState(t *testing.T) {
	if _, _, ok := RequestInfoFrom(context.Background()); ok {
		t.Fatal("bare context should not carry request info")
	}

	svc := &Service{}
	svc.BindRequestInfo(nil) // 不应 panic

	r := req(t, http.MethodGet, "/api/core/whatever")
	svc.BindRequestInfo(r) // ctx 无 state，安全 no-op
	if _, _, ok := RequestInfoFrom(r.Context()); ok {
		t.Fatal("context without state should not carry request info")
	}
}

func TestShouldAutoAudit(t *testing.T) {
	cases := []struct {
		name   string
		method string
		path   string
		want   bool
	}{
		// Vision worker 面的机器调用按秒级频率触发,不是用户操作,不进审计。
		{"vision worker register", http.MethodPost, "/api/core/vision/worker/register", false},
		{"vision ingest", http.MethodPost, "/api/core/vision/ingest/11", false},
		{"vision heartbeat", http.MethodPost, "/api/core/vision/tasks/11/heartbeat", false},
		// 用户对 vision 任务的操作照常审计。
		{"vision task create", http.MethodPost, "/api/core/vision/tasks", true},
		{"vision task start", http.MethodPost, "/api/core/vision/tasks/11/start", true},
		{"vision task delete", http.MethodDelete, "/api/core/vision/tasks/11", true},
		// 既有行为不受影响。
		{"get is never audited", http.MethodGet, "/api/core/vision/tasks", false},
		{"mqtt auth skipped", http.MethodPost, "/api/core/mqtt/auth", false},
		{"non core path skipped", http.MethodPost, "/internal/whatever", false},
		{"regular core write audited", http.MethodPost, "/api/core/users", true},
		{"audit export is audited", http.MethodPost, "/api/core/audit-logs/export", true},
		{"audit search skipped", http.MethodPost, "/api/core/audit-logs/page", false},
		{"audit options skipped", http.MethodPost, "/api/core/audit-logs/filter-options", false},
		// 登录日志查询是读操作，且路径含 "login" 会被误判成 Login 业务，必须跳过。
		{"login log search skipped", http.MethodPost, "/api/core/login-logs/page", false},
		{"auth login skipped", http.MethodPost, "/api/core/auth/login", false},
		{"mini entry exchange skipped", http.MethodPost, "/api/core/auth/mini-entry-exchange", false},
		{"mini entry issue skipped", http.MethodPost, "/api/core/auth/mini-entry-ticket", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldAutoAudit(req(t, c.method, c.path)); got != c.want {
				t.Fatalf("shouldAutoAudit(%s %s) = %v, want %v", c.method, c.path, got, c.want)
			}
		})
	}
}

func TestAuditTrustedProxyClientIP(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		trust                           bool
		realIP, forwarded, remote, want string
	}{
		{"untrusted spoof", false, "203.0.113.5", "203.0.113.6", "192.0.2.10:9000", "192.0.2.10"},
		{"trusted real IP", true, "203.0.113.5", "203.0.113.6, 192.0.2.1", "192.0.2.10:9000", "203.0.113.5"},
		{"trusted forwarded takes last hop", true, "", "203.0.113.6, 192.0.2.1", "192.0.2.10:9000", "192.0.2.1"},
		{"trusted appended spoof ignored", true, "", "6.6.6.6, 203.0.113.9", "192.0.2.10:9000", "203.0.113.9"},
		{"trusted IPv6", true, "2001:db8::123", "", "[::1]:9000", "2001:db8::123"},
		{"invalid real ip falls back to xff", true, "invalid", "unknown, 203.0.113.6", "[::1]:9000", "203.0.113.6"},
		{"all headers invalid falls back to socket", true, "invalid", "unknown", "[::1]:9000", "::1"},
		{"absent headers", true, "", "", "192.0.2.10", "192.0.2.10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{}
			WithTrustProxyHeaders(tc.trust)(s)
			r := req(t, http.MethodPost, "/api/core/users")
			r.RemoteAddr = tc.remote
			r.Header.Set("X-Real-IP", tc.realIP)
			r.Header.Set("X-Forwarded-For", tc.forwarded)
			require.Equal(t, tc.want, s.BuildAuditLog(r, ResponseMeta{StatusCode: 200}).IP)
		})
	}
}

func TestAuditAsyncResultPreservesHTTPCode(t *testing.T) {
	for _, tc := range []struct {
		state, result string
		code          int
	}{
		{repo.AsyncJobStatusPending, "accepted", 200},
		{repo.AsyncJobStatusRunning, "accepted", 200},
		{repo.AsyncJobStatusSucceeded, "success", 200},
		{repo.AsyncJobStatusFailed, "failed", 200},
		{repo.AsyncJobStatusCanceled, "failed", 200},
		{repo.AsyncJobStatusPending, "failed", 403},
	} {
		t.Run(tc.state+http.StatusText(tc.code), func(t *testing.T) {
			item := (&Service{}).BuildAuditLog(req(t, http.MethodPost, "/api/core/uns/import-jobs"), ResponseMeta{StatusCode: tc.code, JobKey: "job_test", JobStatus: tc.state})
			require.EqualValues(t, tc.code, item.Code)
			require.Equal(t, tc.result, item.Result)
			require.Contains(t, item.DetailJSON, `"jobKey":"job_test"`)
		})
	}
}

func TestSanitizeDetailRemovesNestedUserContacts(t *testing.T) {
	detail := map[string]any{
		"email": "top@example.com",
		"request": map[string]any{
			"phone": "13800138000",
			"users": []any{
				map[string]any{"email": "nested@example.com", "userId": float64(1001)},
			},
		},
		"keep": "value",
	}

	got := sanitizeDetail(detail)
	if _, ok := got["email"]; ok {
		t.Fatal("top-level email was not removed")
	}
	request, ok := got["request"].(map[string]any)
	if !ok {
		t.Fatalf("request has unexpected type %T", got["request"])
	}
	if _, ok := request["phone"]; ok {
		t.Fatal("nested phone was not removed")
	}
	users, ok := request["users"].([]any)
	if !ok || len(users) != 1 {
		t.Fatalf("users has unexpected value %#v", request["users"])
	}
	user, ok := users[0].(map[string]any)
	if !ok {
		t.Fatalf("user has unexpected type %T", users[0])
	}
	if _, ok := user["email"]; ok {
		t.Fatal("email in nested array was not removed")
	}
	if user["userId"] != json.Number("1001") || got["keep"] != "value" {
		t.Fatalf("non-PII values changed: %#v", got)
	}
}

func TestAuditTypedSnapshotSanitization(t *testing.T) {
	type snapshot struct {
		ID       int64  `json:"id"`
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	got := marshalDetail(sanitizeDetail(map[string]any{"after": []snapshot{{ID: 9007199254740993, Password: "secret", Email: "private@example.com"}}}))
	want := `{"after":[{"id":9007199254740993}]}`
	if got != want {
		t.Fatalf("typed snapshot changed or leaked private data: %s", got)
	}
}

func TestAuditIAMFailureModule(t *testing.T) {
	for _, tc := range []struct{ path, module, id string }{
		{"/api/core/iam/users", ResTypeUserManagement, ""},
		{"/api/core/iam/roles/1", ResTypeRole, "1"},
		{"/api/core/iam/resources/1", ResTypeResource, "1"},
		{"/api/core/iam/users/42/password/reset", ResTypeUserManagement, "42"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			item := (&Service{}).BuildAuditLog(req(t, http.MethodPost, tc.path), ResponseMeta{StatusCode: 409})
			if item == nil || item.ResType != tc.module || item.ResID != tc.id {
				t.Fatalf("failed operation module = %#v, want %s", item, tc.module)
			}
		})
	}
}

func TestSanitizeDetailRemovesImportPasswords(t *testing.T) {
	got := sanitizeDetail(map[string]any{"request": map[string]any{"passwords": map[string]any{"2": "device-secret"}, "jobKey": "job-1"}})
	request := got["request"].(map[string]any)
	if _, exists := request["passwords"]; exists {
		t.Fatal("batch device passwords leaked into audit detail")
	}
	if request["jobKey"] != "job-1" {
		t.Fatal("non-sensitive task identifier was removed")
	}
}

// BindRequestInfo 应把中间件采集的 client IP 与 User-Agent 存入 audit state，
// 并可通过 RequestInfoFrom 从同一 ctx 读回。
func TestBindRequestInfoAndRequestInfoFrom(t *testing.T) {
	svc := &Service{}
	r := req(t, http.MethodPost, "/api/core/auth/login")
	r.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh) Chrome/120.0.0.0")
	r = r.WithContext(svc.WithState(r.Context()))

	svc.BindRequestInfo(r)

	ip, userAgent, ok := RequestInfoFrom(r.Context())
	require.True(t, ok, "expected request info in context")
	require.Equal(t, "192.0.2.1", ip)
	require.NotEmpty(t, userAgent)
}

// 默认不信任转发头：未认证调用方伪造 X-Forwarded-For 不得污染登录日志的来源 IP。
func TestBindRequestInfo_IgnoresSpoofedForwardedHeaders(t *testing.T) {
	svc := &Service{}
	r := req(t, http.MethodPost, "/api/core/auth/login")
	r.Header.Set("X-Forwarded-For", "6.6.6.6")
	r.Header.Set("X-Real-IP", "7.7.7.7")
	r = r.WithContext(svc.WithState(r.Context()))

	svc.BindRequestInfo(r)

	ip, _, ok := RequestInfoFrom(r.Context())
	require.True(t, ok, "expected request info in context")
	require.Equal(t, "192.0.2.1", ip, "spoofed forwarded headers must be ignored")
}

// 部署在可信反向代理之后（WithTrustProxyHeaders）时才采信转发头。
func TestBindRequestInfo_TrustsForwardedHeadersWhenEnabled(t *testing.T) {
	svc := New(context.Background(), WithTrustProxyHeaders(true))
	r := req(t, http.MethodPost, "/api/core/auth/login")
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 10.0.0.1")
	r = r.WithContext(svc.WithState(r.Context()))

	svc.BindRequestInfo(r)

	ip, _, ok := RequestInfoFrom(r.Context())
	require.True(t, ok, "expected request info in context")
	require.Equal(t, "10.0.0.1", ip, "forwarded header last hop should be honored")
}

func TestAuditBusinessOutcomePreservesHTTPFailure(t *testing.T) {
	svc := &Service{}
	for _, tc := range []struct {
		httpCode       int
		business, want string
	}{{200, "accepted", "accepted"}, {200, "failed", "failed"}, {403, "accepted", "failed"}} {
		r := req(t, http.MethodPost, "/api/core/cluster/snapshot")
		r = r.WithContext(svc.WithState(r.Context()))
		svc.Record(r.Context(), RecordInput{ResType: ResTypeCluster, BusinessType: BizReplace, Result: tc.business})
		row := svc.BuildAuditLog(r, ResponseMeta{StatusCode: tc.httpCode})
		require.Equal(t, tc.want, row.Result)
		require.EqualValues(t, tc.httpCode, row.Code)
	}
}

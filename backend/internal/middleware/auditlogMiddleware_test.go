// 审计中间件回归：关联头、异步任务结果，以及被拒绝/失败的敏感导出。
package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/domain/audit"
	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestAuditCorrelationHeaders(t *testing.T) {
	trace := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	for _, tc := range []struct{ requestID, trace, want string }{
		{"explicit-id", trace, "explicit-id"}, {"", trace, trace}, {"", "", ""},
	} {
		s := &audit.Service{}
		r := httptest.NewRequest(http.MethodPost, "/api/core/users", nil)
		r.Header.Set("X-Request-ID", tc.requestID)
		r.Header.Set("Traceparent", tc.trace)
		w := httptest.NewRecorder()
		NewAuditLogMiddleware(s).Handle(func(w http.ResponseWriter, r *http.Request) {
			item := s.BuildAuditLog(r, audit.ResponseMeta{StatusCode: 200})
			require.NotEmpty(t, item.RequestID)
			if tc.want != "" {
				require.Equal(t, tc.want, item.RequestID)
			}
			require.Equal(t, tc.trace, r.Header.Get("Traceparent"))
			require.Equal(t, item.RequestID, w.Header().Get("X-Request-ID"))
		})(w, r)
	}
}

func TestAuditFailedExportAndAsyncPersistence(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	s := audit.New(context.Background())
	for _, tc := range []struct {
		path, body, result string
		status             int
	}{
		{"/api/core/audit-logs/export", `{"code":403,"msg":"permission denied"}`, "failed", 403},
		{"/api/core/audit-logs/export", `{"code":400,"msg":"auditLog.exportLimit"}`, "failed", 400},
		{"/api/core/audit-logs/export", `{"code":500,"msg":"database unavailable"}`, "failed", 500},
		{"/api/core/uns/import-jobs", `{"code":200,"data":{"job":{"jobKey":"job_test","jobType":"uns.import","status":"pending"}}}`, "accepted", 200},
		{"/api/core/iam/users/import", `{"code":200,"data":{"jobKey":"job_flat"}}`, "accepted", 200},
		{"/api/core/iam/users/export-jobs", `{"code":200,"data":{"job":{"jobKey":"job_test","jobType":"iam.user.export","status":"running"}}}`, "accepted", 200},
	} {
		r := httptest.NewRequest(http.MethodPost, tc.path, nil)
		w := httptest.NewRecorder()
		NewAuditLogMiddleware(s).Handle(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		})(w, r)
		var event repo.OutboxEvent
		require.NoError(t, store.CommonDB().Where("event_type = ?", audit.OutboxEventType).Order("id DESC").First(&event).Error)
		var payload struct {
			Record repo.AuditLog `json:"record"`
		}
		require.NoError(t, json.Unmarshal(event.Payload, &payload))
		require.Equal(t, tc.result, payload.Record.Result)
		require.EqualValues(t, tc.status, payload.Record.Code)
		if tc.path == "/api/core/audit-logs/export" {
			require.Equal(t, "AuditLog", payload.Record.ResType)
			require.Equal(t, audit.BizExport, payload.Record.BusinessType)
		}
	}
}

func TestAuditAsyncResponseDetection(t *testing.T) {
	for _, body := range []string{`{"data":{"job":{"status":"pending"}}}`, `{"data":{"status":"pending"}}`, `{"data":`, `{"data":{"job":{"jobType":"uns.import","status":"pending"}}}`} {
		w := &auditResponseWriter{ResponseWriter: httptest.NewRecorder(), responseBody: []byte(body)}
		key, _ := w.AsyncJob()
		require.Empty(t, key)
	}
}

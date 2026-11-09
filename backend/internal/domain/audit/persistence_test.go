// 请求上下文取消仍持久化；消费重试不会重复落库，时间和作用域保持。
package audit

import (
	"backend/internal/repo"
	"backend/internal/testkit"
	"context"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuditDurablePersistence(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	s := New(context.Background())
	now := time.Now().UTC().Truncate(time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.persist(ctx, repo.AuditLog{UserID: 1, OperatorEmail: "private@example.com", ResType: "UNS", ResID: "42", ScopeType: 2, ScopeID: 8, Code: 202, BusinessType: "Import", DetailJSON: `{"after":{"path":"工厂/设备"}}`, CreatedTime: now})
	var event repo.OutboxEvent
	require.NoError(t, store.CommonDB().Where("event_type = ?", OutboxEventType).First(&event).Error)
	require.NotContains(t, string(event.Payload), "private@example.com")
	require.NoError(t, s.Consume(context.Background(), event))
	require.NoError(t, s.Consume(context.Background(), event))
	rows, total, err := repo.NewAuditRepo(context.Background()).ListAuditLogs(context.Background(), repo.AuditLogFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "accepted", rows[0].Result)
	require.Equal(t, now.UnixMilli(), rows[0].CreatedAt)
	require.EqualValues(t, 8, rows[0].ScopeID)
}

func TestAuditRequestMetadata(t *testing.T) {
	s := &Service{}
	r := httptest.NewRequest("POST", "/api/core/users?token=secret&name=keep", nil)
	r.RemoteAddr = "127.0.0.1:10000"
	r.Header.Set("X-Forwarded-For", "attacker")
	r.Header.Set("X-Request-ID", "req-123")
	r.Header.Set("User-Agent", "browser")
	r = r.WithContext(s.WithState(r.Context()))
	s.Record(r.Context(), RecordInput{ResType: "UNS", ResID: "1", BusinessType: BizUpdate, Detail: map[string]any{"after": map[string]any{"path": "设备", "password": "secret"}}})
	item := s.BuildAuditLog(r, ResponseMeta{StatusCode: 400, Status: "validation failed"})
	require.NotNil(t, item)
	require.Equal(t, "req-123", item.RequestID)
	require.Equal(t, "127.0.0.1", item.IP)
	require.Equal(t, "设备", item.ResourcePath)
	require.NotContains(t, item.DetailJSON, "secret")
	require.Equal(t, "validation failed", item.ResultMsg)
}

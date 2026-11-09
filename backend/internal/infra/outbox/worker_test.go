// 专用消费者仅领取自己的事件；清理只涉及超期已完成记录，重放仍保持审计幂等。
package outbox

import (
	"context"
	"testing"
	"time"

	"backend/internal/domain/audit"
	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestDedicatedWorkerAndCompletedRetention(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	s := audit.New(ctx)
	out := repo.NewOutboxRepo(ctx)
	old := time.Now().UTC().Add(-48 * time.Hour)
	for _, event := range []repo.OutboxEvent{
		{EventID: "done-old-audit", EventType: audit.OutboxEventType, Status: "done"},
		{EventID: "done-other", EventType: "business", Status: "done"},
		{EventID: "dead-audit", EventType: audit.OutboxEventType, Status: "dead"},
		{EventID: "pending-audit", EventType: audit.OutboxEventType, Status: "pending", NextRetryTime: time.Now().Add(time.Hour).UnixMilli()},
	} {
		event.Payload = []byte(`{}`)
		event.CreatedTime, event.UpdatedTime = old, old
		require.NoError(t, store.CommonDB().Create(&event).Error)
	}
	event := repo.OutboxEvent{EventID: "audit-replay", EventType: audit.OutboxEventType, Payload: []byte(`{"record":{"userId":1,"resType":"UNS","code":200},"createdTime":"2026-09-06T00:00:00Z"}`)}
	require.NoError(t, out.EnqueueOutbox(ctx, event))
	require.NoError(t, out.EnqueueOutbox(ctx, repo.OutboxEvent{EventID: "business-pending", EventType: "business"}))
	business := NewWorker(ctx)
	business.Register("business", func(context.Context, repo.OutboxEvent) error { return nil })
	count, err := business.drain(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	worker := NewWorker(ctx, WithCompletedRetention(24*time.Hour))
	worker.Register(audit.OutboxEventType, s.Consume)
	count, err = worker.drain(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	deleted, err := worker.pruneCompleted(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	var remaining []string
	require.NoError(t, store.CommonDB().Model(&repo.OutboxEvent{}).Order("event_id").Pluck("event_id", &remaining).Error)
	require.ElementsMatch(t, []string{"done-other", "dead-audit", "pending-audit", "audit-replay", "business-pending"}, remaining)
	// 清理窗口结束后再次收到同一个事件，目标表仍只产生一条审计记录。
	require.NoError(t, store.CommonDB().Model(&repo.OutboxEvent{}).Where("event_id = ?", event.EventID).Update("updated_time", old).Error)
	deleted, err = worker.pruneCompleted(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)
	require.NoError(t, out.EnqueueOutbox(ctx, event))
	_, err = worker.drain(ctx)
	require.NoError(t, err)
	var auditCount int64
	require.NoError(t, store.CommonDB().Model(&repo.AuditLog{}).Count(&auditCount).Error)
	require.EqualValues(t, 1, auditCount)
	deleted, err = business.pruneCompleted(ctx)
	require.NoError(t, err)
	require.Zero(t, deleted)
}

func TestOutboxCleanupBatchAndFailure(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	out := repo.NewOutboxRepo(ctx)
	require.NoError(t, store.CommonDB().Exec(`INSERT INTO sys_outbox_event(event_id,event_type,status,created_time,updated_time) SELECT 'old-'||n,'audit.record','done',now()-interval '2 days',now()-interval '2 days' FROM generate_series(1,1001) n`).Error)
	count, err := out.PruneCompletedOutbox(ctx, []string{audit.OutboxEventType}, time.Now().Add(-24*time.Hour), 1000)
	require.NoError(t, err)
	require.EqualValues(t, 1000, count)
	count, err = out.PruneCompletedOutbox(ctx, []string{audit.OutboxEventType}, time.Now().Add(-24*time.Hour), 1000)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = out.PruneCompletedOutbox(canceled, []string{audit.OutboxEventType}, time.Now(), 1000)
	require.Error(t, err)
	_, err = out.PruneCompletedOutbox(ctx, []string{audit.OutboxEventType}, time.Now(), 1001)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	count, err = out.PruneCompletedOutbox(ctx, nil, time.Now(), 1000)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestStorageBatchFairnessAndOrderedRetry(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	out := repo.NewStorageOutboxRepo(ctx)
	for _, id := range []string{"a1", "a2", "a3", "a4", "b1", "b2", "b3"} {
		require.NoError(t, out.EnqueueOutbox(ctx, repo.OutboxEvent{EventID: id, EventType: "storage", AggregateID: id[:1]}))
	}
	first, err := out.ClaimOutboxByTypes(ctx, "test", []string{"storage"}, 2, 60000)
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.Equal(t, "a", first[0].AggregateID)
	second, err := out.ClaimOutboxByTypes(ctx, "test", []string{"storage"}, 2, 60000)
	require.NoError(t, err)
	require.Len(t, second, 2)
	require.Equal(t, "b", second[0].AggregateID)
	// Later A events cannot overtake its failed head, even after leases release.
	for _, event := range first {
		require.NoError(t, out.MarkOutboxRetry(ctx, event.EventID, 1, time.Now().Add(time.Hour).UnixMilli(), "offline"))
	}
	for _, event := range second {
		require.NoError(t, out.MarkOutboxDone(ctx, event.EventID))
	}
	third, err := out.ClaimOutboxByTypes(ctx, "test", []string{"storage"}, 2, 60000)
	require.NoError(t, err)
	require.Len(t, third, 1)
	require.Equal(t, "b3", third[0].EventID)
	require.NoError(t, out.MarkOutboxDone(ctx, third[0].EventID))
	blocked, err := out.ClaimOutboxByTypes(ctx, "test", []string{"storage"}, 2, 60000)
	require.NoError(t, err)
	require.Empty(t, blocked)
}

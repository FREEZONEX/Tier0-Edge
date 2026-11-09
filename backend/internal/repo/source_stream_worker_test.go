package repo_test

// 验证真实 PostgreSQL 中的过期任务收口：已入队也会结束，已发出操作不冒充丢弃。
import (
	"context"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 确认连接恢复不伪造数据时间；旧配置、停用和删除不能被迟到回调修复。
func TestObserveSourceWatchConnectionIdentity(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	r := repo.NewSourceStreamRepo(store)
	source, err := r.CreateSource(ctx, repo.Source{Name: "watch", Protocol: "mqtt", Document: "{}"})
	require.NoError(t, err)
	require.NoError(t, r.ObserveSourceWatch(ctx, source.ID, source.ConfigRevision))
	source, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, item *repo.Source) error {
		item.DesiredState = "enabled"
		return nil
	})
	require.NoError(t, err)
	// Data time belongs to source_state, not the connection configuration.
	require.NoError(t, r.ObserveSourceData(ctx, source.ID, source.ConfigRevision, 456))
	source, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, item *repo.Source) error {
		item.Name, item.Health, item.HealthMessage = "renamed", "unhealthy", "SourceStream.protocolFailed"
		return nil
	})
	require.NoError(t, err)
	require.EqualValues(t, 456, source.LastDataAt, "real data observation seeded the timestamp")
	require.NoError(t, r.ObserveSourceWatch(ctx, source.ID, source.ConfigRevision), "metadata edits do not change the connection identity")
	source, err = r.Source(ctx, source.ID)
	require.NoError(t, err)
	require.Equal(t, "healthy", source.Health)
	require.Empty(t, source.HealthMessage)
	require.Positive(t, source.CheckedAt)
	require.EqualValues(t, 456, source.LastDataAt)
	old := source.ConfigRevision
	source, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, item *repo.Source) error {
		item.ConfigRevision++
		item.Health = "unhealthy"
		return nil
	})
	require.NoError(t, err)
	require.ErrorIs(t, r.ObserveSourceWatch(ctx, source.ID, old), repo.ErrConflict)
	require.NoError(t, r.ObserveSourceWatch(ctx, source.ID, source.ConfigRevision))
	source, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, item *repo.Source) error { item.DeletedAt = time.Now().UnixMilli(); return nil })
	require.NoError(t, err)
	require.ErrorIs(t, r.ObserveSourceWatch(ctx, source.ID, source.ConfigRevision), repo.ErrConflict)
	require.ErrorIs(t, r.ObserveSourceWatch(ctx, source.ID+1, source.ConfigRevision), repo.ErrConflict)
}

func TestExpireStreamTasks(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	r := repo.NewSourceStreamRepo(store)
	source, err := r.CreateSource(ctx, repo.Source{Name: "expiry test", Protocol: "mqtt", Document: "{}"})
	require.NoError(t, err)
	source, err = r.ChangeSource(ctx, source.ID, source.Revision, func(_ *gorm.DB, source *repo.Source) error { source.DesiredState = "enabled"; return nil })
	require.NoError(t, err)
	require.NoError(t, r.ObserveSource(ctx, source.ID, source.Revision, "healthy", "", nil))
	stream, err := r.CreateStream(ctx, repo.Stream{SourceID: source.ID, Name: "expiry stream", Model: "subscribe", Document: "{}"}, nil)
	require.NoError(t, err)
	stream, err = r.ActivateStream(ctx, stream.ID, stream.Revision, source.ConfigRevision, 0, "{}", nil)
	require.NoError(t, err)
	now := time.Now().UnixMilli()
	old := now - time.Hour.Milliseconds()
	for _, fixture := range []struct {
		id, status string
		at         int64
		sent       int64
	}{
		{"unsent-old", "pending", old, 0},
		{"sent-old", "pending", old, old},
		{"partial-old", "partial-failure", old, 0},
		{"fresh", "pending", now, 0},
		{"dispatched", "dispatching", old, old},
		{"unknown", "result-unknown", old, old},
		{"completed", "accepted", old, old},
	} {
		require.NoError(t, store.UnsDB().Create(&repo.StreamTask{ID: fixture.id, SourceID: source.ID, StreamID: stream.ID, VersionID: stream.ActiveVersion, SourceRevision: source.ConfigRevision, Model: "subscribe", Output: "{}", ReceivedAt: fixture.at, CreatedAt: fixture.at, Status: fixture.status, SentAt: fixture.sent}).Error)
		require.NoError(t, store.UnsDB().Create(&repo.StreamRun{ID: fixture.id, SourceID: source.ID, StreamID: stream.ID, VersionID: stream.ActiveVersion, Model: "subscribe", Status: fixture.status, MessageID: fixture.id, Summary: "{}", CreatedAt: fixture.at}).Error)
	}
	_, err = r.ExpireStreamTasks(ctx, 0)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	count, err := r.ExpireStreamTasks(ctx, now-time.Minute.Milliseconds())
	require.NoError(t, err)
	require.EqualValues(t, 3, count)
	for _, id := range []string{"unsent-old", "sent-old", "partial-old"} {
		task, err := r.Task(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "dropped", task.Status)
		var run repo.StreamRun
		require.NoError(t, store.UnsDB().Where("id=?", id).Take(&run).Error)
		require.Equal(t, "dropped", run.Status)
		require.Contains(t, run.Summary, "messageExpired")
	}
	for id, status := range map[string]string{"fresh": "pending", "dispatched": "dispatching", "unknown": "result-unknown", "completed": "accepted"} {
		task, err := r.Task(ctx, id)
		require.NoError(t, err)
		require.Equal(t, status, task.Status)
	}
	count, err = r.ExpireStreamTasks(ctx, now-time.Minute.Milliseconds())
	require.NoError(t, err)
	require.Zero(t, count, "expiry is idempotent")
	pending, err := r.PendingDelivery(ctx)
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, "fresh", pending[0].ID)
	// Invalidation must retain completed/dispatched states and prior receipts.
	require.NoError(t, store.UnsDB().Model(&repo.StreamRun{}).Where("id=?", "fresh").Update("summary", `{"targets":[{"topicId":1,"status":"succeeded"}]}`).Error)
	for id, want := range map[string]string{"fresh": "dropped", "dispatched": "dispatching", "unknown": "result-unknown", "completed": "accepted", "unsent-old": "dropped"} {
		run := repo.StreamRun{ID: id, SourceID: source.ID, StreamID: stream.ID, VersionID: stream.ActiveVersion, Model: "subscribe", Status: "dropped", MessageID: id, Summary: `{"phase":"reference-validation","reason":"inactiveReference"}`, CreatedAt: now}
		require.NoError(t, r.DiscardQueuedExecution(ctx, run))
		require.NoError(t, r.DiscardQueuedExecution(ctx, run))
		task, err := r.Task(ctx, id)
		require.NoError(t, err)
		require.Equal(t, want, task.Status)
		var recorded repo.StreamRun
		require.NoError(t, store.UnsDB().Where("id=?", id).Take(&recorded).Error)
		require.Equal(t, want, recorded.Status)
		if id == "fresh" {
			require.Contains(t, recorded.Summary, "inactiveReference")
			require.Contains(t, recorded.Summary, "targets")
		}
	}
	neverExecuted := repo.StreamRun{ID: "invalidated-before-transform", SourceID: source.ID, StreamID: stream.ID, VersionID: stream.ActiveVersion, Model: "publish", Status: "dropped", MessageID: "before-transform", Summary: `{"reason":"inactiveReference"}`, CreatedAt: now}
	require.NoError(t, r.DiscardQueuedExecution(ctx, neverExecuted))
	require.NoError(t, r.DiscardQueuedExecution(ctx, neverExecuted))
	var recorded repo.StreamRun
	require.NoError(t, store.UnsDB().Where("id=?", neverExecuted.ID).Take(&recorded).Error)
	require.Equal(t, "dropped", recorded.Status)
	neverExecuted.Status = "succeeded"
	require.ErrorIs(t, r.DiscardQueuedExecution(ctx, neverExecuted), repo.ErrInvalidArgument)
}

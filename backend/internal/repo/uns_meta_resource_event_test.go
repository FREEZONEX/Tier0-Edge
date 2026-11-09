package repo_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 真实 PG 验证提交后通知、最外层回滚、savepoint 回滚与通知失败不回滚数据。
func TestMetadataDirectEventsCommitRollbackAndPublishFailure(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	db := store.UnsDB()
	ctx := context.Background()
	collectionID, topicID := uuid.NewString(), uuid.NewString()
	now := time.Now().UnixMilli()
	require.NoError(t, db.Create(&repo.UnsMeta{Kind: "collection", ID: collectionID, Data: json.RawMessage(`{"name":"Direct events"}`), Revision: 1, CreatedAt: now, UpdatedAt: now}).Error)
	doc, _ := json.Marshal(map[string]any{"name": "Events", "viewMode": "records", "collectionId": collectionID})
	require.NoError(t, db.Create(&repo.UnsMeta{ID: topicID, Kind: "topic", Data: doc, Revision: 1, CreatedAt: now, UpdatedAt: now}).Error)
	event := repo.MetadataResourceEvent{MessageID: "original", CollectionID: collectionID, RecordID: "pump-1", ReceivedAt: now, Data: map[string]any{"value": 4}, Change: "record"}
	inside, offline := false, false
	var ids []string
	require.NoError(t, repo.ConfigureResourceEventPublisher(db, func(_ context.Context, subject, id string, raw []byte) error {
		require.False(t, inside, "must not publish while transaction body is running")
		var count int64
		require.NoError(t, db.Model(&repo.UnsRecord{}).Where("collection_id=?", collectionID).Count(&count).Error)
		require.EqualValues(t, 1, count, "a separate connection observes committed data")
		require.Contains(t, subject, "tier0.events.topic.meta-")
		ids = append(ids, id)
		if offline {
			return errors.New("NATS unavailable")
		}
		return nil
	}))
	abort := errors.New("rollback")
	require.ErrorIs(t, repo.MetadataEventTransaction(ctx, db, func(tx *gorm.DB) error {
		require.NoError(t, repo.EnqueueMetadataResourceEvents(ctx, tx, event))
		return abort
	}), abort)
	require.Empty(t, ids)
	require.NoError(t, repo.MetadataEventTransaction(ctx, db, func(tx *gorm.DB) error {
		inside = true
		defer func() { inside = false }()
		require.NoError(t, tx.Create(&repo.UnsRecord{CollectionID: collectionID, ID: "pump-1", Data: json.RawMessage(`{"value":4}`), Revision: 1, CreatedAt: now, UpdatedAt: now}).Error)
		require.ErrorIs(t, repo.NewUnsMetaRepo(tx).Transaction(ctx, func(child *gorm.DB) error {
			e := event
			e.MessageID = "rolled-back-child"
			require.NoError(t, repo.EnqueueMetadataResourceEvents(ctx, child, e))
			return abort
		}), abort)
		require.Empty(t, ids)
		return repo.EnqueueMetadataResourceEvents(ctx, tx, event)
	}))
	require.Len(t, ids, 2, "Collection and records Topic each receive a direct event")
	offline = true
	require.NoError(t, repo.MetadataEventTransaction(ctx, db, func(tx *gorm.DB) error {
		if err := tx.Model(&repo.UnsRecord{}).Where("collection_id=? AND id='pump-1'", collectionID).Update("revision", 2).Error; err != nil {
			return err
		}
		event.MessageID = "second"
		return repo.EnqueueMetadataResourceEvents(ctx, tx, event)
	}), "notification failure is not a failed business write")
	require.Len(t, ids, 4)
	var row repo.UnsRecord
	require.NoError(t, db.Where("collection_id=?", collectionID).Take(&row).Error)
	require.EqualValues(t, 2, row.Revision)
	for _, table := range []string{"public.uns_metadata_event_outbox", "public.stream_event_outbox"} {
		var count int64
		require.NoError(t, db.Table(table).Count(&count).Error)
		require.Zero(t, count)
	}
	require.ErrorIs(t, db.Transaction(func(tx *gorm.DB) error {
		return repo.NewUnsMetaRepo(tx).Transaction(ctx, func(child *gorm.DB) error { return repo.EnqueueMetadataResourceEvents(ctx, child, event) })
	}), repo.ErrInvalidArgument, "an unobservable outer commit cannot publish prematurely")
	require.Len(t, ids, 4, "there is no pending event to flush after NATS recovers")
	var restartedIDs []string
	require.NoError(t, repo.ConfigureResourceEventPublisher(db.Session(&gorm.Session{}), func(_ context.Context, _ string, id string, _ []byte) error {
		restartedIDs = append(restartedIDs, id)
		return nil
	}))
	event.MessageID = "after-worker-restart"
	require.NoError(t, repo.MetadataEventTransaction(ctx, db, func(tx *gorm.DB) error {
		return repo.EnqueueMetadataResourceEvents(ctx, tx, event)
	}))
	require.Len(t, restartedIDs, 2, "existing sessions publish through the replacement transport")
	require.Len(t, ids, 4, "a restarted Worker must not keep publishing through its closed predecessor")
}

// 遗留表仅作为迁移历史保留，不再由业务发送器读写；历史及索引不被破坏。
func TestRetiredMetadataEventOutboxHistoryPreserved(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	db := store.UnsDB()
	for first := 1; first <= 20003; first += 100 {
		last := first + 99
		if last > 20003 {
			last = 20003
		}
		require.NoError(t, db.Exec(`INSERT INTO public.uns_metadata_event_outbox(id,data,revision,created_at,updated_at)
		SELECT 'outbox-index-'||n,
		jsonb_build_object('type','metadataEventOutbox','identity','metadataEventOutbox:outbox-index-'||n,'payload',jsonb_build_object('status',CASE WHEN n>20000 THEN 'pending' ELSE 'sent' END,'event',repeat('historical-payload',64))),
		1,n,n FROM generate_series(?::int,?::int) n`, first, last).Error)
	}
	require.NoError(t, db.Exec(`ANALYZE public.uns_metadata_event_outbox`).Error)
	var plan string
	require.NoError(t, db.Raw(`EXPLAIN (FORMAT JSON) SELECT * FROM public.uns_metadata_event_outbox
		WHERE data->'payload'->>'status'='pending' ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&plan).Error)
	require.Contains(t, plan, "idx_uns_metadata_outbox_pending", "normal planner must skip completed payloads without forcing an index")
	var total, pending int64
	require.NoError(t, db.Table("public.uns_metadata_event_outbox").Where("id LIKE 'outbox-index-%'").Count(&total).Error)
	require.NoError(t, db.Table("public.uns_metadata_event_outbox").Where("id LIKE 'outbox-index-%' AND data->'payload'->>'status'='pending'").Count(&pending).Error)
	require.EqualValues(t, 20003, total)
	require.EqualValues(t, 3, pending)
	var definition string
	require.NoError(t, db.Raw(`SELECT indexdef FROM pg_indexes WHERE schemaname='public' AND indexname='idx_uns_metadata_outbox_pending'`).Scan(&definition).Error)
	require.Contains(t, definition, "created_at, id")
	require.Contains(t, definition, "pending")
	t.Log("normal planner selected pending-only index with 20000 completed and 3 pending rows; history preserved")
}

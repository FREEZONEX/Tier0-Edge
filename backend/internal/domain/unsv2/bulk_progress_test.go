package unsv2

import (
	"backend/internal/repo"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	assert "github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"sync/atomic"
	"testing"
)

// A real commit whose acknowledgement is lost: neither a rollback fixture nor
// an error before SQL execution can exercise the unknown-outcome boundary.
type bulkCommitPool struct {
	*sql.DB
	commits atomic.Int32
	failAt  int32
}

func (p *bulkCommitPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := p.DB.BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &bulkCommitTx{Tx: tx, pool: p}, nil
}

type bulkCommitTx struct {
	*sql.Tx
	pool *bulkCommitPool
}

func (t *bulkCommitTx) Commit() error {
	if err := t.Tx.Commit(); err != nil {
		return err
	}
	if t.pool.commits.Add(1) == t.pool.failAt {
		return errors.New("test lost commit acknowledgement")
	}
	return nil
}
func loseBulkCommit(t *testing.T, ctx context.Context, store *repo.Store, after int32) context.Context {
	t.Helper()
	sqlDB, err := store.UnsDB().DB()
	assert.NoError(t, err)
	db := store.UnsDB().Session(&gorm.Session{NewDB: true})
	config := *db.Config
	db.Config = &config
	pool := &bulkCommitPool{DB: sqlDB, failAt: after}
	db.Config.ConnPool = pool
	db.Statement = &gorm.Statement{DB: db, ConnPool: pool, Context: ctx}
	return repo.WithUnsTransaction(ctx, db)
}

func TestRecordBulkProgressSurvivesUnknownCommit(t *testing.T) {
	store, s, ctx := batchReadFixture(t)
	ctx = loseBulkCommit(t, ctx, store, 3) // root intent, first item, second item acknowledgement
	raw := []byte(`{"collectionId":"data","atomic":false,"ordered":false,"idempotencyKey":"bulk-unknown","operations":[{"insertOne":{"id":"a","data":{"value":1}}},{"insertOne":{"id":"b","data":{"value":2}}},{"insertOne":{"id":"c","data":{"value":3}}}]}`)
	_, err := s.Execute(ctx, "recordBulkWrite", raw)
	var fault *Fault
	assert.ErrorAs(t, err, &fault)
	assert.False(t, fault.RolledBack)
	progress := fault.Data.(map[string]any)
	assert.EqualValues(t, 1, progress["insertedCount"])
	assert.EqualValues(t, 1, progress["nextIndex"])
	assert.EqualValues(t, 2, progress["unknownUntil"])
	assert.NotContains(t, progress, "resumeAfter")
	assert.Empty(t, progress["writeErrors"])
	var count int64
	assert.NoError(t, store.UnsDB().Model(&repo.UnsRecord{}).Where("id IN ('a','b','c')").Count(&count).Error)
	assert.EqualValues(t, 2, count)
	replay, err := s.Execute(ctx, "recordBulkWrite", raw)
	assert.NoError(t, err)
	assert.EqualValues(t, 3, replay.(map[string]any)["insertedCount"])
	assert.NoError(t, store.UnsDB().Model(&repo.UnsRecord{}).Where("id IN ('a','b','c')").Count(&count).Error)
	assert.EqualValues(t, 3, count)
}

func TestMetadataBulkProgressSurvivesUnknownCommit(t *testing.T) {
	store, s, ctx := batchReadFixture(t)
	ctx = loseBulkCommit(t, ctx, store, 3)
	raw := []byte(`{"kind":"directory","atomic":false,"ordered":false,"idempotencyKey":"meta-unknown","operations":[{"insertOne":{"id":"a","data":{"name":"A"}}},{"insertOne":{"id":"b","data":{"name":"B"}}},{"insertOne":{"id":"c","data":{"name":"C"}}}]}`)
	_, err := s.Execute(ctx, "metadataBulkWrite", raw)
	var fault *Fault
	assert.ErrorAs(t, err, &fault)
	assert.False(t, fault.RolledBack)
	progress := fault.Data.(map[string]any)
	assert.EqualValues(t, 1, progress["insertedCount"])
	assert.EqualValues(t, 1, progress["nextIndex"])
	assert.EqualValues(t, 2, progress["unknownUntil"])
	var count int64
	assert.NoError(t, store.UnsDB().Model(&repo.UnsMeta{}).Where("kind='directory' AND id IN ('a','b','c')").Count(&count).Error)
	assert.EqualValues(t, 2, count)
	replay, err := s.Execute(ctx, "metadataBulkWrite", raw)
	assert.NoError(t, err)
	assert.EqualValues(t, 3, replay.(map[string]any)["insertedCount"])
	assert.NoError(t, store.UnsDB().Model(&repo.UnsMeta{}).Where("kind='directory' AND id IN ('a','b','c')").Count(&count).Error)
	assert.EqualValues(t, 3, count)
}

func TestBulkOrderedProgressAndFilterUnknown(t *testing.T) {
	_, s, ctx := batchReadFixture(t)
	raw := []byte(`{"collectionId":"data","atomic":false,"ordered":true,"operations":[{"insertOne":{"id":"a","data":{"value":1}}},{"insertOne":{"id":"a","data":{"value":2}}},{"insertOne":{"id":"c","data":{"value":3}}}]}`)
	result, err := s.Execute(ctx, "recordBulkWrite", raw)
	assert.NoError(t, err)
	progress := result.(map[string]any)
	assert.EqualValues(t, 2, progress["nextIndex"])
	assert.NotContains(t, progress, "unknownUntil")
	assert.NotContains(t, progress, "resumeAfter")
	meta := []byte(`{"kind":"directory","atomic":false,"ordered":true,"operations":[{"insertOne":{"id":"a","data":{"name":"A"}}},{"insertOne":{"id":"a","data":{"name":"B"}}},{"insertOne":{"id":"c","data":{"name":"C"}}}]}`)
	result, err = s.Execute(ctx, "metadataBulkWrite", meta)
	assert.NoError(t, err)
	assert.EqualValues(t, 2, result.(map[string]any)["nextIndex"])
	input := map[string]any{"status": "partiallyCommitted", "nextIndex": 1, "unknownUntil": 2, "resumeAfter": "a", "hasMore": true}
	result = recordBatchResponse("updateMany", input)
	assert.Equal(t, true, result.(map[string]any)["outcomeUnknown"])
	assert.NotContains(t, result, "resumeAfter")
	assert.NotContains(t, result, "nextIndex")
	// Returned error progress remains JSON serializable and does not enter item faults.
	encoded, err := json.Marshal(recordBatchFault("bulkWrite", &Fault{Code: "TEMPORARILY_UNAVAILABLE", Data: input}))
	assert.NoError(t, err)
	assert.NotContains(t, string(encoded), "nextIndex")
}

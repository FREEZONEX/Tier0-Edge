package unsv2

import (
	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/testkit"
	"context"
	"encoding/json"
	"fmt"
	assert "github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"strings"
	"sync/atomic"
	"testing"
)

func batchReadFixture(t *testing.T) (*repo.Store, *Service, context.Context) {
	nativeEdgeTestProduct(t)
	store, _ := testkit.NewTestDB(t)
	assert.NoError(t, repo.ConfigureResourceEventPublisher(store.UnsDB(), func(context.Context, string, string, []byte) error { return nil }))
	ctx := graphContext("uns.read", "uns.write")
	m := unsmeta.New()
	_, err := m.Save(ctx, "data", "collection", 0, unsmeta.Document{Name: "Data", RecordSchema: &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}, "group": {Type: "string"}}}})
	assert.NoError(t, err)
	_, err = m.SaveRecord(ctx, "data", "first", 0, map[string]any{"value": 2, "group": "A"})
	assert.NoError(t, err)
	return store, New(m, func(_ context.Context, actor contextx.Subject) (contextx.Subject, error) { return actor, nil }), ctx
}

func TestRecordQueryBatchMixedResultsAndBudgets(t *testing.T) {
	store, s, ctx := batchReadFixture(t)
	raw := []byte(`{"queries":[{"id":"get","op":"get","collectionId":"data","ids":["missing","first"]},{"id":"count","op":"count","collectionId":"data"},{"id":"distinct","op":"distinct","collectionId":"data","field":"group"},{"id":"bad","op":"update","collectionId":"data"},{"id":"find","op":"find","collectionId":"data"}]}`)
	out, err := s.Execute(ctx, "recordQueryBatch", raw)
	assert.NoError(t, err)
	results := out.(map[string]any)["results"].(map[string]any)
	get := results["get"].(map[string]any)
	assert.Equal(t, true, get["ok"])
	assert.Equal(t, []string{"missing"}, get["data"].(map[string]any)["missing"])
	assert.EqualValues(t, 1, results["count"].(map[string]any)["data"].(map[string]any)["count"])
	assert.Equal(t, false, results["bad"].(map[string]any)["ok"])
	assert.Equal(t, true, results["find"].(map[string]any)["ok"])
	_, err = s.Execute(ctx, "recordQueryBatch", []byte(`{"queries":[{"id":"same","op":"count","collectionId":"data"},{"id":"same","op":"count","collectionId":"data"}]}`))
	assert.Error(t, err)
	queries := []map[string]any{}
	for i := 0; i < 11; i++ {
		queries = append(queries, map[string]any{"id": fmt.Sprint(i), "op": "find", "collectionId": "data", "limit": 200})
	}
	request, _ := json.Marshal(map[string]any{"queries": queries})
	_, err = s.Execute(ctx, "recordQueryBatch", request)
	var fault *Fault
	assert.ErrorAs(t, err, &fault)
	assert.Equal(t, "RESPONSE_TOO_LARGE", fault.Code)
	// Center-only reads cannot claim the local transaction snapshot.
	assert.NoError(t, store.UnsDB().Create(&repo.GlobalStoragePolicy{ID: "edge", Mode: "center", CenterID: "center", Revision: 1}).Error)
	out, err = s.Execute(ctx, "recordQueryBatch", []byte(`{"queries":[{"id":"remote","op":"count","collectionId":"data"}]}`))
	assert.NoError(t, err)
	item := out.(map[string]any)["results"].(map[string]any)["remote"].(map[string]any)
	assert.Equal(t, false, item["ok"])
	assert.Equal(t, "UNSUPPORTED_OPERATION", item["error"].(*Fault).Code)
}

func TestRecordQueryBatchKeepsOneSnapshot(t *testing.T) {
	store, s, ctx := batchReadFixture(t)
	sqlDB, err := store.UnsDB().DB()
	assert.NoError(t, err)
	var reads atomic.Int32
	inserted := false
	// Run between the two reads, after get has drained and closed its SQL rows.
	// Calling the whole write domain from an After(row) hook leaves the reader
	// open while waiting on another transaction; it is not a safe interleaving.
	assert.NoError(t, store.UnsDB().Callback().Row().Before("gorm:row").Register("test:concurrent_record", func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "public.uns_record") && reads.Add(1) == 2 {
			_, err := sqlDB.ExecContext(tx.Statement.Context, `INSERT INTO public.uns_record(collection_id,id,data,revision,created_at,updated_at) VALUES('data','later','{"value":3}',1,1,1)`)
			assert.NoError(t, err)
			inserted = true
		}
	}))
	t.Cleanup(func() { store.UnsDB().Callback().Row().Remove("test:concurrent_record") })
	out, err := s.Execute(ctx, "recordQueryBatch", []byte(`{"queries":[{"id":"first","op":"get","collectionId":"data","ids":["first"]},{"id":"second","op":"count","collectionId":"data"}]}`))
	assert.NoError(t, err)
	assert.True(t, inserted)
	results := out.(map[string]any)["results"].(map[string]any)
	assert.EqualValues(t, 1, results["second"].(map[string]any)["data"].(map[string]any)["count"])
	var count int64
	assert.NoError(t, store.UnsDB().Model(&repo.UnsRecord{}).Where("collection_id='data'").Count(&count).Error)
	assert.EqualValues(t, 2, count)
}

func TestRecordQueryBatchTotalByteLimit(t *testing.T) {
	store, s, ctx := batchReadFixture(t)
	data, _ := json.Marshal(map[string]any{"payload": strings.Repeat("x", 70000)})
	records := []repo.UnsRecord{}
	ids := []string{}
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("record-%d", i)
		ids = append(ids, id)
		records = append(records, repo.UnsRecord{CollectionID: "data", ID: id, Revision: 1, Data: data})
	}
	assert.NoError(t, store.UnsDB().CreateInBatches(records, 20).Error)
	body, _ := json.Marshal(map[string]any{"queries": []any{map[string]any{"id": "a", "op": "get", "collectionId": "data", "ids": ids}, map[string]any{"id": "b", "op": "get", "collectionId": "data", "ids": ids}}})
	_, err := s.Execute(ctx, "recordQueryBatch", body)
	var fault *Fault
	assert.ErrorAs(t, err, &fault)
	assert.Equal(t, "RESPONSE_TOO_LARGE", fault.Code)
}

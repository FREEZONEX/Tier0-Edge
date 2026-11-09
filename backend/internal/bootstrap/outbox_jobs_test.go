// 审计消费者必须独立装配，业务消费者不领取审计事件。
package bootstrap

import (
	"backend/internal/config"
	"backend/internal/domain/asset"
	"backend/internal/domain/dataingest"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"backend/internal/domain/audit"
	"backend/internal/domain/uns"
	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestAuditOutboxWorkerIsolation(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	app := &App{Audit: audit.New(ctx)}
	out := repo.NewOutboxRepo(ctx)
	require.NoError(t, out.EnqueueOutbox(ctx, repo.OutboxEvent{EventID: "audit-first", EventType: audit.OutboxEventType, Payload: []byte(`{"record":{"userId":1,"resType":"UNS","code":200},"createdTime":"2026-09-06T00:00:00Z"}`)}))
	require.NoError(t, out.EnqueueOutbox(ctx, repo.OutboxEvent{EventID: "business-second", EventType: "test.business"}))
	shared := newOutboxWorker(app)
	processed := make(chan struct{}, 1)
	shared.Register("test.business", func(context.Context, repo.OutboxEvent) error { processed <- struct{}{}; return nil })
	shared.Start(ctx)
	defer shared.Stop()
	select {
	case <-processed:
	case <-time.After(10 * time.Second):
		t.Fatal("business event not processed")
	}
	var event repo.OutboxEvent
	require.NoError(t, store.CommonDB().Where("event_id = ?", "audit-first").First(&event).Error)
	require.Equal(t, "pending", event.Status)
	require.Empty(t, event.LockedBy)
	dedicated := newAuditOutboxWorker(app)
	dedicated.Start(ctx)
	defer dedicated.Stop()
	require.Eventually(t, func() bool {
		var count int64
		err := store.CommonDB().Model(&repo.AuditLog{}).Where("event_id = ?", "audit-first").Count(&count).Error
		return err == nil && count == 1
	}, 10*time.Second, 20*time.Millisecond)
}

func TestUnsImportWorker_DetachedAndRetry(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	files, err := asset.New(ctx, asset.Config{Driver: "local", FileRoot: t.TempDir(), PublicBaseURL: "http://localhost"})
	require.NoError(t, err)
	app := &App{Asset: files, UNS: uns.New(ctx, nil)}
	raw := []byte(`{"namespace":[{"name":"background","type":"PATH","alias":"background_alias"}]}`)
	uploaded, err := files.UploadContent(ctx, asset.UploadContentCommand{FileName: "nodes.json", ContentType: "application/json", Size: int64(len(raw)), Reader: bytes.NewReader(raw), UserID: 1})
	require.NoError(t, err)
	requestCtx, cancel := context.WithCancel(ctx)
	accepted, err := app.UNS.CreateImportJob(requestCtx, false, anyJobInt64(uploaded["fileId"]), 1)
	require.NoError(t, err)
	job := accepted["job"].(repo.AsyncJob)
	cancel() // The browser request lifetime must not own worker execution.
	var event repo.OutboxEvent
	require.NoError(t, store.CommonDB().Where("aggregate_id = ?", job.JobKey).Take(&event).Error)
	// A temporary dependency outage returns an error for outbox retry.
	unavailable := &App{UNS: app.UNS}
	require.Error(t, unavailable.handleUnsImportJob(ctx, event))
	worker := newOutboxWorker(app)
	worker.Start(ctx)
	defer worker.Stop()
	require.Eventually(t, func() bool {
		current, err := repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, job.JobKey)
		return err == nil && current.Status == repo.AsyncJobStatusSucceeded
	}, 10*time.Second, 20*time.Millisecond)
	require.NoError(t, app.handleUnsImportJob(ctx, event)) // terminal redelivery is a no-op
	var count int64
	require.NoError(t, store.CommonDB().Model(&repo.UnsNode{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	var audits []repo.AuditLog
	require.NoError(t, store.CommonDB().Where("res_id = ?", job.JobKey).Find(&audits).Error)
	require.Len(t, audits, 1)
	require.Equal(t, "success", audits[0].Result)
	require.Contains(t, audits[0].DetailJSON, `"imported":1`)
	// Invalid payload produces an explicit failed job and no extra nodes.
	raw = []byte(`{"namespace":[{"name":"partial","type":"PATH"},{"name":"conflict","type":"PATH","alias":"background_alias"}]}`)
	uploaded, err = files.UploadContent(ctx, asset.UploadContentCommand{FileName: "invalid.json", ContentType: "application/json", Size: int64(len(raw)), Reader: bytes.NewReader(raw), UserID: 1})
	require.NoError(t, err)
	accepted, err = app.UNS.CreateImportJob(ctx, false, anyJobInt64(uploaded["fileId"]), 1)
	require.NoError(t, err)
	failed := accepted["job"].(repo.AsyncJob)
	require.Eventually(t, func() bool {
		current, err := repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, failed.JobKey)
		return err == nil && current.Status == repo.AsyncJobStatusFailed && current.ErrorMessage != ""
	}, 10*time.Second, 20*time.Millisecond)
	require.NoError(t, store.CommonDB().Model(&repo.UnsNode{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestUnsImportJob_QueueFailureIsAtomic(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	require.NoError(t, store.CommonDB().Exec(`DROP TABLE sys_outbox_event`).Error)
	_, err := uns.New(ctx, nil).CreateImportJob(ctx, false, 123, 1)
	require.Error(t, err)
	var count int64
	require.NoError(t, store.CommonDB().Model(&repo.AsyncJob{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestUnsImportWorker_FinalAttemptFailsJob(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	accepted, err := uns.New(ctx, nil).CreateImportJob(ctx, false, 123, 1)
	require.NoError(t, err)
	job := accepted["job"].(repo.AsyncJob)
	var event repo.OutboxEvent
	require.NoError(t, store.CommonDB().Where("aggregate_id = ?", job.JobKey).Take(&event).Error)
	event.Attempts = 4
	require.NoError(t, (&App{}).handleUnsImportJob(ctx, event))
	job, err = repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, job.JobKey)
	require.NoError(t, err)
	require.Equal(t, repo.AsyncJobStatusFailed, job.Status)
	require.Contains(t, job.ErrorMessage, "dependencies")
	var audits []repo.AuditLog
	require.NoError(t, store.CommonDB().Where("res_id = ?", job.JobKey).Find(&audits).Error)
	require.Len(t, audits, 1)
	require.Equal(t, "failed", audits[0].Result)
	require.Contains(t, audits[0].DetailJSON, `"sourceFileId":123`)

}

func TestUnsImportHistory_RealDDLAndRetry(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	ctx := context.Background()
	ingest, err := dataingest.New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	defer ingest.Close()
	files, err := asset.New(ctx, asset.Config{Driver: "local", FileRoot: t.TempDir(), PublicBaseURL: "http://localhost"})
	require.NoError(t, err)
	app := &App{Asset: files, UNS: uns.New(ctx, ingest), DataIngest: ingest}
	nodes := make([]map[string]any, 0, 1252)
	for i := 0; i < 1251; i++ {
		topicType := []string{"METRIC", "STATE", "ACTION"}[i%3]
		nodes = append(nodes, map[string]any{"name": fmt.Sprintf("history%d", i), "alias": fmt.Sprintf("history_%d", i), "type": "TOPIC", "topicType": topicType, "enableHistory": "true", "fields": []map[string]any{{"name": "v", "type": "DOUBLE"}}})
	}
	nodes = append(nodes, map[string]any{"name": "no_history", "alias": "no_history", "type": "TOPIC", "topicType": "METRIC", "enableHistory": "false"})
	raw, err := json.Marshal(map[string]any{"namespace": nodes})
	require.NoError(t, err)
	uploaded, err := files.UploadContent(ctx, asset.UploadContentCommand{FileName: "history.json", ContentType: "application/json", Size: int64(len(raw)), Reader: bytes.NewReader(raw), UserID: 1})
	require.NoError(t, err)
	accepted, err := app.UNS.CreateImportJob(ctx, false, anyJobInt64(uploaded["fileId"]), 1)
	require.NoError(t, err)
	key := accepted["job"].(repo.AsyncJob).JobKey
	shared := newOutboxWorker(app)
	started := time.Now()
	shared.Start(ctx)
	defer shared.Stop()
	require.Eventually(t, func() bool {
		var event repo.OutboxEvent
		return store.CommonDB().Where("aggregate_id = ? AND event_type = ?", key, "uns.import.job").Take(&event).Error == nil && event.Status == "done"
	}, 15*time.Second, 20*time.Millisecond)
	job, err := repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, key)
	require.NoError(t, err)
	require.Equal(t, repo.AsyncJobStatusRunning, job.Status)
	require.EqualValues(t, 70, job.Progress)
	var batches []repo.OutboxEvent
	require.NoError(t, store.CommonDB().Where("event_type = ?", unsPhysicalEnsureEventType).Order("id").Find(&batches).Error)
	require.Len(t, batches, 13)
	for _, event := range batches {
		require.Equal(t, "pending", event.Status) // shared worker must never claim DDL
		var req unsPhysicalEnsureRequest
		require.NoError(t, json.Unmarshal(event.Payload, &req))
		require.LessOrEqual(t, len(req.Nodes), 100)
		require.Equal(t, key, req.ImportJobKey)
	}
	metadataDuration := time.Since(started)
	// Force a real view/table conflict, then recover the same batch without reimporting metadata.
	require.NoError(t, store.CommonDB().Exec(`CREATE TABLE public.history_0 (blocked int)`).Error)
	require.Error(t, app.handleUnsPhysicalEnsure(ctx, batches[0]))
	job, err = repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, key)
	require.NoError(t, err)
	require.Equal(t, repo.AsyncJobStatusRunning, job.Status)
	require.EqualValues(t, 70, job.Progress)
	require.NoError(t, store.CommonDB().Exec(`DROP TABLE public.history_0`).Error)
	require.NoError(t, app.handleUnsPhysicalEnsure(ctx, batches[0]))
	require.NoError(t, app.handleUnsPhysicalEnsure(ctx, batches[0])) // DDL and completion checkpoint are idempotent
	job, err = repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, key)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal(job.ResultJSON, &result))
	require.Len(t, result["historyCompletedBatches"], 1)
	// Simulate redelivery of the metadata job while DDL is still pending.
	var importEvent repo.OutboxEvent
	require.NoError(t, store.CommonDB().Where("aggregate_id = ? AND event_type = ?", key, "uns.import.job").Take(&importEvent).Error)
	require.NoError(t, app.handleUnsImportJob(ctx, importEvent))
	physical := newUnsPhysicalOutboxWorker(app)
	physical.Start(ctx)
	defer physical.Stop()
	require.Eventually(t, func() bool {
		current, err := repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, key)
		return err == nil && current.Status == repo.AsyncJobStatusSucceeded
	}, 90*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool {
		var count int64
		return store.CommonDB().Model(&repo.OutboxEvent{}).Where("event_type = ? AND status <> 'done'", unsPhysicalEnsureEventType).Count(&count).Error == nil && count == 0
	}, 5*time.Second, 20*time.Millisecond)
	t.Logf("1251 history topics: metadata=%s total including real DDL and injected retry=%s", metadataDuration, time.Since(started))
	var views, tables, disabled int64
	require.NoError(t, store.CommonDB().Raw(`SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname ~ '^history_[0-9]+$' AND c.relkind='v'`).Scan(&views).Error)
	require.NoError(t, store.CommonDB().Raw(`SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname ~ '^history_[0-9]+$' AND c.relkind='r'`).Scan(&tables).Error)
	require.EqualValues(t, 417, views)
	require.EqualValues(t, 834, tables)
	require.NoError(t, store.CommonDB().Raw(`SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname='no_history'`).Scan(&disabled).Error)
	require.Zero(t, disabled)
	var metric repo.UnsNode
	require.NoError(t, store.CommonDB().Where("alias = ?", "history_0").Take(&metric).Error)
	require.NoError(t, store.CommonDB().Exec(`INSERT INTO public.uns_timeserial (_id,_timestamp,double_1) VALUES (?,now(),42)`, metric.ID).Error)
	var value float64
	require.NoError(t, store.CommonDB().Raw(`SELECT v FROM public.history_0 LIMIT 1`).Scan(&value).Error)
	require.EqualValues(t, 42, value)
	for _, name := range []string{"history_1", "history_2"} {
		require.NoError(t, store.CommonDB().Exec(fmt.Sprintf(`INSERT INTO public.%s (_timestamp,_json) VALUES (now(),'{}')`, name)).Error)
	}
}

func TestUnsImportHistory_FinalFailurePreservesMetadata(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	svc := uns.New(ctx, nil)
	accepted, err := svc.CreateImportJob(ctx, false, 123, 1)
	require.NoError(t, err)
	key := accepted["job"].(repo.AsyncJob).JobKey
	_, err = svc.ImportJob(ctx, "history.json", []byte(`{"namespace":[{"name":"saved","alias":"saved","type":"TOPIC","topicType":"METRIC","enableHistory":"true"}]}`), 1, key, nil)
	require.NoError(t, err)
	var event repo.OutboxEvent
	require.NoError(t, store.CommonDB().Where("event_type = ?", unsPhysicalEnsureEventType).Take(&event).Error)
	event.Attempts = 4
	require.Error(t, (&App{UNS: svc}).handleUnsPhysicalEnsure(ctx, event))
	job, err := repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, key)
	require.NoError(t, err)
	require.Equal(t, repo.AsyncJobStatusFailed, job.Status)
	require.Contains(t, string(job.ResultJSON), "historyFailed")
	var audits []repo.AuditLog
	require.NoError(t, store.CommonDB().Where("res_id = ?", key).Find(&audits).Error)
	require.Len(t, audits, 1)
	require.Equal(t, "failed", audits[0].Result)
	require.Contains(t, audits[0].DetailJSON, `"imported":1`)
	require.Contains(t, audits[0].DetailJSON, `"phase":"historyFailed"`)
	require.Contains(t, audits[0].DetailJSON, `"sourceFileId":123`)

	var count int64
	require.NoError(t, store.CommonDB().Model(&repo.UnsNode{}).Where("alias = ?", "saved").Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, svc.CompleteImportHistory(ctx, key, event.EventID, nil))
	job, err = repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, key)
	require.NoError(t, err)
	require.Equal(t, repo.AsyncJobStatusFailed, job.Status)
}

func TestUnsImportHistory_ReimportUpdatesViewsWithoutLosingData(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	ctx := context.Background()
	ingest, err := dataingest.New(ctx, config.DataIngestConf{Enabled: true}, dsn)
	require.NoError(t, err)
	defer ingest.Close()
	files, err := asset.New(ctx, asset.Config{Driver: "local", FileRoot: t.TempDir(), PublicBaseURL: "http://localhost"})
	require.NoError(t, err)
	app := &App{Asset: files, UNS: uns.New(ctx, ingest), DataIngest: ingest}
	importFile := func(fields string) (string, repo.OutboxEvent) {
		raw := []byte(`{"namespace":[{"name":"temp","alias":"repeat_history","type":"TOPIC","topicType":"METRIC","enableHistory":"true","fields":` + fields + `}]}`)
		uploaded, err := files.UploadContent(ctx, asset.UploadContentCommand{FileName: "repeat.json", ContentType: "application/json", Size: int64(len(raw)), Reader: bytes.NewReader(raw), UserID: 1})
		require.NoError(t, err)
		accepted, err := app.UNS.CreateImportJob(ctx, false, anyJobInt64(uploaded["fileId"]), 1)
		require.NoError(t, err)
		key := accepted["job"].(repo.AsyncJob).JobKey
		var event repo.OutboxEvent
		require.NoError(t, store.CommonDB().Where("aggregate_id = ?", key).Take(&event).Error)
		require.NoError(t, app.handleUnsImportJob(ctx, event))
		job, err := repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, key)
		require.NoError(t, err)
		if job.Status == repo.AsyncJobStatusSucceeded {
			var count int64
			require.NoError(t, store.CommonDB().Model(&repo.OutboxEvent{}).Where("event_type = ? AND payload->>'importJobKey' = ?", unsPhysicalEnsureEventType, key).Count(&count).Error)
			require.Zero(t, count)
			return key, repo.OutboxEvent{}
		}
		require.Equal(t, repo.AsyncJobStatusRunning, job.Status)
		event = repo.OutboxEvent{}
		require.NoError(t, store.CommonDB().Where("event_type = ? AND payload->>'importJobKey' = ?", unsPhysicalEnsureEventType, key).Take(&event).Error)
		return key, event
	}
	firstFields := `[{"name":"v","type":"DOUBLE"},{"name":"x","type":"INTEGER"}]`
	_, originalEvent := importFile(firstFields)
	require.NoError(t, app.handleUnsPhysicalEnsure(ctx, originalEvent))
	var original repo.UnsNode
	require.NoError(t, store.CommonDB().Where("alias = ?", "repeat_history").Take(&original).Error)
	require.NoError(t, store.CommonDB().Exec(`INSERT INTO public.uns_timeserial (_id,_timestamp,double_1) VALUES (?,now(),42)`, original.ID).Error)
	for i := 0; i < 3; i++ {
		key, event := importFile(firstFields)
		require.Empty(t, event.EventID)
		job, err := repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, key)
		require.NoError(t, err)
		require.Equal(t, repo.AsyncJobStatusSucceeded, job.Status)
		var result map[string]any
		require.NoError(t, json.Unmarshal(job.ResultJSON, &result))
		require.EqualValues(t, 0, result["imported"])
		require.EqualValues(t, 0, result["updated"])
		require.EqualValues(t, 1, result["skipped"])
	}
	// Add and reorder fields. Synchronize physical storage without losing history.
	_, changed := importFile(`[{"name":"x","type":"INTEGER"},{"name":"v","type":"DOUBLE"},{"name":"flag","type":"BOOLEAN"}]`)
	require.NoError(t, app.handleUnsPhysicalEnsure(ctx, changed))
	var kind string
	require.NoError(t, store.CommonDB().Raw(`SELECT data_type FROM information_schema.columns WHERE table_schema='public' AND table_name='repeat_history' AND column_name='flag'`).Scan(&kind).Error)
	require.Equal(t, "boolean", kind)
	require.NoError(t, store.CommonDB().Raw(`SELECT data_type FROM information_schema.columns WHERE table_schema='public' AND table_name='uns_timeserial' AND column_name='bool_1'`).Scan(&kind).Error)
	require.Equal(t, "boolean", kind)
	key, latest := importFile(`[{"name":"v","type":"DOUBLE"},{"name":"x","type":"INTEGER"},{"name":"flag","type":"BOOLEAN"},{"name":"note","type":"STRING"}]`)
	// An old batch redelivered after a later import must use the latest schema.
	require.NoError(t, app.handleUnsPhysicalEnsure(ctx, originalEvent))
	require.NoError(t, app.handleUnsPhysicalEnsure(ctx, latest))
	require.NoError(t, app.handleUnsPhysicalEnsure(ctx, changed))
	job, err := repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, key)
	require.NoError(t, err)
	require.Equal(t, repo.AsyncJobStatusSucceeded, job.Status)
	var columns []string
	require.NoError(t, store.CommonDB().Raw(`SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name='repeat_history' ORDER BY ordinal_position`).Scan(&columns).Error)
	require.Equal(t, []string{"_id", "_timestamp", "_quality", "v", "x", "flag", "note"}, columns)
	var value float64
	require.NoError(t, store.CommonDB().Raw(`SELECT v FROM public.repeat_history`).Scan(&value).Error)
	require.EqualValues(t, 42, value)
	require.NoError(t, store.CommonDB().Exec(`INSERT INTO public.uns_timeserial (_id,_timestamp,double_1,bool_1,str_1) VALUES (?,now(),43,true,'new field works')`, original.ID).Error)
	var projected struct {
		V    float64
		Flag bool
		Note string
	}
	require.NoError(t, store.CommonDB().Raw(`SELECT v,flag,note FROM public.repeat_history WHERE v=43`).Scan(&projected).Error)
	require.EqualValues(t, 43, projected.V)
	require.True(t, projected.Flag)
	require.Equal(t, "new field works", projected.Note)
	var current repo.UnsNode
	require.NoError(t, store.CommonDB().Where("alias = ?", "repeat_history").Take(&current).Error)
	require.Equal(t, original.ID, current.ID)
	var count int64
	require.NoError(t, store.CommonDB().Model(&repo.UnsNode{}).Where("type = 2").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

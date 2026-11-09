package repo_test

// 验证私有运行表、冻结结果、游标回滚/CAS及迁移；业务三表不承载执行历史。
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestStreamRuntimeTablesFrozenResultAndHistory(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx, r := context.Background(), repo.NewSourceStreamRepo(store)
	doc := `{"script":"module.exports=()=>{}"}`
	manifest := v2RequestManifest()
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "private-runtime", Model: "program", Document: doc}, 0, manifest)
	require.NoError(t, err)
	stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	_, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
	require.NoError(t, err)
	run := repo.StreamRun{ID: "private-run", StreamID: stream.ID, VersionID: version.ID, EntryID: "request", Model: "program", Status: "processing", CreatedAt: time.Now().UnixMilli(), Summary: `{"requestHash":"stable","script":"private-code","secret":"private-key"}`}
	created, err := r.AdmitProgramV2Run(ctx, run, true)
	require.NoError(t, err)
	require.True(t, created)
	task := repo.StreamTask{ID: run.ID, StreamID: run.StreamID, VersionID: run.VersionID, EntryID: run.EntryID, Model: "program", ReceivedAt: run.CreatedAt, Output: `{"data":{"z":2,"a":9007199254740993123},"outputs":[]}`}
	first, err := r.PutProgramV2Task(ctx, task, run)
	require.NoError(t, err)
	task.Output = `{"outputs":[],"data":{"changed":true}}`
	replay, err := r.PutProgramV2Task(ctx, task, run)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	var stored string
	require.NoError(t, store.UnsDB().Raw("SELECT output::text FROM public.uns_stream_task WHERE id=?", run.ID).Scan(&stored).Error)
	require.JSONEq(t, first.Output, stored)
	require.Contains(t, stored, "9007199254740993123")
	require.NoError(t, r.FinishProgramV2Task(ctx, run.ID, "succeeded", `{"secret":"private-key"}`, 8))
	var finishedAt int64
	require.NoError(t, store.UnsDB().Raw("SELECT finished_at FROM public.uns_stream_run WHERE id=?", run.ID).Scan(&finishedAt).Error)
	require.Positive(t, finishedAt)
	var history []string
	require.NoError(t, store.UnsDB().Raw("SELECT row_to_json(h)::text FROM public.uns_stream_run_history h WHERE run_id=? ORDER BY state_revision", run.ID).Scan(&history).Error)
	require.Len(t, history, 2)
	for _, item := range history {
		require.NotContains(t, item, "private-key")
		require.NotContains(t, item, "private-code")
		require.NotContains(t, item, "requestHash")
	}
	var count int64
	for _, sql := range []string{
		"SELECT count(*) FROM public.uns_meta WHERE id LIKE 'tier0.system.stream.%'",
		"SELECT count(*) FROM public.uns_record WHERE collection_id LIKE 'tier0.system.stream.%'",
		"SELECT count(*) FROM public.uns_sample WHERE collection_id LIKE 'tier0.system.stream.%'",
	} {
		require.NoError(t, store.UnsDB().Raw(sql).Scan(&count).Error)
		require.Zero(t, count)
	}
	for _, table := range []string{"uns_stream_run", "uns_stream_task", "uns_stream_effect", "uns_stream_interval_cursor", "uns_stream_interval_outbox", "uns_stream_event_match"} {
		var kind string
		require.NoError(t, store.UnsDB().Raw("SELECT relkind::text FROM pg_class WHERE oid=to_regclass(?)", "public."+table).Scan(&kind).Error)
		require.Equal(t, "r", kind, table)
	}
}

func TestStreamRuntimeTablesCheckpointRollbackAndCAS(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx, r := context.Background(), repo.NewSourceStreamRepo(store)
	change := repo.ProgramCheckpointCommit{StreamID: 41, EntryID: "poll", RunID: "checkpoint-1", Digest: strings.Repeat("d", 64), Value: json.RawMessage(`{"page":2}`)}
	abort := errors.New("abort outputs")
	apply := func(rollback bool) error {
		return store.UnsDB().Transaction(func(tx *gorm.DB) error {
			created, finish, err := repo.LockProgramCheckpointGORM(ctx, tx, change)
			if err != nil || !created {
				return err
			}
			if err := finish(); err != nil {
				return err
			}
			if rollback {
				return abort
			}
			return nil
		})
	}
	require.ErrorIs(t, apply(true), abort)
	cursor, err := r.ProgramCheckpoint(ctx, 41, "poll")
	require.NoError(t, err)
	require.Zero(t, cursor.Revision)
	require.NoError(t, apply(false))
	require.NoError(t, apply(false))
	cursor, err = r.ProgramCheckpoint(ctx, 41, "poll")
	require.NoError(t, err)
	require.EqualValues(t, 1, cursor.Revision)
	require.JSONEq(t, `{"page":2}`, string(cursor.Value))
	change.RunID = "checkpoint-2"
	require.ErrorIs(t, apply(false), repo.ErrConflict)
	var count int64
	require.NoError(t, store.UnsDB().Raw("SELECT count(*) FROM public.uns_stream_checkpoint").Scan(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, store.UnsDB().Raw("SELECT count(*) FROM public.uns_stream_commit_receipt").Scan(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, store.UnsDB().Raw("SELECT count(*) FROM public.uns_meta WHERE kind='task'").Scan(&count).Error)
	require.Zero(t, count)
}

func TestStreamRuntimeTablesConcurrentHistory(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r := repo.NewSourceStreamRepo(store)
	for _, id := range []string{"concurrent-a", "concurrent-b"} {
		created, err := r.ReserveRun(ctx, repo.StreamRun{ID: id, StreamID: 71, VersionID: 1, Model: "program", Status: "processing", Summary: `{}`, CreatedAt: time.Now().UnixMilli()})
		require.NoError(t, err)
		require.True(t, created)
	}
	tx := store.UnsDB().WithContext(ctx).Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	require.NoError(t, tx.Exec("UPDATE public.uns_stream_run SET status='succeeded' WHERE id='concurrent-a'").Error)
	done := make(chan error, 1)
	go func() { done <- r.FinishRun(ctx, "concurrent-b", "succeeded", `{}`, 1) }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, tx.Commit().Error)
	var count int64
	require.NoError(t, store.UnsDB().Raw("SELECT count(*) FROM public.uns_stream_run_history WHERE stream_id=71").Scan(&count).Error)
	require.EqualValues(t, 4, count)
}

func TestStreamRuntimeTablesForwardMigrationPreservesExecution(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	require.Contains(t, strings.ToLower(dsn), "t_teststreamruntimetablesforward")
	db, ctx := store.UnsDB(), context.Background()
	// Reconstruct the pre-015 state only inside this testkit-owned database.
	require.NoError(t, db.Exec(`DROP TRIGGER reject_legacy_stream_meta_task ON public.uns_meta;
 DROP FUNCTION public.reject_legacy_stream_meta_task();
 DROP TRIGGER stream_runtime_history ON public.uns_stream_run;
 DROP TRIGGER stream_run_lifecycle ON public.uns_stream_run;
 DROP TRIGGER stream_task_lifecycle ON public.uns_stream_task;
 DROP FUNCTION public.capture_stream_runtime_history();
 DROP FUNCTION public.stamp_stream_runtime_state();
 DROP FUNCTION public.protect_stream_runtime_storage();
 DROP TABLE public.uns_stream_checkpoint,public.uns_stream_commit_receipt,public.uns_metadata_event_outbox,
  public.uns_stream_meta_task_archive,public.uns_stream_run_history;
 DROP INDEX public.idx_stream_result_active,public.idx_stream_run_terminal;
 ALTER TABLE public.uns_stream_run DROP COLUMN updated_at,DROP COLUMN finished_at,DROP COLUMN state_revision;
 ALTER TABLE public.uns_stream_task DROP COLUMN updated_at,DROP COLUMN finished_at,DROP COLUMN payload_compacted_at;
 DELETE FROM public.sys_schema_migration WHERE scope='sink' AND version='3.0.0' AND sequence IN (15,16);`).Error)
	run := repo.StreamRun{ID: "legacy-run", StreamID: 73, VersionID: 19, EntryID: "poll", Model: "program", Status: "processing", Summary: `{}`, CreatedAt: 1730000000000}
	task := repo.StreamTask{ID: run.ID, StreamID: run.StreamID, VersionID: run.VersionID, EntryID: run.EntryID, Model: "program", Output: `{"data":{"large":9007199254740993123},"outputs":[]}`, Status: "pending", ReceivedAt: run.CreatedAt, CreatedAt: run.CreatedAt}
	require.NoError(t, db.Create(&run).Error)
	require.NoError(t, db.Create(&task).Error)
	keyJSON, err := json.Marshal([]any{int64(73), "poll"})
	require.NoError(t, err)
	key := sha256.Sum256(keyJSON)
	identity := "programCheckpoint:" + hex.EncodeToString(key[:])
	require.NoError(t, db.Exec("INSERT INTO public.uns_meta(kind,id,data,revision,created_at,updated_at) VALUES('task',?,?::jsonb,4,1,2)", identity, `{"type":"programCheckpoint","payload":{"revision":3,"value":{"page":4}}}`).Error)
	require.NoError(t, db.Exec(`INSERT INTO public.uns_meta(kind,id,data,revision,created_at,updated_at)
 SELECT 'task','metadataEventOutbox:bulk-'||n,jsonb_build_object('type','metadataEventOutbox','payload',jsonb_build_object('status','sent')),1,1,1 FROM generate_series(1,5000)n`).Error)
	require.NoError(t, db.Exec(`INSERT INTO public.uns_meta(kind,id,data,revision,created_at,updated_at) VALUES('task','unrelated','{"type":"otherJob","payload":{}}',1,1,1)`).Error)
	require.NoError(t, store.Migrate(ctx))
	r := repo.NewSourceStreamRepo(store)
	migrated, err := r.Task(ctx, run.ID)
	require.NoError(t, err)
	require.JSONEq(t, task.Output, migrated.Output)
	require.Contains(t, migrated.Output, "9007199254740993123")
	cursor, err := r.ProgramCheckpoint(ctx, 73, "poll")
	require.NoError(t, err)
	require.EqualValues(t, 3, cursor.Revision)
	require.JSONEq(t, `{"page":4}`, string(cursor.Value))
	var count int64
	require.NoError(t, db.Raw("SELECT count(*) FROM public.uns_stream_checkpoint WHERE id=? AND revision=4", identity).Scan(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Raw("SELECT count(*) FROM public.uns_metadata_event_outbox").Scan(&count).Error)
	require.EqualValues(t, 5000, count)
	require.NoError(t, db.Raw("SELECT count(*) FROM public.uns_stream_meta_task_archive").Scan(&count).Error)
	require.EqualValues(t, 5001, count)
	require.NoError(t, db.Raw("SELECT count(*) FROM public.uns_meta WHERE kind='task' AND id='unrelated'").Scan(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Raw("SELECT count(*) FROM public.uns_stream_run_history").Scan(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Raw("SELECT count(*) FROM public.uns_stream_run WHERE finished_at IS NOT NULL").Scan(&count).Error)
	require.Zero(t, count)
	require.NoError(t, store.Migrate(ctx))
	require.ErrorContains(t, db.Exec("INSERT INTO public.uns_meta(kind,id,data,revision,created_at,updated_at) VALUES('task',?,?::jsonb,1,1,1)", identity, `{"type":"programCheckpoint","payload":{"revision":0,"value":null}}`).Error, "private Stream runtime storage")
	require.NoError(t, db.Raw("SELECT count(*) FROM public.uns_meta WHERE kind='task' AND id=?", identity).Scan(&count).Error)
	require.Zero(t, count, "old writers must not recreate shadow state after cutover")
}

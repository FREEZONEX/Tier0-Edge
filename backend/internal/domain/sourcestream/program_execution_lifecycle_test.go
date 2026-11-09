package sourcestream_test

// 独立真实 PG：显式启动、独占连接、真实会话终止与无 NATS 失主收尾。
// 不连接 KVM/New2、不运行设备操作；冻结输出恢复另行验收。
import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	domain "backend/internal/domain/sourcestream"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func lifecycleOwnerID(t *testing.T, store *repo.Store) string {
	t.Helper()
	var ids []string
	require.NoError(t, store.UnsDB().Table("public.uns_stream_execution_owner").Order("created_at DESC,id DESC").Limit(1).Pluck("id", &ids).Error)
	require.Len(t, ids, 1)
	return ids[0]
}

func terminateLifecycleOwner(t *testing.T, store *repo.Store, id string) {
	t.Helper()
	var pids []int
	require.NoError(t, store.UnsDB().Raw(`SELECT lock.pid FROM pg_locks lock
		JOIN pg_stat_activity activity ON activity.pid=lock.pid
		JOIN public.uns_stream_execution_owner owner ON owner.id=?::uuid
		WHERE lock.locktype='advisory' AND lock.granted AND lock.objsubid=2
		AND lock.classid::bigint=? AND lock.objid::bigint=(owner.lock_id::bigint & 4294967295)
		AND activity.datname=current_database() AND lock.pid<>pg_backend_pid()`, id, int32(0x5354524d)).Scan(&pids).Error)
	require.Len(t, pids, 1, "only the exact owner in this isolated database is eligible")
	var terminated bool
	require.NoError(t, store.UnsDB().Raw(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		WHERE pid=? AND datname=current_database() AND pid<>pg_backend_pid()`, pids[0]).Scan(&terminated).Error)
	require.True(t, terminated)
}

func lifecycleRun(t *testing.T, store *repo.Store, ownerID string) repo.StreamRun {
	t.Helper()
	r, ctx := repo.NewSourceStreamRepo(store), context.Background()
	manifest := streamexec.ProgramManifest{Sources: []streamexec.ProgramSource{}, Topics: []streamexec.ProgramTopic{},
		Entries: []streamexec.ProgramEntry{{ID: "request", Kind: "request"}}}
	doc := `{"script":"not executed"}`
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "lifecycle test", Model: "program", Document: doc}, 0, manifest)
	require.NoError(t, err)
	stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	_, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
	require.NoError(t, err)
	run := repo.StreamRun{ID: "lifecycle-owned", StreamID: stream.ID, VersionID: version.ID, EntryID: "request", Model: "program", Status: "processing", Summary: `{"requestHash":"unchanged"}`, CreatedAt: time.Now().UnixMilli()}
	created, err := r.AdmitOwnedProgramV2Run(ctx, run, true, ownerID, []int64{19})
	require.NoError(t, err)
	require.True(t, created)
	return run
}

func TestProgramExecutionLifecycleExplicitStartAndClose(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	s, ctx := domain.New(nil, nil, ""), context.Background()
	t.Cleanup(s.Close)
	require.False(t, s.ProgramExecutionReady())
	require.NoError(t, s.StartProgramExecutions(ctx))
	require.True(t, s.ProgramExecutionReady())
	id := lifecycleOwnerID(t, store)
	require.NoError(t, s.StartProgramExecutions(ctx))
	require.Equal(t, id, lifecycleOwnerID(t, store), "start is idempotent without replacing the generation")
	db, err := store.UnsDB().DB()
	require.NoError(t, err)
	require.Eventually(t, func() bool { return db.Stats().InUse == 1 }, 5*time.Second, 20*time.Millisecond)
	s.Close()
	s.Close()
	require.False(t, s.ProgramExecutionReady())
	require.ErrorIs(t, s.StartProgramExecutions(ctx), domain.ErrUnavailable)
	require.Eventually(t, func() bool { return db.Stats().InUse == 0 }, 5*time.Second, 20*time.Millisecond)
}

func TestProgramExecutionLifecycleSessionLossRotatesAndRetiresWithoutNATS(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	s, ctx := domain.New(nil, nil, ""), context.Background()
	t.Cleanup(s.Close)
	require.NoError(t, s.StartProgramExecutions(ctx))
	id := lifecycleOwnerID(t, store)
	run := lifecycleRun(t, store, id)
	terminateLifecycleOwner(t, store, id)
	require.Eventually(t, func() bool {
		var count int64
		err := store.UnsDB().Table("public.uns_stream_execution_owner").Where("id<>?", id).Count(&count).Error
		return err == nil && count == 1 && s.ProgramExecutionReady()
	}, 10*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool {
		stored, err := repo.NewSourceStreamRepo(store).Run(ctx, run.ID)
		return err == nil && stored.Status == "unknown"
	}, 10*time.Second, 50*time.Millisecond)
	stored, err := repo.NewSourceStreamRepo(store).Run(ctx, run.ID)
	require.NoError(t, err)
	require.Contains(t, stored.Summary, "execution-owner-lost")
	require.Contains(t, stored.Summary, "unchanged")
	_, err = repo.NewSourceStreamRepo(store).Task(ctx, run.ID)
	require.ErrorIs(t, err, repo.ErrNotFound)
}

func TestProgramExecutionLifecycleParentCancellationReleasesSession(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	s := domain.New(nil, nil, "")
	t.Cleanup(s.Close)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, s.StartProgramExecutions(ctx))
	cancel()
	db, err := store.UnsDB().DB()
	require.NoError(t, err)
	require.Eventually(t, func() bool { return !s.ProgramExecutionReady() && db.Stats().InUse == 0 }, 5*time.Second, 20*time.Millisecond)
	require.ErrorIs(t, s.StartProgramExecutions(context.Background()), domain.ErrUnavailable, "a canceled lifecycle cannot report a successful restart")
}

func TestProgramExecutionLifecycleActualRequestAdmissionFailsClosed(t *testing.T) {
	s, store, _, ctx := actualUnstartedProgramService(t)
	doc := actualProgramDocument(`module.exports=api=>{api.stream.onRequest({id:"echo"},context=>context.input);};`)
	view, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	view, err = s.Deploy(ctx, view["id"].(int64), view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	id, version := view["id"].(int64), view["activeVersion"].(int64)
	require.Equal(t, "starting", view["executionState"])
	_, err = s.InvokeEntry(ctx, id, version, "echo", "before-start", 7, 3000)
	require.ErrorIs(t, err, domain.ErrUnavailable)
	r := repo.NewSourceStreamRepo(store)
	_, total, err := r.Runs(ctx, repo.StreamRunFilter{StreamID: id})
	require.NoError(t, err)
	require.Zero(t, total)
	require.NoError(t, s.StartProgramExecutions(ctx))
	result, err := s.InvokeEntry(ctx, id, version, "echo", "owned-sync", 7, 3000)
	require.NoError(t, err)
	require.False(t, result["usesNATS"].(bool))
	require.Equal(t, float64(7), result["result"].(map[string]any)["data"])
	_, err = s.InvokeEntry(ctx, id, version, "echo", "owned-sync", 7, 3000)
	require.NoError(t, err)
	runs, total, err := r.Runs(ctx, repo.StreamRunFilter{StreamID: id})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	var ownerID string
	require.NoError(t, store.UnsDB().Table("public.uns_stream_run_owner").Where("run_id=?", runs[0].ID).Pluck("owner_id", &ownerID).Error)
	require.Equal(t, lifecycleOwnerID(t, store), ownerID)
	lineage, owned, err := r.ProgramRunLineage(ctx, runs[0].ID)
	require.NoError(t, err)
	require.True(t, owned)
	require.Empty(t, lineage)
	s.Close()
	_, err = s.InvokeEntry(ctx, id, version, "echo", "after-close", 7, 3000)
	require.ErrorIs(t, err, domain.ErrUnavailable)
	_, total, err = r.Runs(ctx, repo.StreamRunFilter{StreamID: id})
	require.NoError(t, err)
	require.EqualValues(t, 1, total, "closed services never admit an unowned replacement execution")
}

func TestProgramExecutionLifecycleActualOwnerLossCancelsScriptWithoutReplay(t *testing.T) {
	s, store, sink, ctx := actualUnstartedProgramService(t)
	topic := actualProgramTopic(t, ctx, store, "blocked read")
	started := make(chan struct{}, 1)
	var calls atomic.Int32
	s.SetProgramUNS(func(callCtx context.Context, _ int64) (json.RawMessage, error) {
		calls.Add(1)
		started <- struct{}{}
		<-callCtx.Done()
		return nil, callCtx.Err()
	}, sink)
	require.NoError(t, s.StartProgramExecutions(ctx))
	doc := actualProgramDocument(fmt.Sprintf(`module.exports=api=>{
	 const input=api.uns.use({topicId:%q,access:"read"});
	 api.stream.onRequest({id:"wait"},()=>input.read({timeoutMs:15000}));
	};`, strconv.FormatInt(topic, 10)))
	doc["timeoutMs"] = 20000
	view, err := s.SaveStream(ctx, 0, 0, doc)
	require.NoError(t, err)
	view, err = s.Deploy(ctx, view["id"].(int64), view["revision"].(int64), 0, 0)
	require.NoError(t, err)
	id, version := view["id"].(int64), view["activeVersion"].(int64)
	before := lifecycleOwnerID(t, store)
	finished := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := s.InvokeEntry(ctx, id, version, "wait", "owner-lost", nil, 20000)
		finished <- err
	}()
	t.Cleanup(func() {
		s.Close()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("request did not exit during bounded cleanup")
		}
	})
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("actual child did not reach the bounded host read")
	}
	terminateLifecycleOwner(t, store, before)
	select {
	case err := <-finished:
		require.ErrorIs(t, err, domain.ErrUnknownResult)
	case <-time.After(10 * time.Second):
		t.Fatal("lost generation did not cancel before the original 15-second read timeout")
	}
	require.Eventually(t, func() bool { return s.ProgramExecutionReady() && lifecycleOwnerID(t, store) != before }, 10*time.Second, 50*time.Millisecond)
	_, err = s.InvokeEntry(ctx, id, version, "wait", "owner-lost", nil, 20000)
	require.ErrorIs(t, err, domain.ErrUnknownResult)
	require.EqualValues(t, 1, calls.Load(), "same idempotency key must not restart the host read")
	runs, total, err := repo.NewSourceStreamRepo(store).Runs(ctx, repo.StreamRunFilter{StreamID: id})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, "unknown", runs[0].Status)
	_, err = repo.NewSourceStreamRepo(store).Task(ctx, runs[0].ID)
	require.ErrorIs(t, err, repo.ErrNotFound)
	actualProgramOutboxCount(t, store, 0)
}

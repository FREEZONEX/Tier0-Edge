package repo_test

// 真实独立 PostgreSQL：验证会话所有权、失主收尾、冻结结果和历史隔离。
// 不执行用户脚本，不连接设备，不修改 KVM 或 New2。
import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func ownerProgram(t *testing.T, r *repo.SourceStreamRepo) (repo.Stream, repo.StreamVersion) {
	t.Helper()
	ctx := context.Background()
	doc := `{"script":"run"}`
	manifest := v2RequestManifest()
	stream, err := r.SaveProgramV2(ctx, repo.Stream{Name: "owned execution", Model: "program", Document: doc}, 0, manifest)
	require.NoError(t, err)
	stream, version, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, 0, doc, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
	require.NoError(t, err)
	return stream, version
}

func ownerRun(stream repo.Stream, version repo.StreamVersion, id string) repo.StreamRun {
	return repo.StreamRun{ID: id, StreamID: stream.ID, VersionID: version.ID, EntryID: "request", Model: "program", Status: "processing", Summary: `{"phase":"admitted","requestHash":"original"}`, CreatedAt: time.Now().UnixMilli()}
}

func openOwner(t *testing.T, r *repo.SourceStreamRepo) *repo.ProgramExecutionOwner {
	t.Helper()
	o, err := r.OpenProgramExecutionOwner(context.Background(), uuid.NewString())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, o.Close()) })
	return o
}

// Socket close is asynchronous at PostgreSQL. Observe actual lock release,
// without retiring rows or claiming that a local Close already proved loss.
func ownerSessionReleased(t *testing.T, store *repo.Store, id string) {
	t.Helper()
	require.Eventually(t, func() bool {
		free := false
		err := store.UnsDB().Transaction(func(tx *gorm.DB) error {
			var lockID int32
			if err := tx.Raw(`SELECT lock_id FROM public.uns_stream_execution_owner WHERE id=?`, id).Scan(&lockID).Error; err != nil {
				return err
			}
			return tx.Raw(`SELECT pg_try_advisory_xact_lock(?,?)`, int32(0x5354524d), lockID).Scan(&free).Error
		})
		return err == nil && free
	}, 5*time.Second, 20*time.Millisecond)
}

func TestProgramExecutionOwnerLiveSessionDoesNotConsumePerRunConnections(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r, ctx := repo.NewSourceStreamRepo(store), context.Background()
	db, err := store.UnsDB().DB()
	require.NoError(t, err)
	db.SetMaxOpenConns(10)
	o := openOwner(t, r)
	require.NoError(t, o.Ping(ctx))
	_, err = r.OpenProgramExecutionOwner(ctx, o.ID())
	require.ErrorIs(t, err, repo.ErrConflict)
	stream, version := ownerProgram(t, r)
	for i := 0; i < 12; i++ {
		created, err := r.AdmitOwnedProgramV2Run(ctx, ownerRun(stream, version, fmt.Sprintf("live-%02d", i)), true, o.ID(), nil)
		require.NoError(t, err)
		require.True(t, created)
	}
	require.Equal(t, 1, db.Stats().InUse, "only the incarnation keeps a connection, not twelve admitted runs")
	recovery, err := r.RecoverProgramExecutionOwner(ctx, o.ID(), 100)
	require.NoError(t, err)
	require.False(t, recovery.Lost)
	require.Empty(t, recovery.Interrupted)
	ids, err := r.ProgramExecutionOwners(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, []string{o.ID()}, ids)
	stored, err := r.Run(ctx, "live-00")
	require.NoError(t, err)
	require.Equal(t, "processing", stored.Status)
}

func TestProgramExecutionOwnerLostSessionBoundedRecoveryPreservesOtherAndLegacyRuns(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r, ctx := repo.NewSourceStreamRepo(store), context.Background()
	stream, version := ownerProgram(t, r)
	o, other := openOwner(t, r), openOwner(t, r)
	for _, id := range []string{"lost-a", "lost-b", "lost-c"} {
		created, err := r.AdmitOwnedProgramV2Run(ctx, ownerRun(stream, version, id), true, o.ID(), []int64{7, 8})
		require.NoError(t, err)
		require.True(t, created)
	}
	_, err := r.AdmitOwnedProgramV2Run(ctx, ownerRun(stream, version, "other-live"), true, other.ID(), nil)
	require.NoError(t, err)
	_, err = r.AdmitProgramV2Run(ctx, ownerRun(stream, version, "legacy-unowned"), true)
	require.NoError(t, err)
	require.NoError(t, o.Close())
	require.Error(t, o.Ping(ctx))
	_, err = r.OpenProgramExecutionOwner(ctx, o.ID())
	require.ErrorIs(t, err, repo.ErrConflict, "lost generations cannot reconnect with the same identity")
	first, err := r.RecoverProgramExecutionOwner(ctx, o.ID(), 2)
	require.NoError(t, err)
	require.True(t, first.Lost)
	require.Len(t, first.Interrupted, 2)
	second, err := r.RecoverProgramExecutionOwner(ctx, o.ID(), 2)
	require.NoError(t, err)
	require.Len(t, second.Interrupted, 1)
	for _, id := range []string{"lost-a", "lost-b", "lost-c"} {
		stored, err := r.Run(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "unknown", stored.Status)
		var summary map[string]any
		require.NoError(t, json.Unmarshal([]byte(stored.Summary), &summary))
		require.Equal(t, "original", summary["requestHash"])
		require.Equal(t, "execution-owner-lost", summary["reason"])
	}
	for _, id := range []string{"other-live", "legacy-unowned"} {
		stored, err := r.Run(ctx, id)
		require.NoError(t, err)
		require.Equal(t, "processing", stored.Status)
	}
	ids, err := r.ProgramExecutionOwners(ctx, 100)
	require.NoError(t, err)
	require.Equal(t, []string{other.ID()}, ids)
}

func TestProgramExecutionOwnerPreparedResultAndLineageSurviveRecovery(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r, ctx := repo.NewSourceStreamRepo(store), context.Background()
	stream, version := ownerProgram(t, r)
	o, other := openOwner(t, r), openOwner(t, r)
	run := ownerRun(stream, version, "prepared-owned")
	_, err := r.AdmitOwnedProgramV2Run(ctx, run, true, o.ID(), []int64{17, 19})
	require.NoError(t, err)
	created, err := r.AdmitOwnedProgramV2Run(ctx, run, true, other.ID(), []int64{99})
	require.NoError(t, err)
	require.False(t, created, "same intent does not change owner or original lineage")
	task, err := r.PutProgramV2Task(ctx, repo.StreamTask{ID: run.ID, StreamID: stream.ID, VersionID: version.ID, EntryID: run.EntryID, Model: "program", Output: `{"outputs":[],"checkpoint":{"revision":0,"value":{"b":2,"a":1}}}`, ReceivedAt: time.Now().UnixMilli()}, run)
	require.NoError(t, err)
	require.NoError(t, o.Close())
	ownerSessionReleased(t, store, o.ID())
	for i := 0; i < 2; i++ {
		recovery, err := r.RecoverProgramExecutionOwner(ctx, o.ID(), 10)
		require.NoError(t, err)
		require.True(t, recovery.Lost)
		require.Empty(t, recovery.Interrupted)
		require.Len(t, recovery.Prepared, 1)
		require.Equal(t, task, recovery.Prepared[0].Task)
		require.Equal(t, []int64{17, 19}, recovery.Prepared[0].Lineage)
	}
	stored, err := r.Run(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "processing", stored.Status, "repo returns durable output; it does not invent commit success")
}

func TestProgramExecutionOwnerLossFencesBeforeRecovery(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r, ctx := repo.NewSourceStreamRepo(store), context.Background()
	stream, version := ownerProgram(t, r)
	o := openOwner(t, r)
	run := ownerRun(stream, version, "late-owned")
	_, err := r.AdmitOwnedProgramV2Run(ctx, run, true, o.ID(), nil)
	require.NoError(t, err)
	effect := repo.StreamProgramEffect{RunID: run.ID, EntryID: run.EntryID, CallID: "device", OpID: "write", RequestHash: strings.Repeat("a", 64)}
	_, err = r.ReserveProgramEffect(ctx, effect)
	require.NoError(t, err)
	require.NoError(t, o.Close())
	ownerSessionReleased(t, store, o.ID())
	_, err = r.AdmitOwnedProgramV2Run(ctx, ownerRun(stream, version, "not-admitted"), true, o.ID(), nil)
	require.ErrorIs(t, err, repo.ErrConflict)
	_, claimed, err := r.ClaimProgramEffect(ctx, run.ID, run.EntryID, effect.CallID, effect.RequestHash)
	require.ErrorIs(t, err, repo.ErrConflict)
	require.False(t, claimed, "do not dispatch while recovery has not yet observed session loss")
	_, err = r.ReserveProgramEffect(ctx, repo.StreamProgramEffect{RunID: run.ID, EntryID: run.EntryID, CallID: "late-device", OpID: "write", RequestHash: strings.Repeat("b", 64)})
	require.ErrorIs(t, err, repo.ErrConflict)
	_, err = r.PutProgramV2Task(ctx, repo.StreamTask{ID: run.ID, StreamID: stream.ID, VersionID: version.ID, EntryID: run.EntryID, Model: "program", Output: `{"outputs":[]}`, ReceivedAt: time.Now().UnixMilli()}, run)
	require.ErrorIs(t, err, repo.ErrConflict)
}

func TestProgramExecutionOwnerValidationAndPoolSettings(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r, ctx := repo.NewSourceStreamRepo(store), context.Background()
	for _, id := range []string{"", "not-a-generation", uuid.Nil.String(), "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"} {
		_, err := r.OpenProgramExecutionOwner(ctx, id)
		require.ErrorIs(t, err, repo.ErrInvalidArgument)
	}
	o := openOwner(t, r)
	for _, n := range []int{0, 101} {
		_, err := r.RecoverProgramExecutionOwner(ctx, o.ID(), n)
		require.ErrorIs(t, err, repo.ErrInvalidArgument)
		_, err = r.ProgramExecutionOwners(ctx, n)
		require.ErrorIs(t, err, repo.ErrInvalidArgument)
	}
	_, err := r.AdmitOwnedProgramV2Run(ctx, repo.StreamRun{}, true, o.ID(), []int64{0})
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	_, err = r.AdmitOwnedProgramV2Run(ctx, repo.StreamRun{}, true, o.ID(), make([]int64, 32))
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	require.NoError(t, o.Close())
	var setting string
	require.NoError(t, store.UnsDB().Raw("SHOW idle_session_timeout").Scan(&setting).Error)
	require.Equal(t, "0", setting, "private session settings are discarded, not leaked to the pool")
}

func TestProgramExecutionOwnerRecoveryCannotImpersonateLiveSession(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	stream, version := ownerProgram(t, r)
	o := openOwner(t, r)
	_, err := r.AdmitOwnedProgramV2Run(context.Background(), ownerRun(stream, version, "already-admitted"), true, o.ID(), nil)
	require.NoError(t, err)
	type scopeKey struct{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	guardReady, recoveryReady, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var guardOnce, recoveryOnce, releaseOnce sync.Once
	rowCallback, queryCallback := "test-owner-guard-pause", "test-owner-recovery-pause"
	var workers sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		releaseOnce.Do(func() { close(release) })
		workers.Wait()
		require.NoError(t, store.UnsDB().Callback().Row().Remove(rowCallback))
		require.NoError(t, store.UnsDB().Callback().Query().Remove(queryCallback))
	})
	require.NoError(t, store.UnsDB().Callback().Row().Before("gorm:row").Register(rowCallback, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(scopeKey{}) == "guard" && strings.Contains(tx.Statement.SQL.String(), "pg_try_advisory_xact_lock") {
			guardOnce.Do(func() { close(guardReady) })
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}))
	require.NoError(t, store.UnsDB().Callback().Query().Before("gorm:query").Register(queryCallback, func(tx *gorm.DB) {
		if tx.Statement.Context.Value(scopeKey{}) == "recovery" && strings.Contains(tx.Statement.Table, "execution_owner") {
			recoveryOnce.Do(func() { close(recoveryReady) })
		}
	}))
	type admissionResult struct {
		created bool
		err     error
	}
	type recoveryResult struct {
		value repo.ProgramOwnerRecovery
		err   error
	}
	admission, recovery := make(chan admissionResult, 1), make(chan recoveryResult, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		created, err := r.AdmitOwnedProgramV2Run(context.WithValue(ctx, scopeKey{}, "guard"), ownerRun(stream, version, "must-not-admit"), true, o.ID(), nil)
		admission <- admissionResult{created, err}
	}()
	select {
	case <-guardReady:
	case <-ctx.Done():
		t.Fatal("admission did not reach its owner guard")
	}
	require.NoError(t, o.Close())
	ownerSessionReleased(t, store, o.ID())
	workers.Add(1)
	go func() {
		defer workers.Done()
		value, err := r.RecoverProgramExecutionOwner(context.WithValue(ctx, scopeKey{}, "recovery"), o.ID(), 10)
		recovery <- recoveryResult{value, err}
	}()
	select {
	case <-recoveryReady:
	case <-ctx.Done():
		t.Fatal("recovery did not reach the owner row")
	}
	releaseOnce.Do(func() { close(release) })
	result, restored := <-admission, <-recovery
	require.ErrorIs(t, result.err, repo.ErrConflict, "recovery's lock is not evidence that the original execution session still exists")
	require.False(t, result.created)
	require.NoError(t, restored.err)
	require.True(t, restored.value.Lost)
	require.Equal(t, []string{"already-admitted"}, restored.value.Interrupted)
	_, err = r.Run(context.Background(), "must-not-admit")
	require.ErrorIs(t, err, repo.ErrNotFound)
}

func TestProgramExecutionOwnerLeaseExpiresWithoutHeartbeat(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	stream, version := ownerProgram(t, r)
	o := openOwner(t, r)
	ctx, cancel := context.WithTimeout(context.Background(), repo.ProgramExecutionOwnerLease+12*time.Second)
	defer cancel()
	run := ownerRun(stream, version, "expired-generation")
	_, err := r.AdmitOwnedProgramV2Run(ctx, run, true, o.ID(), nil)
	require.NoError(t, err)
	// Real wall time and the actual production lease; no clock changes and no
	// Ping on the owning session. Queries below use other pooled connections.
	require.Eventually(t, func() bool {
		recovered, err := r.RecoverProgramExecutionOwner(ctx, o.ID(), 10)
		return err == nil && recovered.Lost && len(recovered.Interrupted) == 1
	}, repo.ProgramExecutionOwnerLease+10*time.Second, 100*time.Millisecond)
	require.Error(t, o.Ping(ctx), "an expired sql.Conn must not reconnect as the old generation")
	_, err = r.OpenProgramExecutionOwner(ctx, o.ID())
	require.ErrorIs(t, err, repo.ErrConflict)
	stored, err := r.Run(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "unknown", stored.Status)
	_, err = r.Task(ctx, run.ID)
	require.ErrorIs(t, err, repo.ErrNotFound)
}

func TestProgramExecutionOwnerFreezeSerializesWithRetirement(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	stream, version := ownerProgram(t, r)
	o := openOwner(t, r)
	run := ownerRun(stream, version, "freeze-before-loss")
	_, err := r.AdmitOwnedProgramV2Run(context.Background(), run, true, o.ID(), []int64{23})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	var workers sync.WaitGroup
	callback := "test-owner-task-freeze"
	t.Cleanup(func() {
		cancel()
		once.Do(func() { close(release) })
		workers.Wait()
		require.NoError(t, store.UnsDB().Callback().Create().Remove(callback))
	})
	require.NoError(t, store.UnsDB().Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if _, ok := tx.Statement.Model.(*repo.StreamTask); ok {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
			}
		}
	}))
	type frozenResult struct {
		task repo.StreamTask
		err  error
	}
	finished := make(chan frozenResult, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		task, err := r.PutProgramV2Task(ctx, repo.StreamTask{ID: run.ID, StreamID: stream.ID, VersionID: version.ID, EntryID: run.EntryID, Model: "program", Output: `{"outputs":[],"data":{"known":true}}`, ReceivedAt: time.Now().UnixMilli()}, run)
		finished <- frozenResult{task, err}
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("result did not reach the locked freeze transaction")
	}
	require.NoError(t, o.Close())
	ownerSessionReleased(t, store, o.ID())
	waitCtx, stopWait := context.WithTimeout(ctx, 75*time.Millisecond)
	blocked, err := r.RecoverProgramExecutionOwner(waitCtx, o.ID(), 10)
	stopWait()
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.False(t, blocked.Lost, "failed retirement must not report rolled-back changes")
	once.Do(func() { close(release) })
	frozen := <-finished
	require.NoError(t, frozen.err)
	recovered, err := r.RecoverProgramExecutionOwner(ctx, o.ID(), 10)
	require.NoError(t, err)
	require.True(t, recovered.Lost)
	require.Empty(t, recovered.Interrupted)
	require.Len(t, recovered.Prepared, 1)
	require.Equal(t, frozen.task, recovered.Prepared[0].Task)
	require.Equal(t, []int64{23}, recovered.Prepared[0].Lineage)
	_, err = r.PutProgramV2Task(ctx, frozen.task, run)
	require.ErrorIs(t, err, repo.ErrConflict, "late owners cannot freeze again after retirement")
	actual, err := r.Task(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, frozen.task, actual)
}

func TestProgramExecutionOwnerMigrationAndReferences(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r, ctx := repo.NewSourceStreamRepo(store), context.Background()
	stream, version := ownerProgram(t, r)
	o := openOwner(t, r)
	run := ownerRun(stream, version, "migration-preserved")
	_, err := r.AdmitOwnedProgramV2Run(ctx, run, true, o.ID(), []int64{41})
	require.NoError(t, err)
	require.NoError(t, store.Migrate(ctx), "repeat production migration must retain the owned run")
	require.NoError(t, o.Ping(ctx))
	var binding struct {
		OwnerID string
		Lineage string
	}
	require.NoError(t, store.UnsDB().Table("public.uns_stream_run_owner").Where("run_id=?", run.ID).Take(&binding).Error)
	require.Equal(t, o.ID(), binding.OwnerID)
	require.JSONEq(t, `[41]`, binding.Lineage)
	var index int64
	require.NoError(t, store.UnsDB().Raw(`SELECT count(*) FROM pg_indexes WHERE schemaname='public' AND indexname='idx_program_run_owner'`).Scan(&index).Error)
	require.EqualValues(t, 1, index)
	err = store.UnsDB().Exec(`INSERT INTO public.uns_stream_run_owner(run_id,owner_id) VALUES (?,?)`, "missing-run", o.ID()).Error
	var constraint *pgconn.PgError
	require.ErrorAs(t, err, &constraint)
	require.Equal(t, "23503", constraint.Code)
	require.Equal(t, "uns_stream_run_owner_run_id_fkey", constraint.ConstraintName)
	unbound := ownerRun(stream, version, "existing-unbound-run")
	_, err = r.AdmitProgramV2Run(ctx, unbound, true)
	require.NoError(t, err)
	err = store.UnsDB().Exec(`INSERT INTO public.uns_stream_run_owner(run_id,owner_id) VALUES (?,?)`, unbound.ID, uuid.NewString()).Error
	require.ErrorAs(t, err, &constraint)
	require.Equal(t, "23503", constraint.Code)
	require.Equal(t, "uns_stream_run_owner_owner_id_fkey", constraint.ConstraintName, "a valid run cannot acquire an unknown owner")
}

func TestProgramExecutionOwnerConcurrentPingAndClose(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	o := openOwner(t, r)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start, results := make(chan struct{}), make(chan error, 17)
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			results <- o.Ping(ctx)
		}()
	}
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		results <- o.Close()
	}()
	close(start)
	workers.Wait()
	close(results)
	for err := range results {
		if err != nil {
			require.ErrorIs(t, err, repo.ErrConflict, "closed sessions do not reconnect")
		}
	}
	require.NoError(t, o.Close())
	require.ErrorIs(t, o.Ping(ctx), repo.ErrConflict)
}

func TestProgramExecutionOwnerLockSlotCollision(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r, ctx := repo.NewSourceStreamRepo(store), context.Background()
	// A fixed, independently generated collision; never brute force in tests.
	firstID, secondID := "00000000-0000-4000-8000-000000000394", "00000000-0000-4000-8000-00000000226b"
	firstHash, secondHash := sha256.Sum256([]byte(firstID)), sha256.Sum256([]byte(secondID))
	require.Equal(t, binary.BigEndian.Uint32(firstHash[:4]), binary.BigEndian.Uint32(secondHash[:4]))
	first, err := r.OpenProgramExecutionOwner(ctx, firstID)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, first.Close()) })
	second, err := r.OpenProgramExecutionOwner(ctx, secondID)
	if second != nil {
		t.Cleanup(func() { require.NoError(t, second.Close()) })
	}
	require.ErrorIs(t, err, repo.ErrConflict, "a colliding generation must not impersonate the original holder")
	require.Nil(t, second)
	require.NoError(t, first.Ping(ctx))
	require.NoError(t, first.Close())
	ownerSessionReleased(t, store, firstID)
	_, err = r.OpenProgramExecutionOwner(ctx, secondID)
	require.ErrorIs(t, err, repo.ErrConflict, "registered lock slots are never reassigned, even after session loss")
	var rows int64
	require.NoError(t, store.UnsDB().Table("public.uns_stream_execution_owner").Where("id=?", secondID).Count(&rows).Error)
	require.Zero(t, rows)
	unrelated, err := r.OpenProgramExecutionOwner(ctx, "00000000-0000-4000-8000-000000000001")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, unrelated.Close()) })
	require.NoError(t, unrelated.Ping(ctx))
}

func TestProgramExecutionOwnerLockNamespaceIsolation(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	owner := openOwner(t, r)
	const namespace int32 = 0x5354524d // Reserved STRM pair-lock namespace.
	var iamNamespace int32
	require.NoError(t, store.UnsDB().Raw(`SELECT hashtext('tier0:user_name')`).Scan(&iamNamespace).Error)
	require.NotEqual(t, namespace, iamNamespace, "the existing IAM pair-lock namespace must remain separate")
	hash := sha256.Sum256([]byte(owner.ID()))
	slot := int32(binary.BigEndian.Uint32(hash[:4]))
	legacyKey := int64(namespace)<<32 | int64(uint32(slot))
	var bigintFree, pairFree bool
	err := store.UnsDB().Transaction(func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT pg_try_advisory_xact_lock(?)`, legacyKey).Scan(&bigintFree).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT pg_try_advisory_xact_lock(?,?)`, namespace, slot).Scan(&pairFree).Error
	})
	require.NoError(t, err)
	require.True(t, bigintFree, "the same bit pattern in a bigint lock must not alias the execution owner")
	require.False(t, pairFree, "the actual execution pair lock is held by a different physical session")
}

func TestProgramExecutionOwnerUnpreparedScanSkipsFrozenResultsAndPages(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r, ctx := repo.NewSourceStreamRepo(store), context.Background()
	stream, version := ownerProgram(t, r)
	first, second := openOwner(t, r), openOwner(t, r)
	for _, owner := range []*repo.ProgramExecutionOwner{first, second} {
		for i := 0; i < 3; i++ {
			run := ownerRun(stream, version, fmt.Sprintf("%s-frozen-%d", owner.ID(), i))
			_, err := r.AdmitOwnedProgramV2Run(ctx, run, true, owner.ID(), nil)
			require.NoError(t, err)
			_, err = r.PutProgramV2Task(ctx, repo.StreamTask{ID: run.ID, StreamID: run.StreamID, VersionID: run.VersionID, EntryID: run.EntryID, Model: "program", Output: `{"outputs":[]}`, ReceivedAt: time.Now().UnixMilli()}, run)
			require.NoError(t, err)
		}
		_, err := r.AdmitOwnedProgramV2Run(ctx, ownerRun(stream, version, owner.ID()+"-unprepared"), true, owner.ID(), []int64{29})
		require.NoError(t, err)
	}
	page, err := r.UnpreparedProgramExecutionOwners(ctx, "", 1)
	require.NoError(t, err)
	require.Len(t, page, 1)
	other, err := r.UnpreparedProgramExecutionOwners(ctx, page[0], 1)
	require.NoError(t, err)
	require.Len(t, other, 1)
	require.NotEqual(t, page, other)
	require.NoError(t, first.Close())
	require.NoError(t, second.Close())
	ownerSessionReleased(t, store, first.ID())
	ownerSessionReleased(t, store, second.ID())
	for _, id := range []string{page[0], other[0]} {
		recovered, err := r.RetireUnpreparedProgramExecutionOwner(ctx, id, 1)
		require.NoError(t, err)
		require.Equal(t, []string{id + "-unprepared"}, recovered.Interrupted)
		require.Empty(t, recovered.Prepared, "large frozen output rows are not loaded for retirement")
		stored, err := r.Run(ctx, id+"-frozen-0")
		require.NoError(t, err)
		require.Equal(t, "processing", stored.Status)
	}
	page, err = r.UnpreparedProgramExecutionOwners(ctx, "", 100)
	require.NoError(t, err)
	require.Empty(t, page)
	_, err = r.UnpreparedProgramExecutionOwners(ctx, "invalid-cursor", 1)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
}

type programOwnerQueryCapture struct {
	logger.Interface
	sql string
}

func (c *programOwnerQueryCapture) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	c.sql, _ = query()
}

func TestProgramExecutionOwnerPendingIndexSkipsCompletedHistory(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r, ctx := repo.NewSourceStreamRepo(store), context.Background()
	stream, version := ownerProgram(t, r)
	owner := openOwner(t, r)
	_, err := r.AdmitOwnedProgramV2Run(ctx, ownerRun(stream, version, "one-pending"), true, owner.ID(), nil)
	require.NoError(t, err)
	for first := 1; first <= 10000; first += 100 {
		require.NoError(t, store.UnsDB().Exec(`INSERT INTO public.uns_stream_run(id,stream_id,version_id,entry_id,model,status,created_at)
			SELECT 'history-'||n,?,?,'request','program','succeeded',? FROM generate_series(?::int,?::int) n`, stream.ID, version.ID, time.Now().UnixMilli(), first, first+99).Error)
	}
	require.NoError(t, store.UnsDB().Exec(`INSERT INTO public.uns_stream_run_owner(run_id,owner_id)
		SELECT id,?::uuid FROM public.uns_stream_run WHERE id LIKE 'history-%'`, owner.ID()).Error)
	require.NoError(t, store.UnsDB().Exec(`ANALYZE public.uns_stream_run,public.uns_stream_run_owner,public.uns_stream_execution_owner`).Error)
	capture := &programOwnerQueryCapture{Interface: logger.Discard}
	ids, err := repo.NewSourceStreamRepo(store.UnsDB().Session(&gorm.Session{Logger: capture})).UnpreparedProgramExecutionOwners(ctx, "", 16)
	require.NoError(t, err)
	require.Equal(t, []string{owner.ID()}, ids)
	require.Contains(t, capture.sql, "SELECT")
	var plan string
	require.NoError(t, store.UnsDB().Raw("EXPLAIN (ANALYZE,FORMAT JSON) "+capture.sql).Scan(&plan).Error)
	require.Contains(t, plan, "idx_program_unprepared_runs", "the actual production query must use the partial pending index without disabling sequential scans")
}

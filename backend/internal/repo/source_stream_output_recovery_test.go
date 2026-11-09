package repo_test

// 独立真实 PG：冻结与调度原子性、领取围栏、持久预算、隔离及自然查询计划。
// 不执行脚本、不连接设备、不操作 KVM/New2 或共享消息队列。
import (
	"context"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func recoveryFixture(t *testing.T) (*repo.Store, *repo.SourceStreamRepo, *repo.ProgramExecutionOwner, repo.Stream, repo.StreamVersion) {
	t.Helper()
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	stream, version := ownerProgram(t, r)
	return store, r, openOwner(t, r), stream, version
}

func freezeRecoveryTask(t *testing.T, r *repo.SourceStreamRepo, owner *repo.ProgramExecutionOwner, stream repo.Stream, version repo.StreamVersion, id string) repo.StreamTask {
	t.Helper()
	ctx := context.Background()
	run := ownerRun(stream, version, id)
	created, err := r.AdmitOwnedProgramV2Run(ctx, run, true, owner.ID(), []int64{19})
	require.NoError(t, err)
	require.True(t, created)
	task, err := r.PutProgramV2Task(ctx, repo.StreamTask{ID: id, StreamID: stream.ID, VersionID: version.ID, EntryID: run.EntryID, Model: "program", Output: `{"outputs":[],"data":{"marker":7}}`, ReceivedAt: time.Now().UnixMilli()}, run)
	require.NoError(t, err)
	return task
}

func dueRecovery(t *testing.T, store *repo.Store, id string) {
	t.Helper()
	require.NoError(t, store.UnsDB().Table("public.uns_stream_run_owner").Where("run_id=?", id).Update("recovery_after", 0).Error)
}

type recoveryBinding struct {
	RecoveryState    string
	RecoveryAttempts int
	RecoveryAfter    int64
	RecoveryToken    *string
}

func recoveryState(t *testing.T, store *repo.Store, id string) recoveryBinding {
	t.Helper()
	var row recoveryBinding
	require.NoError(t, store.UnsDB().Table("public.uns_stream_run_owner").Where("run_id=?", id).Take(&row).Error)
	return row
}

func TestProgramOutputRecoveryFreezeAndScheduleAreAtomic(t *testing.T) {
	store, r, owner, stream, version := recoveryFixture(t)
	ctx := context.Background()
	run := ownerRun(stream, version, "atomic-schedule")
	_, err := r.AdmitOwnedProgramV2Run(ctx, run, true, owner.ID(), nil)
	require.NoError(t, err)
	require.NoError(t, store.UnsDB().Exec(`CREATE FUNCTION public.reject_output_schedule() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'schedule unavailable'; END $$;
		CREATE TRIGGER reject_output_schedule BEFORE UPDATE OF recovery_state ON public.uns_stream_run_owner FOR EACH ROW WHEN(NEW.recovery_state='pending') EXECUTE FUNCTION public.reject_output_schedule()`).Error)
	_, err = r.PutProgramV2Task(ctx, repo.StreamTask{ID: run.ID, StreamID: stream.ID, VersionID: version.ID, EntryID: run.EntryID, Model: "program", Output: `{"outputs":[]}`, ReceivedAt: time.Now().UnixMilli()}, run)
	require.Error(t, err)
	_, err = r.Task(ctx, run.ID)
	require.ErrorIs(t, err, repo.ErrNotFound)
	require.Equal(t, "waiting", recoveryState(t, store, run.ID).RecoveryState)
	require.NoError(t, store.UnsDB().Exec(`DROP TRIGGER reject_output_schedule ON public.uns_stream_run_owner; DROP FUNCTION public.reject_output_schedule()`).Error)
	task, err := r.PutProgramV2Task(ctx, repo.StreamTask{ID: run.ID, StreamID: stream.ID, VersionID: version.ID, EntryID: run.EntryID, Model: "program", Output: `{"outputs":[]}`, ReceivedAt: time.Now().UnixMilli()}, run)
	require.NoError(t, err)
	state := recoveryState(t, store, task.ID)
	require.Equal(t, "pending", state.RecoveryState)
	require.Zero(t, state.RecoveryAttempts)
	require.Positive(t, state.RecoveryAfter)
	require.NoError(t, store.UnsDB().Exec(`UPDATE public.uns_stream_run_owner SET recovery_after=floor(extract(epoch FROM clock_timestamp())*1000)::bigint+86400000 WHERE run_id=?`, task.ID).Error)
	_, found, err := r.ClaimProgramOutputRecovery(ctx)
	require.NoError(t, err)
	require.False(t, found)
	dueRecovery(t, store, task.ID)
	claim, found, err := r.ClaimProgramOutputRecovery(ctx)
	require.NoError(t, err)
	require.True(t, found)
	loaded, err := r.LoadProgramOutputRecovery(ctx, claim)
	require.NoError(t, err)
	require.Equal(t, task, loaded)
	require.NoError(t, r.FinishProgramV2Task(ctx, task.ID, "succeeded", `{"phase":"uns-commit"}`, 1))
	require.Equal(t, "completed", recoveryState(t, store, task.ID).RecoveryState)
	accepted, err := r.FailProgramOutputRecovery(ctx, claim, true, repo.ProgramRecoveryInvalid)
	require.NoError(t, err)
	require.False(t, accepted)
	stored, err := r.Run(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", stored.Status)
}

func TestProgramOutputRecoveryConcurrentClaimAndStaleToken(t *testing.T) {
	store, r, owner, stream, version := recoveryFixture(t)
	task := freezeRecoveryTask(t, r, owner, stream, version, "claim-once")
	dueRecovery(t, store, task.ID)
	type outcome struct {
		claim repo.ProgramOutputRecoveryClaim
		found bool
		err   error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := 0; i < 2; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			c, f, e := r.ClaimProgramOutputRecovery(context.Background())
			results <- outcome{c, f, e}
		}()
	}
	close(start)
	workers.Wait()
	close(results)
	var first repo.ProgramOutputRecoveryClaim
	claims := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.found {
			claims++
			first = result.claim
		}
	}
	require.Equal(t, 1, claims)
	require.Equal(t, 1, first.Attempt)
	dueRecovery(t, store, task.ID) // Isolated fixture simulates claim expiry, not a system-clock change.
	second, found, err := repo.NewSourceStreamRepo(store).ClaimProgramOutputRecovery(context.Background())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, 2, second.Attempt)
	require.NotEqual(t, first.Token, second.Token)
	accepted, err := r.FailProgramOutputRecovery(context.Background(), first, true, repo.ProgramRecoveryInvalid)
	require.NoError(t, err)
	require.False(t, accepted)
	_, err = r.LoadProgramOutputRecovery(context.Background(), first)
	require.ErrorIs(t, err, repo.ErrConflict)
	accepted, err = r.FailProgramOutputRecovery(context.Background(), second, false, repo.ProgramRecoveryUnavailable)
	require.NoError(t, err)
	require.True(t, accepted)
	state := recoveryState(t, store, task.ID)
	require.Equal(t, "pending", state.RecoveryState)
	require.Equal(t, 2, state.RecoveryAttempts)
	require.Nil(t, state.RecoveryToken)
}

func TestProgramOutputRecoveryFiniteBudgetSurvivesNewRepo(t *testing.T) {
	store, r, owner, stream, version := recoveryFixture(t)
	task := freezeRecoveryTask(t, r, owner, stream, version, "bounded-retry")
	for attempt := 1; attempt <= repo.ProgramOutputRecoveryMaxAttempts; attempt++ {
		dueRecovery(t, store, task.ID)
		r = repo.NewSourceStreamRepo(store)
		claim, found, err := r.ClaimProgramOutputRecovery(context.Background())
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, attempt, claim.Attempt)
		accepted, err := r.FailProgramOutputRecovery(context.Background(), claim, false, repo.ProgramRecoveryUnavailable)
		require.NoError(t, err)
		require.True(t, accepted)
	}
	state := recoveryState(t, store, task.ID)
	require.Equal(t, "quarantined", state.RecoveryState)
	require.Equal(t, repo.ProgramOutputRecoveryMaxAttempts, state.RecoveryAttempts)
	dueRecovery(t, store, task.ID)
	_, found, err := r.ClaimProgramOutputRecovery(context.Background())
	require.NoError(t, err)
	require.False(t, found)
	stored, err := r.Task(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, task.Output, stored.Output)
	require.Equal(t, "partial-failure", stored.Status)
	run, err := r.Run(context.Background(), task.ID)
	require.NoError(t, err)
	require.Contains(t, run.Summary, "retryExhausted")
	require.NoError(t, r.FinishProgramV2Task(context.Background(), task.ID, "succeeded", `{"phase":"uns-commit"}`, 1))
	require.Equal(t, "completed", recoveryState(t, store, task.ID).RecoveryState, "successful explicit output replay removes quarantine without rerunning a script")
}

func TestProgramOutputRecoveryOversizedResultDoesNotBlockNext(t *testing.T) {
	store, r, owner, stream, version := recoveryFixture(t)
	bad := freezeRecoveryTask(t, r, owner, stream, version, "a-oversized")
	good := freezeRecoveryTask(t, r, owner, stream, version, "b-valid")
	require.NoError(t, store.UnsDB().Exec(`UPDATE public.uns_stream_task SET output=jsonb_build_object('data',repeat('x',?)) WHERE id=?`, repo.MaxFrozenProgramOutputBytes+1, bad.ID).Error)
	dueRecovery(t, store, bad.ID)
	dueRecovery(t, store, good.ID)
	claim, found, err := r.ClaimProgramOutputRecovery(context.Background())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, bad.ID, claim.Run.ID)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	loaded, err := r.LoadProgramOutputRecovery(context.Background(), claim)
	runtime.ReadMemStats(&after)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	require.Empty(t, loaded.Output, "oversized text is never projected into Go")
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(repo.MaxFrozenProgramOutputBytes/2), "rejecting the oversized row must not allocate a full result buffer in Backend")
	accepted, err := r.FailProgramOutputRecovery(context.Background(), claim, true, repo.ProgramRecoveryInvalid)
	require.NoError(t, err)
	require.True(t, accepted)
	next, found, err := r.ClaimProgramOutputRecovery(context.Background())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, good.ID, next.Run.ID)
	loaded, err = r.LoadProgramOutputRecovery(context.Background(), next)
	require.NoError(t, err)
	require.Equal(t, good.Output, loaded.Output)
}

func TestProgramOutputRecoveryMigrationPreservesClaimsAndUnownedRows(t *testing.T) {
	store, r, owner, stream, version := recoveryFixture(t)
	task := freezeRecoveryTask(t, r, owner, stream, version, "backfill-owned")
	require.NoError(t, store.UnsDB().Table("public.uns_stream_run_owner").Where("run_id=?", task.ID).Updates(map[string]any{"recovery_state": "waiting", "recovery_after": 0}).Error)
	legacy := ownerRun(stream, version, "unowned-legacy")
	_, err := r.AdmitProgramV2Run(context.Background(), legacy, true)
	require.NoError(t, err)
	_, err = r.PutProgramV2Task(context.Background(), repo.StreamTask{ID: legacy.ID, StreamID: stream.ID, VersionID: version.ID, EntryID: legacy.EntryID, Model: "program", Output: `{"outputs":[]}`, ReceivedAt: time.Now().UnixMilli()}, legacy)
	require.NoError(t, err)
	migration, err := os.ReadFile("migrations/versions/sink/V3.0.0_014__program_output_recovery.sql")
	require.NoError(t, err)
	require.NoError(t, store.UnsDB().Transaction(func(tx *gorm.DB) error { return tx.Exec(string(migration)).Error }))
	require.Equal(t, "pending", recoveryState(t, store, task.ID).RecoveryState)
	var adopted int64
	require.NoError(t, store.UnsDB().Table("public.uns_stream_run_owner").Where("run_id=?", legacy.ID).Count(&adopted).Error)
	require.Zero(t, adopted)
	claim, found, err := r.ClaimProgramOutputRecovery(context.Background())
	require.NoError(t, err)
	require.True(t, found)
	require.NoError(t, store.Migrate(context.Background()))
	require.Equal(t, claim.Token, *recoveryState(t, store, task.ID).RecoveryToken)
}

type outputRecoveryQueryCapture struct {
	logger.Interface
	sql string
}

func (c *outputRecoveryQueryCapture) Trace(_ context.Context, _ time.Time, query func() (string, int64), _ error) {
	sql, _ := query()
	if strings.Contains(sql, "SKIP LOCKED") && strings.Contains(sql, "binding.recovery_state") {
		c.sql = sql
	}
}

func TestProgramOutputRecoveryQueueIndexUsesProductionQuery(t *testing.T) {
	store, r, owner, stream, version := recoveryFixture(t)
	task := freezeRecoveryTask(t, r, owner, stream, version, "pending-output")
	for first := 1; first <= 10000; first += 100 {
		require.NoError(t, store.UnsDB().Exec(`INSERT INTO public.uns_stream_run(id,stream_id,version_id,entry_id,model,status,created_at)
			SELECT 'done-'||n,?,?,'request','program','succeeded',? FROM generate_series(?::int,?::int)n`, stream.ID, version.ID, time.Now().UnixMilli(), first, first+99).Error)
	}
	require.NoError(t, store.UnsDB().Exec(`INSERT INTO public.uns_stream_run_owner(run_id,owner_id,recovery_state)
		SELECT id,?::uuid,'completed' FROM public.uns_stream_run WHERE id LIKE 'done-%'`, owner.ID()).Error)
	require.NoError(t, store.UnsDB().Exec(`ANALYZE public.uns_stream_run,public.uns_stream_run_owner`).Error)
	dueRecovery(t, store, task.ID)
	capture := &outputRecoveryQueryCapture{Interface: logger.Discard}
	_, found, err := repo.NewSourceStreamRepo(store.UnsDB().Session(&gorm.Session{Logger: capture})).ClaimProgramOutputRecovery(context.Background())
	require.NoError(t, err)
	require.True(t, found)
	require.NotEmpty(t, capture.sql)
	require.NoError(t, store.UnsDB().Table("public.uns_stream_run_owner").Where("run_id=?", task.ID).Updates(map[string]any{"recovery_state": "pending", "recovery_token": nil, "recovery_after": 0}).Error)
	var plan string
	require.NoError(t, store.UnsDB().Raw("EXPLAIN (ANALYZE,FORMAT JSON) "+capture.sql).Scan(&plan).Error)
	require.Contains(t, plan, "idx_program_output_recovery_due", "completed history must not force the due queue to scan the entire association table")
}

func TestProgramOutputRecoveryFinalClaimExpiryDoesNotResetBudget(t *testing.T) {
	store, r, owner, stream, version := recoveryFixture(t)
	task := freezeRecoveryTask(t, r, owner, stream, version, "crash-budget")
	for attempt := 1; attempt <= repo.ProgramOutputRecoveryMaxAttempts; attempt++ {
		dueRecovery(t, store, task.ID)
		claim, found, err := repo.NewSourceStreamRepo(store).ClaimProgramOutputRecovery(context.Background())
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, attempt, claim.Attempt)
		require.False(t, claim.Quarantined)
		// No completion/failure callback: simulate a worker exiting after claim.
	}
	dueRecovery(t, store, task.ID)
	claim, found, err := repo.NewSourceStreamRepo(store).ClaimProgramOutputRecovery(context.Background())
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, claim.Quarantined)
	require.Equal(t, repo.ProgramOutputRecoveryMaxAttempts, claim.Attempt)
	_, found, err = r.ClaimProgramOutputRecovery(context.Background())
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, "quarantined", recoveryState(t, store, task.ID).RecoveryState)
}

func TestProgramOutputRecoveryAcceptsBoundedJSONBWhitespaceExpansion(t *testing.T) {
	store, r, owner, stream, version := recoveryFixture(t)
	ctx := context.Background()
	run := ownerRun(stream, version, "storage-expansion")
	_, err := r.AdmitOwnedProgramV2Run(ctx, run, true, owner.ID(), nil)
	require.NoError(t, err)
	// ~6 MiB encoded input becomes ~9 MiB JSONB text, without raising the
	// script's 8 MiB encoded-result allowance.
	output := `{"outputs":[],"data":[` + strings.Repeat("0,", 3000000) + `0]}`
	task, err := r.PutProgramV2Task(ctx, repo.StreamTask{ID: run.ID, StreamID: run.StreamID, VersionID: run.VersionID, EntryID: run.EntryID, Model: "program", Output: output, ReceivedAt: time.Now().UnixMilli()}, run)
	require.NoError(t, err)
	require.Greater(t, len(task.Output), repo.MaxFrozenProgramOutputBytes/2)
	require.Less(t, len(task.Output), repo.MaxFrozenProgramOutputBytes)
	dueRecovery(t, store, task.ID)
	claim, found, err := r.ClaimProgramOutputRecovery(ctx)
	require.NoError(t, err)
	require.True(t, found)
	loaded, err := r.LoadProgramOutputRecovery(ctx, claim)
	require.NoError(t, err)
	require.Equal(t, task.Output, loaded.Output, "recovery must retain exactly the persisted JSONB representation")
}

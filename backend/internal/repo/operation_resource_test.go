package repo_test

// 独立 PG 验证原子注册、冲突、草稿隔离、停用、回滚与跨版本幂等预约。
import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"backend/internal/operationcontract"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func operationManifest(id string) streamexec.ProgramManifest {
	manifest := v2RequestManifest()
	manifest.Entries[0].Control = &operationcontract.Resource{Code: id, Name: "启动", ContractVersion: "1.0", SideEffect: "write", Idempotency: "required", TimeoutMs: 1000,
		InputSchema: json.RawMessage(`{"type":"object"}`), OutputSchema: json.RawMessage(`{"type":"object"}`)}
	return manifest
}

func stageOperation(t *testing.T, r *repo.SourceStreamRepo, stream repo.Stream, manifest streamexec.ProgramManifest, tag string) (repo.Stream, repo.StreamVersion) {
	t.Helper()
	stream.Name, stream.Model = "Operations "+tag, "program"
	stream.Document = fmt.Sprintf(`{"script":%q}`, tag)
	stream, err := r.SaveProgramV2(context.Background(), stream, stream.Revision, manifest)
	require.NoError(t, err)
	stream, version, err := r.StageProgramV2(context.Background(), stream.ID, stream.Revision, 0, stream.Document, manifest, []repo.StreamSourceSnapshot{})
	require.NoError(t, err)
	return stream, version
}

func TestOperationResourceLifecycleAndReservation(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	ctx := context.Background()
	manifest := operationManifest("device.start")
	stream, first := stageOperation(t, r, repo.Stream{}, manifest, "first")
	_, _, err := r.Operation(ctx, "device.start")
	require.Error(t, err, "a staged candidate must not publish operation metadata")
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, first.ID)
	require.NoError(t, err)
	row, operation, err := r.Operation(ctx, "device.start")
	require.NoError(t, err)
	require.Equal(t, "active", operation.Status)
	require.Equal(t, strconv.FormatInt(first.ID, 10), operation.Binding.DeploymentVersion)
	reservation, created, err := r.ClaimOperationInvocation(ctx, "device.start", "saved-intent", strings.Repeat("a", 64), 1, 10)
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, first.ID, reservation.VersionID)
	var wg sync.WaitGroup
	createdCount := 0
	var createdMu sync.Mutex
	claimErrors := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, won, claimErr := r.ClaimOperationInvocation(ctx, "device.start", "concurrent-intent", strings.Repeat("c", 64), 1, 10)
			claimErrors <- claimErr
			if won {
				createdMu.Lock()
				createdCount++
				createdMu.Unlock()
			}
		}()
	}
	wg.Wait()
	close(claimErrors)
	for claimErr := range claimErrors {
		require.NoError(t, claimErr)
	}
	require.Equal(t, 1, createdCount, "concurrent callers must reserve one physical execution")

	other, conflicting := stageOperation(t, r, repo.Stream{}, manifest, "other")
	_, err = r.ActivateStagedProgramV2(ctx, other.ID, other.Revision, conflicting.ID)
	require.ErrorIs(t, err, repo.ErrConflict)
	after, _, err := r.Operation(ctx, "device.start")
	require.NoError(t, err)
	require.Equal(t, row.Revision, after.Revision)
	require.JSONEq(t, string(row.Data), string(after.Data))
	stillDraft, err := r.Stream(ctx, other.ID)
	require.NoError(t, err)
	require.Zero(t, stillDraft.ActiveVersion)
	require.Equal(t, conflicting.ID, stillDraft.PendingVersion)

	manifest.Entries[0].Control.Name = "启动设备（新名称）"
	stream, second := stageOperation(t, r, stream, manifest, "second")
	_, unchanged, err := r.Operation(ctx, "device.start")
	require.NoError(t, err)
	require.Equal(t, "启动", unchanged.Name, "draft edits do not change active contracts")
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, second.ID)
	require.NoError(t, err)
	_, operation, err = r.Operation(ctx, "device.start")
	require.NoError(t, err)
	require.Equal(t, "启动设备（新名称）", operation.Name)
	replayed, created, err := r.ClaimOperationInvocation(ctx, "device.start", "saved-intent", strings.Repeat("a", 64), 1, 10)
	require.NoError(t, err)
	require.False(t, created)
	// JSONB preserves the contract, not its textual key order or spacing.
	require.JSONEq(t, reservation.Contract, replayed.Contract)
	replayed.Contract = reservation.Contract
	require.Equal(t, reservation, replayed, "same intent must keep its original deployment")
	_, _, err = r.ClaimOperationInvocation(ctx, "device.start", "saved-intent", strings.Repeat("b", 64), 1, 10)
	require.ErrorIs(t, err, repo.ErrControlIdempotencyConflict)

	manifest.Entries[0].Control.InputSchema = json.RawMessage(`{"type":"string"}`)
	stream, incompatible := stageOperation(t, r, stream, manifest, "incompatible")
	_, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, incompatible.ID)
	require.ErrorIs(t, err, repo.ErrConflict)
	_, operation, err = r.Operation(ctx, "device.start")
	require.NoError(t, err)
	require.Equal(t, strconv.FormatInt(second.ID, 10), operation.Binding.DeploymentVersion)

	stream, err = r.DisableStream(ctx, stream.ID, stream.Revision, false)
	require.NoError(t, err)
	_, operation, err = r.Operation(ctx, "device.start")
	require.NoError(t, err)
	require.Equal(t, "disabled", operation.Status)
	_, _, err = r.ClaimOperationInvocation(ctx, "device.start", "new-intent", strings.Repeat("a", 64), 1, 10)
	require.ErrorIs(t, err, repo.ErrControlInactive)
	_, created, err = r.ClaimOperationInvocation(ctx, "device.start", "saved-intent", strings.Repeat("a", 64), 1, 10)
	require.NoError(t, err)
	require.False(t, created, "reading an existing reservation is not a new operation")

	first, err = r.Version(ctx, stream.ID, first.ID)
	require.NoError(t, err)
	frozenManifest, err := first.ProgramV2Manifest()
	require.NoError(t, err)
	frozenSources, err := first.Sources()
	require.NoError(t, err)
	frozenBindings, err := first.ProgramV2Bindings()
	require.NoError(t, err)
	stream, rollback, err := r.StageProgramV2(ctx, stream.ID, stream.Revision, first.ID, first.Document, frozenManifest, frozenSources, frozenBindings)
	require.NoError(t, err)
	stream, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, rollback.ID)
	require.NoError(t, err)
	_, operation, err = r.Operation(ctx, "device.start")
	require.NoError(t, err)
	require.Equal(t, "active", operation.Status)
	require.Equal(t, strconv.FormatInt(first.ID, 10), operation.Binding.DeploymentVersion)
	// A corrupted private binding must not turn a non-exposed entry into an API.
	require.NoError(t, store.UnsDB().Exec(`UPDATE public.uns_meta SET data=jsonb_set(data,'{binding,entryId}','"missing"'::jsonb) WHERE kind='control' AND id='device.start'`).Error)
	_, _, err = r.ClaimOperationInvocation(ctx, "device.start", "bad-binding", strings.Repeat("a", 64), 1, 10)
	require.ErrorIs(t, err, repo.ErrControlInactive)
}

func TestOperationResourceAtomicRegistrationAndRemoval(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	ctx := context.Background()
	manifest := operationManifest("device.start")
	stream, version := stageOperation(t, r, repo.Stream{}, manifest, "atomic")
	stream, err := r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, version.ID)
	require.NoError(t, err)
	require.NoError(t, store.UnsDB().Exec(`CREATE FUNCTION public.reject_operation_fixture() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.kind='control' AND NEW.id='device.fail' THEN RAISE EXCEPTION 'fixture rejection'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_operation_fixture BEFORE INSERT ON public.uns_meta FOR EACH ROW EXECUTE FUNCTION public.reject_operation_fixture();`).Error)
	second := operationManifest("device.fail").Entries[0]
	second.ID = "fail"
	manifest.Entries = append(manifest.Entries, second)
	stream, candidate := stageOperation(t, r, stream, manifest, "rejected")
	_, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, candidate.ID)
	require.Error(t, err)
	_, retained, err := r.Operation(ctx, "device.start")
	require.NoError(t, err)
	require.Equal(t, "active", retained.Status)
	require.Equal(t, strconv.FormatInt(version.ID, 10), retained.Binding.DeploymentVersion)
	_, _, err = r.Operation(ctx, "device.fail")
	require.Error(t, err)
	stored, err := r.Stream(ctx, stream.ID)
	require.NoError(t, err)
	require.Equal(t, version.ID, stored.ActiveVersion)
	stream, empty := stageOperation(t, r, stored, v2RequestManifest(), "removed")
	_, err = r.ActivateStagedProgramV2(ctx, stream.ID, stream.Revision, empty.ID)
	require.NoError(t, err)
	_, retained, err = r.Operation(ctx, "device.start")
	require.NoError(t, err)
	require.Equal(t, "disabled", retained.Status)
	rows, total, err := r.Operations(ctx, "device", "", 1, 50)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
}

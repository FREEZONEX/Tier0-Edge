package repo_test

// 验证操作日志的 CAS、重入保护、请求哈希冲突及未知结果不再派发。
import (
	"context"
	"strings"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func programV2Fixture(t *testing.T) (*repo.SourceStreamRepo, *repo.Store, repo.Stream, repo.StreamVersion) {
	t.Helper()
	store, _ := testkit.NewTestDB(t)
	r := repo.NewSourceStreamRepo(store)
	stream := repo.Stream{ID: 7001, Name: "program-v2", Model: "program", Document: `{}`, Revision: 1, Status: "enabled", ActiveVersion: 7002, CreatedAt: time.Now().UnixMilli()}
	require.NoError(t, store.CommonDB().Create(&stream).Error)
	version := repo.StreamVersion{ID: 7002, StreamID: stream.ID, SourceManifest: "[]", ProgramManifest: `{"sources":[],"topics":[],"entries":[{"id":"tick","kind":"interval","intervalMs":1000}]}`, Document: `{}`, ActivatedAt: time.Now().UnixMilli(), CreatedAt: time.Now().UnixMilli()}
	require.NoError(t, store.CommonDB().Create(&version).Error)
	return r, store, stream, version
}

func TestProgramV2EffectJournal(t *testing.T) {
	r, _, stream, version := programV2Fixture(t)
	ctx := context.Background()
	reserved, err := r.ReserveRun(ctx, repo.StreamRun{ID: "effect-run", StreamID: stream.ID, VersionID: version.ID, EntryID: "tick", Model: "program", Status: "processing", Summary: "{}", CreatedAt: time.Now().UnixMilli()})
	require.NoError(t, err)
	require.True(t, reserved)
	effect := repo.StreamProgramEffect{RunID: "effect-run", EntryID: "tick", CallID: "call-1", OpID: "source.write", RequestHash: strings.Repeat("a", 64)}
	item, err := r.ReserveProgramEffect(ctx, effect)
	require.NoError(t, err)
	require.Equal(t, "pending", item.Status)
	require.Equal(t, "null", item.Result)
	require.ErrorIs(t, r.FinishProgramEffect(ctx, effect.RunID, effect.EntryID, effect.CallID, effect.RequestHash, "completed", `{}`), repo.ErrConflict)
	item, claimed, err := r.ClaimProgramEffect(ctx, effect.RunID, effect.EntryID, effect.CallID, effect.RequestHash)
	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, "dispatching", item.Status)
	_, claimed, err = r.ClaimProgramEffect(ctx, effect.RunID, effect.EntryID, effect.CallID, effect.RequestHash)
	require.NoError(t, err)
	require.False(t, claimed)
	require.NoError(t, r.FinishProgramEffect(ctx, effect.RunID, effect.EntryID, effect.CallID, effect.RequestHash, "unknown", `{"reason":"timeout"}`))
	item, claimed, err = r.ClaimProgramEffect(ctx, effect.RunID, effect.EntryID, effect.CallID, effect.RequestHash)
	require.NoError(t, err)
	require.False(t, claimed)
	require.Equal(t, "unknown", item.Status)
	effect.RequestHash = strings.Repeat("b", 64)
	_, err = r.ReserveProgramEffect(ctx, effect)
	require.ErrorIs(t, err, repo.ErrConflict)
	_, _, err = r.ClaimProgramEffect(ctx, effect.RunID, effect.EntryID, effect.CallID, effect.RequestHash)
	require.ErrorIs(t, err, repo.ErrConflict)
	_, err = r.ReserveProgramEffect(ctx, repo.StreamProgramEffect{RunID: "effect-run", EntryID: "other", CallID: "x", OpID: "source.write", RequestHash: strings.Repeat("a", 64)})
	require.ErrorIs(t, err, repo.ErrConflict)
	late := repo.StreamProgramEffect{RunID: "effect-run", EntryID: "tick", CallID: "late", OpID: "source.write", RequestHash: strings.Repeat("d", 64)}
	_, err = r.ReserveProgramEffect(ctx, late)
	require.NoError(t, err)
	require.NoError(t, r.FinishRun(ctx, late.RunID, "failed", `{}`, 1))
	_, claimed, err = r.ClaimProgramEffect(ctx, late.RunID, late.EntryID, late.CallID, late.RequestHash)
	require.ErrorIs(t, err, repo.ErrConflict)
	require.False(t, claimed)
}

func TestProgramV2EffectTerminalStates(t *testing.T) {
	r, _, stream, version := programV2Fixture(t)
	ctx := context.Background()
	for _, status := range []string{"completed", "rejected"} {
		runID := "effect-" + status
		_, err := r.ReserveRun(ctx, repo.StreamRun{ID: runID, StreamID: stream.ID, VersionID: version.ID, EntryID: "tick", Model: "program", Status: "processing", Summary: "{}", CreatedAt: time.Now().UnixMilli()})
		require.NoError(t, err)
		item := repo.StreamProgramEffect{RunID: runID, EntryID: "tick", CallID: "call", OpID: "source.invoke", RequestHash: strings.Repeat("c", 64)}
		_, err = r.ReserveProgramEffect(ctx, item)
		require.NoError(t, err)
		_, claimed, err := r.ClaimProgramEffect(ctx, runID, "tick", "call", item.RequestHash)
		require.NoError(t, err)
		require.True(t, claimed)
		require.NoError(t, r.FinishProgramEffect(ctx, runID, "tick", "call", item.RequestHash, status, `{"ok":true}`))
		require.NoError(t, r.FinishProgramEffect(ctx, runID, "tick", "call", item.RequestHash, status, `{"ok":false}`))
		stored, err := r.ReserveProgramEffect(ctx, item)
		require.NoError(t, err)
		require.JSONEq(t, `{"ok":true}`, stored.Result)
		_, claimed, err = r.ClaimProgramEffect(ctx, runID, "tick", "call", item.RequestHash)
		require.NoError(t, err)
		require.False(t, claimed)
	}
}

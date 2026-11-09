package sourcestream

// 独立 Common/UNS PostgreSQL 库捕获同库夹具无法暴露的错误连接回退。
import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestFailProgramV2RunUsesUNSJournalWithSeparateDatabases(t *testing.T) {
	ctx := context.Background()
	previousCommon, previousUNS := repo.GetCommonConn(ctx), repo.GetUnsConn(ctx)
	common, _ := testkit.NewTestDB(t)
	uns, _ := testkit.NewTestDB(t)
	// Registered last so fallback is restored before either disposable DB closes.
	t.Cleanup(func() { repo.SetStoreForTest(previousCommon, previousUNS) })
	repo.SetStoreForTest(common.CommonDB(), uns.UnsDB())
	require.NoError(t, common.CommonDB().Exec("DROP TABLE public.uns_stream_effect").Error)
	var commonMissing bool
	require.NoError(t, common.CommonDB().Raw("SELECT to_regclass('public.uns_stream_effect') IS NULL").Scan(&commonMissing).Error)
	require.True(t, commonMissing)
	r := repo.NewSourceStreamRepo(ctx)
	s := &Service{}
	for _, item := range []struct {
		id, effectStatus, want string
		cancel                 bool
		cause                  error
	}{
		{id: "separate-failed", want: "failed"},
		{id: "separate-dispatching", effectStatus: "dispatching", want: "unknown"},
		{id: "separate-unknown", effectStatus: "unknown", want: "unknown"},
		{id: "separate-cancelled", want: "failed", cancel: true},
		{id: "separate-timeout", want: "failed", cause: streamexec.ErrTimeout},
		{id: "separate-uncertain", want: "unknown", cause: ErrUnknownResult},
	} {
		t.Run(item.id, func(t *testing.T) {
			run := repo.StreamRun{ID: item.id, StreamID: 7001, VersionID: 7002, EntryID: "request", Model: "program", Status: "processing", Summary: `{}`}
			reserved, err := r.ReserveRun(ctx, run)
			require.NoError(t, err)
			require.True(t, reserved)
			if item.effectStatus != "" {
				effect := repo.StreamProgramEffect{RunID: run.ID, EntryID: run.EntryID, CallID: "1", OpID: "source.request", RequestHash: strings.Repeat("a", 64)}
				_, err = r.ReserveProgramEffect(ctx, effect)
				require.NoError(t, err)
				_, claimed, err := r.ClaimProgramEffect(ctx, run.ID, run.EntryID, effect.CallID, effect.RequestHash)
				require.NoError(t, err)
				require.True(t, claimed)
				if item.effectStatus == "unknown" {
					require.NoError(t, r.FinishProgramEffect(ctx, run.ID, run.EntryID, effect.CallID, effect.RequestHash, "unknown", `{}`))
				}
			}
			callCtx := ctx
			if item.cancel {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				callCtx = cancelled
			}
			cause := errors.New("test program execution failed")
			if item.cause != nil {
				cause = item.cause
			}
			err = s.failProgramV2Run(callCtx, run, cause)
			require.ErrorIs(t, err, cause)
			if item.want == "failed" {
				require.ErrorIs(t, err, ErrExecutionFailed)
				require.NotErrorIs(t, err, ErrUnknownResult)
				require.EqualError(t, err, ErrExecutionFailed.Error(), "do not expose the original cause in the public message")
			} else {
				require.ErrorIs(t, err, ErrUnknownResult)
				require.NotErrorIs(t, err, ErrExecutionFailed)
				require.EqualError(t, err, ErrUnknownResult.Error(), "unknown results also redact the underlying cause")
			}
			stored, err := r.Run(ctx, run.ID)
			require.NoError(t, err)
			require.Equal(t, item.want, stored.Status)
			var summary map[string]any
			require.NoError(t, json.Unmarshal([]byte(stored.Summary), &summary))
			require.Equal(t, "execution", summary["phase"])
			require.Equal(t, "SourceStream.executionFailed", summary["error"])
			require.Equal(t, executionFailureCode(cause), summary["errorCode"])
			require.Equal(t, false, summary["operationRepeated"])
		})
	}
	var unsRuns, commonRuns, unsEffects int64
	require.NoError(t, uns.UnsDB().Model(&repo.StreamRun{}).Count(&unsRuns).Error)
	require.NoError(t, common.CommonDB().Model(&repo.StreamRun{}).Count(&commonRuns).Error)
	require.NoError(t, uns.UnsDB().Model(&repo.StreamProgramEffect{}).Count(&unsEffects).Error)
	require.Equal(t, int64(6), unsRuns)
	require.Zero(t, commonRuns)
	require.Equal(t, int64(2), unsEffects)
	// A failed database finalization is not proof of a terminal execution.
	// Keep it retryable without ever authorizing another script evaluation.
	require.NoError(t, uns.UnsDB().Exec("ALTER TABLE public.uns_stream_run RENAME TO test_unavailable_stream_run").Error)
	cause := errors.New("test finalization unavailable")
	err := s.failProgramV2Run(ctx, repo.StreamRun{ID: "not-recorded", EntryID: "request", Summary: `{}`}, cause)
	require.ErrorIs(t, err, cause)
	require.NotErrorIs(t, err, ErrExecutionFailed)
	require.NotErrorIs(t, err, ErrUnknownResult)
}

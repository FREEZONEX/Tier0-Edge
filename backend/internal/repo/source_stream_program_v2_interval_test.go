package repo_test

// 验证定时游标与发布意图事务持久化，重启不重复创建历史 slot。
import (
	"context"
	"testing"

	"backend/internal/repo"
	"github.com/stretchr/testify/require"
)

func TestProgramV2IntervalCursorAndOutbox(t *testing.T) {
	r, store, stream, version := programV2Fixture(t)
	ctx := context.Background()
	request := repo.ProgramIntervalSchedule{StreamID: stream.ID, VersionID: version.ID, EntryID: "tick", IntervalMs: 1000, FirstDueAt: 1000, Now: 3500, MaxSlots: 2}
	items, err := r.ScheduleProgramInterval(ctx, request)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.EqualValues(t, 1000, items[0].DueAt)
	require.EqualValues(t, 2000, items[1].DueAt)
	restarted := repo.NewSourceStreamRepo(store)
	remaining, err := restarted.ScheduleProgramInterval(ctx, request)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.EqualValues(t, 3000, remaining[0].DueAt)
	again, err := restarted.ScheduleProgramInterval(ctx, request)
	require.NoError(t, err)
	require.Empty(t, again)
	pending, err := r.PendingProgramIntervals(ctx, 200)
	require.NoError(t, err)
	require.Len(t, pending, 3)
	require.NoError(t, r.MarkProgramIntervalSent(ctx, items[0].ID))
	require.NoError(t, r.MarkProgramIntervalSent(ctx, items[0].ID))
	pending, err = r.PendingProgramIntervals(ctx, 200)
	require.NoError(t, err)
	require.Len(t, pending, 2)
	require.NoError(t, store.CommonDB().Model(&repo.Stream{}).Where("id=?", stream.ID).Update("status", "disabled").Error)
	pending, err = r.PendingProgramIntervals(ctx, 200)
	require.NoError(t, err)
	require.Empty(t, pending)
	_, err = r.ScheduleProgramInterval(ctx, request)
	require.ErrorIs(t, err, repo.ErrConflict)
}

func TestProgramV2IntervalRejectsInvalidEntry(t *testing.T) {
	r, _, stream, version := programV2Fixture(t)
	request := repo.ProgramIntervalSchedule{StreamID: stream.ID, VersionID: version.ID, EntryID: "other", IntervalMs: 1000, FirstDueAt: 1000, Now: 2000, MaxSlots: 1}
	_, err := r.ScheduleProgramInterval(context.Background(), request)
	require.ErrorIs(t, err, repo.ErrConflict)
	request.EntryID = "tick"
	request.IntervalMs = 2000
	_, err = r.ScheduleProgramInterval(context.Background(), request)
	require.ErrorIs(t, err, repo.ErrConflict)
	request.IntervalMs = 1000
	request.MaxSlots = 101
	_, err = r.ScheduleProgramInterval(context.Background(), request)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
}

// 过期意图是独立终态，不能把主动丢弃记录成已经收到发布确认。
func TestProgramV2IntervalDiscardIsNotPublishAcknowledgement(t *testing.T) {
	r, store, stream, version := programV2Fixture(t)
	ctx := context.Background()
	items, err := r.ScheduleProgramInterval(ctx, repo.ProgramIntervalSchedule{
		StreamID: stream.ID, VersionID: version.ID, EntryID: "tick", IntervalMs: 1000,
		FirstDueAt: 1000, Now: 2000, MaxSlots: 2,
	})
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.NoError(t, r.MarkProgramIntervalDiscarded(ctx, items[0].ID))
	require.NoError(t, r.MarkProgramIntervalDiscarded(ctx, items[0].ID))
	require.ErrorIs(t, r.MarkProgramIntervalSent(ctx, items[0].ID), repo.ErrConflict)
	require.NoError(t, r.MarkProgramIntervalSent(ctx, items[1].ID))
	require.ErrorIs(t, r.MarkProgramIntervalDiscarded(ctx, items[1].ID), repo.ErrConflict)
	var discarded repo.StreamProgramIntervalOutbox
	require.NoError(t, store.CommonDB().Where("id=?", items[0].ID).Take(&discarded).Error)
	require.EqualValues(t, -1, discarded.SentAt)
	require.JSONEq(t, items[0].Payload, discarded.Payload)
	require.Equal(t, items[0].DueAt, discarded.DueAt)
	pending, err := r.PendingProgramIntervals(ctx, 200)
	require.NoError(t, err)
	require.Empty(t, pending)
}

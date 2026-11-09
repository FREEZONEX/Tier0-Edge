package repo_test

// PostgreSQL 真实事务验证入口隔离、CAS、幂等和回滚后游标不前移。
import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestProgramCheckpointAtomicCASAndIsolation(t *testing.T) {
	store, dsn := testkit.NewTestDB(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	require.NoError(t, err)
	defer conn.Close(ctx)
	r := repo.NewSourceStreamRepo(store)
	empty, err := r.ProgramCheckpoint(ctx, 10, "poll")
	require.NoError(t, err)
	require.JSONEq(t, `null`, string(empty.Value))
	require.Zero(t, empty.Revision)
	change := repo.ProgramCheckpointCommit{StreamID: 10, EntryID: "poll", RunID: "run1", Digest: strings.Repeat("a", 64), Value: json.RawMessage(`{"page":1}`)}
	apply := func(change repo.ProgramCheckpointCommit, commit bool) (bool, error) {
		tx, err := conn.Begin(ctx)
		require.NoError(t, err)
		defer tx.Rollback(ctx)
		created, finish, err := repo.LockProgramCheckpoint(ctx, tx, change)
		if err != nil || !created {
			return created, err
		}
		if err = finish(); err != nil {
			return false, err
		}
		if commit {
			err = tx.Commit(ctx)
		}
		return created, err
	}
	created, err := apply(change, false)
	require.NoError(t, err)
	require.True(t, created)
	committed, err := r.ProgramCheckpointCommitted(ctx, change.RunID, change.Digest)
	require.NoError(t, err)
	require.False(t, committed)
	empty, err = r.ProgramCheckpoint(ctx, 10, "poll")
	require.NoError(t, err)
	require.Zero(t, empty.Revision)
	created, err = apply(change, true)
	require.NoError(t, err)
	require.True(t, created)
	committed, err = r.ProgramCheckpointCommitted(ctx, change.RunID, change.Digest)
	require.NoError(t, err)
	require.True(t, committed)
	checkpoint, err := r.ProgramCheckpoint(ctx, 10, "poll")
	require.NoError(t, err)
	require.EqualValues(t, 1, checkpoint.Revision)
	require.JSONEq(t, `{"page":1}`, string(checkpoint.Value))
	created, err = apply(change, true)
	require.NoError(t, err)
	require.False(t, created)
	change.Digest = strings.Repeat("b", 64)
	_, err = r.ProgramCheckpointCommitted(ctx, change.RunID, change.Digest)
	require.ErrorIs(t, err, repo.ErrConflict)
	_, err = apply(change, true)
	require.ErrorIs(t, err, repo.ErrConflict)
	change.RunID = "run2"
	_, err = apply(change, true)
	require.ErrorIs(t, err, repo.ErrConflict)
	change.ExpectedRevision = 1
	change.Value = json.RawMessage(`{"page":2}`)
	created, err = apply(change, true)
	require.NoError(t, err)
	require.True(t, created)
	checkpoint, err = repo.NewSourceStreamRepo(store).ProgramCheckpoint(ctx, 10, "poll")
	require.NoError(t, err)
	require.EqualValues(t, 2, checkpoint.Revision)
	for _, ref := range []struct {
		id    int64
		entry string
	}{{10, "other"}, {11, "poll"}} {
		cp, err := r.ProgramCheckpoint(ctx, ref.id, ref.entry)
		require.NoError(t, err)
		require.Zero(t, cp.Revision)
	}
	_, err = r.ProgramCheckpoint(ctx, 0, "poll")
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	change.Value = json.RawMessage(`"` + strings.Repeat("a", repo.MaxProgramCheckpointBytes) + `"`)
	_, err = apply(change, true)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
}

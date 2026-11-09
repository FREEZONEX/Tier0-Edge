package repo_test

// 真实 PostgreSQL 覆盖 GORM 游标 CAS、入口隔离、事务回滚、重放和批次凭证。
import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestProgramCheckpointGORMAtomicCAS(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	change := repo.ProgramCheckpointCommit{StreamID: 10, EntryID: "poll", RunID: "gorm-run-1", Digest: strings.Repeat("a", 64), Value: json.RawMessage(`{"page":1}`)}
	abort := errors.New("abort outputs")
	apply := func(input repo.ProgramCheckpointCommit, rollback bool) (bool, error) {
		created := false
		err := store.UnsDB().Transaction(func(tx *gorm.DB) error {
			var finish func() error
			var err error
			created, finish, err = repo.LockProgramCheckpointGORM(ctx, tx, input)
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
		return created, err
	}
	created, err := apply(change, true)
	require.True(t, created)
	require.ErrorIs(t, err, abort)
	checkpoint, err := repo.NewSourceStreamRepo(store).ProgramCheckpoint(ctx, 10, "poll")
	require.NoError(t, err)
	require.Zero(t, checkpoint.Revision)
	created, err = apply(change, false)
	require.NoError(t, err)
	require.True(t, created)
	created, err = apply(change, false)
	require.NoError(t, err)
	require.False(t, created)
	change.Digest = strings.Repeat("b", 64)
	_, err = apply(change, false)
	require.ErrorIs(t, err, repo.ErrConflict)
	change.RunID = "gorm-run-2"
	_, err = apply(change, false)
	require.ErrorIs(t, err, repo.ErrConflict)
	change.ExpectedRevision = 1
	change.Value = json.RawMessage(`{"page":2}`)
	created, err = apply(change, false)
	require.NoError(t, err)
	require.True(t, created)
	checkpoint, err = repo.NewSourceStreamRepo(store).ProgramCheckpoint(ctx, 10, "poll")
	require.NoError(t, err)
	require.EqualValues(t, 2, checkpoint.Revision)
	require.JSONEq(t, `{"page":2}`, string(checkpoint.Value))
	other, err := repo.NewSourceStreamRepo(store).ProgramCheckpoint(ctx, 10, "other")
	require.NoError(t, err)
	require.Zero(t, other.Revision)
	change.Value = json.RawMessage(`"` + strings.Repeat("a", repo.MaxProgramCheckpointBytes) + `"`)
	_, err = apply(change, false)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	_, err = repo.ProgramMetadataCommitted(ctx, "", change.Digest)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	committed, err := repo.ProgramMetadataCommitted(ctx, "no-batch", change.Digest)
	require.NoError(t, err)
	require.False(t, committed)
	for index := 0; index < 2; index++ {
		require.NoError(t, store.UnsDB().Transaction(func(tx *gorm.DB) error {
			created, err := repo.ReserveProgramMetadataCommit(tx, "batch", change.Digest)
			require.Equal(t, index == 0, created)
			return err
		}))
	}
	committed, err = repo.ProgramMetadataCommitted(ctx, "batch", change.Digest)
	require.NoError(t, err)
	require.True(t, committed)
	_, err = repo.ProgramMetadataCommitted(ctx, "batch", strings.Repeat("c", 64))
	require.ErrorIs(t, err, repo.ErrConflict)
}

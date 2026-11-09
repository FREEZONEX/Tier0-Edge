package repo_test

// 真实 PG/Timescale：只在缺少分区时提前加锁；已存在分区不串行化。
import (
	"context"
	"testing"
	"time"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUnsSampleWriteGateOnlyLocksMissingChunks(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	db := store.UnsDB()
	var timescale bool
	require.NoError(t, db.Raw(`SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='timescaledb')`).Scan(&timescale).Error)
	if !timescale {
		t.Skip("Timescale chunk-lock integration requires the timescaledb extension")
	}
	var hypertable bool
	require.NoError(t, db.Raw(`SELECT EXISTS(SELECT 1 FROM timescaledb_information.hypertables WHERE hypertable_schema='public' AND hypertable_name='uns_sample')`).Scan(&hypertable).Error)
	require.True(t, hypertable, "this integration case requires the actual test Timescale hypertable")
	stamp := time.Now().UTC().Truncate(24 * time.Hour).Add(12 * time.Hour)
	checkGate := func(tx *gorm.DB, want bool) {
		var locked bool
		require.NoError(t, tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid()
		 AND relation='public.uns_sample'::regclass AND mode='ShareUpdateExclusiveLock' AND granted)`).Scan(&locked).Error)
		require.Equal(t, want, locked)
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, repo.PrepareUnsSampleWrites(tx, stamp))
		checkGate(tx, true)
		return nil
	}))
	schema := &unsmeta.Schema{Type: "object", Properties: map[string]unsmeta.Schema{"value": {Type: "number"}}, Required: []string{"value"}}
	created, err := unsmeta.New().CreateCollection(ctx, "", unsmeta.Document{Name: "Chunk gate", RecordSchema: schema, SampleSchema: schema})
	require.NoError(t, err)
	collection := created["collection"].(repo.UnsMeta).ID
	_, err = unsmeta.CommitProgram(ctx, unsmeta.ProgramCommit{RunID: "first-chunk", StreamID: 1, VersionID: 1, EntryID: "run", ReceivedAt: stamp.UnixMilli(),
		Writes: []unsmeta.ProgramWrite{{CollectionID: collection, Value: unsmeta.IngestValue{RecordID: "r", Record: map[string]any{"value": 1}, Sample: map[string]any{"value": 1}, Time: stamp.UnixMilli()}}}})
	require.NoError(t, err)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, repo.PrepareUnsSampleWrites(tx, stamp, stamp.Add(time.Second)))
		checkGate(tx, false)
		require.NoError(t, repo.PrepareUnsSampleWrites(tx, stamp, stamp.Add(8*24*time.Hour)))
		checkGate(tx, true)
		return nil
	}))
	// A writer wholly inside an existing chunk must not wait for a creation
	// gate held by an unrelated future-chunk transaction.
	holder := db.Begin()
	require.NoError(t, holder.Error)
	defer holder.Rollback()
	require.NoError(t, repo.PrepareUnsSampleWrites(holder, stamp.Add(8*24*time.Hour)))
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	// The write includes metadata notifications, so the outer transaction must
	// expose its commit boundary to the same helper used by production writes.
	require.NoError(t, repo.MetadataEventTransaction(bounded, db, func(tx *gorm.DB) error {
		if err := repo.PrepareUnsSampleWrites(tx, stamp); err != nil {
			return err
		}
		_, err := unsmeta.New().AppendSamples(repo.WithUnsTransaction(bounded, tx), collection,
			[]unsmeta.SampleInput{{ID: "existing-chunk", RecordID: "r", Time: stamp.Add(time.Second).UnixMilli(), Data: map[string]any{"value": 2}}})
		return err
	}))
}

func TestUnsSampleWriteGatePlainPostgres(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	db := store.UnsDB()
	var timescale bool
	require.NoError(t, db.Raw(`SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='timescaledb')`).Scan(&timescale).Error)
	if timescale {
		t.Skip("plain PostgreSQL case requires a database without Timescale")
	}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		require.NoError(t, repo.PrepareUnsSampleWrites(tx, time.Now()))
		var locked bool
		require.NoError(t, tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=pg_backend_pid()
		 AND relation='public.uns_sample'::regclass AND mode='ShareUpdateExclusiveLock' AND granted)`).Scan(&locked).Error)
		require.False(t, locked, "plain PostgreSQL writes need no Timescale chunk lock")
		return nil
	}))
}

func TestUnsSampleWriteGateRejectsInvalidUsage(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	require.NoError(t, repo.PrepareUnsSampleWrites(nil))
	require.ErrorIs(t, repo.PrepareUnsSampleWrites(nil, time.Now()), repo.ErrInvalidArgument)
	require.ErrorIs(t, repo.PrepareUnsSampleWrites(store.UnsDB(), time.Now()), repo.ErrInvalidArgument)
	require.NoError(t, store.UnsDB().Transaction(func(tx *gorm.DB) error {
		require.ErrorIs(t, repo.PrepareUnsSampleWrites(tx, time.UnixMilli(0)), repo.ErrInvalidArgument)
		require.ErrorIs(t, repo.PrepareUnsSampleWrites(tx, time.UnixMilli(253402300800000)), repo.ErrInvalidArgument)
		require.ErrorIs(t, repo.PrepareUnsSampleWrites(tx, make([]time.Time, 10001)...), repo.ErrInvalidArgument)
		return nil
	}))
}

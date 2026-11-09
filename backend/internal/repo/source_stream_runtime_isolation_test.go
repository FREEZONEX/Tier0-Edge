package repo_test

// 使用独立数据库证明私有任务权限与业务Sample实际压缩可以同时成立。
import (
	"backend/internal/repo"
	"backend/internal/testkit"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"strings"
	"testing"
	"time"
)

func TestStreamRuntimeIsolationPrivateTablesAndSampleCompression(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	db, ctx := store.UnsDB(), context.Background()
	now := time.Now().UnixMilli()
	require.NoError(t, db.Create(&repo.UnsMeta{Kind: "collection", ID: "runtime-business", Data: json.RawMessage(`{"name":"Business","recordSchema":{"type":"object"},"sampleSchema":{"type":"object"}}`), Revision: 1, CreatedAt: now, UpdatedAt: now}).Error)
	require.NoError(t, db.Create(&repo.UnsRecord{CollectionID: "runtime-business", ID: "machine", Data: json.RawMessage(`{"value":37}`), Revision: 1, CreatedAt: now, UpdatedAt: now}).Error)
	require.NoError(t, db.Create(&repo.UnsSample{CollectionID: "runtime-business", RecordID: "machine", ID: "sample", Time: time.Now().Add(-180 * 24 * time.Hour), ReceivedAt: time.Now(), Data: json.RawMessage(`{"value":38}`)}).Error)
	created, err := repo.NewSourceStreamRepo(store).ReserveRun(ctx, repo.StreamRun{ID: "private-runtime", StreamID: 61, VersionID: 7, Model: "program", Status: "processing", Summary: `{}`, CreatedAt: now})
	require.NoError(t, err)
	require.True(t, created)
	var rls bool
	require.NoError(t, db.Raw("SELECT relrowsecurity FROM pg_class WHERE oid='public.uns_sample'::regclass").Scan(&rls).Error)
	require.False(t, rls)
	var timescale bool
	require.NoError(t, db.Raw("SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname='timescaledb')").Scan(&timescale).Error)
	if timescale {
		require.NoError(t, db.Exec("SELECT public.configure_sample_timeseries(10,90)").Error)
		var compressed bool
		require.NoError(t, db.Raw("SELECT compression_enabled FROM timescaledb_information.hypertables WHERE hypertable_schema='public' AND hypertable_name='uns_sample'").Scan(&compressed).Error)
		require.True(t, compressed)
		require.NoError(t, db.Exec(`SELECT compress_chunk(format('%I.%I',chunk_schema,chunk_name)::regclass)
   FROM timescaledb_information.chunks WHERE hypertable_schema='public' AND hypertable_name='uns_sample'`).Error)
		var chunks int64
		require.NoError(t, db.Raw("SELECT count(*) FROM timescaledb_information.chunks WHERE hypertable_schema='public' AND hypertable_name='uns_sample' AND is_compressed").Scan(&chunks).Error)
		require.Positive(t, chunks)
	}
	for _, invalid := range []struct{ table, payload string }{
		{"uns_stream_checkpoint", `{"type":null,"payload":{}}`},
		{"uns_stream_commit_receipt", `{"type":"ingestReceipt","payload":{"digest":null}}`},
		{"uns_metadata_event_outbox", `{"type":"metadataEventOutbox","payload":{"status":null}}`},
	} {
		var rejected *pgconn.PgError
		err := db.Exec("INSERT INTO public."+invalid.table+"(id,data,revision,created_at,updated_at) VALUES('invalid-null',?::jsonb,1,1,1)", invalid.payload).Error
		require.ErrorAs(t, err, &rejected)
		require.Equal(t, "23514", rejected.Code, "SQL NULL must not bypass private type contracts")
	}
	role := "runtime_private_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := `"` + role + `"`
	require.NoError(t, db.Exec("CREATE ROLE "+quoted+" NOLOGIN NOINHERIT NOSUPERUSER NOBYPASSRLS").Error)
	t.Cleanup(func() {
		require.NoError(t, db.Exec("DROP OWNED BY "+quoted).Error)
		require.NoError(t, db.Exec("DROP ROLE "+quoted).Error)
	})
	require.NoError(t, db.Exec("GRANT USAGE ON SCHEMA public TO "+quoted).Error)
	require.NoError(t, db.Exec("GRANT SELECT ON public.uns_meta,public.uns_record,public.uns_sample TO "+quoted).Error)
	asRole := func(sql string) error {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SET LOCAL ROLE " + quoted).Error; err != nil {
				return err
			}
			return tx.Exec(sql).Error
		})
	}
	require.NoError(t, asRole("SELECT * FROM public.uns_sample WHERE collection_id='runtime-business'"))
	for _, table := range []string{"uns_stream_run", "uns_stream_task", "uns_stream_effect", "uns_stream_checkpoint", "uns_stream_commit_receipt", "uns_metadata_event_outbox", "uns_stream_run_history", "uns_stream_meta_task_archive", "uns_stream_run_owner"} {
		var denied *pgconn.PgError
		require.ErrorAs(t, asRole("SELECT * FROM public."+table), &denied, table)
		require.Equal(t, "42501", denied.Code)
		require.NoError(t, db.Exec("GRANT SELECT ON public."+table+" TO "+quoted).Error)
		var count int64
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SET LOCAL ROLE " + quoted).Error; err != nil {
				return err
			}
			return tx.Raw("SELECT count(*) FROM public." + table).Scan(&count).Error
		}))
		require.Zero(t, count, "RLS must fail closed after accidental grants")
	}
	require.NoError(t, db.Exec("SELECT public.protect_stream_runtime_storage()").Error)
	var denied *pgconn.PgError
	require.ErrorAs(t, asRole("SELECT public.protect_stream_runtime_storage()"), &denied)
	require.Equal(t, "42501", denied.Code)
}

package repo_test

// 验证一次性升级前的只读门禁：旧库、变更/缺失迁移在基线 DDL 前停止，
// 不修改原数据和账本；控制库与 UNS 库分离时也必须先检查两个连接。
import (
	"context"
	"fmt"
	"testing"

	"backend/internal/config"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigrationPreflightRejectsLegacyStorageBeforeDDL(t *testing.T) {
	for _, table := range []string{"uns_meta", "uns_record", "uns_sample"} {
		t.Run(table, func(t *testing.T) {
			store, _ := testkit.NewTestDB(t)
			db := store.UnsDB()
			require.NoError(t, db.Exec(fmt.Sprintf(`CREATE SCHEMA uns;
				CREATE TABLE uns.%s (id text PRIMARY KEY, data jsonb NOT NULL);
				INSERT INTO uns.%s VALUES ('preserved', '{"value":42}');`, table, table)).Error)
			dropPreflightProbeIndex(t, db)
			err := store.Migrate(context.Background())
			require.ErrorContains(t, err, "legacy uns storage")
			require.ErrorContains(t, err, "one-time offline cutover")
			assertPreflightProbeIndexAbsent(t, db)
			var value int
			require.NoError(t, db.Raw("SELECT (data->>'value')::int FROM uns."+table+" WHERE id='preserved'").Scan(&value).Error)
			require.Equal(t, 42, value)
		})
	}
}

func TestMigrationPreflightRejectsChangedLedgerBeforeDDL(t *testing.T) {
	for _, scope := range []string{"common", "sink"} {
		t.Run(scope, func(t *testing.T) {
			store, _ := testkit.NewTestDB(t)
			db := store.CommonDB()
			var id int64
			require.NoError(t, db.Raw("SELECT id FROM public.sys_schema_migration WHERE scope=? ORDER BY id LIMIT 1", scope).Scan(&id).Error)
			require.Positive(t, id)
			require.NoError(t, db.Exec("UPDATE public.sys_schema_migration SET checksum=repeat('f',64) WHERE id=?", id).Error)
			dropPreflightProbeIndex(t, db)
			err := store.Migrate(context.Background())
			require.ErrorContains(t, err, "migration preflight: migration checksum mismatch")
			assertPreflightProbeIndexAbsent(t, db)
			var unchanged bool
			require.NoError(t, db.Raw("SELECT checksum=repeat('f',64) AND success FROM public.sys_schema_migration WHERE id=?", id).Scan(&unchanged).Error)
			require.True(t, unchanged, "preflight must not rewrite the existing ledger")
		})
	}
}

func TestMigrationPreflightRejectsMissingMigrationBeforeDDL(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	db := store.CommonDB()
	require.NoError(t, db.Exec(`INSERT INTO public.sys_schema_migration
		(scope,version,sequence,name,checksum,success)
		VALUES ('sink','99.0.0',1,'not_in_target_release',repeat('a',64),true)`).Error)
	dropPreflightProbeIndex(t, db)
	err := store.Migrate(context.Background())
	require.ErrorContains(t, err, "is not included in this release")
	assertPreflightProbeIndexAbsent(t, db)
	var count int64
	require.NoError(t, db.Table("public.sys_schema_migration").Where("name = ?", "not_in_target_release").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

func TestMigrationPreflightChecksSeparateUNSBeforeCommonDDL(t *testing.T) {
	common, commonDSN := testkit.NewTestDB(t)
	sink, sinkDSN := testkit.NewTestDB(t)
	require.NoError(t, sink.UnsDB().Exec("CREATE SCHEMA uns; CREATE TABLE uns.uns_meta (id text PRIMARY KEY)").Error)
	dropPreflightProbeIndex(t, common.CommonDB())
	store, err := repo.Open(context.Background(), config.DatabaseConf{UnsDbUrl: commonDSN, SinkDbUrl: sinkDSN})
	require.NoError(t, err)
	t.Cleanup(store.Close)
	err = store.Migrate(context.Background())
	require.ErrorContains(t, err, "legacy uns storage")
	assertPreflightProbeIndexAbsent(t, common.CommonDB())
}

func dropPreflightProbeIndex(t *testing.T, db *gorm.DB) {
	t.Helper()
	// Baseline DDL would recreate this index. Its absence proves that the error
	// occurred before baseline writes, not just before versioned migrations.
	require.NoError(t, db.Exec("DROP INDEX public.idx_uns_meta_parent").Error)
}

func assertPreflightProbeIndexAbsent(t *testing.T, db *gorm.DB) {
	t.Helper()
	var exists bool
	require.NoError(t, db.Raw("SELECT to_regclass('public.idx_uns_meta_parent') IS NOT NULL").Scan(&exists).Error)
	require.False(t, exists, "rejected upgrade must not run baseline DDL")
}

func TestProjectAppPrincipalMigrationBackfillsOnce(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	db := store.CommonDB()
	r := repo.NewProjectRepo(db)
	project := testkit.Insert(t, db, &repo.Project{ProjectKey: "principal-upgrade", Name: "Upgrade", Status: "active", OwnerUserID: 1})
	for _, name := range []string{"live", "retired"} {
		app, err := r.CreateProjectApp(ctx, aliasTestApp(project.ID, name, nil))
		require.NoError(t, err)
		if name == "retired" {
			require.NoError(t, r.DeleteProjectApp(ctx, project.ID, app.ID, 1))
		}
	}
	// Reconstruct the pre-migration schema only inside this disposable test DB.
	require.NoError(t, db.Exec(`DROP TRIGGER project_app_principal_immutable ON project_app;
		DROP FUNCTION tier0_project_app_principal_immutable();
		ALTER TABLE project_app DROP COLUMN app_principal_id;
		DELETE FROM sys_schema_migration WHERE scope='common' AND version='3.0.0' AND sequence=32 AND name='project_app_principal';`).Error)
	require.NoError(t, store.Migrate(ctx))
	var apps []repo.ProjectApp
	require.NoError(t, db.Unscoped().Order("id").Find(&apps).Error)
	require.Len(t, apps, 2)
	for _, app := range apps {
		parsed, err := uuid.Parse(app.AppPrincipalID)
		require.NoError(t, err)
		require.NotEqual(t, uuid.Nil, parsed)
	}
	require.NotEqual(t, apps[0].AppPrincipalID, apps[1].AppPrincipalID)
	require.NoError(t, store.Migrate(ctx))
	var again []repo.ProjectApp
	require.NoError(t, db.Unscoped().Order("id").Find(&again).Error)
	require.Equal(t, apps, again, "repeated startup never rotates live or retired App identities")
	var count int64
	require.NoError(t, db.Table("sys_schema_migration").Where("scope='common' AND name='project_app_principal' AND success").Count(&count).Error)
	require.Equal(t, int64(1), count)
}

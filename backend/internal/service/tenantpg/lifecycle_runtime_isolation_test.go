package tenantpg

// 验证真实 UNS 数据角色授权及再次授权，均拒绝私有任务和控制表访问，
// 不影响业务表及独立项目数据库。使用 testkit 独立库，不操作现有部署。
import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/testkit"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestUNSDataRoleRefreshKeepsRuntimePrivate(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx, db := context.Background(), store.UnsDB()
	const business = "runtime-grant-business"
	now := time.Now().UnixMilli()
	require.NoError(t, db.Create(&repo.UnsMeta{Kind: "collection", ID: business, Revision: 1,
		CreatedAt: now, UpdatedAt: now,
		Data: json.RawMessage(`{"name":"Grant business","recordSchema":{"type":"object"},"sampleSchema":{"type":"object"}}`)}).Error)
	require.NoError(t, db.Create(&repo.UnsRecord{CollectionID: business, ID: "machine", Revision: 1,
		CreatedAt: now, UpdatedAt: now, Data: json.RawMessage(`{"value":37}`)}).Error)
	require.NoError(t, db.Create(&repo.UnsSample{CollectionID: business, RecordID: "machine", ID: "observation",
		Time: time.UnixMilli(now), ReceivedAt: time.UnixMilli(now), Data: json.RawMessage(`{"value":38}`)}).Error)
	_, err := repo.NewSourceStreamRepo(store).ReserveRun(ctx, repo.StreamRun{ID: "grant-private-run", StreamID: 61,
		VersionID: 7, Model: "program", Status: "processing", Summary: `{}`, CreatedAt: now})
	require.NoError(t, err)
	role := "runtime_grants_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := `"` + role + `"`
	require.NoError(t, db.Exec(`CREATE ROLE `+quoted+` NOLOGIN NOINHERIT NOSUPERUSER NOBYPASSRLS`).Error)
	t.Cleanup(func() {
		require.NoError(t, db.Exec(`DROP OWNED BY `+quoted).Error)
		require.NoError(t, db.Exec(`DROP ROLE `+quoted).Error)
	})
	// Reproduce pre-fix shared-public grants, including column grants and future
	// defaults; merely skipping these objects would leave the vulnerability.
	require.NoError(t, db.Exec(`GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO `+quoted).Error)
	require.NoError(t, db.Exec(`GRANT USAGE,SELECT,UPDATE ON ALL SEQUENCES IN SCHEMA public TO `+quoted).Error)
	require.NoError(t, db.Exec(`GRANT SELECT(password) ON public.sys_user_info TO `+quoted).Error)
	require.NoError(t, db.Exec(`GRANT SELECT(password) ON public.sys_user_info TO PUBLIC`).Error)
	require.NoError(t, db.Exec(`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT,INSERT,UPDATE,DELETE ON TABLES TO `+quoted).Error)
	require.NoError(t, db.Exec(`ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE,SELECT,UPDATE ON SEQUENCES TO `+quoted).Error)
	require.NoError(t, db.Exec(`ALTER DEFAULT PRIVILEGES GRANT SELECT ON TABLES TO `+quoted).Error)
	require.NoError(t, db.Exec(`ALTER DEFAULT PRIVILEGES GRANT SELECT ON TABLES TO PUBLIC`).Error)
	creator := `"runtime_creator_` + strings.ReplaceAll(uuid.NewString(), "-", "") + `"`
	require.NoError(t, db.Exec(`CREATE ROLE `+creator+` NOLOGIN NOSUPERUSER NOBYPASSRLS`).Error)
	t.Cleanup(func() {
		require.NoError(t, db.Exec(`DROP OWNED BY `+creator).Error)
		require.NoError(t, db.Exec(`DROP ROLE `+creator).Error)
	})
	require.NoError(t, db.Exec(`ALTER DEFAULT PRIVILEGES FOR ROLE `+creator+` GRANT SELECT ON TABLES TO `+quoted).Error)
	asRole := func(sql string) error {
		return db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(`SET LOCAL ROLE ` + quoted).Error; err != nil {
				return err
			}
			return tx.Exec(sql).Error
		})
	}
	for attempt := 0; attempt < 2; attempt++ {
		if attempt == 0 {
			require.NoError(t, grantDataRoleSchemaPrivileges(ctx, db, "public", role, ""))
		} else {
			require.NoError(t, grantSchemaPrivileges(ctx, db, "public", role))
		}
		var canCreate bool
		require.NoError(t, db.Raw(`SELECT has_schema_privilege(?,'public','CREATE')`, role).Scan(&canCreate).Error)
		require.False(t, canCreate, "platform public is not an App-owned schema")
		var defaultGrants int64
		require.NoError(t, db.Raw(`SELECT count(*) FROM pg_default_acl d
			LEFT JOIN pg_namespace n ON n.oid=d.defaclnamespace
			CROSS JOIN LATERAL aclexplode(d.defaclacl) a JOIN pg_roles r ON r.oid=a.grantee
			WHERE r.rolname=? AND d.defaclobjtype IN ('r','S')
			AND (d.defaclnamespace=0 OR n.nspname='public')`, role).Scan(&defaultGrants).Error)
		require.Zero(t, defaultGrants, "all creator global/public defaults must be cleared")
		require.NoError(t, db.Exec(`CREATE TABLE IF NOT EXISTS public.runtime_future_control(id bigserial PRIMARY KEY)`).Error)
		for _, sql := range []string{
			`SELECT * FROM public.sys_user_info`, `SELECT password FROM public.sys_user_info`,
			`SELECT * FROM public.sys_api_key`, `UPDATE public.sys_api_key SET owner_id=1`,
			`SELECT * FROM public.sys_schema_migration`, `SELECT * FROM public.sys_role_resource`,
			`SELECT * FROM public.sys_ai_provider_config`, `SELECT * FROM public.sys_gateway_route`,
			`SELECT * FROM public.project_app`, `SELECT * FROM public.stream_receipt`,
			`SELECT * FROM public.stream_event_outbox`, `SELECT * FROM public.source_credential_projection`,
			`SELECT * FROM public.program_version_projection`, `SELECT * FROM public.runtime_future_control`,
			`SELECT nextval('public.sys_user_info_user_id_seq')`,
		} {
			var denied *pgconn.PgError
			require.ErrorAs(t, asRole(sql), &denied, sql)
			require.Equal(t, "42501", denied.Code, sql)
		}
		require.NoError(t, asRole(`SELECT * FROM public.uns_record WHERE collection_id='`+business+`'`))
		require.NoError(t, asRole(`SELECT * FROM public.uns_sample WHERE collection_id='`+business+`'`))
		var count int64
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(`SET LOCAL ROLE ` + quoted).Error; err != nil {
				return err
			}
			return tx.Raw(`SELECT count(*) FROM public.uns_meta WHERE id LIKE 'tier0.system.stream.%'`).Scan(&count).Error
		}))
		require.Zero(t, count)
		var pgErr *pgconn.PgError
		require.ErrorAs(t, asRole(`SELECT * FROM public.uns_stream_run`), &pgErr)
		require.Equal(t, "42501", pgErr.Code)
	}

	// An independent Project database also uses public but has no UNS store.
	// Its ordinary future business-table contract must remain unchanged.
	projectName := "runtime_project_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	projectQuoted := `"` + projectName + `"`
	require.NoError(t, db.Exec(`CREATE DATABASE `+projectQuoted).Error)
	projectURL, err := url.Parse(testkit.DSN())
	require.NoError(t, err)
	projectURL.Path = "/" + projectName
	project, err := gorm.Open(postgres.Open(projectURL.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	projectSQL, err := project.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, projectSQL.Close())
		require.NoError(t, db.Exec(`DROP DATABASE `+projectQuoted+` WITH (FORCE)`).Error)
	})
	require.NoError(t, grantDataRoleSchemaPrivileges(ctx, project, "public", role, ""))
	require.NoError(t, project.Exec(`CREATE TABLE public.project_items(id bigserial PRIMARY KEY,value text)`).Error)
	require.NoError(t, project.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SET LOCAL ROLE ` + quoted).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO public.project_items(value) VALUES ('business')`).Error
	}))
	// Unrelated inherited grants cannot be safely removed by this normalizer.
	// They must fail authorization refresh rather than silently report success.
	group := `"runtime_group_` + strings.ReplaceAll(uuid.NewString(), "-", "") + `"`
	require.NoError(t, db.Exec(`CREATE ROLE `+group+` NOLOGIN NOSUPERUSER NOBYPASSRLS`).Error)
	t.Cleanup(func() {
		require.NoError(t, db.Exec(`DROP OWNED BY `+group).Error)
		require.NoError(t, db.Exec(`DROP ROLE `+group).Error)
	})
	require.NoError(t, db.Exec(`ALTER ROLE `+quoted+` INHERIT`).Error)
	require.NoError(t, db.Exec(`GRANT `+group+` TO `+quoted).Error)
	require.NoError(t, db.Exec(`GRANT SELECT ON public.sys_user_info TO `+group).Error)
	err = grantDataRoleSchemaPrivileges(ctx, db, "public", role, "")
	require.ErrorContains(t, err, "retains forbidden ownership or inherited access")
}

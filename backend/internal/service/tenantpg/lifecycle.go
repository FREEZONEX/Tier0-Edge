package tenantpg

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const dupDatabaseErrCode = "42P04"
const nodeREDRoleName = "nodered"
const marimoRoleName = "marimo"

var managedDataRoleNames = []string{nodeREDRoleName, marimoRoleName}

// Lifecycle 负责租户 PG 的 DDL（CREATE DATABASE / CREATE SCHEMA / CREATE ROLE）
type Lifecycle struct {
	registry   *Registry
	roleSecret string
}

// NewLifecycle 创建 Lifecycle
func NewLifecycle(registry *Registry, roleSecret string) *Lifecycle {
	return &Lifecycle{registry: registry, roleSecret: roleSecret}
}

// GetAdminProjectDB 打开指定 project database 的 admin 短连接，供外部模块执行迁移等 DDL。
func (l *Lifecycle) GetAdminProjectDB(ctx context.Context, projectDB string) (*gorm.DB, func(), error) {
	return l.registry.GetAdminProjectDB(ctx, projectDB)
}

// CreateDatabase 在 PG 中幂等创建 database + 受限 role + GRANT
func (l *Lifecycle) CreateDatabase(ctx context.Context, databaseName string) error {
	if err := ValidateIdentifier(databaseName); err != nil {
		return fmt.Errorf("tenantpg: invalid database name: %w", err)
	}

	admin, closer, err := l.registry.GetAdminDB(ctx)
	if err != nil {
		return fmt.Errorf("tenantpg: open admin conn: %w", err)
	}
	defer closer()

	// 1. CREATE DATABASE（42P04 duplicate_database 视为成功）
	stmt := fmt.Sprintf(`CREATE DATABASE "%s"`, databaseName)
	if err := admin.WithContext(ctx).Exec(stmt).Error; err != nil {
		if !isDuplicateDatabaseErr(err) {
			return fmt.Errorf("tenantpg: create database %q: %w", databaseName, err)
		}
	}

	// 2. 在 admin DB（cluster 级）幂等创建 project role + GRANT CONNECT。
	username := ""
	if l.roleSecret != "" {
		username, err = l.ensureProjectRole(ctx, admin, databaseName)
		if err != nil {
			return err
		}
	}

	dataRoles, err := existingRoles(ctx, admin, managedDataRoleNames)
	if err != nil {
		return err
	}
	for _, roleName := range dataRoles {
		if err := grantDatabaseConnect(ctx, admin, databaseName, roleName); err != nil {
			return err
		}
	}

	// 3. 连接新建的 project database，授予 public schema 数据权限。
	projAdmin, projCloser, err := l.registry.GetAdminProjectDB(ctx, databaseName)
	if err != nil {
		return fmt.Errorf("tenantpg: open project admin conn: %w", err)
	}
	defer projCloser()

	if username != "" {
		if err := grantSchemaPrivileges(ctx, projAdmin, "public", username); err != nil {
			return err
		}
	}
	for _, roleName := range dataRoles {
		if err := grantDataRoleSchemaPrivileges(ctx, projAdmin, "public", roleName, username); err != nil {
			return err
		}
	}
	return nil
}

// CreateSchema 在 Project Database 下幂等创建 schema + GRANT 到受限 role
func (l *Lifecycle) CreateSchema(ctx context.Context, projectDatabase, schemaName string) error {
	if err := ValidateIdentifier(projectDatabase); err != nil {
		return fmt.Errorf("tenantpg: invalid project database name: %w", err)
	}
	if err := ValidateIdentifier(schemaName); err != nil {
		return fmt.Errorf("tenantpg: invalid schema name: %w", err)
	}

	ownerRole := ""
	if l.roleSecret != "" {
		admin, closeAdmin, err := l.registry.GetAdminDB(ctx)
		if err != nil {
			return err
		}
		ownerRole, err = l.ensureProjectRole(ctx, admin, projectDatabase)
		closeAdmin()
		if err != nil {
			return err
		}
	}

	projAdmin, closer, err := l.registry.GetAdminProjectDB(ctx, projectDatabase)
	if err != nil {
		return fmt.Errorf("tenantpg: open admin project conn: %w", err)
	}
	defer closer()

	stmt := fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS "%s"`, schemaName)
	if err := projAdmin.WithContext(ctx).Exec(stmt).Error; err != nil {
		return fmt.Errorf("tenantpg: create schema %q: %w", schemaName, err)
	}

	if ownerRole != "" {
		if err := grantSchemaPrivileges(ctx, projAdmin, schemaName, ownerRole); err != nil {
			return err
		}
	}

	dataRoles, err := existingRoles(ctx, projAdmin, managedDataRoleNames)
	if err != nil {
		return err
	}
	for _, roleName := range dataRoles {
		// Old project databases may predate managed data-role grants. Creating a
		// new App is an ownership event for this database, so repair CONNECT here
		// without requiring an upgrade-wide ACL replay.
		if err := grantDatabaseConnect(ctx, projAdmin, projectDatabase, roleName); err != nil {
			return err
		}
		if err := grantDataRoleSchemaPrivileges(ctx, projAdmin, schemaName, roleName, ownerRole); err != nil {
			return err
		}
	}
	return nil
}

// Prepare only this project's role. Existing credentials and privileges are
// validated, never rotated or downgraded as a side effect of App deployment.
func (l *Lifecycle) ensureProjectRole(ctx context.Context, admin *gorm.DB, databaseName string) (string, error) {
	return l.prepareProjectRole(ctx, admin, databaseName, false)
}

// restoreConnect is reserved for explicit restore maintenance. Normal App
// deployment validates existing access rather than repeatedly rewriting ACLs.
func (l *Lifecycle) prepareProjectRole(ctx context.Context, admin *gorm.DB, databaseName string, restoreConnect bool) (string, error) {
	if strings.TrimSpace(l.roleSecret) == "" {
		return "", fmt.Errorf("tenantpg: project role secret is required")
	}
	username, password, err := DeriveCredentials(l.roleSecret, databaseName)
	if err != nil {
		return "", fmt.Errorf("tenantpg: derive credentials: %w", err)
	}
	err = admin.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "tier0-project-role:"+username).Error; err != nil {
			return fmt.Errorf("tenantpg: lock project role: %w", err)
		}
		exists, err := inspectProjectRole(ctx, tx, username)
		if err != nil {
			return err
		}
		if exists {
			if restoreConnect {
				// Validate credentials before changing access to the restored DB.
				if err := l.authenticateProjectRole(ctx, "template1", username, password); err != nil {
					return err
				}
				return grantDatabaseConnect(ctx, tx, databaseName, username)
			}
			return nil
		}
		// Both values are derived/validated identifiers or hex. Suppress SQL
		// tracing even on failure: the DDL necessarily contains a password.
		createRole := fmt.Sprintf(`CREATE ROLE "%s" LOGIN PASSWORD '%s' NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS`, username, password)
		if err := tx.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Exec(createRole).Error; err != nil {
			return fmt.Errorf("tenantpg: create project role %q failed; check database role-management permissions", username)
		}
		return grantDatabaseConnect(ctx, tx, databaseName, username)
	})
	if err != nil {
		return "", err
	}
	if err := l.authenticateProjectRole(ctx, databaseName, username, password); err != nil {
		return "", err
	}
	return username, nil
}

func inspectProjectRole(ctx context.Context, db *gorm.DB, username string) (bool, error) {
	var role struct {
		Name        string
		CanLogin    bool
		Superuser   bool
		CreateDB    bool
		CreateRole  bool
		Replication bool
		BypassRLS   bool
		Membership  bool
	}
	if err := db.WithContext(ctx).Raw(`SELECT r.rolname AS name, r.rolcanlogin AS can_login,
		r.rolsuper AS superuser, r.rolcreatedb AS create_db, r.rolcreaterole AS create_role,
		r.rolreplication AS replication, r.rolbypassrls AS bypass_rls,
		EXISTS (SELECT 1 FROM pg_auth_members m WHERE m.member=r.oid) AS membership
		FROM pg_roles r WHERE r.rolname = ?`, username).Scan(&role).Error; err != nil {
		return false, fmt.Errorf("tenantpg: inspect project role: %w", err)
	}
	if role.Name == "" {
		return false, nil
	}
	if !role.CanLogin || role.Superuser || role.CreateDB || role.CreateRole || role.Replication || role.BypassRLS || role.Membership {
		return true, fmt.Errorf("tenantpg: unsafe project role %q; explicit credential maintenance is required", username)
	}
	return true, nil
}

func (l *Lifecycle) authenticateProjectRole(ctx context.Context, databaseName, username, password string) error {
	dsn, err := BuildProjectConnDSNWithCreds(l.registry.baseDSN, databaseName, username, password)
	if err != nil {
		return err
	}
	timeout := l.registry.cfg.ConnectTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := pgx.Connect(connectCtx, dsn)
	if err != nil {
		// Do not expose a credential-bearing connection error or repair a
		// mismatch by ALTER ROLE. Existing role grants are still untouched.
		return fmt.Errorf("tenantpg: project role authentication failed for %q; check saved credentials and database access", username)
	}
	var currentUser, sessionUser string
	identityErr := conn.QueryRow(connectCtx, `SELECT current_user, session_user`).Scan(&currentUser, &sessionUser)
	_ = conn.Close(connectCtx)
	if identityErr != nil || currentUser != username || sessionUser != username {
		return fmt.Errorf("tenantpg: project role identity verification failed for %q", username)
	}
	return nil
}

// DropSchema 在 Project Database 下删除 schema
func (l *Lifecycle) DropSchema(ctx context.Context, projectDatabase, schemaName string) error {
	if err := ValidateIdentifier(projectDatabase); err != nil {
		return fmt.Errorf("tenantpg: invalid project database name: %w", err)
	}
	if err := ValidateIdentifier(schemaName); err != nil {
		return fmt.Errorf("tenantpg: invalid schema name: %w", err)
	}

	projAdmin, closer, err := l.registry.GetAdminProjectDB(ctx, projectDatabase)
	if err != nil {
		return fmt.Errorf("tenantpg: open admin project conn: %w", err)
	}
	defer closer()

	stmt := fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schemaName)
	if err := projAdmin.WithContext(ctx).Exec(stmt).Error; err != nil {
		return fmt.Errorf("tenantpg: drop schema %q: %w", schemaName, err)
	}
	return nil
}

// EnsureSchemaMarkerTable 在指定 project database 的 schema 中创建占位表，
// 避免 drizzle-kit push 把 freshly created 的空 schema 误删。
func (l *Lifecycle) EnsureSchemaMarkerTable(ctx context.Context, projectDatabase, schemaName string) error {
	if err := ValidateIdentifier(projectDatabase); err != nil {
		return fmt.Errorf("tenantpg: invalid project database name: %w", err)
	}
	if err := ValidateIdentifier(schemaName); err != nil {
		return fmt.Errorf("tenantpg: invalid schema name: %w", err)
	}

	projAdmin, closer, err := l.registry.GetAdminProjectDB(ctx, projectDatabase)
	if err != nil {
		return fmt.Errorf("tenantpg: open admin project conn: %w", err)
	}
	defer closer()

	sql := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS "%s"."__schema_marker" (id int PRIMARY KEY)`, schemaName)
	if err := projAdmin.WithContext(ctx).Exec(sql).Error; err != nil {
		return fmt.Errorf("tenantpg: create schema marker table: %w", err)
	}
	return nil
}

// DropSchemaMarkerTable 删除指定 schema 的占位表。
// drizzle-kit push 会把「schema 内唯一的占位表」当作新业务表的重命名候选并触发交互确认，
// 因此部署流程在 push 前调用本方法移除占位表，push 后再用 EnsureSchemaMarkerTable 重建。
func (l *Lifecycle) DropSchemaMarkerTable(ctx context.Context, projectDatabase, schemaName string) error {
	if err := ValidateIdentifier(projectDatabase); err != nil {
		return fmt.Errorf("tenantpg: invalid project database name: %w", err)
	}
	if err := ValidateIdentifier(schemaName); err != nil {
		return fmt.Errorf("tenantpg: invalid schema name: %w", err)
	}

	projAdmin, closer, err := l.registry.GetAdminProjectDB(ctx, projectDatabase)
	if err != nil {
		return fmt.Errorf("tenantpg: open admin project conn: %w", err)
	}
	defer closer()

	sql := fmt.Sprintf(`DROP TABLE IF EXISTS "%s"."__schema_marker"`, schemaName)
	if err := projAdmin.WithContext(ctx).Exec(sql).Error; err != nil {
		return fmt.Errorf("tenantpg: drop schema marker table: %w", err)
	}
	return nil
}

// DropDatabase 删除 database
func (l *Lifecycle) DropDatabase(ctx context.Context, databaseName string) error {
	if err := ValidateIdentifier(databaseName); err != nil {
		return fmt.Errorf("tenantpg: invalid database name: %w", err)
	}

	// 先关闭该 database 的缓存连接，避免 DROP DATABASE 因 active connection 失败
	_ = l.registry.CloseProjectConn(databaseName)

	admin, closer, err := l.registry.GetAdminDB(ctx)
	if err != nil {
		return fmt.Errorf("tenantpg: open admin conn: %w", err)
	}
	defer closer()

	stmt := fmt.Sprintf(`DROP DATABASE IF EXISTS "%s" WITH (FORCE)`, databaseName)
	if err := admin.WithContext(ctx).Exec(stmt).Error; err != nil {
		return fmt.Errorf("tenantpg: drop database %q: %w", databaseName, err)
	}
	return nil
}

// grantSchemaPrivileges 授予受限 role 对指定 schema 的 USAGE+CREATE + 表/序列默认权限
func grantSchemaPrivileges(ctx context.Context, db *gorm.DB, schemaName, roleName string) error {
	unsStore, err := isUNSStoreSchema(ctx, db, schemaName)
	if err != nil {
		return err
	}
	if unsStore {
		return grantUNSDataRolePrivileges(ctx, db, roleName, "")
	}
	stmts := []string{
		fmt.Sprintf(`GRANT USAGE, CREATE ON SCHEMA "%s" TO "%s"`, schemaName, roleName),
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES IN SCHEMA "%s" GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO "%s"`, schemaName, roleName),
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES IN SCHEMA "%s" GRANT USAGE, SELECT ON SEQUENCES TO "%s"`, schemaName, roleName),
	}
	for _, s := range stmts {
		if err := db.WithContext(ctx).Exec(s).Error; err != nil {
			return fmt.Errorf("tenantpg: grant on schema %q: %w", schemaName, err)
		}
	}
	return nil
}

func grantDatabaseConnect(ctx context.Context, db *gorm.DB, databaseName, roleName string) error {
	stmt := fmt.Sprintf(`GRANT CONNECT ON DATABASE "%s" TO "%s"`, databaseName, roleName)
	if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
		return fmt.Errorf("tenantpg: grant %q connect on %q: %w", roleName, databaseName, err)
	}
	return nil
}

func roleExists(ctx context.Context, db *gorm.DB, roleName string) (bool, error) {
	var exists bool
	if err := db.WithContext(ctx).
		Raw(`SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = ?)`, roleName).
		Scan(&exists).Error; err != nil {
		return false, fmt.Errorf("tenantpg: check role %q: %w", roleName, err)
	}
	return exists, nil
}

func existingRoles(ctx context.Context, db *gorm.DB, roleNames []string) ([]string, error) {
	existing := make([]string, 0, len(roleNames))
	for _, roleName := range roleNames {
		exists, err := roleExists(ctx, db, roleName)
		if err != nil {
			return nil, err
		}
		if exists {
			existing = append(existing, roleName)
		}
	}
	return existing, nil
}

// grantDataRoleSchemaPrivileges keeps a managed data role non-owning while
// granting it access to current and future data objects in one project schema.
func grantDataRoleSchemaPrivileges(ctx context.Context, db *gorm.DB, schemaName, roleName, ownerRole string) error {
	unsStore, err := isUNSStoreSchema(ctx, db, schemaName)
	if err != nil {
		return err
	}
	if unsStore {
		return grantUNSDataRolePrivileges(ctx, db, roleName, ownerRole)
	}
	stmts := []string{
		fmt.Sprintf(`REVOKE CREATE ON SCHEMA "%s" FROM "%s"`, schemaName, roleName),
		fmt.Sprintf(`GRANT USAGE ON SCHEMA "%s" TO "%s"`, schemaName, roleName),
		fmt.Sprintf(`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA "%s" TO "%s"`, schemaName, roleName),
		fmt.Sprintf(`REVOKE TRUNCATE ON ALL TABLES IN SCHEMA "%s" FROM "%s"`, schemaName, roleName),
		fmt.Sprintf(`GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA "%s" TO "%s"`, schemaName, roleName),
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES IN SCHEMA "%s" GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO "%s"`, schemaName, roleName),
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES IN SCHEMA "%s" REVOKE TRUNCATE ON TABLES FROM "%s"`, schemaName, roleName),
		fmt.Sprintf(`ALTER DEFAULT PRIVILEGES IN SCHEMA "%s" GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO "%s"`, schemaName, roleName),
	}
	if ownerRole != "" {
		stmts = append(stmts,
			fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE "%s" IN SCHEMA "%s" GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO "%s"`, ownerRole, schemaName, roleName),
			fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE "%s" IN SCHEMA "%s" REVOKE TRUNCATE ON TABLES FROM "%s"`, ownerRole, schemaName, roleName),
			fmt.Sprintf(`ALTER DEFAULT PRIVILEGES FOR ROLE "%s" IN SCHEMA "%s" GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO "%s"`, ownerRole, schemaName, roleName),
		)
	}
	for _, stmt := range stmts {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("tenantpg: grant data role %q on schema %q: %w", roleName, schemaName, err)
		}
	}
	return nil
}

func isUNSStoreSchema(ctx context.Context, db *gorm.DB, schemaName string) (bool, error) {
	if schemaName != "public" {
		return false, nil
	}
	// Independent Project databases also use public; they retain their existing
	// future business-table grants, unlike the shared platform/UNS database.
	var exists bool
	err := db.WithContext(ctx).Raw(`SELECT to_regclass('public.uns_meta') IS NOT NULL`).Scan(&exists).Error
	return exists, err
}

// UNS contains public business data and private synchronization/execution state.
// Blanket/default grants would expose queued payloads and let an App forge the
// receiver's object mappings. Grant existing business objects individually.
func grantUNSDataRolePrivileges(ctx context.Context, db *gorm.DB, roleName, ownerRole string) error {
	quote := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	role := quote(roleName)
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		stmts := []string{
			`REVOKE CREATE ON SCHEMA public FROM PUBLIC`,
			`REVOKE CREATE ON SCHEMA public FROM ` + role,
			`GRANT USAGE ON SCHEMA public TO ` + role,
			`ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON TABLES FROM ` + role,
			`ALTER DEFAULT PRIVILEGES IN SCHEMA public REVOKE ALL ON SEQUENCES FROM ` + role,
		}
		if ownerRole != "" {
			stmts = append(stmts,
				`ALTER DEFAULT PRIVILEGES FOR ROLE `+quote(ownerRole)+` IN SCHEMA public REVOKE ALL ON TABLES FROM `+role,
				`ALTER DEFAULT PRIVILEGES FOR ROLE `+quote(ownerRole)+` IN SCHEMA public REVOKE ALL ON SEQUENCES FROM `+role)
		}
		var defaults []struct{ OwnerName, SchemaName, ObjectKind, Grantee string }
		if err := tx.Raw(`SELECT DISTINCT owner.rolname AS owner_name,COALESCE(n.nspname,'') AS schema_name,
			d.defaclobjtype::text AS object_kind,CASE WHEN a.grantee=0 THEN 'PUBLIC' ELSE target.rolname END AS grantee
			FROM pg_default_acl d JOIN pg_roles owner ON owner.oid=d.defaclrole
			LEFT JOIN pg_namespace n ON n.oid=d.defaclnamespace
			CROSS JOIN LATERAL aclexplode(d.defaclacl) a
			LEFT JOIN pg_roles target ON target.oid=a.grantee
			WHERE d.defaclobjtype IN ('r','S') AND (d.defaclnamespace=0 OR n.nspname='public')
			AND (a.grantee=0 OR target.rolname=?)`, roleName).Scan(&defaults).Error; err != nil {
			return err
		}
		for _, acl := range defaults {
			schema := ""
			if acl.SchemaName != "" {
				schema = " IN SCHEMA " + quote(acl.SchemaName)
			}
			objectType := "TABLES"
			if acl.ObjectKind == "S" {
				objectType = "SEQUENCES"
			}
			grantee := quote(acl.Grantee)
			if acl.Grantee == "PUBLIC" {
				grantee = "PUBLIC"
			}
			stmts = append(stmts, "ALTER DEFAULT PRIVILEGES FOR ROLE "+quote(acl.OwnerName)+schema+
				" REVOKE ALL ON "+objectType+" FROM "+grantee+" CASCADE")
		}
		writable := []string{"uns_meta", "uns_record", "uns_sample", "uns_record_unique_key", "uns_timeserial"}
		var objects []struct {
			Name, Kind      string
			AllowedSequence bool
		}
		if err := tx.Raw(`SELECT c.relname AS name,c.relkind::text AS kind,
			EXISTS(SELECT 1 FROM pg_depend d JOIN pg_class parent ON parent.oid=d.refobjid
			 JOIN pg_namespace pn ON pn.oid=parent.relnamespace
			 WHERE d.classid='pg_class'::regclass AND d.refclassid='pg_class'::regclass
			 AND d.objid=c.oid AND d.deptype IN ('a','i') AND d.refobjsubid>0
			 AND pn.nspname='public' AND parent.relname IN ?) AS allowed_sequence
			FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
			WHERE n.nspname='public' AND c.relkind IN ('r','p','v','m','f','S') ORDER BY c.oid`, writable).Scan(&objects).Error; err != nil {
			return err
		}
		for _, object := range objects {
			objectType, privileges := "TABLE", ""
			if object.Kind == "S" {
				objectType = "SEQUENCE"
				if object.AllowedSequence {
					privileges = "USAGE, SELECT, UPDATE"
				}
			} else if object.Name == "uns_collection" {
				privileges = "SELECT"
			} else {
				for _, table := range writable {
					if object.Name == table {
						privileges = "SELECT, INSERT, UPDATE, DELETE"
						break
					}
				}
			}
			name := "public." + quote(object.Name)
			// Revoke existing grants too: skipping an object would retain the old
			// blanket public grant after a shared Common/Sink upgrade.
			stmts = append(stmts, "REVOKE ALL ON "+objectType+" "+name+" FROM "+role+" CASCADE")
			if privileges == "" || privileges == "SELECT" {
				stmts = append(stmts, "REVOKE ALL ON "+objectType+" "+name+" FROM PUBLIC CASCADE")
			} else if objectType == "TABLE" {
				stmts = append(stmts, "REVOKE TRUNCATE,REFERENCES,TRIGGER ON TABLE "+name+" FROM PUBLIC CASCADE")
			}
			if privileges != "" {
				stmts = append(stmts, "GRANT "+privileges+" ON "+objectType+" "+name+" TO "+role)
			}
		}
		var columns []struct{ TableName, ColumnName string }
		if err := tx.Raw(`SELECT c.relname AS table_name,a.attname AS column_name
			FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
			JOIN pg_attribute a ON a.attrelid=c.oid
			WHERE n.nspname='public' AND c.relkind IN ('r','p','v','m','f')
			AND a.attnum>0 AND NOT a.attisdropped AND a.attacl IS NOT NULL`).Scan(&columns).Error; err != nil {
			return err
		}
		for _, column := range columns {
			stmts = append(stmts, "REVOKE ALL ("+quote(column.ColumnName)+") ON TABLE public."+
				quote(column.TableName)+" FROM "+role+" CASCADE")
			publicPrivileges := "ALL"
			for _, table := range writable {
				if column.TableName == table {
					publicPrivileges = "REFERENCES"
					break
				}
			}
			stmts = append(stmts, "REVOKE "+publicPrivileges+" ("+quote(column.ColumnName)+") ON TABLE public."+
				quote(column.TableName)+" FROM PUBLIC CASCADE")
		}
		for _, stmt := range stmts {
			if err := tx.Exec(stmt).Error; err != nil {
				return fmt.Errorf("tenantpg: grant UNS data role %q: %w", roleName, err)
			}
		}
		// Explicit REVOKE cannot remove ownership or unrelated inherited group
		// privileges. Fail closed rather than silently leaving those escape paths
		// or modifying another group's members and grants.
		for _, object := range objects {
			name := "public." + quote(object.Name)
			if object.Kind == "S" && object.AllowedSequence {
				continue
			}
			if object.Kind != "S" {
				allowed := false
				for _, table := range writable {
					allowed = allowed || object.Name == table
				}
				if allowed {
					continue
				}
			}
			var inherited bool
			var err error
			if object.Kind == "S" {
				err = tx.Raw(`SELECT has_sequence_privilege(?,?,'USAGE,SELECT,UPDATE')`, roleName, name).Scan(&inherited).Error
			} else {
				privileges, columns := "SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER", "SELECT,INSERT,UPDATE,REFERENCES"
				if object.Name == "uns_collection" {
					privileges, columns = "INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER", "INSERT,UPDATE,REFERENCES"
				}
				err = tx.Raw(`SELECT has_table_privilege(?,?,?) OR has_any_column_privilege(?,?,?)`,
					roleName, name, privileges, roleName, name, columns).Scan(&inherited).Error
			}
			if err != nil {
				return err
			}
			if inherited {
				return fmt.Errorf("tenantpg: data role %q retains forbidden ownership or inherited access to %s", roleName, name)
			}
		}
		// Keep runtime table isolation in the same authorization transaction.
		if err := tx.Exec(`SELECT public.protect_stream_runtime_storage()`).Error; err != nil {
			return fmt.Errorf("tenantpg: protect UNS runtime data after grants for %q: %w", roleName, err)
		}
		return nil
	})
}

func isDuplicateDatabaseErr(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == dupDatabaseErrCode
	}
	return false
}

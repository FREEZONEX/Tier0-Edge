package tenantpg

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const ProjectRestoreContractRevision = 2
const ProjectRestoreMaxContractBytes = 1 << 20
const projectRestoreMaxRoles = 4096
const projectRestoreMaxGrants = (projectRestoreMaxRoles + 1) * 3

// Empty Grantee denotes PUBLIC, not a role named PUBLIC. Grantor is retained
// so unsupported grant chains fail preflight instead of changing their meaning.
type ProjectRestoreDatabaseGrant struct {
	Grantor   string `json:"grantor"`
	Grantee   string `json:"grantee"`
	Privilege string `json:"privilege"`
	Grantable bool   `json:"grantable"`
}

// ProjectRestoreContract binds a native project dump to its role dependencies.
// It contains neither role passwords nor the installation's derivation key.
type ProjectRestoreContract struct {
	SchemaRevision int                           `json:"schemaRevision"`
	Database       string                        `json:"database"`
	DumpSHA256     string                        `json:"dumpSHA256"`
	Roles          []string                      `json:"roles"`
	DatabaseOwner  string                        `json:"databaseOwner"`
	DatabaseACL    []ProjectRestoreDatabaseGrant `json:"databaseACL"`
}

type ProjectRestoreCheck struct {
	Database                    string `json:"database"`
	RolesValidated              int    `json:"rolesValidated"`
	RequiresProjectRoleCreation bool   `json:"requiresProjectRoleCreation"`
}

type ProjectRestorePreparation struct {
	Database            string `json:"database"`
	ProjectRolePrepared bool   `json:"projectRolePrepared"`
}

type ProjectRestoreACLResult struct {
	Database       string `json:"database"`
	GrantsRestored int    `json:"grantsRestored"`
}

// RestoreDatabaseACL is explicit restore maintenance, never normal App
// provisioning. Only the named database's grants change; owners, passwords,
// role attributes and schema/table permissions are untouched.
func (l *Lifecycle) RestoreDatabaseACL(ctx context.Context, contract ProjectRestoreContract) (ProjectRestoreACLResult, error) {
	result := ProjectRestoreACLResult{Database: contract.Database}
	if _, err := l.CheckRestoreContract(ctx, contract); err != nil {
		return result, err
	}
	admin, closeAdmin, err := l.registry.GetAdminDB(ctx)
	if err != nil {
		return result, fmt.Errorf("tenantpg: cannot open restore ACL coordinator")
	}
	defer closeAdmin()
	err = admin.WithContext(ctx).Transaction(func(lock *gorm.DB) error {
		// Use the management DB: advisory locks in another database would not
		// serialize this operation with ordinary project-role preparation.
		if err := lock.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "tier0-project-role:"+DeriveRoleName(contract.Database)).Error; err != nil {
			return fmt.Errorf("tenantpg: cannot lock project ACL restoration")
		}
		db, closeDB, err := l.registry.GetAdminProjectDB(ctx, contract.Database)
		if err != nil {
			return fmt.Errorf("tenantpg: cannot open restore ACL target")
		}
		defer closeDB()
		return db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Transaction(func(tx *gorm.DB) error {
			owner, actual, err := readRestoreDatabaseACL(tx)
			if err != nil || owner != contract.DatabaseOwner {
				return fmt.Errorf("tenantpg: restore database owner differs; explicit ownership maintenance is required")
			}
			var actor string
			if err := tx.Raw(`SELECT current_user`).Scan(&actor).Error; err != nil || actor != owner {
				return fmt.Errorf("tenantpg: restore ACL maintenance identity differs from database owner")
			}
			grantees := make(map[string]bool)
			for _, grant := range actual {
				if grant.Grantor != owner {
					return fmt.Errorf("tenantpg: target database grant chains require explicit maintenance")
				}
				grantees[grant.Grantee] = true
			}
			ordered := make([]string, 0, len(grantees))
			for name := range grantees {
				ordered = append(ordered, name)
			}
			sort.Strings(ordered)
			database := pgx.Identifier{contract.Database}.Sanitize()
			for _, name := range ordered {
				if err := tx.Exec("REVOKE ALL PRIVILEGES ON DATABASE " + database + " FROM " + restoreGrantRecipient(name)).Error; err != nil {
					return fmt.Errorf("tenantpg: cannot revoke target database privileges during restore")
				}
			}
			for _, grant := range contract.DatabaseACL {
				option := ""
				if grant.Grantable {
					option = " WITH GRANT OPTION"
				}
				// Privilege is an enum validated before any database mutation.
				if err := tx.Exec("GRANT " + grant.Privilege + " ON DATABASE " + database + " TO " + restoreGrantRecipient(grant.Grantee) + option).Error; err != nil {
					return fmt.Errorf("tenantpg: cannot restore declared database privilege")
				}
			}
			owner, actual, err = readRestoreDatabaseACL(tx)
			if err != nil || owner != contract.DatabaseOwner || !slices.Equal(actual, contract.DatabaseACL) {
				return fmt.Errorf("tenantpg: restored database ACL does not match its declaration")
			}
			return nil
		})
	})
	if err == nil {
		result.GrantsRestored = len(contract.DatabaseACL)
	}
	return result, err
}

func restoreGrantRecipient(name string) string {
	if name == "" {
		return "PUBLIC"
	}
	return pgx.Identifier{name}.Sanitize()
}

func restoreGrantKey(grant ProjectRestoreDatabaseGrant) string {
	return grant.Grantee + "\x00" + grant.Privilege + "\x00" + grant.Grantor
}

func readRestoreDatabaseACL(db *gorm.DB) (string, []ProjectRestoreDatabaseGrant, error) {
	var owner string
	var grants []ProjectRestoreDatabaseGrant
	if err := db.Raw(`SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname=current_database()`).Scan(&owner).Error; err != nil || owner == "" {
		return "", nil, fmt.Errorf("tenantpg: cannot read restore database owner")
	}
	if err := db.Raw(`SELECT grantor.rolname AS grantor, COALESCE(grantee.rolname, '') AS grantee,
		a.privilege_type AS privilege, a.is_grantable AS grantable
		FROM pg_database d CROSS JOIN LATERAL aclexplode(COALESCE(d.datacl, acldefault('d', d.datdba))) a
		JOIN pg_roles grantor ON grantor.oid=a.grantor
		LEFT JOIN pg_roles grantee ON grantee.oid=a.grantee
		WHERE d.datname=current_database() LIMIT ?`, projectRestoreMaxGrants+1).Scan(&grants).Error; err != nil {
		return "", nil, fmt.Errorf("tenantpg: cannot read restore database privileges")
	}
	if len(grants) > projectRestoreMaxGrants {
		return "", nil, fmt.Errorf("tenantpg: restore database privileges exceed their budget")
	}
	if grants == nil {
		grants = []ProjectRestoreDatabaseGrant{}
	}
	sort.Slice(grants, func(i, j int) bool { return restoreGrantKey(grants[i]) < restoreGrantKey(grants[j]) })
	return owner, grants, nil
}

// PrepareRestoreContract is an explicit maintenance operation, not preflight.
// The caller must already have created the target database. It neither creates
// schemas/data nor changes existing passwords or object ownership.
func (l *Lifecycle) PrepareRestoreContract(ctx context.Context, contract ProjectRestoreContract) (ProjectRestorePreparation, error) {
	result := ProjectRestorePreparation{Database: contract.Database}
	if _, err := l.CheckRestoreContract(ctx, contract); err != nil {
		return result, err
	}
	db, closeDB, err := l.registry.GetAdminProjectDB(ctx, contract.Database)
	if err != nil {
		return result, fmt.Errorf("tenantpg: restore target database must already exist and be accessible")
	}
	defer closeDB()
	// Registry connections can be lazy: prove the database exists before DDL.
	var actualDatabase string
	if err := db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).
		Raw(`SELECT current_database()`).Scan(&actualDatabase).Error; err != nil || actualDatabase != contract.Database {
		return result, fmt.Errorf("tenantpg: restore target database must already exist and be accessible")
	}
	managedRole := DeriveRoleName(contract.Database)
	index := sort.SearchStrings(contract.Roles, managedRole)
	if index == len(contract.Roles) || contract.Roles[index] != managedRole {
		return result, nil
	}
	// PostgreSQL advisory locks are database-scoped. Use the same management
	// database as normal App provisioning, not the restored project connection.
	admin, closeAdmin, err := l.registry.GetAdminDB(ctx)
	if err != nil {
		return result, fmt.Errorf("tenantpg: cannot open restore role catalog")
	}
	defer closeAdmin()
	if _, err := l.prepareProjectRole(ctx, admin, contract.Database, true); err != nil {
		return result, err
	}
	result.ProjectRolePrepared = true
	return result, nil
}

func (c ProjectRestoreContract) Validate() error {
	if c.SchemaRevision != ProjectRestoreContractRevision {
		return fmt.Errorf("tenantpg: unsupported project restore contract revision")
	}
	if err := ValidateIdentifier(c.Database); err != nil {
		return fmt.Errorf("tenantpg: invalid restore database identifier")
	}
	if err := validateRestoreDumpHash(c.DumpSHA256); err != nil {
		return err
	}
	if len(c.Roles) == 0 || len(c.Roles) > projectRestoreMaxRoles {
		return fmt.Errorf("tenantpg: invalid restore role reference count")
	}
	for i, name := range c.Roles {
		// Quoted PostgreSQL role names need not match our generated identifiers.
		// Use bound catalog values or safely quoted identifiers, never raw SQL.
		if len(name) == 0 || len(name) > 63 || !utf8.ValidString(name) || strings.ContainsRune(name, 0) {
			return fmt.Errorf("tenantpg: invalid restore role reference")
		}
		if i > 0 && c.Roles[i-1] >= name {
			return fmt.Errorf("tenantpg: restore role references must be unique and sorted")
		}
	}
	containsRole := func(name string) bool {
		index := sort.SearchStrings(c.Roles, name)
		return index < len(c.Roles) && c.Roles[index] == name
	}
	if c.DatabaseOwner == "" || !containsRole(c.DatabaseOwner) {
		return fmt.Errorf("tenantpg: restore database owner must be a declared role")
	}
	if c.DatabaseACL == nil || len(c.DatabaseACL) > projectRestoreMaxGrants {
		return fmt.Errorf("tenantpg: invalid restore database privilege count")
	}
	for i, grant := range c.DatabaseACL {
		if !containsRole(grant.Grantor) || (grant.Grantee != "" && !containsRole(grant.Grantee)) {
			return fmt.Errorf("tenantpg: database privilege refers to an undeclared role")
		}
		switch grant.Privilege {
		case "CONNECT", "CREATE", "TEMPORARY":
		default:
			return fmt.Errorf("tenantpg: unsupported restore database privilege")
		}
		if grant.Grantee == "" && grant.Grantable {
			return fmt.Errorf("tenantpg: PUBLIC cannot receive grant options")
		}
		if i > 0 && restoreGrantKey(c.DatabaseACL[i-1]) >= restoreGrantKey(grant) {
			return fmt.Errorf("tenantpg: database privileges must be unique and sorted")
		}
	}
	encoded, err := json.Marshal(c)
	if err != nil || len(encoded)+1 > ProjectRestoreMaxContractBytes {
		return fmt.Errorf("tenantpg: restore contract exceeds its input budget")
	}
	return nil
}

func validateRestoreDumpHash(value string) error {
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 || value != strings.ToLower(value) {
		return fmt.Errorf("tenantpg: invalid restore dump SHA-256")
	}
	return nil
}

// ExportRestoreContract reads catalogs only. Saving data must not depend on a
// working project-role key; credential checks belong to restore preflight.
func (l *Lifecycle) ExportRestoreContract(ctx context.Context, databaseName, dumpSHA256 string) (ProjectRestoreContract, error) {
	contract := ProjectRestoreContract{SchemaRevision: ProjectRestoreContractRevision, Database: databaseName, DumpSHA256: dumpSHA256}
	if err := ValidateIdentifier(databaseName); err != nil {
		return contract, fmt.Errorf("tenantpg: invalid restore database identifier")
	}
	if err := validateRestoreDumpHash(dumpSHA256); err != nil {
		return contract, err
	}
	db, closeDB, err := l.registry.GetAdminProjectDB(ctx, databaseName)
	if err != nil {
		return contract, fmt.Errorf("tenantpg: cannot open project restore catalog")
	}
	defer closeDB()
	db = db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		contract.DatabaseOwner, contract.DatabaseACL, err = readRestoreDatabaseACL(tx)
		if err != nil {
			return err
		}
		// Shared dependencies include ownership, ACLs and default ACLs. Database
		// ACL dependencies have dbid=0, unlike dependencies inside the database.
		// Include database/schema owners explicitly (built-in roles may be pinned).
		return tx.Raw(`WITH current_db AS (SELECT oid, datdba FROM pg_database WHERE datname=current_database()),
			role_refs AS (
				SELECT d.refobjid AS role_id FROM pg_shdepend d, current_db c
				WHERE d.refclassid='pg_authid'::regclass AND
				 (d.dbid=c.oid OR (d.dbid=0 AND d.classid='pg_database'::regclass AND d.objid=c.oid))
				UNION SELECT datdba FROM current_db
				UNION SELECT nspowner FROM pg_namespace
			)
			SELECT DISTINCT r.rolname FROM role_refs d JOIN pg_roles r ON r.oid=d.role_id LIMIT ?`, projectRestoreMaxRoles+1).Scan(&contract.Roles).Error
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return contract, fmt.Errorf("tenantpg: cannot inspect project restore role references")
	}
	sort.Strings(contract.Roles)
	return contract, contract.Validate()
}

// CheckRestoreContract never creates roles or grants access. A missing managed
// role is a plan for the explicit restore phase, not a claim it is ready to run.
func (l *Lifecycle) CheckRestoreContract(ctx context.Context, contract ProjectRestoreContract) (ProjectRestoreCheck, error) {
	result := ProjectRestoreCheck{Database: contract.Database}
	if err := contract.Validate(); err != nil {
		return result, err
	}
	for _, grant := range contract.DatabaseACL {
		if grant.Grantor != contract.DatabaseOwner {
			return result, fmt.Errorf("tenantpg: restore database grant chains require explicit maintenance")
		}
	}
	managedRole := DeriveRoleName(contract.Database)
	managedIndex := sort.SearchStrings(contract.Roles, managedRole)
	needsManagedRole := managedIndex < len(contract.Roles) && contract.Roles[managedIndex] == managedRole
	if needsManagedRole && strings.TrimSpace(l.roleSecret) == "" {
		return result, fmt.Errorf("tenantpg: project role secret is required for restore preflight")
	}
	db, closeDB, err := l.registry.GetAdminDB(ctx)
	if err != nil {
		return result, fmt.Errorf("tenantpg: cannot open restore role catalog")
	}
	defer closeDB()
	db = db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var actor, existingOwner string
		if err := tx.Raw(`SELECT current_user`).Scan(&actor).Error; err != nil || actor != contract.DatabaseOwner {
			return fmt.Errorf("tenantpg: database ownership migration requires explicit maintenance")
		}
		if err := tx.Raw(`SELECT pg_get_userbyid(datdba) FROM pg_database WHERE datname=?`, contract.Database).Scan(&existingOwner).Error; err != nil {
			return fmt.Errorf("tenantpg: cannot inspect restore database owner")
		}
		if existingOwner != "" && existingOwner != contract.DatabaseOwner {
			return fmt.Errorf("tenantpg: restore database owner differs; explicit ownership maintenance is required")
		}
		var existing []string
		if err := tx.Raw(`SELECT rolname FROM pg_roles WHERE rolname IN ?`, contract.Roles).Scan(&existing).Error; err != nil {
			return fmt.Errorf("tenantpg: cannot inspect restore roles")
		}
		found := make(map[string]bool, len(existing))
		for _, name := range existing {
			found[name] = true
		}
		for _, name := range contract.Roles {
			if !found[name] {
				if name != managedRole {
					return fmt.Errorf("tenantpg: required restore role %q is missing; explicit role preparation is required", name)
				}
				result.RequiresProjectRoleCreation = true
				continue
			}
			result.RolesValidated++
		}
		if needsManagedRole && found[managedRole] {
			if _, err := inspectProjectRole(ctx, tx, managedRole); err != nil {
				return err
			}
			_, password, err := DeriveCredentials(l.roleSecret, contract.Database)
			if err != nil {
				return fmt.Errorf("tenantpg: cannot derive restore project credentials")
			}
			// The project database may not exist yet. template1 verifies the
			// password without granting anything or creating the target database.
			return l.authenticateProjectRole(ctx, "template1", managedRole, password)
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	return result, err
}

package repo

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// preflightMigrations must run before baseline DDL. It is a read-only cutover
// gate, not a legacy adapter: an old database requires the explicit one-time
// upgrade, never a second set of writable tables created by normal startup.
func (s *Store) preflightMigrations(ctx context.Context) error {
	if s == nil || s.CommonDB() == nil || s.UnsDB() == nil {
		return fmt.Errorf("migration preflight: database is not initialized")
	}
	var legacy bool
	if err := s.UnsDB().WithContext(ctx).Raw(`SELECT
		to_regclass('uns.uns_meta') IS NOT NULL OR
		to_regclass('uns.uns_record') IS NOT NULL OR
		to_regclass('uns.uns_sample') IS NOT NULL`).Scan(&legacy).Error; err != nil {
		return fmt.Errorf("migration preflight: inspect UNS storage: %w", err)
	}
	if legacy {
		return fmt.Errorf("migration preflight: legacy uns storage requires the explicit one-time offline cutover; no baseline DDL was executed")
	}
	for _, target := range []struct {
		scope string
		db    *gorm.DB
	}{
		{scope: "common", db: s.CommonDB()},
		{scope: "sink", db: s.UnsDB()},
	} {
		if err := preflightMigrationLedger(ctx, target.db, target.scope); err != nil {
			return err
		}
	}
	return nil
}

func preflightMigrationLedger(ctx context.Context, db *gorm.DB, scope string) error {
	var exists bool
	if err := db.WithContext(ctx).Raw("SELECT to_regclass(?) IS NOT NULL", migrationLedgerTable).Scan(&exists).Error; err != nil {
		return fmt.Errorf("migration preflight: inspect %s ledger: %w", scope, err)
	}
	if !exists {
		return nil
	}
	migrations, err := loadVersionedMigrations(scope, "migrations/versions/"+scope)
	if err != nil {
		return err
	}
	key := func(version string, sequence int, name string) string {
		return fmt.Sprintf("%s/%d/%s", version, sequence, name)
	}
	byIdentity := make(map[string]versionedMigration, len(migrations))
	for _, migration := range migrations {
		byIdentity[key(migration.Version, migration.Sequence, migration.Name)] = migration
	}
	var rows []struct {
		Version  string
		Sequence int
		Name     string
		Checksum string
	}
	if err := db.WithContext(ctx).Table(migrationLedgerTable).
		Select("version", "sequence", "name", "checksum").
		Where("scope = ?", scope).Order("version, sequence, name").Find(&rows).Error; err != nil {
		return fmt.Errorf("migration preflight: read %s ledger: %w", scope, err)
	}
	for _, row := range rows {
		identity := key(row.Version, row.Sequence, row.Name)
		migration, ok := byIdentity[identity]
		if !ok {
			return fmt.Errorf("migration preflight: %s migration %s is not included in this release; explicit cutover is required", scope, identity)
		}
		if row.Checksum != migration.Checksum {
			return fmt.Errorf("migration preflight: migration checksum mismatch for %s; no baseline DDL was executed", migration.Path)
		}
	}
	return nil
}

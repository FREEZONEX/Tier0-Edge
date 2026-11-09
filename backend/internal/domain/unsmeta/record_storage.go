package unsmeta

import (
	repo "backend/internal/repo"
	"encoding/json"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Writer reads include tombstones so every insert/upsert can reuse the identity,
// continue revision, and retain created_at. Ordinary readers keep the active scope.
func recordForWrite(tx *gorm.DB, collectionID, recordID string) (repo.UnsRecord, error) {
	var row repo.UnsRecord
	err := tx.Unscoped().Where("collection_id=? AND id=?", collectionID, recordID).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error
	return row, err
}
func recordWriteColumns(old repo.UnsRecord, data json.RawMessage, now int64) map[string]any {
	columns := map[string]any{"data": data, "revision": old.Revision + 1, "updated_at": now}
	if old.DeletedAt.Valid {
		for _, key := range []string{"deleted_at", "lock_token_hash", "lock_principal", "lock_label", "lock_expires_at", "lock_since"} {
			columns[key] = nil
		}
	}
	return columns
}

package repo

import (
	"encoding/json"

	"gorm.io/gorm"
)

// UnsRecordLock identifies both existing and absent records. Callers must hold
// the shared metadata gate (or its exclusive variant) before acquiring locks.
type UnsRecordLock struct {
	CollectionID string
	RecordID     string
}

// LockUnsRecords acquires the complete write/read-check set in a common order.
// Sorting the database hash values also keeps the order stable on hash collisions.
// Multi-record callers must pass all identities before reading mutable rows.
func LockUnsRecords(tx *gorm.DB, records ...UnsRecordLock) error {
	if len(records) == 0 {
		return nil
	}
	keys := make([]string, 0, len(records))
	for _, record := range records {
		keys = append(keys, record.CollectionID+"/"+record.RecordID)
	}
	raw, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	// Materialize the deduplicated keys before invoking the volatile lock
	// function. The outer ORDER BY makes its evaluation order explicit too;
	// ordering only the input JSON would not order hash collisions or hashes.
	return tx.Exec(`WITH keys AS MATERIALIZED (
		SELECT DISTINCT hashtextextended(value, 814206109) AS lock_key
		FROM jsonb_array_elements_text(?::jsonb) AS value
	)
	SELECT pg_advisory_xact_lock(lock_key) FROM keys ORDER BY lock_key`, string(raw)).Error
}

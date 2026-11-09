package repo

import (
	"context"

	"gorm.io/gorm"
)

// Runtime operations share the metadata constraint gate with ordinary UNS writes.
// Take it before owner, Source, Stream and execution locks, never from inside a
// result write after those locks have already been acquired. Source mutations
// use exclusive mode because their callback can archive the Source.
func streamRuntimeTransaction(ctx context.Context, db *gorm.DB, exclusive bool, action func(*gorm.DB) error) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		gate := "SELECT pg_advisory_xact_lock_shared(814206108)"
		if exclusive {
			gate = "SELECT pg_advisory_xact_lock(814206108)"
		}
		if err := tx.Exec(gate).Error; err != nil {
			return err
		}
		return action(tx)
	})
}

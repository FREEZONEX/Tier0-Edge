package repo

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Only the private, typed runtime tables can use this idempotent insert.
// Frozen output is read back from PostgreSQL before its first commit/recovery.
func insertRuntimeIgnore(db *gorm.DB, item any) *gorm.DB {
	switch item.(type) {
	case *StreamRun, *StreamTask, *StreamEventMatch, *StreamProgramIntervalCursor, *StreamProgramIntervalOutbox:
		return db.Clauses(clause.OnConflict{DoNothing: true}).Create(item)
	default:
		out := db.Session(&gorm.Session{})
		out.AddError(ErrInvalidArgument)
		return out
	}
}

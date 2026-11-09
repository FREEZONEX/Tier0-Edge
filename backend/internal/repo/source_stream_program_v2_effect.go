package repo

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The journal records dispatch intent, not a transaction with the device. A
// dispatching record encountered after a crash must never dispatch twice.
type StreamProgramEffect struct {
	RunID       string `gorm:"column:run_id;primaryKey"`
	EntryID     string `gorm:"column:entry_id;primaryKey"`
	CallID      string `gorm:"column:call_id;primaryKey"`
	OpID        string `gorm:"column:op_id"`
	RequestHash string `gorm:"column:request_hash"`
	Status      string `gorm:"column:status"`
	Result      string `gorm:"column:result"`
	CreatedAt   int64  `gorm:"column:created_at"`
	UpdatedAt   int64  `gorm:"column:updated_at"`
}

func (StreamProgramEffect) TableName() string { return "public.uns_stream_effect" }

func validProgramEffectIdentity(runID, entryID, callID, hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return runID != "" && len(runID) <= 64 && entryID != "" && len(entryID) <= 128 &&
		callID != "" && len(callID) <= 128 && err == nil && len(decoded) == 32 && hash == hex.EncodeToString(decoded)
}

func effectQuery(tx *gorm.DB, runID, entryID, callID string) *gorm.DB {
	return tx.Where("run_id=? AND entry_id=? AND call_id=?", runID, entryID, callID)
}

func (r *SourceStreamRepo) ReserveProgramEffect(ctx context.Context, item StreamProgramEffect) (StreamProgramEffect, error) {
	if !validProgramEffectIdentity(item.RunID, item.EntryID, item.CallID, item.RequestHash) || item.OpID == "" || len(item.OpID) > 256 {
		return item, ErrInvalidArgument
	}
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if err := guardProgramRunOwner(tx, item.RunID); err != nil {
			return err
		}
		var run StreamRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", item.RunID).Take(&run).Error; err != nil {
			return err
		}
		if run.EntryID != item.EntryID || run.Model != "program" {
			return ErrConflict
		}
		var existing StreamProgramEffect
		err := effectQuery(tx, item.RunID, item.EntryID, item.CallID).Take(&existing).Error
		if err == nil {
			if existing.RequestHash != item.RequestHash || existing.OpID != item.OpID {
				return ErrConflict
			}
			item = existing
			return nil
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		if run.Status != "processing" {
			return ErrConflict
		}
		item.Status, item.Result = "pending", "null"
		item.CreatedAt, item.UpdatedAt = time.Now().UnixMilli(), time.Now().UnixMilli()
		return tx.Create(&item).Error
	})
	return item, err
}

func (r *SourceStreamRepo) ClaimProgramEffect(ctx context.Context, runID, entryID, callID, requestHash string) (StreamProgramEffect, bool, error) {
	var item StreamProgramEffect
	if !validProgramEffectIdentity(runID, entryID, callID, requestHash) {
		return item, false, ErrInvalidArgument
	}
	claimed := false
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if err := guardProgramRunOwner(tx, runID); err != nil {
			return err
		}
		var run StreamRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", runID).Take(&run).Error; err != nil {
			return err
		}
		if run.Model != "program" || run.EntryID != entryID {
			return ErrConflict
		}
		if err := effectQuery(tx.Clauses(clause.Locking{Strength: "UPDATE"}), runID, entryID, callID).Take(&item).Error; err != nil {
			return err
		}
		if item.RequestHash != requestHash {
			return ErrConflict
		}
		if item.Status != "pending" {
			return nil // Caller inspects terminal/dispatching, never redispatches.
		}
		if run.Status != "processing" {
			return ErrConflict
		}
		result := effectQuery(tx.Model(&StreamProgramEffect{}), runID, entryID, callID).
			Where("status='pending' AND request_hash=?", requestHash).
			Updates(map[string]any{"status": "dispatching", "updated_at": time.Now().UnixMilli()})
		if result.Error != nil {
			return result.Error
		}
		claimed = result.RowsAffected == 1
		if claimed {
			item.Status = "dispatching"
		}
		return nil
	})
	return item, claimed, err
}

// rejected means a definite rejection/no operation; unknown must be used when
// dispatch may have happened. Neither state permits an automatic redispatch.
func (r *SourceStreamRepo) FinishProgramEffect(ctx context.Context, runID, entryID, callID, requestHash, status, result string) error {
	if !validProgramEffectIdentity(runID, entryID, callID, requestHash) ||
		(status != "completed" && status != "unknown" && status != "rejected") || len(result) > 1024*1024 || !json.Valid([]byte(result)) {
		return ErrInvalidArgument
	}
	return streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		var item StreamProgramEffect
		if err := effectQuery(tx.Clauses(clause.Locking{Strength: "UPDATE"}), runID, entryID, callID).Take(&item).Error; err != nil {
			return err
		}
		if item.RequestHash != requestHash {
			return ErrConflict
		}
		if item.Status != "dispatching" {
			// Do not replace a stored result, even with another syntactically
			// valid JSON value. Idempotent completion reuses the durable result.
			if item.Status == status {
				return nil
			}
			return ErrConflict
		}
		q := effectQuery(tx.Model(&StreamProgramEffect{}), runID, entryID, callID).
			Where("status='dispatching' AND request_hash=?", requestHash).
			Updates(map[string]any{"status": status, "result": result, "updated_at": time.Now().UnixMilli()})
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}

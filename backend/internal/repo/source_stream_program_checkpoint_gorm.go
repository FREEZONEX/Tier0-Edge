package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReserveProgramMetadataCommit guards the complete frozen output batch, including
// successful filtered pages without outputs. It must use the business transaction.
func ReserveProgramMetadataCommit(tx *gorm.DB, runID, digest string) (bool, error) {
	if runID == "" || len(runID) > 64 || len(digest) != 64 {
		return false, ErrInvalidArgument
	}
	created, err := putIntegrationTask(tx, "programMetadataCommit", runID, map[string]string{"digest": digest})
	if err != nil || created {
		return created, err
	}
	var receipt map[string]string
	if err := getIntegrationTask(tx, "programMetadataCommit", runID, &receipt); err != nil {
		return false, err
	}
	if receipt["digest"] != digest {
		return false, ErrConflict
	}
	return false, nil
}

// LockProgramCheckpointGORM shares the canonical UNS transaction; it does not
// start another transaction or touch Source acquisition sequence checkpoints.
func LockProgramCheckpointGORM(ctx context.Context, tx *gorm.DB, change ProgramCheckpointCommit) (bool, func() error, error) {
	if change.StreamID <= 0 || change.EntryID == "" || len(change.EntryID) > 128 || change.RunID == "" || len(change.RunID) > 64 || len(change.Digest) != 64 || change.ExpectedRevision < 0 || !json.Valid(change.Value) || len(change.Value) > MaxProgramCheckpointBytes {
		return false, nil, ErrInvalidArgument
	}
	tx = tx.WithContext(ctx)
	created, err := putIntegrationTask(tx, "programCheckpointCommit", change.RunID, map[string]string{"digest": change.Digest})
	if err != nil {
		return false, nil, err
	}
	if !created {
		var receipt map[string]string
		if err := getIntegrationTask(tx, "programCheckpointCommit", change.RunID, &receipt); err != nil {
			return false, nil, err
		}
		if receipt["digest"] != change.Digest {
			return false, nil, ErrConflict
		}
		return false, nil, nil
	}
	id := programIdentity(change.StreamID, change.EntryID)
	if _, err := putIntegrationTask(tx, "programCheckpoint", id, ProgramCheckpoint{Value: json.RawMessage(`null`)}); err != nil {
		return false, nil, err
	}
	var row integrationTaskRecord
	if err := taskQuery(tx, "programCheckpoint").Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", runtimeTaskID("programCheckpoint", id)).Take(&row).Error; err != nil {
		return false, nil, err
	}
	var document integrationTaskData
	var cursor ProgramCheckpoint
	if json.Unmarshal(row.Data, &document) != nil || json.Unmarshal(document.Payload, &cursor) != nil || cursor.Revision != change.ExpectedRevision {
		return false, nil, ErrConflict
	}
	finish := func() error {
		payload, err := json.Marshal(ProgramCheckpoint{Revision: cursor.Revision + 1, Value: change.Value})
		if err != nil {
			return err
		}
		data, err := json.Marshal(integrationTaskData{Type: "programCheckpoint", Identity: "programCheckpoint:" + id, Payload: payload})
		if err != nil {
			return err
		}
		result := taskQuery(tx, "programCheckpoint").Where("id=? AND revision=?", row.ID, row.Revision).Updates(map[string]any{"data": data, "revision": row.Revision + 1, "updated_at": time.Now().UnixMilli()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	}
	return true, finish, nil
}

func ProgramMetadataCommitted(ctx context.Context, runID, digest string) (bool, error) {
	if runID == "" || len(runID) > 64 || len(digest) != 64 {
		return false, ErrInvalidArgument
	}
	var receipt map[string]string
	err := getIntegrationTask(GetUnsConn(ctx).WithContext(ctx), "programMetadataCommit", runID, &receipt)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if receipt["digest"] != digest {
		return false, ErrConflict
	}
	return true, nil
}

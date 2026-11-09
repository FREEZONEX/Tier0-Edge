package repo

import (
	"context"
	"encoding/json"
	"time"

	"backend/internal/streamexec"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func lockProgramV2Execution(tx *gorm.DB, run StreamRun, activeOnly bool) (StreamVersion, error) {
	var version StreamVersion
	if run.ID == "" || len(run.ID) > 64 || run.EntryID == "" || len(run.EntryID) > 128 || run.Model != "program" || run.StreamID <= 0 || run.VersionID <= 0 || !json.Valid([]byte(run.Summary)) || len(run.Summary) > 1024*1024 {
		return version, ErrInvalidArgument
	}
	if err := tx.Where("id=? AND stream_id=?", run.VersionID, run.StreamID).Take(&version).Error; err != nil {
		return version, err
	}
	if version.ActivatedAt <= 0 {
		return version, ErrConflict
	}
	manifest, err := version.ProgramV2Manifest()
	if err != nil {
		return version, err
	}
	ids, _, _, err := programV2References(manifest)
	if err != nil {
		return version, err
	}
	sources, err := lockProgramV2Sources(tx, run.StreamID, ids)
	if err != nil {
		return version, err
	}
	snapshots, err := version.Sources()
	if err != nil {
		return version, err
	}
	if err := validateProgramV2Snapshots(manifest, sources, snapshots, false); err != nil {
		return version, err
	}
	if err := version.validateExecutionSources(sources); err != nil {
		return version, err
	}
	var stream Stream
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND deleted_at=0", run.StreamID).Take(&stream).Error; err != nil {
		return version, err
	}
	if stream.Model != "program" || stream.SourceID != 0 || stream.Status != "enabled" || (activeOnly && stream.ActiveVersion != run.VersionID) {
		return version, ErrConflict
	}
	found := false
	for _, entry := range manifest.Entries {
		if entry.ID != run.EntryID {
			continue
		}
		found = true
		if activeOnly && entry.Kind != "request" && entry.Kind != "http-request" {
			return version, ErrConflict
		}
		if entry.Kind == "source-event" || entry.Kind == "http-request" {
			id, _ := programStableID(entry.SourceID)
			if run.SourceID != id {
				return version, ErrConflict
			}
		} else if run.SourceID != 0 {
			return version, ErrConflict
		}
	}
	if !found {
		return version, ErrConflict
	}
	return version, nil
}

func sameProgramRunIdentity(a, b StreamRun) bool {
	return a.ID == b.ID && a.StreamID == b.StreamID && a.VersionID == b.VersionID && a.EntryID == b.EntryID && a.Model == b.Model && a.SourceID == b.SourceID
}

func programRunRequestHash(summary string) (string, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(summary), &fields) != nil || fields == nil {
		return "", ErrInvalidArgument
	}
	raw, ok := fields["requestHash"]
	if !ok {
		return "", nil
	}
	var hash string
	if json.Unmarshal(raw, &hash) != nil || hash == "" || len(hash) > 128 {
		return "", ErrInvalidArgument
	}
	return hash, nil
}

// Admission precedes script evaluation, so effects always have an existing
// durable parent intent. A replay may inspect the existing run but cannot
// silently change its entry, version, origin or request hash.
func (r *SourceStreamRepo) AdmitProgramV2Run(ctx context.Context, item StreamRun, activeOnly bool) (bool, error) {
	created := false
	if item.Status != "processing" {
		return false, ErrInvalidArgument
	}
	requestHash, err := programRunRequestHash(item.Summary)
	if err != nil {
		return false, err
	}
	err = streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if _, err := lockProgramV2Execution(tx, item, activeOnly); err != nil {
			return err
		}
		if item.CreatedAt == 0 {
			item.CreatedAt = time.Now().UnixMilli()
		}
		q := insertRuntimeIgnore(tx, &item)
		if q.Error != nil {
			return q.Error
		}
		created = q.RowsAffected == 1
		if created {
			return nil
		}
		var old StreamRun
		if err := tx.Where("id=?", item.ID).Take(&old).Error; err != nil {
			return err
		}
		if !sameProgramRunIdentity(item, old) {
			return ErrConflict
		}
		persistedHash, err := programRunRequestHash(old.Summary)
		if err != nil {
			return err
		}
		if requestHash != persistedHash {
			return ErrConflict
		}
		return nil
	})
	return created, normalizeDBError(err)
}

// Store the evaluated outputs once. Topic commit retries use this task rather
// than running the script (and device effects) again.
func (r *SourceStreamRepo) PutProgramV2Task(ctx context.Context, item StreamTask, run StreamRun) (StreamTask, error) {
	if item.ID != run.ID || item.EntryID != run.EntryID || item.StreamID != run.StreamID || item.VersionID != run.VersionID || item.SourceID != run.SourceID || item.Model != "program" ||
		!json.Valid([]byte(item.Output)) || len(item.Output) > streamexec.MaxProgramDataBytes {
		return item, ErrInvalidArgument
	}
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if err := guardProgramRunOwner(tx, run.ID); err != nil {
			return err
		}
		if _, err := lockProgramV2Execution(tx, run, false); err != nil {
			return err
		}
		var admitted StreamRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", run.ID).Take(&admitted).Error; err != nil {
			return err
		}
		if !sameProgramRunIdentity(run, admitted) {
			return ErrConflict
		}
		var stored StreamTask
		err := tx.Where("id=?", item.ID).Take(&stored).Error
		if err == nil {
			if stored.StreamID != item.StreamID || stored.VersionID != item.VersionID || stored.EntryID != item.EntryID || stored.SourceID != item.SourceID || stored.Model != "program" {
				return ErrConflict
			}
			item = stored
			return scheduleProgramOutputRecovery(tx, item.ID)
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		if admitted.Status != "processing" {
			return ErrConflict
		}
		item.Status, item.CreatedAt = "pending", time.Now().UnixMilli()
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		// JSONB normalizes nested JSON and numbers. First commit and recovery
		// must both consume this persisted representation, including checkpoints.
		if err := tx.Where("id=?", item.ID).Take(&item).Error; err != nil {
			return err
		}
		return scheduleProgramOutputRecovery(tx, item.ID)
	})
	return item, normalizeDBError(err)
}

// ResolveProgramV2PreparationFailure is only called by the execution owner
// after its result-persistence attempt has returned. It must not be used as an
// age-based or replay-based takeover of an execution that may still be live.
func (r *SourceStreamRepo) ResolveProgramV2PreparationFailure(ctx context.Context, run StreamRun) (StreamTask, bool, error) {
	var task StreamTask
	recovered := false
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		var admitted StreamRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", run.ID).Take(&admitted).Error; err != nil {
			return err
		}
		if run.Model != "program" || !sameProgramRunIdentity(run, admitted) {
			return ErrConflict
		}
		expected, err := programRunRequestHash(run.Summary)
		if err != nil {
			return err
		}
		actual, err := programRunRequestHash(admitted.Summary)
		if err != nil {
			return err
		}
		if expected != actual {
			return ErrConflict
		}
		err = tx.Where("id=?", run.ID).Take(&task).Error
		if err == nil {
			if task.Model != "program" || task.StreamID != run.StreamID || task.VersionID != run.VersionID || task.EntryID != run.EntryID || task.SourceID != run.SourceID {
				return ErrConflict
			}
			recovered = true
			return nil
		}
		if err != gorm.ErrRecordNotFound {
			return err
		}
		return interruptUnpreparedRuns(tx, "output-not-persisted", "id=?", run.ID)
	})
	return task, recovered, normalizeDBError(err)
}

// Per-target receipts may be retried independently. Never allow a late partial
// failure to roll back a completed task, or lose the original request hash.
func (r *SourceStreamRepo) FinishProgramV2Task(ctx context.Context, id, status, summary string, durationMs int64) error {
	if id == "" || len(id) > 64 || (status != "succeeded" && status != "partial-failure") || durationMs < 0 || len(summary) > 1024*1024 {
		return ErrInvalidArgument
	}
	var incoming map[string]json.RawMessage
	if json.Unmarshal([]byte(summary), &incoming) != nil || incoming == nil {
		return ErrInvalidArgument
	}
	return streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		var run StreamRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&run).Error; err != nil {
			return err
		}
		var task StreamTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", id).Take(&task).Error; err != nil {
			return err
		}
		if run.Model != "program" || task.Model != "program" || task.EntryID != run.EntryID || task.StreamID != run.StreamID || task.VersionID != run.VersionID {
			return ErrConflict
		}
		if task.Status == "succeeded" || run.Status == "succeeded" {
			return completeProgramOutputRecovery(tx, id)
		}
		if (task.Status != "pending" && task.Status != "partial-failure") || (run.Status != "processing" && run.Status != "partial-failure") {
			return ErrConflict
		}
		var original map[string]json.RawMessage
		if json.Unmarshal([]byte(run.Summary), &original) != nil || original == nil {
			return ErrInvalidArgument
		}
		if hash, ok := original["requestHash"]; ok {
			if replacement, present := incoming["requestHash"]; present && !sameProgramDocument(string(hash), string(replacement)) {
				return ErrConflict
			}
			incoming["requestHash"] = hash
		}
		encoded, err := json.Marshal(incoming)
		if err != nil || len(encoded) > 1024*1024 {
			return ErrInvalidArgument
		}
		q := tx.Model(&StreamTask{}).Where("id=? AND model='program' AND status IN ('pending','partial-failure')", id).Update("status", status)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return ErrConflict
		}
		q = tx.Model(&StreamRun{}).Where("id=? AND model='program' AND status IN ('processing','partial-failure')", id).
			Updates(map[string]any{"status": status, "summary": string(encoded), "duration_ms": durationMs})
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return ErrConflict
		}
		if status == "succeeded" {
			return completeProgramOutputRecovery(tx, id)
		}
		return nil
	})
}

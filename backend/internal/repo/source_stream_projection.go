package repo

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Relation indexes are derived guards, never a second configuration store.
func replaceStreamTopics(tx *gorm.DB, streamID int64, topicIDs []int64) error {
	stream, err := NewSourceStreamRepo(tx).Stream(context.Background(), streamID)
	if err != nil {
		return err
	}
	seen := map[int64]bool{}
	for _, id := range topicIDs {
		if id <= 0 {
			return ErrInvalidArgument
		}
		seen[id] = true
	}
	documents := []string{stream.Document}
	if stream.ActiveVersion > 0 {
		version, err := NewSourceStreamRepo(tx).Version(context.Background(), streamID, stream.ActiveVersion)
		if err != nil {
			return err
		}
		documents = append(documents, version.Document)
	}
	for _, doc := range documents {
		var refs struct {
			TopicIDs []int64 `json:"topicIds"`
			Program  struct {
				Trigger struct {
					TopicIDs []int64 `json:"topicIds"`
				} `json:"trigger"`
			} `json:"program"`
		}
		if err := json.Unmarshal([]byte(doc), &refs); err != nil {
			return err
		}
		for _, id := range append(refs.TopicIDs, refs.Program.Trigger.TopicIDs...) {
			if id <= 0 {
				return ErrInvalidArgument
			}
			seen[id] = true
		}
	}
	if err := tx.Where("stream_id=?", streamID).Delete(&StreamTopic{}).Error; err != nil {
		return err
	}
	for id := range seen {
		if err := tx.Create(&StreamTopic{StreamID: streamID, TopicID: id}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *SourceStreamRepo) disableProgramV2(ctx context.Context, id, revision int64, remove bool) (Stream, error) {
	var item Stream
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", int64(846031001)).Error; err != nil {
			return err
		}
		current, err := NewSourceStreamRepo(tx).Stream(ctx, id)
		if err != nil {
			return err
		}
		// 停用和删除只撤销 Stream 准入，不要求失效依赖恢复可执行状态。
		if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND deleted_at=0", id).Take(&item).Error; err != nil {
			return err
		}
		if item.Model != current.Model || item.Revision != revision {
			return ErrConflict
		}
		item.Status = "disabled"
		item.PendingVersion = 0
		item.Revision++
		item.UpdatedAt = time.Now().UnixMilli()
		if remove {
			item.DeletedAt = item.UpdatedAt
		}
		if err = tx.Save(&item).Error; err != nil {
			return err
		}
		if item.Model == "program" {
			if err = interruptUnpreparedProgramRuns(tx, item.ID); err != nil {
				return err
			}
		}
		if err = disableProgramOperations(tx, item.ID); err != nil {
			return err
		}
		if remove {
			if err = tx.Where("stream_id=?", id).Delete(&StreamSource{}).Error; err != nil {
				return err
			}
			return tx.Where("stream_id=?", id).Delete(&StreamTopic{}).Error
		}
		if item.Model == "program" {
			manifest, err := item.ProgramV2DraftManifest()
			if err != nil {
				return err
			}
			return replaceProgramV2References(tx, item, manifest)
		}
		return replaceProgramSources(tx, item)
	})
	return item, normalizeDBError(err)
}

// The caller holds the disabled Stream row, fencing PutProgramV2Task. Do not
// infer failure from age alone or discard frozen output that can be recovered.
func interruptUnpreparedProgramRuns(tx *gorm.DB, streamID int64) error {
	return interruptUnpreparedRuns(tx, "stream-disabled-before-output", "stream_id=?", streamID)
}

// The caller holds the Source row, fencing result preparation for every
// activated version that references it, including historical dependencies.
func interruptUnpreparedSourceRuns(tx *gorm.DB, sourceID int64, reason string) error {
	manifest, err := json.Marshal([]map[string]int64{{"sourceId": sourceID}})
	if err != nil {
		return err
	}
	return interruptUnpreparedRuns(tx, reason, "version_id IN (SELECT id FROM public.program_version_projection WHERE source_manifest @> ?::jsonb)", string(manifest))
}

func interruptUnpreparedRuns(tx *gorm.DB, reason, predicate string, args ...any) error {
	var runs []StreamRun
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where(predicate, args...).
		Where("model='program' AND status='processing' AND NOT EXISTS (SELECT 1 FROM public.uns_stream_task task WHERE task.id=public.uns_stream_run.id)").
		Find(&runs).Error; err != nil {
		return err
	}
	for _, run := range runs {
		var summary map[string]any
		if err := json.Unmarshal([]byte(run.Summary), &summary); err != nil {
			return ErrInvalidArgument
		}
		if summary == nil {
			summary = map[string]any{}
		}
		summary["phase"] = "interrupted"
		summary["error"] = "SourceStream.executionFailed"
		summary["reason"] = reason
		summary["interruptedAt"] = time.Now().UnixMilli()
		encoded, err := json.Marshal(summary)
		if err != nil {
			return err
		}
		q := tx.Model(&StreamRun{}).Where("id=? AND status='processing'", run.ID).
			Updates(map[string]any{"status": "unknown", "summary": string(encoded)})
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return ErrConflict
		}
	}
	return nil
}

package repo

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Payloads are private execution state, never returned by the run-history API.
// Persisted transform results ensure a retry cannot choose different output.
type StreamTask struct {
	ID             string `gorm:"column:id;primaryKey"`
	SourceID       int64  `gorm:"column:source_id;serializer:optional_source"`
	StreamID       int64  `gorm:"column:stream_id"`
	VersionID      int64  `gorm:"column:version_id"`
	EntryID        string `gorm:"column:entry_id;default:''" json:"entryId"`
	SourceRevision int64  `gorm:"column:source_revision"`
	Model          string `gorm:"column:model"`
	Output         string `gorm:"column:output"`
	ReceivedAt     int64  `gorm:"column:received_at"`
	Status         string `gorm:"column:status"`
	SentAt         int64  `gorm:"column:sent_at"`
	CreatedAt      int64  `gorm:"column:created_at"`
}

func (StreamTask) TableName() string { return "public.uns_stream_task" }

type StreamEventMatch struct {
	ID         string `gorm:"column:id;primaryKey"`
	References string `gorm:"column:references"`
	CreatedAt  int64  `gorm:"column:created_at"`
}

func (StreamEventMatch) TableName() string { return "public.uns_stream_event_match" }

// Freeze the subscription set before processing any of its paths. Redelivery
// must not pick up a newly activated version or a newly created subscription.
func (r *SourceStreamRepo) BindEventMatch(ctx context.Context, id string, refs string) (string, error) {
	if id == "" || len(id) > 200 || !json.Valid([]byte(refs)) || len(refs) > 1024*1024 {
		return "", ErrInvalidArgument
	}
	item := StreamEventMatch{ID: id, References: refs, CreatedAt: time.Now().UnixMilli()}
	if err := insertRuntimeIgnore(r.db.WithContext(ctx), &item).Error; err != nil {
		return "", err
	}
	if err := r.db.WithContext(ctx).Where("id=?", id).Take(&item).Error; err != nil {
		return "", err
	}
	return item.References, nil
}

type StreamExecution struct {
	Stream  Stream
	Source  Source
	Version StreamVersion
}

func (r *SourceStreamRepo) ActiveExecutions(ctx context.Context) ([]StreamExecution, error) {
	streams, err := readStreamEntries(streamQuery(r.db.WithContext(ctx)).Where("stream.value->>'status'='enabled' AND (stream.value->>'activeVersion')::bigint>0"))
	if err != nil {
		return nil, err
	}
	items := []StreamExecution{}
	for _, stream := range streams {
		if stream.SourceID == 0 {
			version, err := r.Version(ctx, stream.ID, stream.ActiveVersion)
			if err != nil {
				return nil, err
			}
			if stream.Model == "program" {
				if _, err := r.ProgramVersionSources(ctx, version); err != nil {
					if err == ErrConflict || IsSourceStreamMissing(err) {
						continue
					}
					return nil, err
				}
			}
			items = append(items, StreamExecution{Stream: stream, Version: version})
			continue
		}
		source, err := r.Source(ctx, stream.SourceID)
		if err != nil {
			return nil, err
		}
		version, err := r.Version(ctx, stream.ID, stream.ActiveVersion)
		if err != nil {
			return nil, err
		}
		if source.ConfigRevision == version.SourceRevision {
			items = append(items, StreamExecution{stream, source, version})
		}
	}
	return items, nil
}
func (r *SourceStreamRepo) Task(ctx context.Context, id string) (StreamTask, error) {
	var item StreamTask
	err := r.db.WithContext(ctx).Where("id=?", id).Take(&item).Error
	return item, err
}

// Admission and its audit record share the metadata sink transaction. Business
// output is separately protected by the per-target processing receipt.
func (r *SourceStreamRepo) PutTask(ctx context.Context, item StreamTask, run StreamRun) (StreamTask, error) {
	if item.ID == "" || !json.Valid([]byte(item.Output)) || len(item.Output) > 1024*1024 {
		return item, ErrInvalidArgument
	}
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		// RAW fixes the version at ingestion. A later deployment must not cause
		// already accepted messages to execute the new script.
		if err := admitTask(tx, item); err != nil {
			return err
		}
		item.Status = "pending"
		item.CreatedAt = time.Now().UnixMilli()
		result := insertRuntimeIgnore(tx, &item)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return tx.Where("id=?", item.ID).Take(&item).Error
		}
		return insertRuntimeIgnore(tx, &run).Error
	})
	return item, err
}

func (r *SourceStreamRepo) ClaimDelivery(ctx context.Context, item StreamTask) (bool, error) {
	var claimed bool
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if err := admitTask(tx, item); err != nil {
			return err
		}
		result := tx.Model(&StreamTask{}).Where("id=? AND status='pending' AND model='subscribe'", item.ID).Update("status", "dispatching")
		claimed = result.RowsAffected == 1
		return result.Error
	})
	return claimed, err
}

func (r *SourceStreamRepo) FinishTask(ctx context.Context, id, status, summary string, durationMs int64) error {
	if !json.Valid([]byte(summary)) {
		return ErrInvalidArgument
	}
	return streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if err := tx.Model(&StreamTask{}).Where("id=?", id).Update("status", status).Error; err != nil {
			return err
		}
		return tx.Model(&StreamRun{}).Where("id=?", id).Updates(map[string]any{"status": status, "summary": summary, "duration_ms": durationMs}).Error
	})
}

// Retire an invalidated queued reference without overwriting completed work or
// an already-dispatched operation. Preserve target receipts in the run summary.
func (r *SourceStreamRepo) DiscardQueuedExecution(ctx context.Context, run StreamRun) error {
	if run.ID == "" || run.Status != "dropped" || !json.Valid([]byte(run.Summary)) {
		return ErrInvalidArgument
	}
	return streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		if err := insertRuntimeIgnore(tx, &run).Error; err != nil {
			return err
		}
		result := tx.Model(&StreamTask{}).Where("id=? AND status IN ?", run.ID, []string{"pending", "partial-failure"}).Update("status", "dropped")
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		return tx.Model(&StreamRun{}).Where("id=?", run.ID).Updates(map[string]any{
			"status": "dropped", "summary": gorm.Expr("summary || ?::jsonb", run.Summary),
		}).Error
	})
}

func (r *SourceStreamRepo) PendingDelivery(ctx context.Context) ([]StreamTask, error) {
	items := []StreamTask{}
	err := r.db.WithContext(ctx).Where("model='subscribe' AND status='pending' AND sent_at=0").Order("created_at,id").Limit(50).Find(&items).Error
	return items, err
}

// JetStream expiry/eviction cannot update the durable task table. Retire old
// undispatched tasks separately, including tasks whose queue message was sent.
// Never classify an already-dispatched device operation as safely dropped.
func (r *SourceStreamRepo) ExpireStreamTasks(ctx context.Context, cutoff int64) (int64, error) {
	if cutoff <= 0 {
		return 0, ErrInvalidArgument
	}
	var count int64
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		var items []StreamTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
			Where("status IN ? AND (received_at<=? OR created_at<=?)", []string{"pending", "partial-failure"}, cutoff, cutoff).
			Order("created_at,id").Limit(100).Find(&items).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		ids := make([]string, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.ID)
		}
		result := tx.Model(&StreamTask{}).Where("id IN ?", ids).Update("status", "dropped")
		if result.Error != nil {
			return result.Error
		}
		count = result.RowsAffected
		return tx.Model(&StreamRun{}).Where("id IN ?", ids).Updates(map[string]any{
			"status": "dropped", "summary": `{"phase":"message-expiry","reason":"messageExpired","usesNATS":true,"operationRepeated":false}`,
		}).Error
	})
	return count, err
}

func (r *SourceStreamRepo) MarkDeliverySent(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&StreamTask{}).Where("id=?", id).Update("sent_at", time.Now().UnixMilli()).Error
}

func (r *SourceStreamRepo) ObserveSourceData(ctx context.Context, id, revision, at int64) error {
	return streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		_, data, err := sourceRow(tx, id, "SHARE")
		if err != nil {
			return err
		}
		if data.ConfigRevision != revision {
			return ErrConflict
		}
		row, state, err := stateRow(tx, id, true)
		if err != nil {
			return err
		}
		if at > state.LastDataAt {
			state.LastDataAt = at
		}
		state.Health = "healthy"
		state.HealthMessage = ""
		state.CheckedAt = time.Now().UnixMilli()
		return saveMetaData(tx, &row, state)
	})
}

// A dedicated PostgreSQL session owns the single active async worker. Pull
// consumers alone do not provide application ownership or source ordering.
func (r *SourceStreamRepo) ObserveSourceWatch(ctx context.Context, id, configRevision int64) error {
	result := r.db.WithContext(ctx).Model(&Source{}).
		Where("id=? AND config_revision=? AND deleted_at=0", id, configRevision).
		Updates(map[string]any{"health": "healthy", "health_message": "", "checked_at": time.Now().UnixMilli()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrConflict
	}
	return nil
}

// A dedicated PostgreSQL session owns the single active async worker. Pull
// consumers alone do not provide application ownership or source ordering.
func (r *SourceStreamRepo) OwnWorker(ctx context.Context, key string) (*sql.Conn, error) {
	db, err := r.db.DB()
	if err != nil {
		return nil, err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	var owned bool
	if err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, key).Scan(&owned); err != nil || !owned {
		conn.Close()
		if err == nil {
			err = ErrConflict
		}
		return nil, err
	}
	return conn, nil
}

func ReleaseStreamWorker(conn *sql.Conn, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var released bool
	if conn == nil {
		return
	}
	if err := conn.QueryRowContext(ctx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, key).Scan(&released); err != nil || !released {
		// Do not return a potentially still-locked session to the pool.
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	_ = conn.Close()
}

func admitTask(tx *gorm.DB, item StreamTask) error {
	_, data, err := sourceRow(tx, item.SourceID, "SHARE")
	if err != nil {
		return err
	}
	c, ok := data.Config.Streams[sourceKey(item.StreamID)]
	if !ok || c.DeletedAt != 0 || c.Status != "enabled" || data.ConfigRevision != item.SourceRevision {
		return ErrConflict
	}
	v, ok := c.Versions[sourceKey(item.VersionID)]
	if !ok || v.SourceRevision != item.SourceRevision {
		return ErrConflict
	}
	return nil
}

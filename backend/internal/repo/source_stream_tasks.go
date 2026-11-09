package repo

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type integrationTaskData struct {
	Type     string          `json:"type"`
	Identity string          `json:"identity"`
	Payload  json.RawMessage `json:"payload"`
}

// integrationTaskRecord is a private storage envelope, never an UNS Record.
type integrationTaskRecord struct {
	ID        string          `gorm:"column:id;primaryKey"`
	Data      json.RawMessage `gorm:"column:data;type:jsonb"`
	Revision  int64           `gorm:"column:revision"`
	CreatedAt int64           `gorm:"column:created_at"`
	UpdatedAt int64           `gorm:"column:updated_at"`
}

func runtimeTaskID(typ, id string) string { return typ + ":" + id }

func runtimeTaskTable(typ string) string {
	switch typ {
	case "programCheckpoint":
		return "public.uns_stream_checkpoint"
	case "ingestReceipt", "programMetadataCommit", "programCheckpointCommit":
		return "public.uns_stream_commit_receipt"
	default:
		return ""
	}
}
func taskQuery(db *gorm.DB, typ string) *gorm.DB {
	table := runtimeTaskTable(typ)
	q := db.Model(&integrationTaskRecord{}).Table(table)
	if table == "" {
		q.AddError(ErrInvalidArgument)
		return q
	}
	return q.Where("data->>'type'=?", typ)
}
func putIntegrationTask(db *gorm.DB, typ, id string, payload any) (bool, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return false, err
	}
	data, err := json.Marshal(integrationTaskData{Type: typ, Identity: typ + ":" + id, Payload: raw})
	if err != nil {
		return false, err
	}
	now := time.Now().UnixMilli()
	row := integrationTaskRecord{ID: runtimeTaskID(typ, id), Data: data, Revision: 1, CreatedAt: now, UpdatedAt: now}
	q := taskQuery(db, typ).Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
	return q.RowsAffected == 1, q.Error
}
func getIntegrationTask(db *gorm.DB, typ, id string, out any) error {
	var row integrationTaskRecord
	if err := taskQuery(db, typ).Where("id=?", runtimeTaskID(typ, id)).Take(&row).Error; err != nil {
		return err
	}
	var d integrationTaskData
	if err := json.Unmarshal(row.Data, &d); err != nil {
		return err
	}
	if d.Type != typ || (d.Identity != "" && d.Identity != typ+":"+id) {
		return ErrConflict
	}
	return json.Unmarshal(d.Payload, out)
}
func patchIntegrationTask(q *gorm.DB, changes any) *gorm.DB {
	raw, err := json.Marshal(changes)
	if err != nil {
		q.AddError(err)
		return q
	}
	return q.Updates(map[string]any{"data": gorm.Expr("jsonb_set(data,'{payload}',(data->'payload') || ?::jsonb)", string(raw)), "revision": gorm.Expr("revision+1"), "updated_at": time.Now().UnixMilli()})
}
func (r *SourceStreamRepo) CreateRun(ctx context.Context, item StreamRun) error {
	if !json.Valid([]byte(item.Summary)) {
		return ErrInvalidArgument
	}
	return streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		return tx.Create(&item).Error
	})
}

// ReserveRun commits the intent before any possibly side-effecting operation.
// A duplicate never re-invokes the device, including after a process crash.
func (r *SourceStreamRepo) ReserveRun(ctx context.Context, item StreamRun) (bool, error) {
	if !json.Valid([]byte(item.Summary)) {
		return false, ErrInvalidArgument
	}
	created := false
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		q := insertRuntimeIgnore(tx, &item)
		created = q.RowsAffected == 1
		return q.Error
	})
	return created, err
}

func (r *SourceStreamRepo) AdmitRun(ctx context.Context, item StreamRun, sourceRevision int64) (bool, error) {
	created := false
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		_, data, err := sourceRow(tx, item.SourceID, "SHARE")
		if err != nil {
			return err
		}
		c, ok := data.Config.Streams[sourceKey(item.StreamID)]
		if data.ConfigRevision != sourceRevision || !ok || c.DeletedAt != 0 || c.Status != "enabled" || c.ActiveVersion != item.VersionID {
			return ErrConflict
		}
		created, err = NewSourceStreamRepo(tx).ReserveRun(ctx, item)
		return err
	})
	return created, normalizeDBError(err)
}
func (r *SourceStreamRepo) FinishRun(ctx context.Context, id, status, summary string, durationMs int64) error {
	if !json.Valid([]byte(summary)) {
		return ErrInvalidArgument
	}
	return streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		q := tx.Model(&StreamRun{}).Where("id = ? AND status = 'processing'", id).Updates(map[string]any{"status": status, "summary": summary, "duration_ms": durationMs})
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected != 1 {
			return ErrConflict
		}
		return nil
	})
}
func (r *SourceStreamRepo) Runs(ctx context.Context, f StreamRunFilter) ([]StreamRun, int64, error) {
	q := r.db.WithContext(ctx).Model(&StreamRun{})
	if f.SourceID > 0 {
		manifest, _ := json.Marshal([]map[string]int64{{"sourceId": f.SourceID}})
		q = q.Where("(source_id = ? OR version_id IN (SELECT id FROM public.program_version_projection WHERE source_manifest @> ?::jsonb))", f.SourceID, string(manifest))
	}
	if f.StreamID > 0 {
		q = q.Where("stream_id = ?", f.StreamID)
	}
	if f.TopicID > 0 {
		ref, _ := json.Marshal([]int64{f.TopicID})
		legacy := streamQuery(r.db.WithContext(ctx)).Where(streamTopicPredicate, string(ref), string(ref)).Select("stream.key")
		q = q.Where("(stream_id IN (SELECT stream_id FROM public.uns_stream_topic WHERE topic_id=?) OR stream_id::text IN (?))", f.TopicID, legacy)
	}
	if f.Model != "" {
		q = q.Where("model = ?", f.Model)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset, size := sourceStreamPage(f.Page, f.Size)
	items := []StreamRun{}
	err := q.Order("created_at DESC, id DESC").Offset(offset).Limit(size).Find(&items).Error
	return items, total, err
}
func (r *SourceStreamRepo) Run(ctx context.Context, id string) (StreamRun, error) {
	var item StreamRun
	err := r.db.WithContext(ctx).Where("id = ?", id).Take(&item).Error
	return item, err
}

package unsmeta

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"backend/internal/repo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *Service) Records(ctx context.Context, id, search string, page, size int) (map[string]any, error) {
	if !ValidID(id) || len(search) > 128 {
		return nil, ErrInvalid
	}
	if out, routed, err := remoteStorageMap(ctx, id, "records", map[string]any{"search": search, "page": page, "size": size}); routed || err != nil {
		return out, err
	}
	if _, _, err := readCollection(ctx, ctx, id); err != nil {
		return nil, err
	}
	page, size = pageBounds(page, size)
	rows, total, err := repo.NewUnsMetaRepo(ctx).Records(ctx, id, search, page, size)
	return map[string]any{"list": rows, "total": total, "page": page, "size": size}, err
}
func (s *Service) SaveRecord(ctx context.Context, collectionID, id string, revision int64, input any) (repo.UnsRecord, error) {
	var saved repo.UnsRecord
	if !ValidID(collectionID) || !ValidID(id) || revision < 0 {
		return saved, ErrInvalid
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 256*1024 {
		return saved, ErrInvalid
	}
	var value any
	if decodeNumbers(raw, &value) != nil {
		return saved, ErrInvalid
	}
	err = repo.NewUnsMetaRepo(ctx).Transaction(ctx, func(tx *gorm.DB) error {
		scope := scopedRecordWriter(ctx, tx)
		if scope == nil {
			if err := lockMetadata(tx, true); err != nil {
				return err
			}
		}
		_, doc, err := writableCollection(ctx, tx, collectionID)
		if err != nil {
			return err
		}
		if object, ok := value.(map[string]any); ok {
			if _, reserved := object["_id"]; reserved {
				return ErrData
			}
			if _, reserved := object["_meta"]; reserved {
				return ErrData
			}
		}
		if doc.RecordSchema == nil || !doc.RecordSchema.accepts(value) {
			return ErrData
		}
		deliveryState := ""
		if object, ok := value.(map[string]any); ok {
			remote, only, e := storageRecord(ctx, tx, collectionID, id, revision, object)
			if e != nil {
				return e
			}
			if only {
				saved = remote
				return nil
			}
			deliveryState = remote.DeliveryState
		}
		identity := repo.UnsRecordLock{CollectionID: collectionID, RecordID: id}
		if scope == nil || !scope.locked[identity] {
			if err := repo.LockUnsRecords(tx, identity); err != nil {
				return err
			}
		}
		old, err := recordForWrite(tx, collectionID, id)
		if err != nil && !errors.Is(err, repo.ErrNotFound) {
			return err
		}
		if old.DeletedAt.Valid {
			return repo.ErrConflict
		}
		if err := CheckRecordLease(ctx, old); err != nil {
			return err
		}
		if (err == nil && ((!old.DeletedAt.Valid && old.Revision != revision) || (old.DeletedAt.Valid && revision != 0))) || (err != nil && revision != 0) {
			return repo.ErrConflict
		}
		if old.SourceID != nil {
			return ErrReferenced
		}
		now := time.Now().UnixMilli()
		saved = repo.UnsRecord{CollectionID: collectionID, ID: id, Data: raw, Revision: old.Revision + 1, CreatedAt: old.CreatedAt, UpdatedAt: now}
		saved.DeliveryState = deliveryState
		saved.SourceTime = old.SourceTime
		saved.LockTokenHash = old.LockTokenHash
		saved.LockPrincipal = old.LockPrincipal
		saved.LockLabel = old.LockLabel
		saved.LockExpiresAt = old.LockExpiresAt
		saved.LockFence = old.LockFence
		saved.LockSince = old.LockSince
		if old.ID == "" {
			saved.CreatedAt = now
			if err := tx.Create(&saved).Error; err != nil {
				return err
			}
			return enqueueRecordEvent(ctx, tx, saved)
		}
		var oldValue any
		if err := decodeNumbers(old.Data, &oldValue); err != nil {
			return err
		}
		if !old.DeletedAt.Valid && equalJSONValues(oldValue, value) {
			saved = old
			if c, _ := ctx.Value(recordLeaseKey{}).(recordLeaseCredential); c.Release {
				saved.LockExpiresAt = nil
				saved.LockSince = nil
				saved.LockTokenHash = nil
				saved.LockLabel = ""
				saved.LockPrincipal = ""
			}
			return releaseRecordLease(ctx, tx, old)
		}
		updates := recordWriteColumns(old, raw, now)
		if err := tx.Unscoped().Model(&old).Updates(updates).Error; err != nil {
			return err
		}
		if err := releaseRecordLease(ctx, tx, old); err != nil {
			return err
		}
		if c, _ := ctx.Value(recordLeaseKey{}).(recordLeaseCredential); c.Release {
			saved.LockExpiresAt = nil
			saved.LockSince = nil
			saved.LockTokenHash = nil
			saved.LockLabel = ""
			saved.LockPrincipal = ""
		}
		return enqueueRecordEvent(ctx, tx, saved)
	})
	return saved, err
}
func (s *Service) DeleteRecord(ctx context.Context, collectionID, id string, revision int64) error {
	if err := repo.RequireLocalCollection(ctx, collectionID); err != nil {
		return err
	}
	if !ValidID(collectionID) || !ValidID(id) || revision <= 0 {
		return ErrInvalid
	}
	return repo.NewUnsMetaRepo(ctx).Transaction(ctx, func(tx *gorm.DB) error {
		if err := lockMetadata(tx, true); err != nil {
			return err
		}
		if err := repo.LockUnsRecords(tx, repo.UnsRecordLock{CollectionID: collectionID, RecordID: id}); err != nil {
			return err
		}
		if _, _, err := writableCollection(ctx, tx, collectionID); err != nil {
			return err
		}
		row, err := recordForUpdate(tx, collectionID, id)
		if err != nil {
			return err
		}
		if row.Revision != revision {
			return repo.ErrConflict
		}
		if err := CheckRecordLease(ctx, row); err != nil {
			return err
		}
		if row.SourceID != nil {
			return ErrReferenced
		}
		var count int64
		if err := tx.Model(&repo.UnsMeta{}).Where("kind = 'topic' AND deleted_at = 0 AND data->'fields' @> ?::jsonb", string(mustJSON([]map[string]string{{"collectionId": collectionID, "recordId": id}}))).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrReferenced
		}
		now := time.Now().UTC()
		revision := row.Revision + 1
		if err := tx.Model(&row).Updates(map[string]any{"deleted_at": now, "revision": revision, "updated_at": now.UnixMilli(), "lock_token_hash": nil, "lock_principal": nil, "lock_label": nil, "lock_expires_at": nil, "lock_since": nil}).Error; err != nil {
			return err
		}
		row.Revision = revision
		row.UpdatedAt = now.UnixMilli()
		return repo.EnqueueMetadataResourceEvents(ctx, tx, repo.MetadataResourceEvent{MessageID: programOutputID("delete", int(row.Revision), collectionID, id), CollectionID: collectionID, RecordID: id, ReceivedAt: row.UpdatedAt, Change: "delete"})
	})
}

type SampleInput struct {
	ID       string         `json:"id"`
	RecordID string         `json:"recordId"`
	Time     int64          `json:"time"`
	Quality  string         `json:"quality"`
	Data     map[string]any `json:"data"`
}

func sampleView(row repo.UnsSample) map[string]any {
	return map[string]any{"id": row.ID, "collectionId": row.CollectionID, "recordId": row.RecordID, "time": row.Time.UnixMilli(), "receivedAt": row.ReceivedAt.UnixMilli(), "quality": row.Quality, "data": row.Data}
}
func (s *Service) Samples(ctx context.Context, collectionID, recordID string, start, end int64, page, size int) (map[string]any, error) {
	if !ValidID(collectionID) || !ValidID(recordID) || start < 0 || end < 0 || (end > 0 && start > end) {
		return nil, ErrInvalid
	}
	if out, routed, err := remoteStorageMap(ctx, collectionID, "samples", map[string]any{"recordId": recordID, "start": start, "end": end, "page": page, "size": size}); routed || err != nil {
		return out, err
	}
	if _, doc, err := readCollection(ctx, ctx, collectionID); err != nil {
		return nil, err
	} else if doc.SampleSchema == nil {
		return nil, ErrInvalid
	}
	page, size = pageBounds(page, size)
	rows, total, err := repo.NewUnsMetaRepo(ctx).Samples(ctx, collectionID, recordID, start, end, page, size)
	if err != nil {
		return nil, err
	}
	list := []map[string]any{}
	for _, row := range rows {
		list = append(list, sampleView(row))
	}
	return map[string]any{"list": list, "total": total, "page": page, "size": size}, nil
}

// AppendSamples is the metadata/data boundary for acquisition owners. It does
// not configure, start, or impersonate a Source/Stream protocol worker.
func (s *Service) AppendSamples(ctx context.Context, collectionID string, input any) (map[string]any, error) {
	var samples []SampleInput
	if !ValidID(collectionID) || Parse(input, &samples) != nil || len(samples) == 0 || len(samples) > 500 {
		return nil, ErrInvalid
	}
	inserted := 0
	err := repo.NewUnsMetaRepo(ctx).Transaction(ctx, func(tx *gorm.DB) error {
		if err := lockMetadata(tx, true); err != nil {
			return err
		}
		_, doc, err := writableCollection(ctx, tx, collectionID)
		if err != nil {
			return err
		}
		if doc.SampleSchema == nil {
			return ErrSchema
		}
		times := make([]time.Time, 0, len(samples))
		for _, sample := range samples {
			if sample.Time <= 0 || sample.Time > 253402300799999 {
				return ErrData
			}
			times = append(times, time.UnixMilli(sample.Time))
		}
		if err := repo.PrepareUnsSampleWrites(tx, times...); err != nil {
			return err
		}
		locks := make([]repo.UnsRecordLock, 0, len(samples))
		for _, sample := range samples {
			locks = append(locks, repo.UnsRecordLock{CollectionID: collectionID, RecordID: sample.RecordID})
		}
		if err := repo.LockUnsRecords(tx, locks...); err != nil {
			return err
		}
		now := time.Now().UTC()
		for _, sample := range samples {
			if !ValidID(sample.ID) || !ValidID(sample.RecordID) || sample.Time <= 0 || sample.Time > 253402300799999 || len(sample.Quality) > 64 || !doc.SampleSchema.accepts(sample.Data) {
				return ErrData
			}
			only, err := RouteCollectionWrite(ctx, tx, collectionID, "sample/"+sample.RecordID+"/"+sample.ID, "sample", IngestValue{RecordID: sample.RecordID, Sample: sample.Data, Time: sample.Time, Quality: sample.Quality})
			if err != nil {
				return err
			}
			if only {
				continue
			}
			if _, err := repo.NewUnsMetaRepo(tx).Record(ctx, collectionID, sample.RecordID); err != nil {
				return err
			}
			row := repo.UnsSample{CollectionID: collectionID, RecordID: sample.RecordID, ID: sample.ID, Time: time.UnixMilli(sample.Time).UTC(), ReceivedAt: now, Quality: sample.Quality, Data: mustJSON(sample.Data)}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 1 {
				if err := repo.EnqueueMetadataResourceEvents(ctx, tx, repo.MetadataResourceEvent{
					MessageID: programOutputID("sample:"+sample.ID, 0, collectionID, sample.RecordID), CollectionID: collectionID, RecordID: sample.RecordID,
					ReceivedAt: now.UnixMilli(), Data: IngestValue{RecordID: sample.RecordID, Sample: sample.Data, Time: sample.Time, Quality: sample.Quality}, Change: "sample",
				}); err != nil {
					return err
				}
				inserted++
				continue
			}
			var existing repo.UnsSample
			if err := tx.Where("collection_id = ? AND record_id = ? AND id = ?", collectionID, sample.RecordID, sample.ID).Take(&existing).Error; err != nil {
				return err
			}
			var data any
			if err := decodeNumbers(existing.Data, &data); err != nil {
				return err
			}
			if existing.Time.UnixMilli() != sample.Time || existing.Quality != sample.Quality || !equalJSONValues(data, sample.Data) {
				return repo.ErrConflict
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	policy, err := repo.CollectionStorage(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	if policy.Mode != "local" {
		return map[string]any{"inserted": inserted, "accepted": len(samples), "deliveryState": "pendingUpload", "centerCommitted": 0}, nil
	}
	return map[string]any{"inserted": inserted, "replayed": len(samples) - inserted}, nil
}

func enqueueRecordEvent(ctx context.Context, tx *gorm.DB, row repo.UnsRecord) error {
	var value map[string]any
	if err := decodeNumbers(row.Data, &value); err != nil {
		return err
	}
	return repo.EnqueueMetadataResourceEvents(ctx, tx, repo.MetadataResourceEvent{
		MessageID: programOutputID("record", int(row.Revision), row.CollectionID, row.ID), CollectionID: row.CollectionID, RecordID: row.ID,
		ReceivedAt: row.UpdatedAt, Data: IngestValue{RecordID: row.ID, Record: value, Time: row.UpdatedAt}, Change: "record",
	})
}

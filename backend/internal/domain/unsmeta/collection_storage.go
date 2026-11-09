package unsmeta

import (
	"backend/internal/repo"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"time"
)

// 校验完成后入队，与调用方的业务写入/检查点使用同一事务。
func RouteCollectionWrite(ctx context.Context, tx *gorm.DB, id, key, kind string, value IngestValue) (bool, error) {
	p, err := repo.CollectionStorage(tx, id)
	if err != nil || p.Mode == "local" {
		return false, err
	}
	if p.TargetCollectionID == "" || p.CenterID == "" {
		return false, repo.ErrStorageDefinitionPending
	}
	eventID := uuid.NewSHA1(uuid.NameSpaceOID, []byte("collection-storage/"+id+"/"+key)).String()
	data, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	body, err := json.Marshal(repo.CollectionStorageDelivery{EventID: eventID, Policy: p, Kind: kind, Value: data})
	if err != nil {
		return false, err
	}
	// 同一写入重试不能改变载荷或原目的地。
	store := repo.NewStorageOutboxRepo(tx)
	prior, err := store.GetOutboxEvent(ctx, eventID)
	if err == nil {
		var a, b any
		_ = json.Unmarshal(prior.Payload, &a)
		_ = json.Unmarshal(body, &b)
		if !equalJSONValues(a, b) {
			return false, repo.ErrConflict
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	} else {
		if err := repo.CheckCollectionStorageAdmission(tx, int64(len(body))); err != nil {
			return false, err
		}
		if err := store.EnqueueOutbox(ctx, repo.OutboxEvent{EventID: eventID, EventType: repo.CollectionStorageEvent, AggregateType: "collection", AggregateID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("storage-order/"+p.CenterID+"/"+p.TargetCollectionID)).String(), Payload: body}); err != nil {
			return false, err
		}
	}
	repo.RecordCollectionDelivery(ctx, repo.CollectionStorageDelivery{EventID: eventID, Policy: p})
	err = nil
	if p.Mode == "both" {
		err = tx.Exec("SELECT set_config('public.collection_storage_write',?,true)", id).Error
	}
	return p.Mode == "center", err
}
func storageRecord(ctx context.Context, tx *gorm.DB, id, rid string, revision int64, value map[string]any) (repo.UnsRecord, bool, error) {
	p, err := repo.CollectionStorage(tx, id)
	if err != nil || p.Mode == "local" {
		return repo.UnsRecord{}, false, err
	}
	if revision != 0 {
		return repo.UnsRecord{}, false, repo.ErrStorageOperation
	}
	now := time.Now().UnixMilli()
	only, err := RouteCollectionWrite(ctx, tx, id, uuid.NewString(), "create", IngestValue{RecordID: rid, Record: value, Time: now})
	return repo.UnsRecord{CollectionID: id, ID: rid, Data: mustJSON(value), UpdatedAt: now, DeliveryState: "pendingUpload"}, only, err
}
func remoteStorageMap(ctx context.Context, id, op string, body any) (map[string]any, bool, error) {
	raw, routed, err := repo.ReadCollectionStorage(ctx, id, op, body)
	if err != nil || !routed {
		return nil, routed, err
	}
	var out map[string]any
	err = decodeNumbers(raw, &out)
	return out, true, err
}

func StorageSchemaCompatible(source, target *Schema) bool { return compatibleSchema(source, target) }

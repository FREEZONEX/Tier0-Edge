package repo

import (
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"time"
)

// ResumeCollectionStorageDelivery only wakes unleased deliveries for the exact
// authenticated center. Payloads, destinations and retry counts stay unchanged.
func ResumeCollectionStorageDelivery(db *gorm.DB, centerID string) error {
	if centerID == "" {
		return errors.New("collection storage center identity is required")
	}
	now := time.Now().UnixMilli()
	return db.Table("uns_collection_storage_outbox").
		Where("status='pending' AND payload->'policy'->>'centerId'=? AND next_retry_time>? AND (locked_until_time=0 OR locked_until_time<?)", centerID, now, now).
		Update("next_retry_time", now).Error
}

const CollectionStorageEvent = "collection.storage.deliver"

// CollectionStoragePolicy is the resolved policy snapshot carried by a delivery.
// It is not a separate per-Collection configuration table.
type CollectionStoragePolicy struct {
	CollectionID       string `gorm:"primaryKey" json:"collectionId"`
	Mode               string `json:"mode"`
	Revision           int64  `json:"revision"`
	CenterID           string `json:"centerId"`
	TargetCollectionID string `json:"targetCollectionId"`
	UpdatedAt          int64  `gorm:"autoUpdateTime:milli" json:"updatedAt"`
}

// GlobalStoragePolicy is authoritative on the Edge. In-flight deliveries retain
// the resolved policy and destination captured when they were accepted.
type GlobalStoragePolicy struct {
	ID        string `gorm:"primaryKey" json:"-"`
	Mode      string `json:"mode"`
	Revision  int64  `json:"revision"`
	CenterID  string `json:"centerId"`
	UpdatedAt int64  `gorm:"autoUpdateTime:milli" json:"updatedAt"`
}

func (GlobalStoragePolicy) TableName() string { return "uns_global_storage_policy" }
func GlobalStorage(in any) (GlobalStoragePolicy, error) {
	p := GlobalStoragePolicy{ID: "edge", Mode: "local"}
	err := GetUnsConn(in).Where("id=?", "edge").Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return p, err
}

type StorageCollectionMapping struct {
	CollectionID       string `gorm:"primaryKey" json:"collectionId"`
	CenterID           string `json:"centerId"`
	TargetCollectionID string `json:"targetCollectionId"`
	SourceRevision     int64  `json:"sourceRevision"`
	TargetRevision     int64  `json:"targetRevision"`
	LastSyncAt         int64  `json:"lastSyncAt"`
	LastAttemptAt      int64  `json:"-"`
	LastError          string `json:"lastError"`
}

func (StorageCollectionMapping) TableName() string { return "uns_storage_collection_mapping" }

type StorageInboundCollection struct {
	NodeID             string `gorm:"primaryKey"`
	CollectionID       string `gorm:"primaryKey"`
	TargetCollectionID string
	SourceRevision     int64
	TargetRevision     int64
	SourceHash         string
}

func (StorageInboundCollection) TableName() string { return "uns_storage_inbound_collection" }
func LocalStorageCollections(in any) *gorm.DB {
	return GetUnsConn(in).Model(&UnsMeta{}).Where("kind='collection' AND deleted_at=0 AND mount_id IS NULL AND NOT EXISTS (SELECT 1 FROM uns_storage_inbound_collection i WHERE i.target_collection_id=uns_meta.id)")
}

var ErrStorageDefinitionPending = errors.New("COLLECTION_DEFINITION_PENDING: synchronize Collection definition with center before remote writes")

func CollectionStorage(in any, id string) (CollectionStoragePolicy, error) {
	return collectionStorage(in, id, false)
}

// Trash reads keep the existing storage destination; recycling is not a switch to local history.
func collectionStorage(in any, id string, includeDeleted bool) (CollectionStoragePolicy, error) {
	p := CollectionStoragePolicy{CollectionID: id, Mode: "local"}
	g, err := GlobalStorage(in)
	if err != nil || g.Mode == "local" {
		return p, err
	}
	local := func() *gorm.DB { return LocalStorageCollections(in) }
	if includeDeleted {
		local = func() *gorm.DB {
			return GetUnsConn(in).Model(&UnsMeta{}).Where("kind='collection' AND mount_id IS NULL AND NOT EXISTS (SELECT 1 FROM uns_storage_inbound_collection i WHERE i.target_collection_id=uns_meta.id)")
		}
	}
	var owned int64
	if err := local().Where(StorageCollectionSelectedSQL).Where("id=?", id).Count(&owned).Error; err != nil {
		return p, err
	}
	if owned == 0 {
		return p, nil
	}
	p.Mode, p.Revision, p.CenterID, p.UpdatedAt = g.Mode, g.Revision, g.CenterID, g.UpdatedAt
	var m StorageCollectionMapping
	err = GetUnsConn(in).Where("collection_id=? AND center_id=?", id, g.CenterID).Take(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	var current int64
	currentQuery := local().Where("id=?", id)
	if includeDeleted {
		currentQuery = currentQuery.Where("(revision=? OR (deleted_at>0 AND revision=?)) AND COALESCE(data->>'purgeJobId','')=''", m.SourceRevision, m.SourceRevision+1)
	} else {
		currentQuery = currentQuery.Where("revision=?", m.SourceRevision)
	}
	if err := currentQuery.Count(&current).Error; err != nil {
		return p, err
	}
	if current == 0 || m.TargetCollectionID == "" {
		return p, nil
	}
	p.TargetCollectionID = m.TargetCollectionID
	return p, nil
}

var ErrStorageOperation = errors.New("COLLECTION_STORAGE_UNSUPPORTED: this operation cannot preserve remote storage semantics")
var ErrStorageUnavailable = errors.New("COLLECTION_STORAGE_UNAVAILABLE: center query failed; local history is not a fallback")

type CollectionStorageDelivery struct {
	EventID string                  `json:"eventId"`
	Policy  CollectionStoragePolicy `json:"policy"`
	Kind    string                  `json:"kind"`
	Value   json.RawMessage         `json:"value"`
}

// Other writers must fail rather than bypass the policy and store locally.
func RequireLocalCollection(in any, id string) error {
	p, err := CollectionStorage(in, id)
	if err != nil {
		return err
	}
	if p.Mode != "local" {
		return ErrStorageOperation
	}
	return nil
}

type storageReadTransport struct {
	Read func(context.Context, CollectionStoragePolicy, string, json.RawMessage) (json.RawMessage, error)
}

func (*storageReadTransport) Name() string              { return "collection-storage-reader" }
func (*storageReadTransport) Initialize(*gorm.DB) error { return nil }
func ConfigureCollectionStorageReader(db *gorm.DB, read func(context.Context, CollectionStoragePolicy, string, json.RawMessage) (json.RawMessage, error)) error {
	return db.Use(&storageReadTransport{Read: read})
}
func ReadCollectionStorage(ctx context.Context, id, operation string, body any) (json.RawMessage, bool, error) {
	p, err := collectionStorage(ctx, id, true)
	if err != nil {
		return nil, false, err
	}
	if p.Mode != "center" {
		return nil, false, nil
	}
	if p.TargetCollectionID == "" {
		return nil, true, ErrStorageDefinitionPending
	}
	transport, _ := GetUnsConn(ctx).Config.Plugins["collection-storage-reader"].(*storageReadTransport)
	if transport == nil {
		return nil, true, ErrStorageUnavailable
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, true, err
	}
	raw, err = transport.Read(ctx, p, operation, raw)
	if err == nil {
		var result map[string]json.RawMessage
		if err = json.Unmarshal(raw, &result); err == nil {
			storageReadIdentity(result, operation, id)
			raw, err = json.Marshal(result)
		}
	}
	return raw, true, err
}

// A read path without remote routing must not return retained local history.
func RequireLocalCollectionRead(in any, id string) error {
	p, err := collectionStorage(in, id, true)
	if err != nil {
		return err
	}
	if p.Mode == "center" {
		return ErrStorageOperation
	}
	return nil
}

// Rewrite envelope/document identity only; user data is never traversed.
func storageReadIdentity(result map[string]json.RawMessage, op, id string) {
	identity, _ := json.Marshal(id)
	if _, ok := result["collectionId"]; ok {
		result["collectionId"] = identity
	}
	for _, key := range []string{"items", "list", "records"} {
		var rows []map[string]json.RawMessage
		if json.Unmarshal(result[key], &rows) != nil {
			continue
		}
		for _, row := range rows {
			if _, ok := row["collectionId"]; ok && op != "record.aggregate" && op != "sample.aggregate" {
				row["collectionId"] = identity
			}
			if op == "record.aggregate" || op == "sample.aggregate" {
				var meta map[string]json.RawMessage
				if json.Unmarshal(row["_meta"], &meta) == nil && meta != nil {
					meta["collectionId"] = identity
					row["_meta"], _ = json.Marshal(meta)
				}
			}
		}
		result[key], _ = json.Marshal(rows)
	}
	// Sample latest endpoints wrap each sample separately from business data.
	for _, key := range []string{"series", "latestSamples"} {
		var rows []map[string]json.RawMessage
		if json.Unmarshal(result[key], &rows) != nil {
			continue
		}
		for _, row := range rows {
			var sample map[string]json.RawMessage
			if json.Unmarshal(row["sample"], &sample) == nil && sample != nil {
				sample["collectionId"] = identity
				row["sample"], _ = json.Marshal(sample)
			}
		}
		result[key], _ = json.Marshal(rows)
	}
}

// Reuse the Fleet installation queue budget and physical admission checks.
func CheckCollectionStorageAdmission(tx *gorm.DB, payloadBytes int64) error {
	if err := CheckFleetStorageAdmission(tx); err != nil {
		return err
	}
	budget, err := lockFleetQueueBudget(tx, "uns_storage_queue_budget", "collection-storage")
	if err != nil {
		return err
	}
	var usage FleetQueueUsage
	if err := tx.Table("uns_collection_storage_outbox").Select("count(*) AS events,COALESCE(sum(octet_length(payload::text)+512),0) AS bytes").Where("status='pending'").Scan(&usage).Error; err != nil {
		return err
	}
	usage.Events++
	usage.Bytes += payloadBytes + fleetQueueRowAllowance
	if usage.exceeds(budget) {
		return ErrFleetQueueFull
	}
	return nil
}

// Capture delivery identities inside the caller's existing write transaction.
// Callers publish them only after commit; no queue payload is exposed.
type collectionDeliveryKey struct{}

func CaptureCollectionDelivery(ctx context.Context) (context.Context, *[]CollectionStorageDelivery) {
	values := []CollectionStorageDelivery{}
	return context.WithValue(ctx, collectionDeliveryKey{}, &values), &values
}
func RecordCollectionDelivery(ctx context.Context, value CollectionStorageDelivery) {
	if values, ok := ctx.Value(collectionDeliveryKey{}).(*[]CollectionStorageDelivery); ok {
		*values = append(*values, value)
	}
}

// Permanent deletion is local to this node; it must not orphan an authoritative center dataset.
func RequireLocalCollectionPurge(in any, id string) error {
	p, err := collectionStorage(in, id, true)
	if err != nil {
		return err
	}
	if p.Mode != "local" {
		return ErrStorageOperation
	}
	return nil
}

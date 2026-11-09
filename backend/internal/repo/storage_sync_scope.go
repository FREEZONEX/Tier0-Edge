package repo

import (
	"errors"
	"gorm.io/gorm"
)

// Selection is independent of the global storage mode. Unselected Collections
// stay local; accepted outbox deliveries retain the destination in their payload.
type StorageSyncScope struct {
	ID            string   `gorm:"primaryKey" json:"-"`
	Mode          string   `json:"mode"`
	CollectionIDs []string `gorm:"serializer:json;type:jsonb" json:"collectionIds"`
	Revision      int64    `json:"revision"`
}

func (StorageSyncScope) TableName() string { return "uns_storage_sync_scope" }
func CollectionSyncScope(in any) (StorageSyncScope, error) {
	p := StorageSyncScope{ID: "edge", Mode: "all", CollectionIDs: []string{}}
	err := GetUnsConn(in).Where("id=?", "edge").Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return p, err
}

// SQL uses the same transaction snapshot as Record/Sample admission.
const StorageCollectionSelectedSQL = "NOT EXISTS (SELECT 1 FROM uns_storage_sync_scope s WHERE s.id='edge' AND s.mode='selected' AND NOT s.collection_ids @> jsonb_build_array(uns_meta.id))"

func StorageSyncCollections(in any) *gorm.DB {
	return LocalStorageCollections(in).Where(StorageCollectionSelectedSQL)
}

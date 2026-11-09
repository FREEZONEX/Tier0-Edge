package cluster

import (
	"backend/internal/domain/license"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"context"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FleetCollectionScopeRequest struct {
	ExpectedRevision int64    `json:"expectedRevision"`
	Mode             string   `json:"mode"`
	CollectionIDs    []string `json:"collectionIds"`
}

// Only the local Edge connection administrator may grant synchronization scope.
// The peer transport has no operation that lets a center enlarge this selection.
func (s *Service) SaveCollectionSyncScope(ctx context.Context, req FleetCollectionScopeRequest) (any, error) {
	if license.Default().IsHost() {
		return nil, ErrHostCannotConnect
	}
	if req.ExpectedRevision < 0 || (req.Mode != "all" && req.Mode != "selected") || len(req.CollectionIDs) > 2000 || (req.Mode == "all" && len(req.CollectionIDs) > 0) {
		return nil, ErrInvalid
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range req.CollectionIDs {
		if !unsmeta.ValidID(id) || seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
		ids = append(ids, id)
	}
	var next repo.StorageSyncScope
	err := repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(814206108)").Error; err != nil {
			return err
		}
		old, err := repo.CollectionSyncScope(tx)
		if err != nil {
			return err
		}
		if old.Revision != req.ExpectedRevision {
			return &FleetRevisionConflict{CurrentRevision: old.Revision}
		}
		if len(ids) > 0 {
			var count int64
			if err := repo.LocalStorageCollections(tx).Where("id IN ?", ids).Count(&count).Error; err != nil {
				return err
			}
			if count != int64(len(ids)) {
				return ErrInvalid
			}
		}
		next = repo.StorageSyncScope{ID: "edge", Mode: req.Mode, CollectionIDs: ids, Revision: old.Revision + 1}
		return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&next).Error
	})
	return next, err
}

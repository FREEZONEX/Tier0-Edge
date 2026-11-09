package cluster

import (
	"backend/internal/domain/license"
	"backend/internal/repo"
	"context"
)

type FleetCollectionSyncRequest struct {
	NodeID string `json:"nodeId"`
	Page   int    `json:"page,omitempty"`
}

func (s *Service) FleetCollectionSync(ctx context.Context, action string, req FleetCollectionSyncRequest) (any, error) {
	if action != "catalog" {
		return nil, ErrInvalid
	}
	if !license.Default().IsHost() {
		return nil, ErrUnauthorized
	}
	if _, err := s.fleetRepo.CollaborationNode(ctx, req.NodeID); err != nil {
		return nil, err
	}
	return s.CallFleetNode(ctx, req.NodeID, "collections.catalog", "node", map[string]any{"page": req.Page})
}
func (s *Service) FleetCollectionCatalog(ctx context.Context, page int) (any, error) {
	if page < 1 {
		page = 1
	}
	var rows []struct {
		ID                 string `json:"id"`
		Name               string `json:"name"`
		Revision           int64  `json:"revision"`
		TargetCollectionID string `json:"targetCollectionId"`
		Selected           bool   `json:"selected"`
		Synced             bool   `json:"synced"`
		LastError          string `json:"lastError"`
	}
	q := repo.LocalStorageCollections(ctx)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	connection, _ := s.storageConnection()
	if err := q.Select(repo.StorageCollectionSelectedSQL+" AS selected,uns_meta.id,uns_meta.data->>'name' AS name,uns_meta.revision, COALESCE(m.target_collection_id,'') AS target_collection_id,COALESCE(m.source_revision=uns_meta.revision AND m.target_collection_id<>'',false) AS synced,COALESCE(m.last_error,'') AS last_error").Joins("LEFT JOIN uns_storage_collection_mapping m ON m.collection_id=uns_meta.id AND m.center_id=?", connection.CenterID).Order("uns_meta.id").Offset((page - 1) * 100).Limit(100).Find(&rows).Error; err != nil {
		return nil, err
	}
	return map[string]any{"items": rows, "total": total, "page": page, "size": 100}, nil
}

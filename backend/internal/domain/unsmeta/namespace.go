package unsmeta

import (
	"context"

	"backend/internal/repo"
	"gorm.io/gorm"
)

// 在元数据事务锁内调用，统一创建、复制、移动和恢复的名称规则。
func validateNamespaceName(ctx context.Context, tx *gorm.DB, id, kind string, doc Document, originNodeID string) error {
	storageKind := kind
	if kind == "folder" {
		storageKind = "directory"
	}
	query := tx.WithContext(ctx).Model(&repo.UnsMeta{}).
		Where("mount_id IS NULL AND CASE WHEN kind='collection' THEN origin_node_id ELSE '' END=?", originNodeID).
		Where("kind IN ('directory','topic','collection') AND deleted_at=0 AND NOT (kind=? AND id=?)", storageKind, id).
		Where("COALESCE(data->>'parentId','')=? AND lower(data->>'name')=lower(CAST(? AS text))", doc.ParentID, doc.Name)

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return repo.ErrDuplicate
	}
	return nil
}

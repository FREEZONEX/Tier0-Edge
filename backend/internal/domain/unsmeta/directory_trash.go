package unsmeta

import (
	"context"
	"errors"
	"time"

	"backend/internal/repo"
	"gorm.io/gorm"
)

var ErrTrashDirectory = errors.New("UnsMeta.trashDirectory")

// 在同一元数据排他事务中改变整棵子树；删除前已在回收站的对象不加入本批次。
func (s *Service) directoryState(ctx context.Context, tx *gorm.DB, root repo.UnsMeta, restore bool) (repo.UnsMeta, error) {
	var rows []repo.UnsMeta
	var err error
	if restore {
		err = tx.Where("kind IN ('directory','topic','collection') AND deleted_at<>0 AND (id=? OR data->>'directoryTrashId'=?)", root.ID, root.ID).Find(&rows).Error
	} else {
		err = tx.Raw(`WITH RECURSIVE subtree AS (
		 SELECT * FROM public.uns_meta WHERE kind='directory' AND id=? AND deleted_at=0
		 UNION
		 SELECT child.* FROM public.uns_meta child JOIN subtree parent ON child.data->>'parentId'=parent.id
		 WHERE parent.kind='directory' AND child.kind IN ('directory','topic','collection') AND child.deleted_at=0
		) SELECT * FROM subtree`, root.ID).Scan(&rows).Error
	}
	if err != nil {
		return root, err
	}
	ids := make([]string, 0, len(rows))
	for i := range rows {
		// Raw Scan 不运行模型的 AfterFind。
		rows[i].Kind = rows[i].StorageKind
		if rows[i].Kind == "directory" {
			rows[i].Kind = "folder"
		}
		doc, err := decode(rows[i])
		if err != nil {
			return root, err
		}
		if rows[i].MountID != nil {
			return root, repo.ErrMountedReadOnly
		}
		if doc.PurgeJobID != "" {
			return root, ErrPurgeChanged
		}
		ids = append(ids, rows[i].ID)
	}
	now, deletedAt := time.Now().UnixMilli(), int64(0)
	if !restore {
		deletedAt = now
	}
	data := gorm.Expr("data-'directoryTrashId'")
	if !restore {
		data = gorm.Expr("jsonb_set(data,'{directoryTrashId}',to_jsonb(?::text))", root.ID)
	}
	if err := tx.Model(&repo.UnsMeta{}).Where("kind IN ('directory','topic','collection') AND id IN ?", ids).
		Updates(map[string]any{"deleted_at": deletedAt, "updated_at": now, "revision": gorm.Expr("revision+1"), "data": data}).Error; err != nil {
		return root, err
	}
	// 同一事务内校验恢复后的完整关系；删除保留外部引用，不让引用阻止回收。
	for _, row := range rows {
		doc, err := decode(row)
		if err != nil {
			return root, err
		}
		if restore {
			err = validateRestoredNamespace(ctx, tx, row, doc)
		}
		if err != nil {
			return root, err
		}
	}
	return repo.NewUnsMetaRepo(tx).Get(ctx, root.ID, true, "directory")
}

func validateRestoredNamespace(ctx context.Context, tx *gorm.DB, row repo.UnsMeta, doc Document) error {
	if err := validateNamespaceName(ctx, tx, row.ID, row.Kind, doc, row.OriginNodeID); err != nil {
		return err
	}
	return validateReferences(ctx, tx, row.ID, doc, true)
}

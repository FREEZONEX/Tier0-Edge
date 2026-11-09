package unsmeta

import (
	"context"
	"errors"
	"time"

	"backend/internal/repo"
)

// NamespacePurgeMembers 在调用方排他事务内固定整棵目录子树，子节点先于父目录清理。
// 包含此前独立回收的后代；任何有效后代都会拒绝整次请求，避免删除活动数据。
func NamespacePurgeMembers(ctx context.Context, id, kind string) ([]repo.UnsMeta, error) {
	tx := repo.GetUnsConn(ctx)
	root, err := repo.NewUnsMetaRepo(tx).Get(ctx, id, true, kind)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if kind != "directory" {
		return []repo.UnsMeta{root}, nil
	}
	var rows []repo.UnsMeta
	if err := tx.Raw(`WITH RECURSIVE subtree AS (
	 SELECT * FROM public.uns_meta WHERE kind='directory' AND id=?
	 UNION
	 SELECT child.* FROM public.uns_meta child JOIN subtree parent ON child.data->>'parentId'=parent.id
	 WHERE parent.kind='directory' AND child.kind IN ('directory','topic','collection')
	) SELECT * FROM subtree ORDER BY kind,id`, id).Scan(&rows).Error; err != nil {
		return nil, err
	}
	children := map[string][]repo.UnsMeta{}
	for _, row := range rows {
		doc, err := decode(row)
		if err != nil {
			return nil, err
		}
		children[doc.ParentID] = append(children[doc.ParentID], row)
	}
	var ordered []repo.UnsMeta
	seen := map[string]bool{}
	var visit func(repo.UnsMeta) error
	visit = func(row repo.UnsMeta) error {
		if seen[row.ID] {
			return ErrCycle
		}
		seen[row.ID] = true
		if row.StorageKind == "directory" || row.Kind == "folder" {
			for _, child := range children[row.ID] {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		ordered = append(ordered, row)
		return nil
	}
	if err := visit(root); err != nil {
		return nil, err
	}
	return ordered, nil
}

// FenceNamespacePurge 固定对象代次并阻止恢复。
func FenceNamespacePurge(ctx context.Context, id, kind, jobID string, dryRun bool) (bool, error) {
	if kind == "collection" {
		return FenceCollectionPurge(ctx, id, jobID, dryRun)
	}
	if kind != "directory" && kind != "topic" {
		return false, ErrInvalid
	}
	tx := repo.GetUnsConn(ctx)
	row, err := repo.NewUnsMetaRepo(tx).Get(ctx, id, true, kind)
	if errors.Is(err, repo.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if row.DeletedAt == 0 {
		return false, repo.ErrConflict
	}
	doc, err := decode(row)
	if err != nil {
		return false, err
	}
	if row.MountID != nil {
		return false, repo.ErrMountedReadOnly
	}
	if doc.PurgeJobID != "" {
		if doc.PurgeJobID == jobID {
			return true, nil
		}
		return false, ErrPurgeChanged
	}
	if dryRun {
		return true, nil
	}
	return true, tx.Exec("UPDATE public.uns_meta SET data=jsonb_set(data,'{purgeJobId}',to_jsonb(?::text)),revision=revision+1,updated_at=? WHERE kind=? AND id=?", jobID, time.Now().UnixMilli(), kind, id).Error
}

// FinishNamespacePurge 只清理目录或引用的元数据，绝不沿引用删除目标集合。
func FinishNamespacePurge(ctx context.Context, id, kind, jobID string) error {
	if kind != "directory" && kind != "topic" {
		return ErrInvalid
	}
	tx := repo.GetUnsConn(ctx)
	row, err := repo.NewUnsMetaRepo(tx).Get(ctx, id, true, kind)
	if err != nil {
		return err
	}
	doc, err := decode(row)
	if err != nil {
		return err
	}
	if row.DeletedAt == 0 || doc.PurgeJobID != jobID {
		return ErrPurgeChanged
	}
	if kind == "directory" {
		var count int64
		if err := tx.Model(&repo.UnsMeta{}).Where("kind IN ('directory','topic','collection') AND data->>'parentId'=?", id).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return ErrReferenced
		}
	}
	return tx.Exec("DELETE FROM public.uns_meta WHERE kind=? AND id=?", kind, id).Error
}

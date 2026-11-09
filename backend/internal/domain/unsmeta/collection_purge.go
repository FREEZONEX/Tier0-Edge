package unsmeta

import (
	"context"
	"errors"
	"time"

	"backend/internal/repo"
)

var ErrPurgeChanged = errors.New("UnsMeta.purgeChanged")

const collectionPurgeBatchSize = 1000

// FenceCollectionPurge 在调用方事务内固定回收站目标，已有同一任务标记可以重试。
// 持久屏障阻止恢复；活动集合校验继续阻止修改和新增引用。
func FenceCollectionPurge(ctx context.Context, id, jobID string, dryRun bool) (bool, error) {
	tx := repo.GetUnsConn(ctx)
	if err := lockMetadata(tx, false); err != nil {
		return false, err
	}
	row, err := repo.NewUnsMetaRepo(tx).Get(ctx, id, true, "collection")
	if errors.Is(err, repo.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := repo.RequireLocalCollectionPurge(ctx, id); err != nil {
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
	return true, tx.Exec("UPDATE public.uns_meta SET data=jsonb_set(data,'{purgeJobId}',to_jsonb(?::text)),revision=revision+1,updated_at=? WHERE kind='collection' AND id=?", jobID, time.Now().UnixMilli(), id).Error
}

// CheckCollectionPurge 只允许持久任务清理它已经固定的那一代集合。
func CheckCollectionPurge(ctx context.Context, id, jobID string) error {
	if err := repo.RequireLocalCollectionPurge(ctx, id); err != nil {
		return err
	}
	row, err := repo.NewUnsMetaRepo(ctx).Get(ctx, id, true, "collection")
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
	return nil
}

// PurgeCollectionBatch 每次仅清理一批，在调用方事务内完成，采样清完才处理记录。
func PurgeCollectionBatch(ctx context.Context, id, jobID string) (bool, error) {
	if err := CheckCollectionPurge(ctx, id, jobID); err != nil {
		return false, err
	}
	tx := repo.GetUnsConn(ctx)
	result := tx.Exec("DELETE FROM public.uns_sample WHERE (collection_id,record_id,id,time) IN (SELECT collection_id,record_id,id,time FROM public.uns_sample WHERE collection_id=? LIMIT ?)", id, collectionPurgeBatchSize)
	if result.Error != nil || result.RowsAffected > 0 {
		return false, result.Error
	}
	result = tx.Exec("DELETE FROM public.uns_record WHERE (collection_id,id) IN (SELECT collection_id,id FROM public.uns_record WHERE collection_id=? ORDER BY id LIMIT ?)", id, collectionPurgeBatchSize)
	return result.RowsAffected == 0, result.Error
}

// FinishCollectionPurge 在调用方最终事务内退休全部索引并删除集合，授权与任务进度由调用方原子提交。
func FinishCollectionPurge(ctx context.Context, id, jobID string) error {
	if err := CheckCollectionPurge(ctx, id, jobID); err != nil {
		return err
	}
	tx := repo.GetUnsConn(ctx)
	if err := retireQueryIndexes(tx, id); err != nil {
		return err
	}
	return tx.Exec("DELETE FROM public.uns_meta WHERE kind='collection' AND id=?", id).Error
}

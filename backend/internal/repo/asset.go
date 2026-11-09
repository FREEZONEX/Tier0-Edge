package repo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

type AssetFile struct {
	ID            int64  `gorm:"column:id;primaryKey" json:"id"`
	FileKey       string `gorm:"column:file_key" json:"fileKey"`
	OriginalName  string `gorm:"column:original_name" json:"originalName"`
	ContentType   string `gorm:"column:content_type" json:"contentType"`
	SizeBytes     int64  `gorm:"column:size_bytes" json:"sizeBytes"`
	Sha256        string `gorm:"column:sha256" json:"sha256"`
	StorageDriver string `gorm:"column:storage_driver" json:"storageDriver"`
	StorageKey    string `gorm:"column:storage_key" json:"storageKey"`
	Bucket        string `gorm:"column:bucket" json:"bucket"`
	Visibility    string `gorm:"column:visibility" json:"visibility"`
	Status        string `gorm:"column:status" json:"status"`
	ProjectID     int64  `gorm:"column:project_id" json:"projectId"`
	AppInstanceID string `gorm:"column:app_instance_id" json:"appInstanceId"`
	SessionID     string `gorm:"column:session_id" json:"sessionId"`
	// OpID 记录分片上传会话的对象存储 uploadId（仅分片上传的 temp 记录使用）。
	OpID string `gorm:"column:op_id" json:"opId"`
	// ExpireAt 是分片上传会话的过期时间（仅 temp 记录使用），过期的 temp 记录
	// 由后台清理任务按需回收。
	ExpireAt *time.Time `gorm:"column:expire_at;type:timestamptz" json:"expireAt"`
	CreatorSoftTime
	// Populated only by ListAssetFiles via the asset_binding join.
	BindingID int64  `gorm:"-" json:"bindingId"`
	OwnerType string `gorm:"-" json:"ownerType"`
	OwnerID   int64  `gorm:"-" json:"ownerId"`
}

func (AssetFile) TableName() string { return "asset_file" }

type AssetFileFilter struct {
	OwnerType string
	OwnerID   int64
}

type AssetBinding struct {
	ID        int64           `gorm:"column:id;primaryKey" json:"id"`
	AssetID   int64           `gorm:"column:asset_id" json:"assetId"`
	OwnerType string          `gorm:"column:owner_type" json:"ownerType"`
	OwnerID   int64           `gorm:"column:owner_id" json:"ownerId"`
	Usage     string          `gorm:"column:usage" json:"usage"`
	SortKey   int             `gorm:"column:sort_key" json:"sortKey"`
	Metadata  json.RawMessage `gorm:"column:metadata;type:jsonb" json:"metadata"`
	CreatorCreateSoftTime
}

func (AssetBinding) TableName() string { return "asset_binding" }

type AssetRepo struct{ db *gorm.DB }

func NewAssetRepo(in any) *AssetRepo { return &AssetRepo{db: GetCommonConn(in)} }

func (r *AssetRepo) CreateAssetFile(ctx context.Context, file AssetFile) (AssetFile, error) {
	file = normalizeAssetFile(file)
	err := r.db.WithContext(ctx).Create(&file).Error
	return file, normalizeDBError(err)
}

func normalizeAssetFile(file AssetFile) AssetFile {
	if file.FileKey == "" {
		file.FileKey = "af_" + randomHex(16)
	}
	if file.StorageDriver == "" {
		file.StorageDriver = "local"
	}
	if file.StorageKey == "" {
		file.StorageKey = file.FileKey
	}
	if file.Visibility == "" {
		file.Visibility = "private"
	}
	if file.Status == "" {
		file.Status = "temp"
	}
	return file
}

// CreateAssetFileWithBinding creates the file row and, when binding is not
// nil, its binding row in one transaction. The file keeps its temp status;
// business confirmation moves it to active through BindAsset.
func (r *AssetRepo) CreateAssetFileWithBinding(ctx context.Context, file AssetFile, binding *AssetBinding) (AssetFile, error) {
	file = normalizeAssetFile(file)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&file).Error; err != nil {
			return normalizeDBError(err)
		}
		if binding != nil {
			binding.AssetID = file.ID
			if len(binding.Metadata) == 0 {
				binding.Metadata = json.RawMessage(`{}`)
			}
			if binding.Usage == "" {
				binding.Usage = "attachment"
			}
			if binding.CreatedTime.IsZero() {
				binding.CreatedTime = repoTimeFromMilli(repoNowMilli())
			}
			if err := tx.Create(binding).Error; err != nil {
				return normalizeDBError(err)
			}
		}
		return nil
	})
	return file, err
}

func (r *AssetRepo) ListAssetFiles(ctx context.Context, filter AssetFileFilter) ([]AssetFile, error) {
	type assetFileRow struct {
		AssetFile
		BindingID int64  `gorm:"column:binding_id"`
		OwnerType string `gorm:"column:owner_type"`
		OwnerID   int64  `gorm:"column:owner_id"`
	}
	q := r.db.WithContext(ctx).Table("asset_file f").
		Select("f.id, f.file_key, f.original_name, f.content_type, f.size_bytes, f.sha256, f.storage_driver, f.storage_key, f.bucket, f.visibility, f.status, f.created_by, f.created_time, f.updated_time, f.deleted_time, COALESCE(b.id,0) AS binding_id, COALESCE(b.owner_type,'') AS owner_type, COALESCE(b.owner_id,0) AS owner_id").
		Joins("LEFT JOIN asset_binding b ON b.asset_id=f.id AND b.deleted_time=0").
		Where("f.deleted_time = 0")
	if strings.TrimSpace(filter.OwnerType) != "" {
		q = q.Where("b.owner_type = ?", strings.TrimSpace(filter.OwnerType))
	}
	if filter.OwnerID != 0 {
		q = q.Where("b.owner_id = ?", filter.OwnerID)
	}
	var rows []assetFileRow
	if err := q.Order("f.id DESC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]AssetFile, 0, len(rows))
	for _, row := range rows {
		item := row.AssetFile
		item.BindingID = row.BindingID
		item.OwnerType = row.OwnerType
		item.OwnerID = row.OwnerID
		out = append(out, item)
	}
	return out, nil
}

func (r *AssetRepo) GetAssetFile(ctx context.Context, id int64) (AssetFile, error) {
	var f AssetFile
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", id).Take(&f).Error
	return f, err
}

// GetAssetFileByOwner 按 id 与绑定关系查询 asset 文件：
// 仅当文件存在且存在一条绑定到 (ownerType, ownerID) 的存活绑定记录时返回。
// 用于导入/覆盖导入等按归属校验的读取路径，防止跨项目越权访问他人上传的文件；
// 未命中（文件不存在或不属于该 owner）返回 ErrNotFound，不泄露文件名与来源。
func (r *AssetRepo) GetAssetFileByOwner(ctx context.Context, id int64, ownerType string, ownerID int64) (AssetFile, error) {
	type assetFileRow struct {
		AssetFile
		BindingID int64  `gorm:"column:binding_id"`
		OwnerType string `gorm:"column:owner_type"`
		OwnerID   int64  `gorm:"column:owner_id"`
	}
	var row assetFileRow
	err := r.db.WithContext(ctx).Table("asset_file f").
		Select("f.id, f.file_key, f.original_name, f.content_type, f.size_bytes, f.sha256, f.storage_driver, f.storage_key, f.bucket, f.visibility, f.status, f.created_by, f.created_time, f.updated_time, f.deleted_time, COALESCE(b.id,0) AS binding_id, COALESCE(b.owner_type,'') AS owner_type, COALESCE(b.owner_id,0) AS owner_id").
		Joins("JOIN asset_binding b ON b.asset_id=f.id AND b.deleted_time=0").
		Where("f.id = ? AND f.deleted_time = 0 AND b.owner_type = ? AND b.owner_id = ?", id, strings.TrimSpace(ownerType), ownerID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AssetFile{}, ErrNotFound
	}
	if err != nil {
		return AssetFile{}, err
	}
	item := row.AssetFile
	item.BindingID = row.BindingID
	item.OwnerType = row.OwnerType
	item.OwnerID = row.OwnerID
	return item, nil
}

// ListActiveAssetFilesByStorageKey returns the active rows for a storage
// key, newest first. storage_key is only indexed, not unique: the same key
// can appear in more than one project scope, so callers qualify the match
// (project access, visibility) instead of blindly taking the newest row.
func (r *AssetRepo) ListActiveAssetFilesByStorageKey(ctx context.Context, storageKey string) ([]AssetFile, error) {
	var rows []AssetFile
	err := r.db.WithContext(ctx).
		Where("storage_key = ? AND deleted_time = 0", strings.TrimSpace(storageKey)).
		Order("id DESC").
		Find(&rows).Error
	return rows, err
}

// GetTempAssetFileByOpID 按分片上传会话的对象存储 uploadId（op_id）查找未完成的
// temp 记录。uploadId 由对象存储生成、全局唯一，可作为分片会话的服务端凭据；
// 未命中或记录已 active/deleted 返回 ErrNotFound。
func (r *AssetRepo) GetTempAssetFileByOpID(ctx context.Context, opID string) (AssetFile, error) {
	var f AssetFile
	err := r.db.WithContext(ctx).
		Where("op_id = ? AND status = 'temp' AND deleted_time = 0", strings.TrimSpace(opID)).
		Take(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AssetFile{}, ErrNotFound
	}
	return f, err
}

// MarkMultipartUploadActive 在 complete 成功后把分片上传的 temp 记录置为 active，
// 并把 size_bytes 覆盖为对象存储汇总的真实大小（分片场景不信任客户端自报大小）。
func (r *AssetRepo) MarkMultipartUploadActive(ctx context.Context, id, sizeBytes int64) error {
	res := r.db.WithContext(ctx).Model(&AssetFile{}).
		Where("id = ? AND status = 'temp' AND deleted_time = 0", id).
		Updates(touchValues(map[string]any{"status": "active", "size_bytes": sizeBytes}, repoNowMilli()))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// SoftDeleteAssetFile soft-deletes the file row and all of its bindings.
func (r *AssetRepo) SoftDeleteAssetFile(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&AssetFile{}).
			Where("id = ? AND deleted_time = 0", id).
			Updates(softDeleteNoDelByValues(0, 0))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		// asset_binding has no updated_time column (CreatorCreateSoftTime),
		// so the soft-delete helpers that touch updated_time cannot be used here.
		return tx.Model(&AssetBinding{}).
			Where("asset_id = ? AND deleted_time = 0", id).
			Update("deleted_time", repoDeleteTimeFromMilli(0)).Error
	})
}

func (r *AssetRepo) BindAsset(ctx context.Context, binding AssetBinding) (AssetBinding, error) {
	now := repoNowMilli()
	if len(binding.Metadata) == 0 {
		binding.Metadata = json.RawMessage(`{}`)
	}
	if binding.Usage == "" {
		binding.Usage = "attachment"
	}
	if binding.CreatedTime.IsZero() {
		binding.CreatedTime = repoTimeFromMilli(now)
	}
	if err := r.db.WithContext(ctx).Create(&binding).Error; err != nil {
		return AssetBinding{}, normalizeDBError(err)
	}
	_ = r.db.WithContext(ctx).Model(&AssetFile{}).
		Where("id = ? AND deleted_time = 0", binding.AssetID).
		Updates(touchValues(map[string]any{"status": "active"}, now)).Error
	return binding, nil
}

func (r *AssetRepo) DeleteAssetBinding(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Model(&AssetBinding{}).
		Where("id = ? AND deleted_time = 0", id).
		Update("deleted_time", repoDeleteTimeFromMilli(0))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *AssetRepo) GetAssetBinding(ctx context.Context, id int64) (AssetBinding, error) {
	var binding AssetBinding
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", id).Take(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AssetBinding{}, ErrNotFound
	}
	return binding, err
}

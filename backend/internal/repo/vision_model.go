package repo

import (
	"context"

	"gorm.io/gorm"
)

// VisionModel 是独立的模型注册表条目(sys_vision_model),供算法通过 model_id 引用。
type VisionModel struct {
	ID           int64  `gorm:"column:id;primaryKey" json:"id"`
	ModelCode    string `gorm:"column:model_code;size:128;not null" json:"modelCode"`
	Name         string `gorm:"column:name;size:128;not null;default:''" json:"name"`
	Version      string `gorm:"column:version;size:32;not null;default:'1.0.0'" json:"version"`
	Source       string `gorm:"column:source;size:16;not null;default:'custom'" json:"source"`
	Format       string `gorm:"column:format;size:16;not null;default:'onnx'" json:"format"`
	FileName     string `gorm:"column:file_name;size:256;not null;default:''" json:"fileName"`
	StorageKey   string `gorm:"column:storage_key;size:512;not null;default:''" json:"storageKey"`
	Sha256       string `gorm:"column:sha256;size:64;not null;default:''" json:"sha256"`
	SizeBytes    int64  `gorm:"column:size_bytes;not null;default:0" json:"sizeBytes"`
	License      string `gorm:"column:license;size:64;not null;default:''" json:"license"`
	SourceURL    string `gorm:"column:source_url;size:512;not null;default:''" json:"sourceUrl"`
	ModelType    string `gorm:"column:model_type;size:32;not null;default:''" json:"modelType"`
	InputWidth   int    `gorm:"column:input_width;not null;default:640" json:"inputWidth"`
	InputHeight  int    `gorm:"column:input_height;not null;default:640" json:"inputHeight"`
	Labels       string `gorm:"column:labels;type:text;not null;default:'[]'" json:"labels"`
	Preprocess   string `gorm:"column:preprocess;type:text;not null;default:'{}'" json:"preprocess"`
	Status       string `gorm:"column:status;size:16;not null;default:'available'" json:"status"`
	ErrorMessage string `gorm:"column:error_message;type:text;not null;default:''" json:"errorMessage"`
	SoftNoDelByTime
}

func (VisionModel) TableName() string { return "sys_vision_model" }

type VisionModelFilter struct {
	Page    int
	Size    int
	Keyword string
	Source  string
}

type VisionModelRepo struct{ db *gorm.DB }

func NewVisionModelRepo(in any) *VisionModelRepo { return &VisionModelRepo{db: GetCommonConn(in)} }

func (r *VisionModelRepo) Create(ctx context.Context, item VisionModel) (VisionModel, error) {
	err := r.db.WithContext(ctx).Create(&item).Error
	return item, normalizeDBError(err)
}

func (r *VisionModelRepo) Get(ctx context.Context, id int64) (VisionModel, error) {
	var item VisionModel
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", id).Take(&item).Error
	return item, normalizeDBError(err)
}

func (r *VisionModelRepo) GetByCode(ctx context.Context, code string) (VisionModel, error) {
	var item VisionModel
	err := r.db.WithContext(ctx).Where("model_code = ? AND deleted_time = 0", code).Take(&item).Error
	return item, normalizeDBError(err)
}

// ListByIDs 批量取模型行(供算法列表拼视图,避免逐条查询)。
func (r *VisionModelRepo) ListByIDs(ctx context.Context, ids []int64) ([]VisionModel, error) {
	items := make([]VisionModel, 0, len(ids))
	if len(ids) == 0 {
		return items, nil
	}
	err := r.db.WithContext(ctx).
		Where("id IN ? AND deleted_time = 0", ids).Find(&items).Error
	return items, normalizeDBError(err)
}

func (r *VisionModelRepo) List(ctx context.Context, filter VisionModelFilter) ([]VisionModel, int64, error) {
	query := r.db.WithContext(ctx).Model(&VisionModel{}).Where("deleted_time = 0")
	if filter.Keyword != "" {
		like := "%" + filter.Keyword + "%"
		query = query.Where("(name ILIKE ? OR model_code ILIKE ? OR file_name ILIKE ?)", like, like, like)
	}
	if filter.Source != "" {
		query = query.Where("source = ?", filter.Source)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, normalizeDBError(err)
	}
	items := make([]VisionModel, 0)
	if filter.Size > 0 {
		query = query.Offset((filter.Page - 1) * filter.Size).Limit(filter.Size)
	}
	err := query.Order("created_time DESC").Find(&items).Error
	return items, total, normalizeDBError(err)
}

func (r *VisionModelRepo) Update(ctx context.Context, id int64, updates map[string]any) error {
	result := r.db.WithContext(ctx).Model(&VisionModel{}).
		Where("id = ? AND deleted_time = 0", id).Updates(updates)
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *VisionModelRepo) SoftDelete(ctx context.Context, id, userID int64) error {
	result := r.db.WithContext(ctx).Model(&VisionModel{}).
		Where("id = ? AND deleted_time = 0", id).
		Updates(softDeleteNoDelByValues(userID, repoNowMilli()))
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// CountAlgorithmRefs 统计有多少未删除算法通过 model_id 引用该模型。
func (r *VisionModelRepo) CountAlgorithmRefs(ctx context.Context, modelID int64) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&VisionAlgorithm{}).
		Where("model_id = ? AND deleted_time = 0", modelID).Count(&total).Error
	return total, normalizeDBError(err)
}

// CountAlgorithmRefsByModelIDs 批量统计引用数(模型列表页一次查询拿全量 refCount)。
func (r *VisionModelRepo) CountAlgorithmRefsByModelIDs(ctx context.Context, modelIDs []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(modelIDs))
	if len(modelIDs) == 0 {
		return out, nil
	}
	type refRow struct {
		ModelID int64 `gorm:"column:model_id"`
		Total   int64 `gorm:"column:total"`
	}
	rows := make([]refRow, 0, len(modelIDs))
	err := r.db.WithContext(ctx).Model(&VisionAlgorithm{}).
		Select("model_id, COUNT(*) AS total").
		Where("model_id IN ? AND deleted_time = 0", modelIDs).
		Group("model_id").Scan(&rows).Error
	if err != nil {
		return nil, normalizeDBError(err)
	}
	for _, row := range rows {
		out[row.ModelID] = row.Total
	}
	return out, nil
}

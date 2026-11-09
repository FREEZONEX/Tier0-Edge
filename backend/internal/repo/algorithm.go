package repo

import (
	"context"

	"gorm.io/gorm"
)

type VisionAlgorithm struct {
	ID            int64  `gorm:"column:id;primaryKey" json:"id"`
	AlgorithmCode string `gorm:"column:algorithm_code;size:128;not null" json:"algorithmCode"`
	Name          string `gorm:"column:name;size:128;not null" json:"name"`
	Source        string `gorm:"column:source;size:16;not null;default:'custom'" json:"source"`
	AlgoType      string `gorm:"column:algo_type;size:32;not null;default:'object_detection'" json:"algoType"`
	Description   string `gorm:"column:description;type:text;not null;default:''" json:"description"`
	Scene         string `gorm:"column:scene;size:256;not null;default:''" json:"scene"`
	Labels        string `gorm:"column:labels;type:text;not null;default:'[]'" json:"labels"`
	DefaultOutput string `gorm:"column:default_output;type:text;not null;default:'[]'" json:"defaultOutput"`
	ModelFormat   string `gorm:"column:model_format;size:16;not null;default:'onnx'" json:"modelFormat"`
	ModelStatus   string `gorm:"column:model_status;size:16;not null;default:'missing'" json:"modelStatus"`
	ModelFile     string `gorm:"column:model_file;size:256;not null;default:''" json:"modelFile"`
	ModelKey      string `gorm:"column:model_key;size:512;not null;default:''" json:"modelKey"`
	ModelError    string `gorm:"column:model_error;type:text;not null;default:''" json:"modelError"`
	ModelID       int64  `gorm:"column:model_id;not null;default:0" json:"modelId"`
	Definition    string `gorm:"column:definition;type:text;not null;default:'{}'" json:"definition"`
	SoftNoDelByTime
}

func (VisionAlgorithm) TableName() string { return "sys_vision_algorithm" }

type VisionAlgorithmFilter struct {
	Page        int
	Size        int
	Keyword     string
	Source      string
	AlgoType    string
	ModelStatus string
}

type VisionAlgorithmRepo struct{ db *gorm.DB }

func NewVisionAlgorithmRepo(in any) *VisionAlgorithmRepo {
	return &VisionAlgorithmRepo{db: GetCommonConn(in)}
}

func (r *VisionAlgorithmRepo) Create(ctx context.Context, item VisionAlgorithm) (VisionAlgorithm, error) {
	err := r.db.WithContext(ctx).Create(&item).Error
	return item, normalizeDBError(err)
}

func (r *VisionAlgorithmRepo) Get(ctx context.Context, id int64) (VisionAlgorithm, error) {
	var item VisionAlgorithm
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", id).Take(&item).Error
	return item, normalizeDBError(err)
}

// GetByCode 按 algorithm_code 取未删除算法行(catalog 启动同步用)。
func (r *VisionAlgorithmRepo) GetByCode(ctx context.Context, code string) (VisionAlgorithm, error) {
	var item VisionAlgorithm
	err := r.db.WithContext(ctx).Where("algorithm_code = ? AND deleted_time = 0", code).Take(&item).Error
	return item, normalizeDBError(err)
}

func (r *VisionAlgorithmRepo) List(ctx context.Context, filter VisionAlgorithmFilter) ([]VisionAlgorithm, int64, error) {
	query := r.db.WithContext(ctx).Model(&VisionAlgorithm{}).Where("deleted_time = 0")
	if filter.Keyword != "" {
		like := "%" + filter.Keyword + "%"
		query = query.Where("(name ILIKE ? OR algorithm_code ILIKE ? OR description ILIKE ?)", like, like, like)
	}
	if filter.Source != "" {
		query = query.Where("source = ?", filter.Source)
	}
	if filter.AlgoType != "" {
		query = query.Where("algo_type = ?", filter.AlgoType)
	}
	if filter.ModelStatus != "" {
		query = query.Where("model_status = ?", filter.ModelStatus)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, normalizeDBError(err)
	}
	items := make([]VisionAlgorithm, 0)
	if filter.Size > 0 {
		query = query.Offset((filter.Page - 1) * filter.Size).Limit(filter.Size)
	}
	// 内置算法优先展示，其余按创建时间倒序。
	err := query.Order("CASE WHEN source = 'builtin' THEN 0 ELSE 1 END").
		Order("created_time DESC").Find(&items).Error
	return items, total, normalizeDBError(err)
}

func (r *VisionAlgorithmRepo) Update(ctx context.Context, id int64, updates map[string]any) error {
	result := r.db.WithContext(ctx).Model(&VisionAlgorithm{}).
		Where("id = ? AND deleted_time = 0", id).Updates(updates)
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *VisionAlgorithmRepo) SoftDelete(ctx context.Context, id, userID int64) error {
	result := r.db.WithContext(ctx).Model(&VisionAlgorithm{}).
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

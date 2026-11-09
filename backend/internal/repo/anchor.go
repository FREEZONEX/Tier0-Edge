package repo

import (
	"context"

	"gorm.io/gorm"
)

type AnchorModel struct {
	ID               int64  `gorm:"column:id;primaryKey" json:"id"`
	Name             string `gorm:"column:name;size:255;not null" json:"name"`
	OriginFile       string `gorm:"column:origin_file;size:512;not null;default:''" json:"originFile"`
	FileAssetID      int64  `gorm:"column:file_asset_id;not null;default:0" json:"fileAssetId"`
	ThumbnailAssetID int64  `gorm:"column:thumbnail_asset_id;not null;default:0" json:"thumbnailAssetId"`
	FileSize         int64  `gorm:"column:file_size;not null;default:0" json:"fileSize"`
	NodesJSON        string `gorm:"column:nodes_json;type:jsonb;default:'[]'" json:"nodesJson"`
	Status           int    `gorm:"column:status;not null;default:2" json:"status"`
	ErrorMsg         string `gorm:"column:error_msg;type:text;default:''" json:"errorMsg"`
	SoftNoDelByTime
}

func (AnchorModel) TableName() string { return "anchor_model" }

type AnchorModelFilter struct {
	Keyword string
	Page    int
	Size    int
}

type AnchorRepo struct{ db *gorm.DB }

func NewAnchorRepo(in any) *AnchorRepo { return &AnchorRepo{db: GetCommonConn(in)} }

func (r *AnchorRepo) CreateModel(ctx context.Context, model AnchorModel) (AnchorModel, error) {
	err := r.db.WithContext(ctx).Create(&model).Error
	return model, normalizeDBError(err)
}

func (r *AnchorRepo) GetModel(ctx context.Context, id int64) (AnchorModel, error) {
	var m AnchorModel
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", id).Take(&m).Error
	return m, normalizeDBError(err)
}

func (r *AnchorRepo) ListModels(ctx context.Context, filter AnchorModelFilter) ([]AnchorModel, int64, error) {
	query := r.db.WithContext(ctx).Model(&AnchorModel{}).Where("deleted_time = 0")
	if filter.Keyword != "" {
		query = query.Where("name ILIKE ?", "%"+filter.Keyword+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, normalizeDBError(err)
	}
	items := make([]AnchorModel, 0)
	if filter.Size > 0 {
		query = query.Offset((filter.Page - 1) * filter.Size).Limit(filter.Size)
	}
	err := query.Order("created_time DESC").Find(&items).Error
	return items, total, normalizeDBError(err)
}

func (r *AnchorRepo) UpdateModel(ctx context.Context, id int64, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	result := r.db.WithContext(ctx).Model(&AnchorModel{}).
		Where("id = ? AND deleted_time = 0", id).
		Updates(updates)
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *AnchorRepo) SoftDeleteModel(ctx context.Context, id int64) error {
	result := r.db.WithContext(ctx).Model(&AnchorModel{}).
		Where("id = ? AND deleted_time = 0", id).
		Update("deleted_time", repoDeleteTimeFromMilli(0))
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

type AnchorInstance struct {
	ID          int64   `gorm:"column:id;primaryKey" json:"id"`
	ModelID     int64   `gorm:"column:model_id;not null;index:idx_anchor_instance_model" json:"modelId"`
	Name        string  `gorm:"column:name;size:255;not null" json:"name"`
	UnsNodeID   string  `gorm:"column:uns_node_id;size:128;not null;default:''" json:"unsNodeId"`
	Topic       string  `gorm:"column:topic;size:512;not null;default:''" json:"topic"`
	BindingJSON string  `gorm:"column:binding_json;type:jsonb;default:'{}'" json:"bindingJson"`
	Height      float64 `gorm:"column:height;not null;default:0" json:"height"`
	Status      int     `gorm:"column:status;not null;default:2" json:"status"`
	ErrorMsg    string  `gorm:"column:error_msg;type:text;default:''" json:"errorMsg"`
	SoftNoDelByTime
}

func (AnchorInstance) TableName() string { return "anchor_instance" }

type AnchorInstanceFilter struct {
	Keyword string
	ModelID int64
	Page    int
	Size    int
}

func (r *AnchorRepo) CreateInstance(ctx context.Context, instance AnchorInstance) (AnchorInstance, error) {
	err := r.db.WithContext(ctx).Create(&instance).Error
	return instance, normalizeDBError(err)
}

func (r *AnchorRepo) GetInstance(ctx context.Context, id int64) (AnchorInstance, error) {
	var m AnchorInstance
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", id).Take(&m).Error
	return m, normalizeDBError(err)
}

func (r *AnchorRepo) ListInstances(ctx context.Context, filter AnchorInstanceFilter) ([]AnchorInstance, int64, error) {
	query := r.db.WithContext(ctx).Model(&AnchorInstance{}).Where("deleted_time = 0")
	if filter.ModelID != 0 {
		query = query.Where("model_id = ?", filter.ModelID)
	}
	if filter.Keyword != "" {
		query = query.Where("name ILIKE ?", "%"+filter.Keyword+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, normalizeDBError(err)
	}
	items := make([]AnchorInstance, 0)
	if filter.Size > 0 {
		query = query.Offset((filter.Page - 1) * filter.Size).Limit(filter.Size)
	}
	err := query.Order("created_time DESC").Find(&items).Error
	return items, total, normalizeDBError(err)
}

func (r *AnchorRepo) CountInstancesByModel(ctx context.Context, modelIDs []int64) (map[int64]int64, error) {
	type row struct {
		ModelID int64 `gorm:"column:model_id"`
		Count   int64 `gorm:"column:count"`
	}
	rows := make([]row, 0)
	err := r.db.WithContext(ctx).Model(&AnchorInstance{}).
		Select("model_id, COUNT(*) AS count").
		Where("model_id IN ? AND deleted_time = 0", modelIDs).
		Group("model_id").
		Find(&rows).Error
	if err != nil {
		return nil, normalizeDBError(err)
	}
	out := make(map[int64]int64, len(rows))
	for _, item := range rows {
		out[item.ModelID] = item.Count
	}
	return out, nil
}

func (r *AnchorRepo) UpdateInstance(ctx context.Context, id int64, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	result := r.db.WithContext(ctx).Model(&AnchorInstance{}).
		Where("id = ? AND deleted_time = 0", id).
		Updates(updates)
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *AnchorRepo) SoftDeleteInstance(ctx context.Context, id int64) error {
	result := r.db.WithContext(ctx).Model(&AnchorInstance{}).
		Where("id = ? AND deleted_time = 0", id).
		Update("deleted_time", repoDeleteTimeFromMilli(0))
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

type AnchorScene struct {
	ID               int64  `gorm:"column:id;primaryKey" json:"id"`
	Name             string `gorm:"column:name;size:255;not null" json:"name"`
	Description      string `gorm:"column:description;type:text;default:''" json:"description"`
	ConfigJSON       string `gorm:"column:config_json;type:jsonb;default:'{}'" json:"configJson"`
	ThumbnailAssetID int64  `gorm:"column:thumbnail_asset_id;not null;default:0" json:"thumbnailAssetId"`
	SoftNoDelByTime
}

func (AnchorScene) TableName() string { return "anchor_scene" }

type AnchorSceneItem struct {
	ID            int64  `gorm:"column:id;primaryKey" json:"id"`
	SceneID       int64  `gorm:"column:scene_id;not null;index:idx_anchor_scene_item_scene" json:"sceneId"`
	InstanceID    int64  `gorm:"column:instance_id;not null;index:idx_anchor_scene_item_instance" json:"instanceId"`
	PlacementJSON string `gorm:"column:placement_json;type:jsonb;default:'{}'" json:"placementJson"`
	Sort          int    `gorm:"column:sort;not null;default:0" json:"sort"`
	SoftOnlyTime
}

func (AnchorSceneItem) TableName() string { return "anchor_scene_item" }

func (r *AnchorRepo) CreateScene(ctx context.Context, scene AnchorScene) (AnchorScene, error) {
	err := r.db.WithContext(ctx).Create(&scene).Error
	return scene, normalizeDBError(err)
}

func (r *AnchorRepo) GetScene(ctx context.Context, id int64) (AnchorScene, error) {
	var m AnchorScene
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", id).Take(&m).Error
	return m, normalizeDBError(err)
}

func (r *AnchorRepo) ListScenes(ctx context.Context, filter AnchorModelFilter) ([]AnchorScene, int64, error) {
	query := r.db.WithContext(ctx).Model(&AnchorScene{}).Where("deleted_time = 0")
	if filter.Keyword != "" {
		query = query.Where("name ILIKE ?", "%"+filter.Keyword+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, normalizeDBError(err)
	}
	items := make([]AnchorScene, 0)
	if filter.Size > 0 {
		query = query.Offset((filter.Page - 1) * filter.Size).Limit(filter.Size)
	}
	err := query.Order("created_time DESC").Find(&items).Error
	return items, total, normalizeDBError(err)
}

func (r *AnchorRepo) UpdateScene(ctx context.Context, id int64, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	result := r.db.WithContext(ctx).Model(&AnchorScene{}).
		Where("id = ? AND deleted_time = 0", id).
		Updates(updates)
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *AnchorRepo) SoftDeleteScene(ctx context.Context, id int64) error {
	result := r.db.WithContext(ctx).Model(&AnchorScene{}).
		Where("id = ? AND deleted_time = 0", id).
		Update("deleted_time", repoDeleteTimeFromMilli(0))
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *AnchorRepo) ListSceneItems(ctx context.Context, sceneIDs []int64) ([]AnchorSceneItem, error) {
	items := make([]AnchorSceneItem, 0)
	if len(sceneIDs) == 0 {
		return items, nil
	}
	err := r.db.WithContext(ctx).
		Where("scene_id IN ? AND deleted_time = 0", sceneIDs).
		Order("sort, id").
		Find(&items).Error
	return items, normalizeDBError(err)
}

// ReplaceSceneItems 全量替换场景条目（编辑器保存语义）
func (r *AnchorRepo) ReplaceSceneItems(ctx context.Context, sceneID int64, items []AnchorSceneItem) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("scene_id = ? AND deleted_time = 0", sceneID).
			Model(&AnchorSceneItem{}).
			Update("deleted_time", repoDeleteTimeFromMilli(0)).Error; err != nil {
			return normalizeDBError(err)
		}
		for index := range items {
			items[index].ID = 0
			items[index].SceneID = sceneID
			if err := tx.Create(&items[index]).Error; err != nil {
				return normalizeDBError(err)
			}
		}
		return nil
	})
}

func (r *AnchorRepo) CountSceneItemsByInstance(ctx context.Context, instanceID int64) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&AnchorSceneItem{}).
		Where("instance_id = ? AND deleted_time = 0", instanceID).
		Count(&total).Error
	return total, normalizeDBError(err)
}

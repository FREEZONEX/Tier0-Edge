package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type AIProviderConfig struct {
	ID               int64  `gorm:"column:id;primaryKey" json:"id"`
	WorkspaceID      int64  `gorm:"column:workspace_id;not null;default:1" json:"workspaceId"`
	Name             string `gorm:"column:name;size:100;not null" json:"name"`
	Provider         string `gorm:"column:provider;size:32;not null" json:"provider"`
	BaseURL          string `gorm:"column:base_url;size:512;not null" json:"baseUrl"`
	Model            string `gorm:"column:model;size:128;not null;default:''" json:"model"`
	APIKeyCiphertext string `gorm:"column:api_key_ciphertext;type:text;not null" json:"-"`
	KeySuffix        string `gorm:"column:key_suffix;size:16;not null;default:''" json:"keySuffix"`

	EmbeddingMode             string    `gorm:"column:embedding_mode;size:16;not null;default:'inherit'" json:"embeddingMode"`
	EmbeddingBaseURL          string    `gorm:"column:embedding_base_url;size:512;not null;default:''" json:"embeddingBaseUrl"`
	EmbeddingAPIKeyCiphertext string    `gorm:"column:embedding_api_key_ciphertext;type:text;not null;default:''" json:"-"`
	EmbeddingKeySuffix        string    `gorm:"column:embedding_key_suffix;size:16;not null;default:''" json:"embeddingKeySuffix"`
	EmbeddingModel            string    `gorm:"column:embedding_model;size:128;not null;default:''" json:"embeddingModel"`
	IsDefault                 bool      `gorm:"column:is_default;not null;default:false" json:"isDefault"`
	Enabled                   bool      `gorm:"column:enabled;not null;default:true" json:"enabled"`
	CreatedBy                 int64     `gorm:"column:created_by" json:"createdBy"`
	UpdatedBy                 int64     `gorm:"column:updated_by" json:"updatedBy"`
	CreatedTime               time.Time `gorm:"column:created_time;autoCreateTime" json:"createdTime"`
	UpdatedTime               time.Time `gorm:"column:updated_time;autoUpdateTime" json:"updatedTime"`
	DeletedTime               int64     `gorm:"column:deleted_time;not null;default:0" json:"-"`
}

func (AIProviderConfig) TableName() string { return "sys_ai_provider_config" }

type AIProviderConfigRepo struct{ db *gorm.DB }

func NewAIProviderConfigRepo(in any) *AIProviderConfigRepo {
	return &AIProviderConfigRepo{db: GetCommonConn(in)}
}

func (r *AIProviderConfigRepo) List(ctx context.Context, workspaceID int64) ([]AIProviderConfig, error) {
	items := make([]AIProviderConfig, 0)
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND deleted_time = 0", workspaceID).
		Order("is_default DESC, id DESC").Find(&items).Error
	return items, normalizeDBError(err)
}

func (r *AIProviderConfigRepo) ListAll(ctx context.Context) ([]AIProviderConfig, error) {
	items := make([]AIProviderConfig, 0)
	err := r.db.WithContext(ctx).
		Where("deleted_time = 0").
		Order("id ASC").Find(&items).Error
	return items, normalizeDBError(err)
}

func (r *AIProviderConfigRepo) Get(ctx context.Context, id int64) (AIProviderConfig, error) {
	var item AIProviderConfig
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", id).Take(&item).Error
	return item, normalizeDBError(err)
}

func (r *AIProviderConfigRepo) GetDefault(ctx context.Context, workspaceID int64) (AIProviderConfig, error) {
	var item AIProviderConfig
	err := r.db.WithContext(ctx).
		Where("workspace_id = ? AND is_default = true AND enabled = true AND deleted_time = 0", workspaceID).
		Take(&item).Error
	return item, normalizeDBError(err)
}

func (r *AIProviderConfigRepo) Create(ctx context.Context, item AIProviderConfig) (AIProviderConfig, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if item.IsDefault {
			if err := clearAIDefault(ctx, tx, item.WorkspaceID, item.UpdatedBy); err != nil {
				return err
			}
		}
		return tx.WithContext(ctx).Create(&item).Error
	})
	return item, normalizeDBError(err)
}

func (r *AIProviderConfigRepo) Update(ctx context.Context, id int64, workspaceID int64, updates map[string]any, makeDefault bool) (AIProviderConfig, error) {
	var item AIProviderConfig
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if makeDefault {
			if err := clearAIDefault(ctx, tx, workspaceID, valueAsInt64(updates["updated_by"])); err != nil {
				return err
			}
		}
		res := tx.WithContext(ctx).Model(&AIProviderConfig{}).Clauses(returningAll()).
			Where("id = ? AND workspace_id = ? AND deleted_time = 0", id, workspaceID).
			Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return tx.WithContext(ctx).Where("id = ?", id).Take(&item).Error
	})
	return item, normalizeDBError(err)
}

func (r *AIProviderConfigRepo) Delete(ctx context.Context, id, workspaceID, userID int64) error {
	now := time.Now().UTC()
	res := r.db.WithContext(ctx).Model(&AIProviderConfig{}).
		Where("id = ? AND workspace_id = ? AND deleted_time = 0", id, workspaceID).
		Updates(map[string]any{
			"enabled":      false,
			"is_default":   false,
			"updated_by":   userID,
			"updated_time": now,
			"deleted_time": now.UnixMilli(),
		})
	if res.Error != nil {
		return normalizeDBError(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func clearAIDefault(ctx context.Context, tx *gorm.DB, workspaceID, userID int64) error {
	return tx.WithContext(ctx).Model(&AIProviderConfig{}).
		Where("workspace_id = ? AND is_default = true AND deleted_time = 0", workspaceID).
		Updates(map[string]any{"is_default": false, "updated_by": userID, "updated_time": time.Now().UTC()}).Error
}

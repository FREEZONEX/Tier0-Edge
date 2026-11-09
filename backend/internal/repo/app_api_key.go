package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// AppApiKey 应用级 API Key
type AppApiKey struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	AppID       string    `gorm:"column:app_id;size:36;not null;uniqueIndex:idx_app_api_keys_app_id" json:"appId"`
	Name        string    `gorm:"column:name;size:64;not null;default:'default'" json:"name"`
	ApiKey      string    `gorm:"column:api_key;size:255;not null" json:"apiKey"`
	ProjectID   int64     `gorm:"column:project_id;not null;index:idx_app_api_keys_project_id" json:"projectId"`
	CreatedTime time.Time `gorm:"column:created_time;autoCreateTime" json:"createdTime"`
	UpdatedTime time.Time `gorm:"column:updated_time;autoUpdateTime" json:"updatedTime"`
}

func (AppApiKey) TableName() string { return "app_api_keys" }

type AppApiKeyRepo struct{ db *gorm.DB }

func NewAppApiKeyRepo(in any) *AppApiKeyRepo {
	return &AppApiKeyRepo{db: GetCommonConn(in)}
}

type AppApiKeyFilter struct {
	AppID  string
	Name   string
	APIKey string
}

func (r *AppApiKeyRepo) fmtFilter(ctx context.Context, f AppApiKeyFilter) *gorm.DB {
	db := r.db.WithContext(ctx).Model(&AppApiKey{})
	if f.AppID != "" {
		db = db.Where("app_id = ?", f.AppID)
	}
	if f.Name != "" {
		db = db.Where("name = ?", f.Name)
	}
	if f.APIKey != "" {
		db = db.Where("api_key = ?", f.APIKey)
	}
	return db
}

func (r *AppApiKeyRepo) Insert(ctx context.Context, data *AppApiKey) error {
	return normalizeDBError(r.db.WithContext(ctx).Create(data).Error)
}

func (r *AppApiKeyRepo) UpdateAPIKey(ctx context.Context, id int64, apiKey string) error {
	res := r.db.WithContext(ctx).Model(&AppApiKey{}).
		Where("id = ?", id).
		Update("api_key", apiKey)
	if res.Error != nil {
		return normalizeDBError(res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *AppApiKeyRepo) FindOneByFilter(ctx context.Context, f AppApiKeyFilter) (*AppApiKey, error) {
	var result AppApiKey
	err := r.fmtFilter(ctx, f).First(&result).Error
	if err != nil {
		return nil, normalizeDBError(err)
	}
	return &result, nil
}

func (r *AppApiKeyRepo) FindByAppID(ctx context.Context, appID string) (*AppApiKey, error) {
	return r.FindOneByFilter(ctx, AppApiKeyFilter{AppID: appID, Name: "default"})
}

func (r *AppApiKeyRepo) FindByAPIKey(ctx context.Context, apiKey string) (*AppApiKey, error) {
	return r.FindOneByFilter(ctx, AppApiKeyFilter{APIKey: apiKey})
}

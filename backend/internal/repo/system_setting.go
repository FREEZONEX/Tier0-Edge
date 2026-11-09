package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SystemSetting struct {
	SettingKey   string    `gorm:"column:setting_key;primaryKey" json:"key"`
	SettingValue string    `gorm:"column:setting_value" json:"value"`
	UpdatedTime  time.Time `gorm:"column:updated_time" json:"updatedTime"`
}

func (SystemSetting) TableName() string { return "sys_setting" }

type SystemSettingRepo struct{ db *gorm.DB }

func NewSystemSettingRepo(in any) *SystemSettingRepo {
	return &SystemSettingRepo{db: GetCommonConn(in)}
}

// GetSetting returns ErrNotFound when the key has never been written.
func (r *SystemSettingRepo) GetSetting(ctx context.Context, key string) (string, error) {
	var item SystemSetting
	err := r.db.WithContext(ctx).Where("setting_key = ?", key).Take(&item).Error
	if err == nil {
		return item.SettingValue, nil
	}
	if err == gorm.ErrRecordNotFound || err == ErrNotFound {
		return "", ErrNotFound
	}
	return "", err
}

func (r *SystemSettingRepo) PutSetting(ctx context.Context, key, value string) error {
	now := time.Now().UTC()
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "setting_key"}},
		DoUpdates: clause.Assignments(map[string]any{"setting_value": value, "updated_time": now}),
	}).Create(&SystemSetting{SettingKey: key, SettingValue: value, UpdatedTime: now}).Error
}

package repo

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// Import receipts use the existing internal task kind, like other durable UNS
// receipts. They commit and roll back with the definitions, never execute jobs.
const appPackageResourceType = "appPackageResource"

type AppPackageResource struct {
	Type          string `json:"type"`
	ResourceID    int64  `json:"resourceId,string"`
	PackageDigest string `json:"packageDigest"`
	AppliedDigest string `json:"appliedDigest"`
}

type AppPackageResourceRepo struct{ db *gorm.DB }

func NewAppPackageResourceRepo(in any) *AppPackageResourceRepo {
	return &AppPackageResourceRepo{db: GetUnsConn(in)}
}

func (r *AppPackageResourceRepo) Get(ctx context.Context, id string) (AppPackageResource, error) {
	var row UnsMeta
	var value AppPackageResource
	if err := r.db.WithContext(ctx).Where("kind='task' AND id=?", "app-resource-"+id).Take(&row).Error; err != nil {
		return value, err
	}
	err := json.Unmarshal(row.Data, &value)
	if err == nil && (value.Type != appPackageResourceType || value.ResourceID < 1) {
		err = ErrInvalidArgument
	}
	return value, err
}

func (r *AppPackageResourceRepo) Create(ctx context.Context, id string, value AppPackageResource) error {
	value.Type = appPackageResourceType
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	return r.db.WithContext(ctx).Create(&UnsMeta{Kind: "task", ID: "app-resource-" + id, Data: raw, Revision: 1, CreatedAt: now, UpdatedAt: now}).Error
}

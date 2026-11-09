package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// VisionWorker 是执行面 worker 的持久化注册。worker 自注册并周期心跳,
// master 侧健康检查循环刷新 status(online/offline)。
type VisionWorker struct {
	ID           int64      `gorm:"column:id;primaryKey" json:"id"`
	WorkerID     string     `gorm:"column:worker_id;size:128;not null" json:"workerId"`
	URL          string     `gorm:"column:url;size:512;not null;default:''" json:"url"`
	Capabilities string     `gorm:"column:capabilities;type:text;not null;default:'{}'" json:"capabilities"`
	Status       string     `gorm:"column:status;size:16;not null;default:'offline'" json:"status"`
	LastSeenAt   *time.Time `gorm:"column:last_seen_at" json:"lastSeenAt"`
	CreatedTime  time.Time  `gorm:"column:created_time" json:"createdTime"`
	UpdatedTime  time.Time  `gorm:"column:updated_time" json:"updatedTime"`
}

func (VisionWorker) TableName() string { return "sys_vision_worker" }

type VisionWorkerRepo struct{ db *gorm.DB }

func NewVisionWorkerRepo(in any) *VisionWorkerRepo { return &VisionWorkerRepo{db: GetCommonConn(in)} }

// Register upsert worker,并把它标记 online、刷新 last_seen_at。
func (r *VisionWorkerRepo) Register(ctx context.Context, workerID, url, capabilities string) error {
	now := repoNowTime()
	item := VisionWorker{
		WorkerID:     workerID,
		URL:          url,
		Capabilities: defaultJSON(capabilities),
		Status:       "online",
		LastSeenAt:   &now,
		CreatedTime:  now,
		UpdatedTime:  now,
	}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "worker_id"}},
		DoUpdates: clause.Assignments(map[string]any{"url": url, "capabilities": item.Capabilities, "status": "online", "last_seen_at": now, "updated_time": now}),
	}).Create(&item).Error
	return normalizeDBError(err)
}

// TouchLastSeen 心跳/健康检查通过时刷新在线时间。
func (r *VisionWorkerRepo) TouchLastSeen(ctx context.Context, workerID string) error {
	now := repoNowTime()
	return normalizeDBError(r.db.WithContext(ctx).Model(&VisionWorker{}).
		Where("worker_id = ?", workerID).
		Updates(map[string]any{"status": "online", "last_seen_at": now, "updated_time": now}).Error)
}

func (r *VisionWorkerRepo) SetStatus(ctx context.Context, workerID, status string) error {
	return normalizeDBError(r.db.WithContext(ctx).Model(&VisionWorker{}).
		Where("worker_id = ?", workerID).
		Updates(map[string]any{"status": status, "updated_time": repoNowTime()}).Error)
}

func (r *VisionWorkerRepo) Get(ctx context.Context, workerID string) (VisionWorker, error) {
	var item VisionWorker
	err := r.db.WithContext(ctx).Where("worker_id = ?", workerID).Take(&item).Error
	return item, normalizeDBError(err)
}

func (r *VisionWorkerRepo) List(ctx context.Context) ([]VisionWorker, error) {
	items := make([]VisionWorker, 0)
	err := r.db.WithContext(ctx).Order("created_time ASC").Find(&items).Error
	return items, normalizeDBError(err)
}

func defaultJSON(s string) string {
	if s == "" {
		return "{}"
	}
	return s
}

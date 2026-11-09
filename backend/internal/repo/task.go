package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type VisionTask struct {
	ID               int64      `gorm:"column:id;primaryKey" json:"id"`
	Name             string     `gorm:"column:name;size:128;not null" json:"name"`
	Note             string     `gorm:"column:note;type:text;not null;default:''" json:"note"`
	CameraIDs        string     `gorm:"column:camera_ids;type:text;not null;default:'[]'" json:"cameraIds"`
	AlgorithmID      int64      `gorm:"column:algorithm_id;not null;default:0" json:"algorithmId"`
	OutputCategories string     `gorm:"column:output_categories;type:text;not null;default:'[]'" json:"outputCategories"`
	Config           string     `gorm:"column:config;type:text;not null;default:'{}'" json:"config"`
	Status           string     `gorm:"column:status;size:16;not null;default:'stopped'" json:"status"`
	DesiredStatus    string     `gorm:"column:desired_status;size:16;not null;default:'stopped'" json:"desiredStatus"`
	ObservedStatus   string     `gorm:"column:observed_status;size:16;not null;default:'stopped'" json:"observedStatus"`
	ObservedReason   string     `gorm:"column:observed_reason;type:text;not null;default:''" json:"observedReason"`
	LastHeartbeatAt  *time.Time `gorm:"column:last_heartbeat_at" json:"lastHeartbeatAt"`
	ProcessedFrames  int64      `gorm:"column:processed_frames;not null;default:0" json:"processedFrames"`
	DetectedObjects  int64      `gorm:"column:detected_objects;not null;default:0" json:"detectedObjects"`
	LastError        string     `gorm:"column:last_error;type:text;not null;default:''" json:"lastError"`
	UnsTopic         string     `gorm:"column:uns_topic;size:512;not null;default:''" json:"unsTopic"`
	LastRunAt        *time.Time `gorm:"column:last_run_at" json:"lastRunAt"`
	SoftNoDelByTime
}

func (VisionTask) TableName() string { return "sys_vision_task" }

type VisionTaskFilter struct {
	Page    int
	Size    int
	Keyword string
	Status  string
}

type VisionTaskRepo struct{ db *gorm.DB }

func NewVisionTaskRepo(in any) *VisionTaskRepo { return &VisionTaskRepo{db: GetCommonConn(in)} }

func (r *VisionTaskRepo) Create(ctx context.Context, item VisionTask) (VisionTask, error) {
	err := r.db.WithContext(ctx).Create(&item).Error
	return item, normalizeDBError(err)
}

func (r *VisionTaskRepo) Get(ctx context.Context, id int64) (VisionTask, error) {
	var item VisionTask
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", id).Take(&item).Error
	return item, normalizeDBError(err)
}

func (r *VisionTaskRepo) List(ctx context.Context, filter VisionTaskFilter) ([]VisionTask, int64, error) {
	query := r.db.WithContext(ctx).Model(&VisionTask{}).Where("deleted_time = 0")
	if filter.Keyword != "" {
		like := "%" + filter.Keyword + "%"
		query = query.Where("(name ILIKE ? OR note ILIKE ?)", like, like)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, normalizeDBError(err)
	}
	items := make([]VisionTask, 0)
	if filter.Size > 0 {
		query = query.Offset((filter.Page - 1) * filter.Size).Limit(filter.Size)
	}
	err := query.Order("created_time DESC").Find(&items).Error
	return items, total, normalizeDBError(err)
}

func (r *VisionTaskRepo) CountByAlgorithm(ctx context.Context, algorithmID int64) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&VisionTask{}).
		Where("algorithm_id = ? AND deleted_time = 0", algorithmID).Count(&total).Error
	return total, normalizeDBError(err)
}

func (r *VisionTaskRepo) Update(ctx context.Context, id int64, updates map[string]any) error {
	result := r.db.WithContext(ctx).Model(&VisionTask{}).
		Where("id = ? AND deleted_time = 0", id).Updates(updates)
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *VisionTaskRepo) SoftDelete(ctx context.Context, id, userID int64) error {
	result := r.db.WithContext(ctx).Model(&VisionTask{}).
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

func (r *VisionTaskRepo) UpdateConfig(ctx context.Context, previous VisionTask, updates map[string]any) error {
	result := r.db.WithContext(ctx).Model(&VisionTask{}).
		Where("id = ? AND deleted_time = 0", previous.ID).
		Where("name = ? AND note = ? AND camera_ids = ? AND algorithm_id = ? AND output_categories = ? AND config = ?", previous.Name, previous.Note, previous.CameraIDs, previous.AlgorithmID, previous.OutputCategories, previous.Config).
		Where("status = ? AND desired_status = ? AND observed_status = ?", previous.Status, previous.DesiredStatus, previous.ObservedStatus).
		Updates(updates)
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrConflict
	}
	return nil
}

func (r *VisionTaskRepo) SoftDeleteChecked(ctx context.Context, previous VisionTask, userID int64) error {
	return r.UpdateConfig(ctx, previous, softDeleteNoDelByValues(userID, repoNowMilli()))
}

// SetDesiredStatus 只由 API 路径(StartTask/StopTask)写,表达用户意图。
func (r *VisionTaskRepo) SetDesiredStatus(ctx context.Context, id int64, desired string) error {
	return r.Update(ctx, id, map[string]any{
		"desired_status": desired,
		"updated_time":   repoNowTime(),
	})
}

func (r *VisionTaskRepo) SetDesiredStatusChecked(ctx context.Context, previous VisionTask, desired string) error {
	return r.UpdateConfig(ctx, previous, map[string]any{"desired_status": desired, "updated_time": repoNowTime()})
}

// SetObservedState 由 reconcile 循环和错误路径写;同步 legacy status 供前端展示。
func (r *VisionTaskRepo) SetObservedState(ctx context.Context, id int64, observed, reason string) error {
	return r.Update(ctx, id, map[string]any{
		"observed_status": observed,
		"observed_reason": reason,
		"status":          observed,
		"updated_time":    repoNowTime(),
	})
}

// UpdateStats 由 worker heartbeat/ingest 调用:累加统计,并把 observed 强制翻到 running
// —— worker 实际产出数据才代表任务真正运行(starting→running 的翻转点)。
func (r *VisionTaskRepo) UpdateStats(ctx context.Context, id, framesDelta, detectionsDelta int64) error {
	now := repoNowTime()
	result := r.db.WithContext(ctx).Model(&VisionTask{}).
		Where("id = ? AND deleted_time = 0", id).
		Updates(map[string]any{
			"processed_frames":  gorm.Expr("processed_frames + ?", framesDelta),
			"detected_objects":  gorm.Expr("detected_objects + ?", detectionsDelta),
			"last_heartbeat_at": now,
			"observed_status":   "running",
			"observed_reason":   "",
			"status":            "running",
			"updated_time":      now,
		})
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ListByDesiredStatus 供 master 启动 replay 使用(重派所有 desired=running 的任务)。
func (r *VisionTaskRepo) ListByDesiredStatus(ctx context.Context, desired string) ([]VisionTask, error) {
	items := make([]VisionTask, 0)
	err := r.db.WithContext(ctx).Model(&VisionTask{}).
		Where("desired_status = ? AND deleted_time = 0", desired).
		Find(&items).Error
	return items, normalizeDBError(err)
}

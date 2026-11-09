package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// VisionJobAssignment 是 task -> worker 的持久化映射,存活于 master 重启之后,
// 让 reconcile 知道某个任务应该在哪个 worker 上跑。一个 task 同时只有一条
// assignment(task_id 唯一)。generation 单调递增,用于区分被替换后的旧任务重启。
type VisionJobAssignment struct {
	TaskID       int64      `gorm:"column:task_id;primaryKey" json:"taskId"`
	WorkerID     string     `gorm:"column:worker_id;size:128;not null;default:''" json:"workerId"`
	WorkerTaskID string     `gorm:"column:worker_task_id;size:128;not null;default:''" json:"workerTaskId"`
	Generation   int        `gorm:"column:generation;not null;default:1" json:"generation"`
	State        string     `gorm:"column:state;size:16;not null;default:'assigned'" json:"state"`
	LastError    string     `gorm:"column:last_error;type:text;not null;default:''" json:"lastError"`
	AssignedAt   *time.Time `gorm:"column:assigned_at" json:"assignedAt"`
	VerifiedAt   *time.Time `gorm:"column:verified_at" json:"verifiedAt"`
	CreatedTime  time.Time  `gorm:"column:created_time" json:"createdTime"`
	UpdatedTime  time.Time  `gorm:"column:updated_time" json:"updatedTime"`
}

func (VisionJobAssignment) TableName() string { return "sys_vision_job_assignment" }

type VisionAssignmentRepo struct{ db *gorm.DB }

func NewVisionAssignmentRepo(in any) *VisionAssignmentRepo {
	return &VisionAssignmentRepo{db: GetCommonConn(in)}
}

// NextGeneration 返回该 task 的下一个 generation(当前 +1,无记录则 1)。
func (r *VisionAssignmentRepo) NextGeneration(ctx context.Context, taskID int64) (int, error) {
	var current VisionJobAssignment
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Take(&current).Error
	if err == gorm.ErrRecordNotFound {
		return 1, nil
	}
	if err != nil {
		return 0, normalizeDBError(err)
	}
	return current.Generation + 1, nil
}

// Upsert 写入/覆盖 assignment(task_id 冲突则更新)。
func (r *VisionAssignmentRepo) Upsert(ctx context.Context, item VisionJobAssignment) error {
	now := repoNowTime()
	item.CreatedTime = now
	item.UpdatedTime = now
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "task_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"worker_id":      item.WorkerID,
			"worker_task_id": item.WorkerTaskID,
			"generation":     item.Generation,
			"state":          item.State,
			"last_error":     item.LastError,
			"assigned_at":    item.AssignedAt,
			"verified_at":    item.VerifiedAt,
			"updated_time":   now,
		}),
	}).Create(&item).Error
	return normalizeDBError(err)
}

func (r *VisionAssignmentRepo) Get(ctx context.Context, taskID int64) (VisionJobAssignment, error) {
	var item VisionJobAssignment
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Take(&item).Error
	return item, normalizeDBError(err)
}

func (r *VisionAssignmentRepo) ListByWorker(ctx context.Context, workerID string) ([]VisionJobAssignment, error) {
	items := make([]VisionJobAssignment, 0)
	err := r.db.WithContext(ctx).Where("worker_id = ?", workerID).Find(&items).Error
	return items, normalizeDBError(err)
}

func (r *VisionAssignmentRepo) List(ctx context.Context) ([]VisionJobAssignment, error) {
	items := make([]VisionJobAssignment, 0)
	err := r.db.WithContext(ctx).Find(&items).Error
	return items, normalizeDBError(err)
}

// SetState 更新 assignment 状态与错误信息(reconcile 用)。
func (r *VisionAssignmentRepo) SetState(ctx context.Context, taskID int64, state, lastError string) error {
	return normalizeDBError(r.db.WithContext(ctx).Model(&VisionJobAssignment{}).
		Where("task_id = ?", taskID).
		Updates(map[string]any{"state": state, "last_error": lastError, "updated_time": repoNowTime()}).Error)
}

// SetVerified 标记 assignment 已被 worker 确认活着(verified_at + state=running)。
func (r *VisionAssignmentRepo) SetVerified(ctx context.Context, taskID int64) error {
	now := repoNowTime()
	return normalizeDBError(r.db.WithContext(ctx).Model(&VisionJobAssignment{}).
		Where("task_id = ?", taskID).
		Updates(map[string]any{"state": "running", "verified_at": now, "updated_time": now}).Error)
}

func (r *VisionAssignmentRepo) Delete(ctx context.Context, taskID int64) error {
	return normalizeDBError(r.db.WithContext(ctx).Where("task_id = ?", taskID).
		Delete(&VisionJobAssignment{}).Error)
}

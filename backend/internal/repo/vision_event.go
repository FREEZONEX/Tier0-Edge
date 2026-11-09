package repo

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"
)

// VisionEvent 是视觉分析事件流条目(alarm 告警事件 / data 数据事件),
// 带证据截图 asset 引用与保留期,deleted_time 毫秒时间戳软删。
type VisionEvent struct {
	ID                int64      `gorm:"column:id;primaryKey" json:"id"`
	TaskID            int64      `gorm:"column:task_id;not null;default:0" json:"taskId"`
	CameraID          int64      `gorm:"column:camera_id;not null;default:0" json:"cameraId"`
	AlgorithmCode     string     `gorm:"column:algorithm_code;size:128;not null;default:''" json:"algorithmCode"`
	EventType         string     `gorm:"column:event_type;size:16;not null;default:'alarm'" json:"eventType"`
	EventName         string     `gorm:"column:event_name;size:128;not null;default:''" json:"eventName"`
	Level             string     `gorm:"column:level;size:16;not null;default:'warning'" json:"level"`
	Message           string     `gorm:"column:message;type:text;not null;default:''" json:"message"`
	ResultContent     string     `gorm:"column:result_content;type:text;not null;default:'[]'" json:"resultContent"`
	Payload           string     `gorm:"column:payload;type:text;not null;default:'{}'" json:"payload"`
	ScreenshotAssetID int64      `gorm:"column:screenshot_asset_id;not null;default:0" json:"screenshotAssetId"`
	EventID           string     `gorm:"column:event_id;size:256;not null;default:''" json:"eventId"`
	UnsTopic          string     `gorm:"column:uns_topic;size:512;not null;default:''" json:"unsTopic"`
	Retention         string     `gorm:"column:retention;size:16;not null;default:'permanent'" json:"retention"`
	CreatedTime       time.Time  `gorm:"column:created_time" json:"createdTime"`
	DeletedTime       int64      `gorm:"column:deleted_time;not null;default:0" json:"deletedTime"`
	ReviewStatus      string     `gorm:"column:review_status;not null;default:'pending'" json:"reviewStatus"`
	ReviewRevision    int64      `gorm:"column:review_revision;not null;default:0" json:"reviewRevision"`
	ReviewedBy        int64      `gorm:"column:reviewed_by;not null;default:0" json:"reviewedBy"`
	ReviewedTime      *time.Time `gorm:"column:reviewed_time" json:"reviewedTime"`
}

func (VisionEvent) TableName() string { return "sys_vision_event" }

type VisionEventFilter struct {
	Page         int
	Size         int
	Search       string // event_name / message ILIKE 模糊匹配
	CameraID     int64
	EventName    string
	TaskID       int64
	Sort         string // latest(默认,created_time DESC) / oldest
	StartTime    int64
	EndTime      int64
	EventType    string
	ReviewStatus string
}

const (
	VisionReviewPending       = "pending"
	VisionReviewConfirmed     = "confirmed"
	VisionReviewFalsePositive = "false_positive"
)

// retentionDays 是非 permanent 保留期取值 → 天数(与前端选项对齐)。
var retentionDays = map[string]int{"30": 30, "60": 60, "180": 180, "365": 365}

type VisionEventRepo struct{ db *gorm.DB }

func NewVisionEventRepo(in any) *VisionEventRepo { return &VisionEventRepo{db: GetCommonConn(in)} }

func (r *VisionEventRepo) Insert(ctx context.Context, item VisionEvent) error {
	item.CreatedTime = repoNowTime()
	return normalizeDBError(r.db.WithContext(ctx).Create(&item).Error)
}

func (r *VisionEventRepo) alive(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Model(&VisionEvent{}).Where("deleted_time = 0")
}

// List 分页返回事件(过滤软删),search 对 event_name/message 做 ILIKE。
func (r *VisionEventRepo) List(ctx context.Context, filter VisionEventFilter) ([]VisionEvent, int64, error) {
	query := r.alive(ctx)
	if filter.TaskID > 0 {
		query = query.Where("task_id = ?", filter.TaskID)
	}
	if filter.CameraID > 0 {
		query = query.Where("camera_id = ?", filter.CameraID)
	}
	if filter.StartTime > 0 {
		query = query.Where("created_time >= ?", time.UnixMilli(filter.StartTime))
	}
	if filter.EndTime > 0 {
		query = query.Where("created_time < ?", time.UnixMilli(filter.EndTime))
	}
	if filter.EventType != "" {
		query = query.Where("event_type = ?", filter.EventType)
	}
	if filter.ReviewStatus != "" {
		query = query.Where("event_type = 'alarm' AND review_status = ?", filter.ReviewStatus)
	}
	if name := strings.TrimSpace(filter.EventName); name != "" {
		query = query.Where("event_name = ?", name)
	}
	if search := strings.TrimSpace(filter.Search); search != "" {
		like := "%" + search + "%"
		query = query.Where("(event_name ILIKE ? OR message ILIKE ?)", like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, normalizeDBError(err)
	}
	order := "created_time DESC, id DESC"
	if filter.Sort == "oldest" {
		order = "created_time ASC, id ASC"
	}
	if filter.Size > 0 {
		query = query.Offset((filter.Page - 1) * filter.Size).Limit(filter.Size)
	}
	items := make([]VisionEvent, 0)
	err := query.Order(order).Find(&items).Error
	return items, total, normalizeDBError(err)
}

func (r *VisionEventRepo) Review(ctx context.Context, id int64, status string, revision, userID int64) error {
	result := r.alive(ctx).Where("id = ? AND event_type = 'alarm' AND review_revision = ?", id, revision).
		Updates(map[string]any{"review_status": status, "reviewed_by": userID, "reviewed_time": repoNowTime(), "review_revision": gorm.Expr("review_revision + 1")})
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		if _, err := r.Get(ctx, id); err != nil {
			return err
		}
		return ErrConflict
	}
	return nil
}

func (r *VisionEventRepo) Get(ctx context.Context, id int64) (VisionEvent, error) {
	var item VisionEvent
	err := r.db.WithContext(ctx).Where("id = ? AND deleted_time = 0", id).Take(&item).Error
	return item, normalizeDBError(err)
}

func (r *VisionEventRepo) SoftDelete(ctx context.Context, id int64) error {
	result := r.db.WithContext(ctx).Model(&VisionEvent{}).
		Where("id = ? AND deleted_time = 0", id).
		Update("deleted_time", repoNowMilli())
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// BatchSoftDelete 幂等批量软删,已删/不存在的 id 直接跳过。
func (r *VisionEventRepo) BatchSoftDelete(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return normalizeDBError(r.db.WithContext(ctx).Model(&VisionEvent{}).
		Where("id IN ? AND deleted_time = 0", ids).
		Update("deleted_time", repoNowMilli()).Error)
}

// DistinctEventNames 返回当前有效事件的事件名去重列表(供列表页筛选)。
func (r *VisionEventRepo) DistinctEventNames(ctx context.Context) ([]string, error) {
	names := make([]string, 0)
	err := r.alive(ctx).Distinct("event_name").
		Where("event_name <> ''").
		Order("event_name ASC").
		Pluck("event_name", &names).Error
	return names, normalizeDBError(err)
}

// DeleteExpired 物理清理超过保留期的事件(retention 非 permanent;含已软删行)。
// 返回删除的总行数。
func (r *VisionEventRepo) DeleteExpired(ctx context.Context) (int64, error) {
	now := repoNowTime()
	var total int64
	for retention, days := range retentionDays {
		cutoff := now.AddDate(0, 0, -days)
		result := r.db.WithContext(ctx).
			Where("retention = ? AND created_time < ?", retention, cutoff).
			Delete(&VisionEvent{})
		if result.Error != nil {
			return total, normalizeDBError(result.Error)
		}
		total += result.RowsAffected
	}
	return total, nil
}

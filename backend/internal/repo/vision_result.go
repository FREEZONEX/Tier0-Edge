package repo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// VisionLatestResult 是每个任务-相机的最新一帧识别结果(供 Results 页展示与实时刷新)。
// task_id + camera_id 复合主键,ingest 时 Upsert 覆盖。
type VisionLatestResult struct {
	TaskID      int64     `gorm:"column:task_id;primaryKey" json:"taskId"`
	CameraID    int64     `gorm:"column:camera_id;primaryKey" json:"cameraId"`
	EventID     string    `gorm:"column:event_id;size:256;not null;default:''" json:"eventId"`
	Payload     string    `gorm:"column:payload;type:text;not null;default:'{}'" json:"payload"`
	ObjectCount int       `gorm:"column:object_count;not null;default:0" json:"objectCount"`
	UpdatedTime time.Time `gorm:"column:updated_time" json:"updatedTime"`
}

func (VisionLatestResult) TableName() string { return "sys_vision_latest_result" }

type VisionLatestResultRepo struct{ db *gorm.DB }

func NewVisionLatestResultRepo(in any) *VisionLatestResultRepo {
	return &VisionLatestResultRepo{db: GetCommonConn(in)}
}

// Upsert 覆盖写入该任务-相机的最新结果。
func (r *VisionLatestResultRepo) Upsert(ctx context.Context, taskID, cameraID int64, eventID, payload string, objectCount int) error {
	now := repoNowTime()
	item := VisionLatestResult{
		TaskID:      taskID,
		CameraID:    cameraID,
		EventID:     eventID,
		Payload:     payload,
		ObjectCount: objectCount,
		UpdatedTime: now,
	}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "task_id"}, {Name: "camera_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"event_id":     eventID,
			"payload":      payload,
			"object_count": objectCount,
			"updated_time": now,
		}),
	}).Create(&item).Error
	return normalizeDBError(err)
}

// ListByTask 返回该任务下各相机的最新结果。
func (r *VisionLatestResultRepo) ListByTask(ctx context.Context, taskID int64) ([]VisionLatestResult, error) {
	items := make([]VisionLatestResult, 0)
	err := r.db.WithContext(ctx).Where("task_id = ?", taskID).
		Order("camera_id ASC").Find(&items).Error
	return items, normalizeDBError(err)
}

// SumObjectCounts 按任务聚合各相机最新结果的目标数(Results 列表"当前目标数")。
// 返回 map[taskID]目标数;无最新结果的任务不在 map 中(视为 0)。
func (r *VisionLatestResultRepo) SumObjectCounts(ctx context.Context, taskIDs []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(taskIDs))
	if len(taskIDs) == 0 {
		return out, nil
	}
	rows := make([]struct {
		TaskID int64 `gorm:"column:task_id"`
		Total  int   `gorm:"column:total"`
	}, 0, len(taskIDs))
	err := r.db.WithContext(ctx).Model(&VisionLatestResult{}).
		Select("task_id, COALESCE(SUM(object_count), 0) AS total").
		Where("task_id IN ?", taskIDs).
		Group("task_id").
		Scan(&rows).Error
	if err != nil {
		return nil, normalizeDBError(err)
	}
	for _, row := range rows {
		out[row.TaskID] = row.Total
	}
	return out, nil
}

// VisionAlarm 是视觉分析告警(alarm.triggered 时落库),供告警列表与 Results 页展示。
type VisionAlarm struct {
	ID            int64     `gorm:"column:id;primaryKey" json:"id"`
	TaskID        int64     `gorm:"column:task_id;not null;default:0" json:"taskId"`
	CameraID      int64     `gorm:"column:camera_id;not null;default:0" json:"cameraId"`
	AlgorithmCode string    `gorm:"column:algorithm_code;size:128;not null;default:''" json:"algorithmCode"`
	AlarmType     string    `gorm:"column:alarm_type;size:64;not null;default:''" json:"alarmType"`
	Message       string    `gorm:"column:message;type:text;not null;default:''" json:"message"`
	Payload       string    `gorm:"column:payload;type:text;not null;default:'{}'" json:"payload"`
	EventID       string    `gorm:"column:event_id;size:256;not null;default:''" json:"eventId"`
	CreatedTime   time.Time `gorm:"column:created_time" json:"createdTime"`
}

func (VisionAlarm) TableName() string { return "sys_vision_alarm" }

type VisionAlarmFilter struct {
	Page   int
	Size   int
	TaskID int64
}

type VisionAlarmRepo struct{ db *gorm.DB }

func NewVisionAlarmRepo(in any) *VisionAlarmRepo { return &VisionAlarmRepo{db: GetCommonConn(in)} }

func (r *VisionAlarmRepo) Insert(ctx context.Context, item VisionAlarm) error {
	item.CreatedTime = repoNowTime()
	return normalizeDBError(r.db.WithContext(ctx).Create(&item).Error)
}

// List 分页返回告警(按创建时间倒序),可按 task 过滤。
func (r *VisionAlarmRepo) List(ctx context.Context, filter VisionAlarmFilter) ([]VisionAlarm, int64, error) {
	query := r.db.WithContext(ctx).Model(&VisionAlarm{})
	if filter.TaskID > 0 {
		query = query.Where("task_id = ?", filter.TaskID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, normalizeDBError(err)
	}
	items := make([]VisionAlarm, 0)
	if filter.Size > 0 {
		query = query.Offset((filter.Page - 1) * filter.Size).Limit(filter.Size)
	}
	err := query.Order("created_time DESC").Find(&items).Error
	return items, total, normalizeDBError(err)
}

// VisionPublishOutbox 是 MQTT/UNS 发布事务性 outbox:ingest 时先写 pending,
// 后台 flush 循环发布后标记 sent。
type VisionPublishOutbox struct {
	ID          int64     `gorm:"column:id;primaryKey" json:"id"`
	Topic       string    `gorm:"column:topic;size:512;not null" json:"topic"`
	Payload     string    `gorm:"column:payload;type:text;not null;default:'{}'" json:"payload"`
	Status      string    `gorm:"column:status;size:16;not null;default:'pending'" json:"status"`
	Attempts    int       `gorm:"column:attempts;not null;default:0" json:"attempts"`
	LastError   string    `gorm:"column:last_error;type:text;not null;default:''" json:"lastError"`
	EventID     string    `gorm:"column:event_id;size:256;not null;default:''" json:"eventId"`
	CreatedTime time.Time `gorm:"column:created_time" json:"createdTime"`
	UpdatedTime time.Time `gorm:"column:updated_time" json:"updatedTime"`
}

func (VisionPublishOutbox) TableName() string { return "sys_vision_publish_outbox" }

type VisionOutboxRepo struct{ db *gorm.DB }

func NewVisionOutboxRepo(in any) *VisionOutboxRepo { return &VisionOutboxRepo{db: GetCommonConn(in)} }

func (r *VisionOutboxRepo) Enqueue(ctx context.Context, topic, payload, eventID string) error {
	now := repoNowTime()
	item := VisionPublishOutbox{
		Topic:       topic,
		Payload:     payload,
		Status:      "pending",
		EventID:     eventID,
		CreatedTime: now,
		UpdatedTime: now,
	}
	return normalizeDBError(r.db.WithContext(ctx).Create(&item).Error)
}

// ListPending 按创建顺序取待发布条目。
func (r *VisionOutboxRepo) ListPending(ctx context.Context, limit int) ([]VisionPublishOutbox, error) {
	if limit <= 0 {
		limit = 50
	}
	items := make([]VisionPublishOutbox, 0)
	err := r.db.WithContext(ctx).Where("status = ?", "pending").
		Order("id ASC").Limit(limit).Find(&items).Error
	return items, normalizeDBError(err)
}

func (r *VisionOutboxRepo) MarkSent(ctx context.Context, id int64) error {
	return normalizeDBError(r.db.WithContext(ctx).Model(&VisionPublishOutbox{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":       "sent",
			"attempts":     gorm.Expr("attempts + 1"),
			"last_error":   "",
			"updated_time": repoNowTime(),
		}).Error)
}

func (r *VisionOutboxRepo) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	return normalizeDBError(r.db.WithContext(ctx).Model(&VisionPublishOutbox{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"attempts":     gorm.Expr("attempts + 1"),
			"last_error":   errMsg,
			"updated_time": repoNowTime(),
		}).Error)
}

package repo

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	AsyncJobStatusPending   = "pending"
	AsyncJobStatusRunning   = "running"
	AsyncJobStatusSucceeded = "succeeded"
	AsyncJobStatusFailed    = "failed"
	AsyncJobStatusCanceled  = "canceled"
)

type AsyncJob struct {
	ID           int64           `gorm:"column:id;primaryKey" json:"id"`
	JobKey       string          `gorm:"column:job_key;uniqueIndex:idx_sys_async_job_job_key" json:"jobKey"`
	JobType      string          `gorm:"column:job_type" json:"jobType"`
	Status       string          `gorm:"column:status" json:"status"`
	Progress     int             `gorm:"column:progress" json:"progress"`
	RequestJSON  json.RawMessage `gorm:"column:request_json;type:jsonb" json:"requestJson"`
	ResultJSON   json.RawMessage `gorm:"column:result_json;type:jsonb" json:"resultJson"`
	ErrorMessage string          `gorm:"column:error_message" json:"errorMessage"`
	StartedTime  int64           `gorm:"column:started_time" json:"startedTime"`
	FinishedTime int64           `gorm:"column:finished_time" json:"finishedTime"`
	CreatorTime
}

func (AsyncJob) TableName() string { return "sys_async_job" }

func (r *AsyncJobRepo) CreateAsyncJob(ctx context.Context, jobType string, request any, userID int64) (AsyncJob, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return AsyncJob{}, err
	}
	job := AsyncJob{
		JobKey:      "job_" + randomHex(16),
		JobType:     jobType,
		Status:      "pending",
		Progress:    0,
		RequestJSON: body,
		ResultJSON:  json.RawMessage(`{}`),
	}
	job.CreatedBy = userID
	err = r.db.WithContext(ctx).Create(&job).Error
	return job, normalizeDBError(err)
}

func (r *AsyncJobRepo) GetAsyncJob(ctx context.Context, jobKey string) (AsyncJob, error) {
	var job AsyncJob
	err := r.db.WithContext(ctx).Where("job_key = ?", jobKey).Take(&job).Error
	return job, err
}

func (r *AsyncJobRepo) MarkAsyncJobRunning(ctx context.Context, jobKey string) error {
	now := repoNowMilli()
	return r.updateAsyncJob(ctx, jobKey, touchValues(map[string]any{
		"status":        AsyncJobStatusRunning,
		"progress":      0,
		"started_time":  now,
		"finished_time": 0,
		"error_message": "",
	}, now))
}

func (r *AsyncJobRepo) UpdateAsyncJobProgress(ctx context.Context, jobKey string, progress int) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	now := repoNowMilli()
	return r.updateAsyncJob(ctx, jobKey, touchValues(map[string]any{"progress": progress}, now))
}

func (r *AsyncJobRepo) MarkAsyncJobSucceeded(ctx context.Context, jobKey string, result any) error {
	body, err := marshalAsyncJobJSON(result)
	if err != nil {
		return err
	}
	now := repoNowMilli()
	return r.updateAsyncJob(ctx, jobKey, touchValues(map[string]any{
		"status":        AsyncJobStatusSucceeded,
		"progress":      100,
		"result_json":   body,
		"error_message": "",
		"finished_time": now,
	}, now))
}

func (r *AsyncJobRepo) MarkAsyncJobFailed(ctx context.Context, jobKey string, message string, result any) error {
	body, err := marshalAsyncJobJSON(result)
	if err != nil {
		return err
	}
	now := repoNowMilli()
	return r.updateAsyncJob(ctx, jobKey, touchValues(map[string]any{
		"status":        AsyncJobStatusFailed,
		"result_json":   body,
		"error_message": message,
		"finished_time": now,
	}, now))
}

func (r *AsyncJobRepo) CompleteAsyncJobWithAudit(ctx context.Context, jobKey, status, message string, result any, audit AuditLog) error {
	body, err := marshalAsyncJobJSON(result)
	if err != nil {
		return err
	}
	if status != AsyncJobStatusSucceeded && status != AsyncJobStatusFailed {
		return ErrInvalidArgument
	}
	now := repoNowMilli()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job AsyncJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("job_key = ?", jobKey).Take(&job).Error; err != nil {
			return err
		}
		if job.Status == AsyncJobStatusSucceeded || job.Status == AsyncJobStatusFailed || job.Status == AsyncJobStatusCanceled {
			return nil
		}
		values := touchValues(map[string]any{
			"status": status, "result_json": body, "error_message": message, "finished_time": now,
		}, now)
		if status == AsyncJobStatusSucceeded {
			values["progress"] = 100
		}
		if err := tx.Model(&AsyncJob{}).Where("job_key = ?", jobKey).Updates(values).Error; err != nil {
			return err
		}
		audit.ResID = jobKey
		if err := NewAuditRepo(tx).InsertAuditLog(ctx, audit); err != nil {
			return err
		}
		return nil
	})
}

func (r *AsyncJobRepo) updateAsyncJob(ctx context.Context, jobKey string, values map[string]any) error {
	res := r.db.WithContext(ctx).Model(&AsyncJob{}).Where("job_key = ?", jobKey).Updates(values)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 && !r.activeOnly {
		return ErrNotFound
	}
	return nil
}

func marshalAsyncJobJSON(value any) (json.RawMessage, error) {
	if value == nil {
		return json.RawMessage(`{}`), nil
	}
	if raw, ok := value.(json.RawMessage); ok {
		if len(raw) == 0 {
			return json.RawMessage(`{}`), nil
		}
		return raw, nil
	}
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return json.RawMessage(`{}`), nil
	}
	return body, nil
}

type AsyncJobRepo struct {
	db         *gorm.DB
	activeOnly bool
}

func NewAsyncJobRepo(in any) *AsyncJobRepo { return &AsyncJobRepo{db: GetCommonConn(in)} }

// ActiveOnly prevents a delayed worker from overwriting a completed job on redelivery.
func (r *AsyncJobRepo) ActiveOnly() *AsyncJobRepo {
	return &AsyncJobRepo{db: r.db.Where("status IN ?", []string{AsyncJobStatusPending, AsyncJobStatusRunning}), activeOnly: true}
}

// GetAsyncJobForUpdate serializes result checkpoints from independent workers.
func (r *AsyncJobRepo) GetAsyncJobForUpdate(ctx context.Context, jobKey string) (AsyncJob, error) {
	var job AsyncJob
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("job_key = ?", jobKey).Take(&job).Error
	return job, err
}

func (r *AsyncJobRepo) UpdateAsyncJobResult(ctx context.Context, jobKey string, progress int, result any) error {
	body, err := marshalAsyncJobJSON(result)
	if err != nil {
		return err
	}
	return r.updateAsyncJob(ctx, jobKey, touchValues(map[string]any{"progress": max(0, min(progress, 99)), "result_json": body}, repoNowMilli()))
}

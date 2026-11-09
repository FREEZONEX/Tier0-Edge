package repo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strconv"
	"time"

	"gorm.io/gorm/clause"
)

const NativeAppInstallJob = "nativeApp.install"

func NativeAppJobKey(userID int64, key string) string {
	sum := sha256.Sum256([]byte(strconv.FormatInt(userID, 10) + "\x00" + key))
	return hex.EncodeToString(sum[:])
}

func (r *AsyncJobRepo) CreateNativeAppJob(ctx context.Context, request any, userID int64, key string) (AsyncJob, bool, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return AsyncJob{}, false, err
	}
	job := AsyncJob{JobKey: NativeAppJobKey(userID, key), JobType: NativeAppInstallJob, Status: AsyncJobStatusPending, RequestJSON: body, ResultJSON: json.RawMessage(`{}`)}
	job.CreatedBy = userID
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "job_key"}}, DoNothing: true}).Create(&job)
	if result.Error != nil {
		return job, false, result.Error
	}
	if result.RowsAffected == 1 {
		return job, true, nil
	}
	existing, err := r.GetAsyncJob(ctx, job.JobKey)
	if err != nil {
		return existing, false, err
	}
	var a, b any
	if json.Unmarshal(existing.RequestJSON, &a) != nil || json.Unmarshal(body, &b) != nil || existing.JobType != NativeAppInstallJob || existing.CreatedBy != userID || !reflect.DeepEqual(a, b) {
		return existing, false, ErrConflict
	}
	return existing, false, nil
}
func (r *AsyncJobRepo) RecoverNativeAppJobs(ctx context.Context) error {
	return r.db.WithContext(ctx).Model(&AsyncJob{}).Where("job_type=? AND status IN ?", NativeAppInstallJob, []string{AsyncJobStatusPending, AsyncJobStatusRunning}).Updates(map[string]any{"status": AsyncJobStatusFailed, "error_message": "App installation interrupted by service restart", "finished_time": time.Now().UnixMilli()}).Error
}

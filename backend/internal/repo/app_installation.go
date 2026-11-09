package repo

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AppInstallation struct {
	AppID            string          `gorm:"column:app_id;primaryKey" json:"appId"`
	ReleaseID        string          `json:"-"`
	AttemptReleaseID string          `json:"-"`
	Manifest         json.RawMessage `gorm:"type:jsonb" json:"-"`
	PackageSHA256    string          `gorm:"column:package_sha256" json:"packageSha256"`
	Name             string          `json:"name"`
	Version          string          `json:"version"`
	DesiredState     string          `json:"desiredState"`
	Status           string          `json:"status"`
	OperationID      string          `json:"operationId,omitempty"`
	Stage            string          `json:"stage,omitempty"`
	LastError        string          `json:"lastError,omitempty"`
	Revision         int64           `json:"revision"`
	CreatedAt        int64           `json:"createdAt"`
	UpdatedAt        int64           `json:"updatedAt"`
}

func (AppInstallation) TableName() string { return "public.native_app_installation" }

type AppInstallationRepo struct{ db *gorm.DB }

func NewAppInstallationRepo(in any) *AppInstallationRepo {
	return &AppInstallationRepo{db: GetCommonConn(in)}
}
func (r *AppInstallationRepo) Get(ctx context.Context, id string) (AppInstallation, error) {
	var row AppInstallation
	err := r.db.WithContext(ctx).Where("app_id = ?", id).Take(&row).Error
	return row, err
}
func (r *AppInstallationRepo) List(ctx context.Context) ([]AppInstallation, error) {
	rows := []AppInstallation{}
	err := r.db.WithContext(ctx).Where("stage <> ?", "uninstalled").Order("created_at DESC,app_id").Find(&rows).Error
	return rows, err
}

func (r *AppInstallationRepo) Uninstall(ctx context.Context, id string) error {
	q := r.db.WithContext(ctx).Model(&AppInstallation{}).Where("app_id=? AND operation_id=''", id).Updates(map[string]any{
		"release_id": "", "attempt_release_id": "", "manifest": json.RawMessage(`{}`), "package_sha256": "",
		"desired_state": "stopped", "status": "stopped", "stage": "uninstalled", "last_error": "", "updated_at": time.Now().UnixMilli(),
	})
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}
func (r *AppInstallationRepo) Begin(ctx context.Context, id, operation string) (AppInstallation, error) {
	var row AppInstallation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UnixMilli()
		initial := AppInstallation{AppID: id, Manifest: json.RawMessage(`{}`), DesiredState: "stopped", Status: "stopped", CreatedAt: now, UpdatedAt: now}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initial).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("app_id = ?", id).Take(&row).Error; err != nil {
			return err
		}
		if row.OperationID != "" {
			return ErrConflict
		}
		return tx.Model(&row).Updates(map[string]any{"operation_id": operation, "stage": "validating", "last_error": "", "updated_at": now}).Error
	})
	return row, err
}
func (r *AppInstallationRepo) Candidate(ctx context.Context, id, operation, release string) error {
	return r.updateOperation(ctx, id, operation, map[string]any{"attempt_release_id": release})
}
func (r *AppInstallationRepo) Stage(ctx context.Context, id, operation, stage string) error {
	return r.updateOperation(ctx, id, operation, map[string]any{"stage": stage})
}
func (r *AppInstallationRepo) Fail(ctx context.Context, id, operation, message string) error {
	return r.updateOperation(ctx, id, operation, map[string]any{"stage": "failed", "last_error": message, "operation_id": ""})
}
func (r *AppInstallationRepo) Commit(ctx context.Context, id, operation string, revision int64, release string, manifest json.RawMessage, digest, name, version string) error {
	q := r.db.WithContext(ctx).Model(&AppInstallation{}).Where("app_id=? AND operation_id=? AND revision=?", id, operation, revision).Updates(map[string]any{"release_id": release, "manifest": manifest, "package_sha256": digest, "name": name, "version": version, "desired_state": "running", "status": "running", "operation_id": "", "stage": "ready", "last_error": "", "revision": revision + 1, "updated_at": time.Now().UnixMilli()})
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}
func (r *AppInstallationRepo) updateOperation(ctx context.Context, id, operation string, values map[string]any) error {
	values["updated_at"] = time.Now().UnixMilli()
	q := r.db.WithContext(ctx).Model(&AppInstallation{}).Where("app_id=? AND operation_id=?", id, operation).Updates(values)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}
func (r *AppInstallationRepo) SetState(ctx context.Context, id, desired, status, message string) error {
	values := map[string]any{"status": status, "last_error": message, "updated_at": time.Now().UnixMilli()}
	if desired != "" {
		values["desired_state"] = desired
	}
	q := r.db.WithContext(ctx).Model(&AppInstallation{}).Where("app_id=?", id).Updates(values)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

// A new process cannot continue a previous process's candidate installation.
// Keep the last committed release and report the interrupted attempt explicitly.
func (r *AppInstallationRepo) Recover(ctx context.Context) error {
	return r.db.WithContext(ctx).Model(&AppInstallation{}).Where("operation_id <> ''").Updates(map[string]any{"operation_id": "", "stage": "failed", "last_error": "App installation interrupted by service restart", "updated_at": time.Now().UnixMilli()}).Error
}

package repo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	FleetOperationStatusQueued    = "queued"
	FleetOperationStatusRunning   = "running"
	FleetOperationStatusSucceeded = "succeeded"
	FleetOperationStatusFailed    = "failed"
	FleetOperationStatusTimedOut  = "timedOut"
	FleetOperationStatusCanceled  = "canceled"
)

type FleetContainerSnapshot struct {
	FleetNodeID    string          `gorm:"column:fleet_node_id;primaryKey" json:"fleetNodeID"`
	NodeStatus     string          `gorm:"column:node_status;->;-:migration" json:"-"`
	SnapshotID     string          `gorm:"column:snapshot_id" json:"snapshotID"`
	ObservedAt     int64           `gorm:"column:observed_at" json:"observedAt"`
	InstallationID string          `gorm:"column:installation_id" json:"installationID"`
	Payload        json.RawMessage `gorm:"column:payload;type:jsonb" json:"payload"`
	CreatedTime    time.Time       `gorm:"column:created_time" json:"-"`
	UpdatedTime    time.Time       `gorm:"column:updated_time" json:"-"`
}

func (FleetContainerSnapshot) TableName() string { return "fleet_container_snapshot" }

type FleetContainerDetail struct {
	ID          int64           `gorm:"column:id;primaryKey" json:"-"`
	FleetNodeID string          `gorm:"column:fleet_node_id" json:"fleetNodeID"`
	ContainerID string          `gorm:"column:container_id" json:"containerID"`
	ObservedAt  int64           `gorm:"column:observed_at" json:"observedAt"`
	Payload     json.RawMessage `gorm:"column:payload;type:jsonb" json:"payload"`
	CreatedTime time.Time       `gorm:"column:created_time" json:"-"`
	UpdatedTime time.Time       `gorm:"column:updated_time" json:"-"`
}

func (FleetContainerDetail) TableName() string { return "fleet_container_detail" }

type FleetOperation struct {
	ID            int64           `gorm:"column:id;primaryKey" json:"-"`
	OperationID   string          `gorm:"column:operation_id" json:"operationID"`
	FleetNodeID   string          `gorm:"column:fleet_node_id" json:"fleetNodeID"`
	ContainerID   string          `gorm:"column:container_id" json:"containerID"`
	OperationType string          `gorm:"column:operation_type" json:"operationType"`
	Status        string          `gorm:"column:status" json:"status"`
	ActorID       int64           `gorm:"column:actor_id" json:"actorID"`
	RequestJSON   json.RawMessage `gorm:"column:request_json;type:jsonb" json:"request,omitempty"`
	ResultJSON    json.RawMessage `gorm:"column:result_json;type:jsonb" json:"result,omitempty"`
	ErrorCode     string          `gorm:"column:error_code" json:"errorCode,omitempty"`
	StartedAt     int64           `gorm:"column:started_at" json:"startedAt"`
	FinishedAt    int64           `gorm:"column:finished_at" json:"finishedAt"`
	CreatedTime   time.Time       `gorm:"column:created_time" json:"-"`
	UpdatedTime   time.Time       `gorm:"column:updated_time" json:"-"`
}

func (FleetOperation) TableName() string { return "fleet_operation" }

type CreateFleetOperationInput struct {
	OperationID   string
	FleetNodeID   string
	ContainerID   string
	OperationType string
	ActorID       int64
	RequestJSON   json.RawMessage
}

func (r *FleetRepo) SaveContainerSnapshot(ctx context.Context, record FleetContainerSnapshot) (bool, error) {
	record.FleetNodeID = strings.TrimSpace(record.FleetNodeID)
	record.SnapshotID = strings.TrimSpace(record.SnapshotID)
	record.InstallationID = strings.TrimSpace(record.InstallationID)
	if record.FleetNodeID == "" || record.SnapshotID == "" || record.ObservedAt <= 0 || !json.Valid(record.Payload) {
		return false, ErrInvalidArgument
	}
	now := time.Now().UTC()
	record.CreatedTime = now
	record.UpdatedTime = now
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "fleet_node_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"snapshot_id":     record.SnapshotID,
			"observed_at":     record.ObservedAt,
			"installation_id": record.InstallationID,
			"payload":         record.Payload,
			"updated_time":    now,
		}),
		Where: clause.Where{Exprs: []clause.Expression{
			clause.Expr{SQL: "fleet_container_snapshot.observed_at <= EXCLUDED.observed_at"},
		}},
	}).Create(&record)
	if result.Error != nil {
		return false, normalizeDBError(result.Error)
	}
	return result.RowsAffected > 0, nil
}

func (r *FleetRepo) ListContainerSnapshots(ctx context.Context) ([]FleetContainerSnapshot, error) {
	var records []FleetContainerSnapshot
	err := r.db.WithContext(ctx).
		Table("fleet_container_snapshot AS snapshot").
		Select("snapshot.*, node.status AS node_status").
		Joins("JOIN fleet_node AS node ON node.fleet_node_id = snapshot.fleet_node_id AND node.status <> ?", FleetNodeStatusDeleted).
		Order("snapshot.fleet_node_id ASC").
		Scan(&records).Error
	return records, normalizeDBError(err)
}

func (r *FleetRepo) SaveContainerDetail(ctx context.Context, record FleetContainerDetail) error {
	record.FleetNodeID = strings.TrimSpace(record.FleetNodeID)
	record.ContainerID = strings.TrimSpace(record.ContainerID)
	if record.FleetNodeID == "" || record.ContainerID == "" || !json.Valid(record.Payload) {
		return ErrInvalidArgument
	}
	now := time.Now().UTC()
	record.CreatedTime = now
	record.UpdatedTime = now
	return normalizeDBError(r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "fleet_node_id"}, {Name: "container_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"observed_at":  record.ObservedAt,
			"payload":      record.Payload,
			"updated_time": now,
		}),
	}).Create(&record).Error)
}

func (r *FleetRepo) ListContainerDetails(ctx context.Context) ([]FleetContainerDetail, error) {
	var records []FleetContainerDetail
	err := r.db.WithContext(ctx).
		Table("fleet_container_detail AS detail").
		Select("detail.*").
		Joins("JOIN fleet_node AS node ON node.fleet_node_id = detail.fleet_node_id AND node.status <> ?", FleetNodeStatusDeleted).
		Order("detail.fleet_node_id ASC, detail.container_id ASC").
		Scan(&records).Error
	return records, normalizeDBError(err)
}

func (r *FleetRepo) CreateOperation(ctx context.Context, input CreateFleetOperationInput) (FleetOperation, bool, error) {
	input.OperationID = strings.TrimSpace(input.OperationID)
	input.FleetNodeID = strings.TrimSpace(input.FleetNodeID)
	input.ContainerID = strings.TrimSpace(input.ContainerID)
	input.OperationType = strings.TrimSpace(input.OperationType)
	if input.OperationID == "" || input.FleetNodeID == "" || input.ContainerID == "" || input.OperationType == "" || (len(input.RequestJSON) > 0 && !json.Valid(input.RequestJSON)) {
		return FleetOperation{}, false, ErrInvalidArgument
	}
	var output FleetOperation
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("operation_id = ?", input.OperationID).Take(&output).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if input.OperationType == "container.restart" {
			var active int64
			if err := tx.Model(&FleetOperation{}).
				Where("fleet_node_id = ? AND container_id = ? AND operation_type = ? AND status IN ?", input.FleetNodeID, input.ContainerID, input.OperationType, []string{FleetOperationStatusQueued, FleetOperationStatusRunning}).
				Count(&active).Error; err != nil {
				return err
			}
			if active > 0 {
				return ErrConflict
			}
		}
		now := time.Now().UTC()
		output = FleetOperation{
			OperationID:   input.OperationID,
			FleetNodeID:   input.FleetNodeID,
			ContainerID:   input.ContainerID,
			OperationType: input.OperationType,
			Status:        FleetOperationStatusQueued,
			ActorID:       input.ActorID,
			RequestJSON:   append(json.RawMessage(nil), input.RequestJSON...),
			CreatedTime:   now,
			UpdatedTime:   now,
		}
		if len(output.RequestJSON) == 0 {
			output.RequestJSON = json.RawMessage(`{}`)
		}
		output.ResultJSON = json.RawMessage(`{}`)
		if err := tx.Create(&output).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	return output, created, normalizeDBError(err)
}

func (r *FleetRepo) GetOperation(ctx context.Context, operationID string) (FleetOperation, error) {
	var record FleetOperation
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		return record, ErrInvalidArgument
	}
	err := r.db.WithContext(ctx).Where("operation_id = ?", operationID).Take(&record).Error
	return record, normalizeDBError(err)
}

func (r *FleetRepo) ListOperations(ctx context.Context, fleetNodeID string, page, size int64) ([]FleetOperation, int64, error) {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	if fleetNodeID == "" {
		return nil, 0, ErrInvalidArgument
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 100 {
		size = 100
	}

	query := r.db.WithContext(ctx).Model(&FleetOperation{}).Where("fleet_node_id = ?", fleetNodeID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, normalizeDBError(err)
	}
	var records []FleetOperation
	err := query.Order("created_time DESC, id DESC").Offset(int((page - 1) * size)).Limit(int(size)).Find(&records).Error
	return records, total, normalizeDBError(err)
}

func (r *FleetRepo) MarkOperationRunning(ctx context.Context, operationID string, startedAt int64) error {
	if strings.TrimSpace(operationID) == "" || startedAt <= 0 {
		return ErrInvalidArgument
	}
	result := r.db.WithContext(ctx).Model(&FleetOperation{}).
		Where("operation_id = ? AND status = ?", operationID, FleetOperationStatusQueued).
		Updates(map[string]any{"status": FleetOperationStatusRunning, "started_at": startedAt, "updated_time": time.Now().UTC()})
	if result.Error != nil {
		return normalizeDBError(result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrConflict
	}
	return nil
}

func (r *FleetRepo) CompleteOperation(ctx context.Context, operationID, status, errorCode string, resultJSON json.RawMessage, finishedAt int64) error {
	operationID = strings.TrimSpace(operationID)
	status = strings.TrimSpace(status)
	if operationID == "" || !validFleetOperationTerminalStatus(status) || finishedAt <= 0 || (len(resultJSON) > 0 && !json.Valid(resultJSON)) {
		return ErrInvalidArgument
	}
	if len(resultJSON) == 0 {
		resultJSON = json.RawMessage(`{}`)
	}
	result := r.db.WithContext(ctx).Model(&FleetOperation{}).
		Where("operation_id = ? AND status IN ?", operationID, []string{FleetOperationStatusQueued, FleetOperationStatusRunning}).
		Updates(map[string]any{
			"status":       status,
			"result_json":  resultJSON,
			"error_code":   strings.TrimSpace(errorCode),
			"finished_at":  finishedAt,
			"updated_time": time.Now().UTC(),
		})
	return normalizeDBError(result.Error)
}

func (r *FleetRepo) CancelInterruptedOperations(ctx context.Context, finishedAt int64) error {
	if finishedAt <= 0 {
		return ErrInvalidArgument
	}
	return normalizeDBError(r.db.WithContext(ctx).Model(&FleetOperation{}).
		Where("status IN ?", []string{FleetOperationStatusQueued, FleetOperationStatusRunning}).
		Updates(map[string]any{
			"status":       FleetOperationStatusCanceled,
			"error_code":   "FLEET_CENTER_RESTARTED",
			"finished_at":  finishedAt,
			"updated_time": time.Now().UTC(),
		}).Error)
}

func validFleetOperationTerminalStatus(status string) bool {
	switch status {
	case FleetOperationStatusSucceeded, FleetOperationStatusFailed, FleetOperationStatusTimedOut, FleetOperationStatusCanceled:
		return true
	default:
		return false
	}
}

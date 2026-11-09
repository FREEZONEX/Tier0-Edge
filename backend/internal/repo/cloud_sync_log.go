package repo

import (
	"context"
	"strings"

	"gorm.io/gorm"
)

const (
	CloudSyncLogSyncTypeUns  = "uns"
	CloudSyncLogSyncTypeUser = "user"

	CloudSyncLogDirectionInbound  = "inbound"
	CloudSyncLogDirectionOutbound = "outbound"

	CloudSyncLogStatusSuccess = "success"
	CloudSyncLogStatusPartial = "partial"
	CloudSyncLogStatusFailed  = "failed"
	CloudSyncLogStatusSkipped = "skipped"
)

type UnsCloudSyncLog struct {
	ID               int64  `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	SyncType         string `gorm:"column:sync_type;type:varchar(32);not null;default:'uns';index" json:"syncType"`
	Direction        string `gorm:"column:direction;type:varchar(16);index" json:"direction"`
	Status           string `gorm:"column:status;type:varchar(16);index" json:"status"`
	SyncMode         string `gorm:"column:sync_mode;type:varchar(32);index" json:"syncMode"`
	ConnectClientKey string `gorm:"column:connect_client_key;type:varchar(128);index" json:"connectClientKey"`
	NodeID           string `gorm:"column:node_id;type:varchar(128);index" json:"nodeID"`
	NodeName         string `gorm:"column:node_name;type:varchar(128)" json:"nodeName"`
	SnapshotVersion  int64  `gorm:"column:snapshot_version;type:bigint;index" json:"snapshotVersion"`
	SnapshotHash     string `gorm:"column:snapshot_hash;type:varchar(64)" json:"snapshotHash"`
	ResultCode       string `gorm:"column:result_code;type:varchar(64);index" json:"resultCode"`
	Summary          string `gorm:"column:summary;type:varchar(512)" json:"summary"`
	ErrorMessage     string `gorm:"column:error_message;type:text" json:"errorMessage"`
	Environment      string `gorm:"column:environment;type:jsonb;not null;default:'{}'" json:"environment"`
	Details          string `gorm:"column:details;type:jsonb;not null;default:'{}'" json:"details"`
	CreatedTime      int64  `gorm:"column:created_time;type:bigint;autoCreateTime:milli" json:"createdTime"`
	UpdatedTime      int64  `gorm:"column:updated_time;type:bigint;autoUpdateTime:milli" json:"updatedTime"`
}

func (UnsCloudSyncLog) TableName() string { return "uns_cloud_sync_log" }

type CloudSyncLogFilter struct {
	SyncType         string
	Direction        string
	Status           string
	ConnectClientKey string
	NodeID           string
}

type CloudSyncLogRepo struct{ db *gorm.DB }

func NewCloudSyncLogRepo(in any) *CloudSyncLogRepo {
	return &CloudSyncLogRepo{db: GetCommonConn(in)}
}

func (r *CloudSyncLogRepo) Insert(ctx context.Context, row *UnsCloudSyncLog) error {
	if row == nil {
		return nil
	}
	row.SyncType = normalizeCloudSyncLogType(row.SyncType)
	row.Direction = strings.TrimSpace(row.Direction)
	row.Status = strings.TrimSpace(row.Status)
	row.SyncMode = strings.TrimSpace(row.SyncMode)
	row.ConnectClientKey = strings.TrimSpace(row.ConnectClientKey)
	row.NodeID = strings.TrimSpace(row.NodeID)
	row.NodeName = strings.TrimSpace(row.NodeName)
	row.ResultCode = strings.TrimSpace(row.ResultCode)
	row.Summary = strings.TrimSpace(row.Summary)
	row.ErrorMessage = strings.TrimSpace(row.ErrorMessage)
	if strings.TrimSpace(row.Environment) == "" {
		row.Environment = "{}"
	}
	if strings.TrimSpace(row.Details) == "" {
		row.Details = "{}"
	}
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *CloudSyncLogRepo) ExistsBySnapshotHash(ctx context.Context, syncType, nodeID, snapshotHash string) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&UnsCloudSyncLog{}).
		Where("sync_type = ? AND node_id = ? AND snapshot_hash = ?", normalizeCloudSyncLogType(syncType), strings.TrimSpace(nodeID), strings.TrimSpace(snapshotHash)).
		Count(&count).Error
	return count > 0, err
}

func (r *CloudSyncLogRepo) List(ctx context.Context, filter CloudSyncLogFilter, page, size int64) ([]UnsCloudSyncLog, int64, error) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
	if size > 100 {
		size = 100
	}
	db := r.withFilter(ctx, filter)
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []UnsCloudSyncLog
	err := db.Order("created_time DESC").
		Order("id DESC").
		Offset(int((page - 1) * size)).
		Limit(int(size)).
		Find(&rows).Error
	return rows, total, err
}

func (r *CloudSyncLogRepo) withFilter(ctx context.Context, filter CloudSyncLogFilter) *gorm.DB {
	db := r.db.WithContext(ctx).Model(&UnsCloudSyncLog{})
	if syncType := normalizeCloudSyncLogType(filter.SyncType); syncType != "" {
		db = db.Where("sync_type = ?", syncType)
	}
	if direction := strings.TrimSpace(filter.Direction); direction != "" {
		db = db.Where("direction = ?", direction)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		db = db.Where("status = ?", status)
	}
	if key := strings.TrimSpace(filter.ConnectClientKey); key != "" {
		db = db.Where("connect_client_key = ?", key)
	}
	if nodeID := strings.TrimSpace(filter.NodeID); nodeID != "" {
		db = db.Where("node_id = ?", nodeID)
	}
	return db
}

func normalizeCloudSyncLogType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", CloudSyncLogSyncTypeUns:
		return CloudSyncLogSyncTypeUns
	case CloudSyncLogSyncTypeUser:
		return CloudSyncLogSyncTypeUser
	default:
		return strings.TrimSpace(raw)
	}
}

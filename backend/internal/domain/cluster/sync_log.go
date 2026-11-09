package cluster

import (
	"context"
	"encoding/json"
	"strings"

	"backend/internal/repo"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	syncResultCodeUNSApplied   = "uns.applied"
	syncResultCodeUNSPartial   = "uns.partial"
	syncResultCodeUNSDuplicate = "uns.duplicate"
	syncResultCodeUNSFailed    = "uns.failed"
	syncResultCodeUserApplied  = "user.applied"
	syncResultCodeUserPartial  = "user.partial"
	syncResultCodeUserFailed   = "user.failed"
)

type SyncLogListFilter struct {
	Page             int64
	Size             int64
	SyncType         string
	Direction        string
	Status           string
	ConnectClientKey string
	NodeID           string
}

type metadataSyncLogRecord struct {
	Direction        string
	Status           string
	SyncMode         string
	ConnectClientKey string
	NodeID           string
	NodeName         string
	SnapshotVersion  int64
	SnapshotHash     string
	ResultCode       string
	Summary          string
	Cause            error
	Details          map[string]any
}

func (s *Service) ListSyncLogs(ctx context.Context, filter SyncLogListFilter) (map[string]any, error) {
	page := filter.Page
	size := filter.Size
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}
	if size > 100 {
		size = 100
	}
	rows, total, err := repo.NewCloudSyncLogRepo(ctx).List(ctx, repo.CloudSyncLogFilter{
		SyncType:         filter.SyncType,
		Direction:        filter.Direction,
		Status:           filter.Status,
		ConnectClientKey: filter.ConnectClientKey,
		NodeID:           filter.NodeID,
	}, page, size)
	if err != nil {
		return nil, err
	}
	list := make([]any, 0, len(rows))
	for _, row := range rows {
		list = append(list, row)
	}
	return map[string]any{
		"list":  list,
		"total": total,
		"page":  page,
		"size":  size,
	}, nil
}

func (s *Service) recordMetadataSyncLog(ctx context.Context, record metadataSyncLogRecord) {
	if record.Status == "" {
		record.Status = repo.CloudSyncLogStatusSuccess
	}
	if record.SyncMode == "" {
		record.SyncMode = syncModeEdgeEdge
	}
	if record.Summary == "" {
		record.Summary = "metadata synchronized"
	}
	if record.Cause != nil {
		record.Status = repo.CloudSyncLogStatusFailed
		if record.ResultCode == "" {
			record.ResultCode = syncResultCodeUNSFailed
		}
	}
	row := &repo.UnsCloudSyncLog{
		SyncType:         repo.CloudSyncLogSyncTypeUns,
		Direction:        record.Direction,
		Status:           record.Status,
		SyncMode:         normalizeSyncMode(record.SyncMode, syncModeEdgeEdge),
		ConnectClientKey: record.ConnectClientKey,
		NodeID:           record.NodeID,
		NodeName:         record.NodeName,
		SnapshotVersion:  record.SnapshotVersion,
		SnapshotHash:     strings.TrimSpace(record.SnapshotHash),
		ResultCode:       strings.TrimSpace(record.ResultCode),
		Summary:          record.Summary,
		Details:          marshalLogJSON(record.Details),
	}
	if record.Cause != nil {
		row.ErrorMessage = record.Cause.Error()
	}
	if err := repo.NewCloudSyncLogRepo(ctx).Insert(ctx, row); err != nil {
		logx.WithContext(ctx).Errorf("cluster sync log insert failed: %v", err)
	}
}

func marshalLogJSON(value map[string]any) string {
	if len(value) == 0 {
		return "{}"
	}
	body, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(body)
}

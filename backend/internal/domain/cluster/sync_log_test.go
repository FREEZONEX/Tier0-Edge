// sync_log_test.go 覆盖同步日志（sync_log.go）：
// DB 路径：ListSyncLogs 分页参数归一化与记录查询、recordMetadataSyncLog
//          成功/失败记录落库（Cause 非空 → failed 状态）。
// 纯函数：marshalLogJSON 空值与非法值兜底。
package cluster

import (
	"errors"
	"math"
	"testing"

	"backend/internal/repo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListSyncLogs_Empty(t *testing.T) {
	_, s := newTestCluster(t)
	resp, err := s.ListSyncLogs(backgroundCtx(), SyncLogListFilter{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp["page"])
	assert.Equal(t, int64(10), resp["size"])
	assert.Equal(t, int64(0), resp["total"])
	assert.Empty(t, resp["list"])
}

func TestListSyncLogs_PaginationClamped(t *testing.T) {
	_, s := newTestCluster(t)
	resp, err := s.ListSyncLogs(backgroundCtx(), SyncLogListFilter{Page: 0, Size: 0})
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp["page"])
	assert.Equal(t, int64(10), resp["size"])

	resp, err = s.ListSyncLogs(backgroundCtx(), SyncLogListFilter{Page: 2, Size: 500})
	require.NoError(t, err)
	assert.Equal(t, int64(2), resp["page"])
	assert.Equal(t, int64(100), resp["size"])
}

func TestListSyncLogs_WithRecords(t *testing.T) {
	store, s := newTestCluster(t)
	ctx := backgroundCtx()
	repo.NewCloudSyncLogRepo(ctx).Insert(ctx, &repo.UnsCloudSyncLog{
		SyncType:         repo.CloudSyncLogSyncTypeUns,
		Direction:        repo.CloudSyncLogDirectionInbound,
		Status:           repo.CloudSyncLogStatusSuccess,
		SyncMode:         syncModeEdgeEdge,
		ConnectClientKey: "mqtt-9",
		NodeID:           "fn_x",
		Summary:          "metadata applied",
		Details:          "{}",
	})
	resp, err := s.ListSyncLogs(ctx, SyncLogListFilter{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), resp["total"])
	list := resp["list"].([]any)
	assert.Len(t, list, 1)

	// 按 NodeID 过滤。
	resp, err = s.ListSyncLogs(ctx, SyncLogListFilter{NodeID: "fn_other"})
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp["total"])
	_ = store
}

func TestRecordMetadataSyncLog_Success(t *testing.T) {
	store, s := newTestCluster(t)
	ctx := backgroundCtx()
	s.recordMetadataSyncLog(ctx, metadataSyncLogRecord{
		Direction:        repo.CloudSyncLogDirectionOutbound,
		SyncMode:         syncModeEdgeEdge,
		ConnectClientKey: "mqtt-9",
		NodeID:           "fn_x",
		NodeName:         "edge-1",
		SnapshotVersion:  7,
		SnapshotHash:     "abc",
		ResultCode:       syncResultCodeUNSApplied,
		Details:          map[string]any{"affectedTopics": 2},
	})
	var row repo.UnsCloudSyncLog
	require.NoError(t, store.CommonDB().Where("connect_client_key = ?", "mqtt-9").First(&row).Error)
	assert.Equal(t, repo.CloudSyncLogStatusSuccess, row.Status)
	assert.Equal(t, int64(7), row.SnapshotVersion)
	assert.Equal(t, "abc", row.SnapshotHash)
	assert.JSONEq(t, `{"affectedTopics":2}`, row.Details)
}

func TestRecordMetadataSyncLog_CauseMarksFailed(t *testing.T) {
	store, s := newTestCluster(t)
	ctx := backgroundCtx()
	s.recordMetadataSyncLog(ctx, metadataSyncLogRecord{
		Direction: repo.CloudSyncLogDirectionInbound,
		NodeID:    "fn_x",
		Cause:     errors.New("boom"),
		Details:   map[string]any{},
	})
	var row repo.UnsCloudSyncLog
	require.NoError(t, store.CommonDB().Where("node_id = ?", "fn_x").Order("id desc").First(&row).Error)
	assert.Equal(t, repo.CloudSyncLogStatusFailed, row.Status)
	assert.Equal(t, "boom", row.ErrorMessage)
	assert.Equal(t, syncResultCodeUNSFailed, row.ResultCode)
}

func TestMarshalLogJSON(t *testing.T) {
	assert.Equal(t, "{}", marshalLogJSON(nil))
	assert.Equal(t, "{}", marshalLogJSON(map[string]any{}))
	assert.Equal(t, `{"a":1}`, marshalLogJSON(map[string]any{"a": 1}))
	// NaN 无法 JSON 序列化 → 兜底 "{}"。
	assert.Equal(t, "{}", marshalLogJSON(map[string]any{"nan": math.NaN()}))
}

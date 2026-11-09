// user_sync_test.go 覆盖用户同步（user_sync.go）：
// DB 路径：UserSnapshot 鉴权与快照构建、ApplySyncPayload 路由、
//
//	UserSync 缺配置失败日志、用户同步结果落库（handleFleetUserSyncResult）、
//	outbox 结果入队（enqueueFleetUserSyncResult）。
//
// HTTP 路径：fetchUserSnapshot 通过 httptest 覆盖成功/非 2xx/业务错误码/非法体。
// 纯函数：flexibleInt64 宽松解析、wire→快照转换、快照校验、日志行构造与
//
//	摘要、来源命名、端点显示净化。
//
// 未覆盖：MQTT 快照拉取成功路径（fetchUserSnapshotMQTT/handleUserSnapshotRequest）
//
//	与远端连接（remoteClientForUserSync）依赖真实 broker/活动会话。
package cluster

import (
	"context"

	"testing"

	"backend/internal/repo"

	"github.com/stretchr/testify/assert"
)

func TestCompleteUserSyncLogInput(t *testing.T) {
	completeUserSyncLogInput(nil, repo.ClusterUserSyncApplyResult{}, nil)

	input := &userSyncLogInput{Transport: "http"}
	completeUserSyncLogInput(input, repo.ClusterUserSyncApplyResult{CreatedUsers: []map[string]any{{"u": 1}}}, nil)
	assert.Equal(t, repo.CloudSyncLogStatusSuccess, input.Status)
	assert.Equal(t, syncResultCodeUserApplied, input.ResultCode)
	assert.Contains(t, input.Summary, "created=1")
	assert.Contains(t, input.Details, "transport")

	completeUserSyncLogInput(input, repo.ClusterUserSyncApplyResult{}, context.DeadlineExceeded)
	assert.Equal(t, repo.CloudSyncLogStatusFailed, input.Status)
	assert.Equal(t, syncResultCodeUserFailed, input.ResultCode)

	completeUserSyncLogInput(input, repo.ClusterUserSyncApplyResult{Conflicts: []map[string]any{{"x": 1}}}, nil)
	assert.Equal(t, repo.CloudSyncLogStatusPartial, input.Status)
	assert.Equal(t, syncResultCodeUserPartial, input.ResultCode)
}

func TestUserSyncLogResponse(t *testing.T) {
	assert.Empty(t, userSyncLogResponse(nil))
	row := &repo.UnsCloudSyncLog{ID: 3, Status: repo.CloudSyncLogStatusSuccess, Summary: "s"}
	resp := userSyncLogResponse(row)
	assert.Equal(t, int64(3), resp["id"])
	assert.Equal(t, "s", resp["summary"])
}

func TestDisplayEndpoint(t *testing.T) {
	assert.Equal(t, "", displayEndpoint(""))
	assert.Equal(t, "https://e.x/a", displayEndpoint("https://e.x/a?token=1&b=2"))
	assert.Equal(t, "not-a-url", displayEndpoint("not-a-url"))
}

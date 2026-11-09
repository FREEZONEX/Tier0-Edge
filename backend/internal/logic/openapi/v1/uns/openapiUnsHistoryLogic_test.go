// openapiUnsHistoryLogic_test.go — OpenapiUnsHistory 逻辑测试。
//
// 测试目标：OpenAPI 历史数据查询入口。
// 正常路径：有效时间范围查询已建节点，返回空历史（sink 未初始化）+ 成功标记；
//   auto 模式分页归一化。
// 异常情况：空 topics / 缺时间 / end<start / 非法时间 / 通配 topic / 未启用 history。
package uns

import (
	"testing"
	"time"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsHistory_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	createOpenapiUnsFileNode(t, svcCtx, "hist-node")

	start := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	end := time.Now().UTC().Format(time.RFC3339Nano)

	l := NewOpenapiUnsHistoryLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsHistory(&types.HistoryReq{
		Topics:    []string{"State/hist-node"},
		StartTime: start,
		EndTime:   end,
	})
	require.NoError(t, err)
	body := resp.Data.(map[string]any)
	require.Equal(t, true, body["success"])
	results := body["results"].([]map[string]any)
	require.Len(t, results, 1)
	require.Equal(t, true, results[0]["success"])
	// sink 未初始化 → 空历史。
	result := results[0]["result"].(map[string]any)
	require.Empty(t, result["values"])
	require.Equal(t, int64(1), body["page"])
}

func TestOpenapiUnsHistory_EmptyTopics(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsHistoryLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsHistory(&types.HistoryReq{})
	require.Error(t, err)
}

func TestOpenapiUnsHistory_MissingTime(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsHistoryLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsHistory(&types.HistoryReq{Topics: []string{"State/hist-node"}})
	require.Error(t, err)
}

func TestOpenapiUnsHistory_EndBeforeStart(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsHistoryLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsHistory(&types.HistoryReq{
		Topics:    []string{"State/hist-node"},
		StartTime: "2024-01-02T00:00:00Z",
		EndTime:   "2024-01-01T00:00:00Z",
	})
	require.Error(t, err)
}

func TestOpenapiUnsHistory_InvalidTime(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsHistoryLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsHistory(&types.HistoryReq{
		Topics:    []string{"State/hist-node"},
		StartTime: "not-a-time",
		EndTime:   "2024-01-01T00:00:00Z",
	})
	require.Error(t, err)
}

func TestOpenapiUnsHistory_WildcardTopic(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsHistoryLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsHistory(&types.HistoryReq{
		Topics:    []string{"State/+"},
		StartTime: "2024-01-01T00:00:00Z",
		EndTime:   "2024-01-02T00:00:00Z",
	})
	require.NoError(t, err)
	body := resp.Data.(map[string]any)
	require.Equal(t, false, body["success"])
	results := body["results"].([]map[string]any)
	require.Equal(t, "history topic does not support wildcard", results[0]["error"].(map[string]any)["message"])
}

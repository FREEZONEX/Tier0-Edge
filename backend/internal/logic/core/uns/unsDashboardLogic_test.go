// TASK-028 unsDashboardLogic.go 单测（svctest + 真实 PG）。
// 目标：覆盖 UNS 仪表盘数据查询链路（Dashboard → dataingest.History）。
// 正常路径：metric 文件节点返回含 fields/list/total 的历史响应（新表无数据时为空列表，
// 缺表/无数据由 dataingest 优雅降级）。
// 异常情况：文件夹节点返回 400（非文件类型）；不存在的节点返回 404。
package uns

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsDashboard_MetricFile(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	id := createTestMetricFile(t, svcCtx, ctx, "dash01")

	resp, err := NewUnsDashboardLogic(ctx, svcCtx).UnsDashboard(
		&types.UnsDashboardReq{NodeId: id})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	data := resp.Data.(map[string]any)
	require.Contains(t, data, "fields")
	require.Contains(t, data, "list")
	require.Contains(t, data, "total")
}

func TestUnsDashboard_FolderRejected(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	id := createTestFolder(t, svcCtx, ctx, "folder-dash")

	_, err := NewUnsDashboardLogic(ctx, svcCtx).UnsDashboard(
		&types.UnsDashboardReq{NodeId: id})
	require.Equal(t, 400, httpErrorStatus(t, err))
}

func TestUnsDashboard_NotFound(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	_, err := NewUnsDashboardLogic(ctx, svcCtx).UnsDashboard(
		&types.UnsDashboardReq{NodeId: 999999})
	require.Equal(t, 404, httpErrorStatus(t, err))
}

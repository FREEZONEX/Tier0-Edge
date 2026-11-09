// TASK-028 UNS logic 测试公共助手。
// 目标：统一构造 svctest 测试环境（独立 PG 库 + tier0 管理员 ctx）与常用断言，
// 供本目录各 _test.go 复用；禁止 t.Parallel()（包级连接为全局单例）。
package uns

import (
	"context"
	"testing"

	"backend/internal/httpx"
	"backend/internal/svc"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

// newUNSLogicEnv 创建独立测试库的 svcCtx 与携带 tier0 管理员(用户1)的 ctx。
func newUNSLogicEnv(t *testing.T) (*svc.ServiceContext, context.Context) {
	t.Helper()
	svcCtx := svctest.NewTestServiceContext(t)
	return svcCtx, svctest.CtxWithUser(1)
}

// createTestFolder 经 UnsNodeCreateLogic 在根下创建文件夹并返回节点 ID。
func createTestFolder(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, name string) int64 {
	t.Helper()
	resp, err := NewUnsNodeCreateLogic(ctx, svcCtx).UnsNodeCreate(
		&types.UnsNodeSaveReq{Name: name, DisplayName: name, NodeType: "folder"})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)
	return resp.Data.(map[string]any)["id"].(int64)
}

// createTestMetricFile 经 UnsNodeCreateLogic 创建 metric 类型文件节点并返回节点 ID
// （领域层会自动补建 "Metric" 父文件夹）。
func createTestMetricFile(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, name string) int64 {
	t.Helper()
	resp, err := NewUnsNodeCreateLogic(ctx, svcCtx).UnsNodeCreate(
		&types.UnsNodeSaveReq{Name: name, NodeType: "file", TopicType: "metric"})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)
	return resp.Data.(map[string]any)["id"].(int64)
}

// softDeleteTestNode 经 UnsNodeDeleteLogic 软删除节点（进回收站）。
func softDeleteTestNode(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, id int64) {
	t.Helper()
	resp, err := NewUnsNodeDeleteLogic(ctx, svcCtx).UnsNodeDelete(&types.NodeIdReq{NodeId: id})
	require.NoError(t, err)
	require.Equal(t, 1, resp.Data.(map[string]any)["deleted"])
}

// httpErrorStatus 断言 err 为 *httpx.HTTPError 并返回其状态码。
func httpErrorStatus(t *testing.T, err error) int {
	t.Helper()
	require.Error(t, err)
	var httpErr *httpx.HTTPError
	require.ErrorAs(t, err, &httpErr)
	return httpErr.Status
}

// TASK-028 unsNodeDetailLogic.go 单测（svctest + 真实 PG）。
// 目标：覆盖节点详情查询链路。
// 正常路径：已创建节点返回完整详情（id/name/namespace 等）。
// 异常情况：不存在的节点返回 404。
package uns

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsNodeDetail_Success(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	id := createTestFolder(t, svcCtx, ctx, "detail-me")

	resp, err := NewUnsNodeDetailLogic(ctx, svcCtx).UnsNodeDetail(&types.NodeIdReq{NodeId: id})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	data := resp.Data.(map[string]any)
	require.Equal(t, id, data["id"].(int64))
	require.Equal(t, "detail-me", data["name"])
	require.Equal(t, "detail-me", data["namespace"])
	require.Equal(t, "active", data["status"])
}

func TestUnsNodeDetail_NotFound(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	_, err := NewUnsNodeDetailLogic(ctx, svcCtx).UnsNodeDetail(&types.NodeIdReq{NodeId: 999999})
	require.Equal(t, 404, httpErrorStatus(t, err))
}

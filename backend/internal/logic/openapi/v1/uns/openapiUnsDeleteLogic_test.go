// openapiUnsDeleteLogic_test.go — 测试。
//
// 测试目标：OpenAPI 删除节点 入口。
// 正常路径：软删节点成功。
// 异常情况：空 topics/通配 topic/不存在 404。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsDeleteAndRestore(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	created := createOpenapiUnsFileNode(t, svcCtx, "lifecycle-node")
	nodeID := created["id"].(int64)

	// 软删除。
	l := NewOpenapiUnsDeleteLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsDelete(&types.NodeDeleteReq{Topics: []string{"State/lifecycle-node"}})
	require.NoError(t, err)
	require.Equal(t, true, resp.Data.(map[string]any)["success"])

	// 恢复。
	lr := NewOpenapiUnsRestoreLogic(ctx, svcCtx)
	resp, err = lr.OpenapiUnsRestore(&types.NodeRestoreReq{Path: "State/lifecycle-node"})
	require.NoError(t, err)
	require.Equal(t, true, resp.Data.(map[string]any)["success"])

	// 恢复后详情可用。
	ld := NewOpenapiUnsNodeDetailLogic(ctx, svcCtx)
	resp, err = ld.OpenapiUnsNodeDetail(&types.NodeIdReq{NodeId: nodeID})
	require.NoError(t, err)
	require.Equal(t, "lifecycle-node", resp.Data.(map[string]any)["name"])
}

func TestOpenapiUnsDelete_EmptyTopics(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsDeleteLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsDelete(&types.NodeDeleteReq{})
	require.Error(t, err)
}

func TestOpenapiUnsDelete_WildcardTopic(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsDeleteLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsDelete(&types.NodeDeleteReq{Topics: []string{"State/#"}})
	require.Error(t, err)
}

func TestOpenapiUnsDelete_TopicNotFound(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsDeleteLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsDelete(&types.NodeDeleteReq{Topics: []string{"State/ghost"}})
	requireHTTPStatus(t, err, 404)
}
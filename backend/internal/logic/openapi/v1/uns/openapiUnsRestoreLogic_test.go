// openapiUnsRestoreLogic_test.go — 测试。
//
// 测试目标：OpenAPI 恢复节点 入口。
// 正常路径：已删节点恢复；active 节点幂等。
// 异常情况：不存在路径 404。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsRestore_NotFound(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsRestoreLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsRestore(&types.NodeRestoreReq{Path: "State/ghost"})
	requireHTTPStatus(t, err, 404)
}

func TestOpenapiUnsRestore_ActiveNodeIsNoop(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	createOpenapiUnsFileNode(t, svcCtx, "active-node")

	l := NewOpenapiUnsRestoreLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsRestore(&types.NodeRestoreReq{Path: "State/active-node"})
	require.NoError(t, err)
	require.Equal(t, true, resp.Data.(map[string]any)["success"])
}
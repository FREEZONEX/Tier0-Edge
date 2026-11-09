// openapiUnsNodesLogic_test.go — OpenapiUnsNodesLogic 测试。
//
// 测试目标：OpenAPI 节点列表（Nodes 别名入口）。
// 正常路径：委托 OpenapiUnsNodeList 返回列表。
// 异常情况：无。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsNodes_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	createOpenapiUnsFileNode(t, svcCtx, "nodes-alias")

	l := NewOpenapiUnsNodesLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsNodes(&types.UnsListReq{Keyword: "nodes-alias"})
	require.NoError(t, err)
	list := resp.Data.(map[string]any)["list"].([]map[string]any)
	// 搜索会带上命中的祖先节点（State 目录），因此列表至少包含目标节点。
	names := make([]string, 0, len(list))
	for _, item := range list {
		names = append(names, item["name"].(string))
	}
	require.Contains(t, names, "nodes-alias")
}

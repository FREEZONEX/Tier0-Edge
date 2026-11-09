// openapiUnsNodeListLogic_test.go — OpenapiUnsNodeListLogic 测试。
//
// 测试目标：OpenAPI 列出 UNS 节点入口。
// 正常路径：根列表返回全部节点；带 ParentId 过滤子节点；关键字过滤。
// 异常情况：无。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsNodeList_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	createOpenapiUnsFileNode(t, svcCtx, "list-node-a")
	createOpenapiUnsFileNode(t, svcCtx, "list-node-b")

	l := NewOpenapiUnsNodeListLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsNodeList(&types.UnsListReq{})
	require.NoError(t, err)
	list := resp.Data.(map[string]any)["list"].([]map[string]any)
	require.GreaterOrEqual(t, len(list), 2)
}

func TestOpenapiUnsNodeList_KeywordFilter(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	createOpenapiUnsFileNode(t, svcCtx, "needle-node")
	createOpenapiUnsFileNode(t, svcCtx, "other-node")

	l := NewOpenapiUnsNodeListLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsNodeList(&types.UnsListReq{Keyword: "needle"})
	require.NoError(t, err)
	list := resp.Data.(map[string]any)["list"].([]map[string]any)
	// 搜索会带上命中的祖先节点，因此列表至少包含目标节点。
	names := make([]string, 0, len(list))
	for _, item := range list {
		names = append(names, item["name"].(string))
	}
	require.Contains(t, names, "needle-node")
	require.NotContains(t, names, "other-node")
}

func TestOpenapiUnsNodes_DelegatesToList(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	createOpenapiUnsFileNode(t, svcCtx, "nodes-alias")

	l := NewOpenapiUnsNodesLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsNodes(&types.UnsListReq{Keyword: "nodes-alias"})
	require.NoError(t, err)
	list := resp.Data.(map[string]any)["list"].([]map[string]any)
	// 搜索会带上命中的祖先节点（State 文件夹），因此列表包含目标 + 祖先。
	require.GreaterOrEqual(t, len(list), 1)
	names := make([]string, 0, len(list))
	for _, item := range list {
		names = append(names, item["name"].(string))
	}
	require.Contains(t, names, "nodes-alias")
}

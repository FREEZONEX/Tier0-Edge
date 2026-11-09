// openapiUnsBrowseLogic_test.go — 测试。
//
// 测试目标：OpenAPI 浏览 UNS 树 入口。
// 正常路径：根目录/路径浏览。
// 异常情况：无。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsBrowse_Root(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	createOpenapiUnsFileNode(t, svcCtx, "browse-node")

	l := NewOpenapiUnsBrowseLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsBrowse(&types.BrowseReq{})
	require.NoError(t, err)
	tree := resp.Data.(map[string]any)["tree"].([]map[string]any)
	require.NotEmpty(t, tree)
	// 根目录包含 State 目录。
	foundState := false
	for _, node := range tree {
		if node["name"] == "State" {
			foundState = true
		}
	}
	require.True(t, foundState)
}

func TestOpenapiUnsBrowse_Path(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	createOpenapiUnsFileNode(t, svcCtx, "browse-child")

	l := NewOpenapiUnsBrowseLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsBrowse(&types.BrowseReq{Path: "State"})
	require.NoError(t, err)
	tree := resp.Data.(map[string]any)["tree"].([]map[string]any)
	require.Len(t, tree, 1)
	require.Equal(t, "State", tree[0]["name"])
}
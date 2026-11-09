// TASK-028 MenuTreeLogic 单元测试：
//   目标：覆盖菜单树（seed 菜单节点、类型过滤、license 过滤）。
//   正常路径：返回 seed 菜单树（/home、/uns、/flow 等），仅含菜单类型节点。
//   异常情况：无（Menus 查询为只读路径，license 未配置时原样返回）。
package iam

import (
	"testing"

	"backend/internal/repo"

	"github.com/stretchr/testify/require"
)

func TestMenuTree_ReturnsSeededMenus(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewMenuTreeLogic(ctx, svcCtx)

	resp, err := l.MenuTree()
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, 200, resp.Code)

	menus, ok := resp.Data.([]repo.Resource)
	require.True(t, ok, "menu tree data should be []repo.Resource")
	require.NotEmpty(t, menus)

	// 首页菜单应存在。
	found := findResourceNodeByKey(t, menus, "home.page")
	require.NotNil(t, found, "seeded home menu should exist")
	require.Equal(t, "/home", found.RoutePath)

	// UNS 分组下应有子菜单 uns.page。
	uns := findResourceNodeByKey(t, menus, "uns")
	require.NotNil(t, uns)
	require.NotEmpty(t, uns.Children)
}

func TestMenuTree_ExcludesActionNodes(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewMenuTreeLogic(ctx, svcCtx)

	resp, err := l.MenuTree()
	require.NoError(t, err)
	require.NotNil(t, resp)
	menus := resp.Data.([]repo.Resource)
	// 纯 action 型节点（type=3，如 uns.read）不应出现在菜单树中。
	require.Nil(t, findResourceNodeByKey(t, menus, "uns.read"))
}

// findResourceNodeByKey 按 resourceKey 深度优先查找资源节点。
func findResourceNodeByKey(t *testing.T, nodes []repo.Resource, key string) *repo.Resource {
	t.Helper()
	for i := range nodes {
		if nodes[i].ResourceKey == key {
			return &nodes[i]
		}
		if found := findResourceNodeByKey(t, nodes[i].Children, key); found != nil {
			return found
		}
	}
	return nil
}

// TASK-028 ResourceTreeLogic 单元测试：
//   目标：覆盖资源树（seed 全量资源树组装、Actions 附带、license 过滤后的节点保留规则）。
//   正常路径：返回 seed 根节点树（含 uns/flow/app 等分组），目录节点带 children 与 actions。
//   异常情况：无（Resources 查询为只读路径，license 未配置时原样返回）。
package iam

import (
	"testing"

	"backend/internal/repo"

	"github.com/stretchr/testify/require"
)

func TestResourceTree_ReturnsSeededTree(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewResourceTreeLogic(ctx, svcCtx)

	resp, err := l.ResourceTree()
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, 200, resp.Code)

	resources, ok := resp.Data.([]repo.Resource)
	require.True(t, ok, "resource tree data should be []repo.Resource")
	require.NotEmpty(t, resources)

	// 找到根目录 App（id=200），其下应有 Project 与 Launchpad 子节点。
	appNode := findResourceNodeByKey(t, resources, "app")
	require.NotNil(t, appNode, "seeded app group should exist")
	require.NotEmpty(t, appNode.Children)
}

func TestResourceTree_ActionCountPresent(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewResourceTreeLogic(ctx, svcCtx)

	resp, err := l.ResourceTree()
	require.NoError(t, err)
	require.NotNil(t, resp)
	resources := resp.Data.([]repo.Resource)

	// 用户管理节点（iam.user.view, id=110）应带 ui/api actions。
	found := findResourceNodeByKey(t, resources, "iam.user.view")
	require.NotNil(t, found)
	require.NotEmpty(t, found.Actions)
	require.Positive(t, found.ActionCount)
}

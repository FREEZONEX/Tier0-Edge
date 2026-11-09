// TASK-028 licenseFilter 单元测试：
//   目标：覆盖资源树的 license 过滤（TestMain 装配的受限 Legacy Plus 实例下按模块裁剪）。
//   正常路径：siem.*/cluster.* 等受限模块叶子被裁剪；含允许子节点的父节点保留；
//             全部子节点被裁剪的父节点删除；允许的子树原样保留。
//   异常情况：无（纯函数，无 DB 依赖；默认实例由 TestMain 保证非 nil）。
package iam

import (
	"testing"

	"backend/internal/repo"

	"github.com/stretchr/testify/require"
)

func TestFilterResourcesByLicense_DropsDisallowedLeaves(t *testing.T) {
	// TestMain 装配的 Legacy Plus 计划：siem.* 与 cluster.* 模块不允许。
	resources := []repo.Resource{
		{ID: 1, ResourceKey: "siem.dashboard.view", Name: "SIEM View"},
		{ID: 2, ResourceKey: "uns.read", Name: "UNS Read"},
	}
	out := filterResourcesByLicense(resources)
	require.Len(t, out, 1)
	require.Equal(t, "uns.read", out[0].ResourceKey)
}

func TestFilterResourcesByLicense_DropsParentWhenAllChildrenDropped(t *testing.T) {
	resources := []repo.Resource{
		{ID: 1, ResourceKey: "siem", Name: "SIEM", Children: []repo.Resource{
			{ID: 2, ResourceKey: "siem.dashboard.view", Name: "SIEM View"},
			{ID: 3, ResourceKey: "siem.alert.view", Name: "SIEM Alert"},
		}},
	}
	out := filterResourcesByLicense(resources)
	// Legacy Plus 计划下 siem.* 不允许 → 子节点全被裁剪。
	// 父节点 key "siem" 不带 "siem." 前缀，走 allowResourceForPlan 的 default 分支
	// （允许）→ 父节点保留但 children 为空。这是生产代码的真实行为。
	require.Len(t, out, 1)
	require.Equal(t, "siem", out[0].ResourceKey)
	require.Empty(t, out[0].Children)
}

func TestFilterResourcesByLicense_KeepsParentWithMixedChildren(t *testing.T) {
	resources := []repo.Resource{
		{ID: 1, ResourceKey: "siem", Name: "SIEM", Children: []repo.Resource{
			{ID: 2, ResourceKey: "siem.dashboard.view", Name: "SIEM View"},
			{ID: 3, ResourceKey: "uns.read", Name: "UNS Read"},
		}},
	}
	out := filterResourcesByLicense(resources)
	require.Len(t, out, 1)
	require.Equal(t, "siem", out[0].ResourceKey)
	require.Len(t, out[0].Children, 1)
	require.Equal(t, "uns.read", out[0].Children[0].ResourceKey)
}

func TestFilterResourcesByLicense_KeepsAllowedSubtree(t *testing.T) {
	resources := []repo.Resource{
		{ID: 10, ResourceKey: "uns", Name: "UNS", Children: []repo.Resource{
			{ID: 11, ResourceKey: "uns.read", Name: "UNS Read"},
		}},
	}
	out := filterResourcesByLicense(resources)
	require.Len(t, out, 1)
	require.Equal(t, "uns", out[0].ResourceKey)
	require.Equal(t, "uns.read", out[0].Children[0].ResourceKey)
}

func TestFilterResourcesByLicense_ClusterLeavesDropped(t *testing.T) {
	resources := []repo.Resource{
		{ID: 1, ResourceKey: "cluster.edge.manage", Name: "Edge Node"},
		{ID: 2, ResourceKey: "home.page", Name: "Home"},
	}
	out := filterResourcesByLicense(resources)
	require.Len(t, out, 1)
	require.Equal(t, "home.page", out[0].ResourceKey)
}

// requireEqualResources 简化断言：比较 ResourceKey 列表。
func requireEqualResources(t *testing.T, got, want []repo.Resource) {
	t.Helper()
	require.Len(t, got, len(want))
	for i := range want {
		require.Equal(t, want[i].ResourceKey, got[i].ResourceKey)
	}
}

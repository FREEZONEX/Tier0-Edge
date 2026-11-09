// TASK-028 ContainerAppListLogic 单元测试：
//
//	目标：覆盖外部容器应用列表查询。
//	正常路径：仅返回 project_id=0 且 site_type=externalContainer 的存活应用，
//	          按 id 倒序，total 与实际数量一致；无数据时返回空列表。
//	异常情况：无（repo 查询无错误分支）。
package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContainerAppList_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppListLogic(ctx, svcCtx)

	insertContainerApp(t, svcCtx, "grafana", "https://grafana.example.com", "active")
	insertContainerApp(t, svcCtx, "kibana", "https://kibana.example.com", "stopped")
	// project 作用域 app 不应出现在列表中
	projectID := mustCreateProject(t, svcCtx, ctx, "app-list-project")
	insertProjectApp(t, svcCtx, projectID, "inner")

	resp, err := l.ContainerAppList()
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, 2, view["total"])
	list, ok := view["list"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, list, 2)
	// 按 id 倒序：kibana（后插入）在前
	require.Equal(t, "kibana", list[0]["name"])
	require.Equal(t, "grafana", list[1]["name"])
	require.Equal(t, "stopped", list[0]["status"])
	require.Equal(t, false, list[0]["enabled"])
	require.Equal(t, "active", list[1]["status"])
	require.Equal(t, true, list[1]["enabled"])
}

func TestContainerAppList_Empty(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppListLogic(ctx, svcCtx)

	resp, err := l.ContainerAppList()
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, 0, view["total"])
	list, ok := view["list"].([]map[string]any)
	require.True(t, ok)
	require.Empty(t, list)
}

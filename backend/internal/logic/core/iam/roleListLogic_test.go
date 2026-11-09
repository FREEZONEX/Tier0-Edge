// TASK-028 RoleListLogic 单元测试：
//   目标：覆盖角色列表（seed 内置 Admin/Operator + 自定义角色，total 统计）。
//   正常路径：返回全部角色，total 与列表长度一致，角色带默认首页与资源列表。
//   异常情况：无（Roles 查询为只读路径，出错仅来自 DB 故障）。
package iam

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestRoleList_ContainsSeedAndCreated(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	mustCreateRole(t, svcCtx, ctx, "custom-role")

	l := NewRoleListLogic(ctx, svcCtx)
	resp, err := l.RoleList(&types.PageReq{})
	require.NoError(t, err)
	view := respData(t, resp)

	list, ok := view["list"].([]repo.Role)
	require.True(t, ok, "role list should be []repo.Role")
	// seed 内置 Admin + Operator + 新建的自定义角色。
	require.Equal(t, 3, len(list))
	require.Equal(t, len(list), view["total"])

	var found bool
	for _, role := range list {
		if role.Name == "custom-role" {
			found = true
			require.NotEmpty(t, role.Code)
		}
	}
	require.True(t, found, "created role should appear in list")
}

func TestRoleList_AdminRoleHasResources(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewRoleListLogic(ctx, svcCtx)
	resp, err := l.RoleList(&types.PageReq{})
	require.NoError(t, err)
	view := respData(t, resp)
	list := view["list"].([]repo.Role)
	require.NotEmpty(t, list)
	// seed Admin 角色应挂有大量资源（含 launchpad.view 等）。
	admin := list[0]
	require.Equal(t, "admin", admin.Code)
	require.NotEmpty(t, admin.ResourceList)
}

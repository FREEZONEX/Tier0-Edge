// TASK-028 UserListLogic 单元测试：
//   目标：覆盖用户列表（seed 管理员 + 新建用户，含角色列表、total 统计）。
//   正常路径：返回全部用户且 total 一致，列表项包含 homePage/roleList。
//   异常情况：无（Users 查询为只读路径，出错仅来自 DB 故障）。
package iam

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUserList_ContainsSeedAndCreated(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	roleID := adminRoleID(t, svcCtx)
	mustCreateUser(t, svcCtx, ctx, "alice", roleID)

	l := NewUserListLogic(ctx, svcCtx)
	resp, err := l.UserList(&types.PageReq{})
	require.NoError(t, err)
	view := respData(t, resp)

	list, ok := view["list"].([]map[string]any)
	require.True(t, ok, "user list should be []map[string]any")
	require.Equal(t, 2, len(list))
	require.Equal(t, len(list), view["total"])

	var found bool
	for _, user := range list {
		if user["userName"] == "alice" {
			found = true
			require.Equal(t, int64(1), user["status"])
			roleList, ok := user["roleList"].([]map[string]any)
			require.True(t, ok)
			require.Len(t, roleList, 1)
		}
	}
	require.True(t, found, "created user should appear in list")
}

func TestUserList_HomePageResolved(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewUserListLogic(ctx, svcCtx)
	resp, err := l.UserList(&types.PageReq{})
	require.NoError(t, err)
	view := respData(t, resp)
	list := view["list"].([]map[string]any)
	require.NotEmpty(t, list)
	// seed 管理员走 Admin 角色默认首页（/home），Users() 会经 launchpad 解析后回填。
	require.Contains(t, list[0]["homePage"].(string), "/")
}

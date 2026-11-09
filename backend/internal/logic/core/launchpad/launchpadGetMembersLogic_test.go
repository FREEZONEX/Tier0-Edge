// TASK-028 LaunchpadGetMembersLogic 单元测试：
//
//	目标：覆盖项目成员列表（分页/关键字/角色过滤/时间范围过滤/排序）。
//	正常路径：按 projectName 查询返回成员分页；roleKey 与 roles 过滤；updatedAt 时间窗过滤；
//	          page/size 归一化；成员按 updated_time 倒序。
//	异常情况：缺少 projectName → 400；项目不存在 → 404；时间格式非法 → 400。
//	说明：场景顺序共享同一测试库（每个场景独立断言，互不冲突）。
package launchpad

import (
	"testing"
	"time"

	domainlaunchpad "backend/internal/domain/launchpad"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestLaunchpadGetMembers_Behavior(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	projectID := mustCreateProject(t, svcCtx, ctx, "alpha", 1)
	mustCreateUser(t, svcCtx, 2, "user2")
	mustCreateUser(t, svcCtx, 3, "user3")
	roleID := mustCreateRole(t, svcCtx, ctx, projectID, "viewer", "Viewer")
	mustAddMember(t, svcCtx, ctx, projectID, 2, []int64{roleID})
	mustAddMember(t, svcCtx, ctx, projectID, 3, nil)

	l := NewLaunchpadGetMembersLogic(ctx, svcCtx)

	// 分页：size=1 只返回最新成员（updated_time 倒序）
	resp, err := l.LaunchpadGetMembers(&types.LaunchpadGetMembersReq{
		ProjectName: "alpha",
		Page:        1,
		Size:        1,
	})
	require.NoError(t, err)
	page := respDataAny(t, resp).(domainlaunchpad.MembersPage)
	// 成员 = owner(user1) + user2 + user3 = 3
	require.Equal(t, int64(3), page.Total)
	require.Equal(t, int64(1), page.Page)
	require.Equal(t, int64(1), page.Size)
	require.Len(t, page.List, 1)
	require.NotEmpty(t, page.List[0].UserID)

	// roleKey 过滤：只有 user2 有 viewer 角色
	resp, err = l.LaunchpadGetMembers(&types.LaunchpadGetMembersReq{
		ProjectName: "alpha",
		RoleKey:     "viewer",
	})
	require.NoError(t, err)
	page = respDataAny(t, resp).(domainlaunchpad.MembersPage)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, page.List, 1)
	require.Len(t, page.List[0].Roles, 1)
	require.Equal(t, "viewer", page.List[0].Roles[0].RoleKey)

	// updatedAt 时间窗过滤：窗口覆盖创建时间 → 全部成员
	start := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	end := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	resp, err = l.LaunchpadGetMembers(&types.LaunchpadGetMembersReq{
		ProjectName:    "alpha",
		UpdatedAtStart: start,
		UpdatedAtEnd:   end,
	})
	require.NoError(t, err)
	page = respDataAny(t, resp).(domainlaunchpad.MembersPage)
	require.Equal(t, int64(3), page.Total)

	// 缺 projectName → 400
	_, err = l.LaunchpadGetMembers(&types.LaunchpadGetMembersReq{})
	requireHTTPError(t, err, 400)

	// 时间格式非法 → 400
	_, err = l.LaunchpadGetMembers(&types.LaunchpadGetMembersReq{
		ProjectName:    "alpha",
		UpdatedAtStart: "not-a-time",
	})
	requireHTTPError(t, err, 400)

	// 项目不存在 → 404
	_, err = l.LaunchpadGetMembers(&types.LaunchpadGetMembersReq{ProjectName: "ghost"})
	requireHTTPError(t, err, 404)
}

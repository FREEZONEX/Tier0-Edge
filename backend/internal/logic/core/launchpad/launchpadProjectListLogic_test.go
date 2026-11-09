// TASK-028 LaunchpadProjectListLogic 单元测试：
//
//	目标：覆盖项目列表（本人项目、admin 查看他人项目、非 admin 越权、关键字过滤）。
//	正常路径：默认返回本人项目；admin 指定 userId 返回目标用户项目；keyword 过滤生效。
//	异常情况：非 admin 指定其他用户返回 403 forbidden。
//	说明：场景顺序共享同一测试库（每个场景独立断言，互不冲突）。
package launchpad

import (
	"testing"

	domainlaunchpad "backend/internal/domain/launchpad"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestLaunchpadProjectList_Behavior(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)

	// user2 建项目，user1（admin）可查看
	mustCreateUser(t, svcCtx, 2, "user2")
	mustCreateProject(t, svcCtx, ctx, "user2-project", 2)
	l := NewLaunchpadProjectListLogic(ctx, svcCtx)
	resp, err := l.LaunchpadProjectList(&types.LaunchpadListReq{UserId: 2})
	require.NoError(t, err)
	list := respDataAny(t, resp).([]domainlaunchpad.Project)
	require.Len(t, list, 1)
	require.Equal(t, "user2-project", list[0].DisplayName)

	// 非 admin（user2）查 user1 越权 → 403（user1 需建项目且把 user2 加为成员，否则列表为空无法演示）
	user1ProjectID := mustCreateProject(t, svcCtx, ctx, "user1-project", 1)
	mustAddMember(t, svcCtx, ctx, user1ProjectID, 2, nil)
	nonAdminCtx := svctest.CtxWithUser(2)
	l2 := NewLaunchpadProjectListLogic(nonAdminCtx, svcCtx)
	_, err = l2.LaunchpadProjectList(&types.LaunchpadListReq{UserId: 1})
	requireHTTPError(t, err, 403)

	// user1 查自己的项目列表
	resp, err = l.LaunchpadProjectList(&types.LaunchpadListReq{})
	require.NoError(t, err)
	list = respDataAny(t, resp).([]domainlaunchpad.Project)
	require.Len(t, list, 1)
	require.Equal(t, "user1-project", list[0].DisplayName)

	// keyword 过滤
	mustCreateProject(t, svcCtx, ctx, "alpha", 1)
	resp, err = l.LaunchpadProjectList(&types.LaunchpadListReq{Keyword: "alpha"})
	require.NoError(t, err)
	list = respDataAny(t, resp).([]domainlaunchpad.Project)
	require.Len(t, list, 1)
	require.Equal(t, "alpha", list[0].DisplayName)
}

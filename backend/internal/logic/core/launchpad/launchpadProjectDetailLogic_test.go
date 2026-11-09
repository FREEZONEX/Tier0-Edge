// TASK-028 LaunchpadProjectDetailLogic 单元测试：
//
//	目标：覆盖项目详情（成员视角返回项目信息与可见应用列表、非成员 404、关键字过滤应用）。
//	正常路径：项目成员可看到项目与角色可见的应用；keyword 过滤生效；owner 非成员不可见。
//	异常情况：项目不存在/无访问权限返回 404（logicx.Error 包装 launchpad.ErrNotFound）。
//	说明：场景顺序共享同一测试库（每个场景独立断言，互不冲突）。
package launchpad

import (
	"testing"

	domainlaunchpad "backend/internal/domain/launchpad"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestLaunchpadProjectDetail_Behavior(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	projectID := mustCreateProject(t, svcCtx, ctx, "alpha", 1)
	mustCreateApp(t, svcCtx, ctx, projectID, "web-app", 1)
	mustCreateApp(t, svcCtx, ctx, projectID, "robot-console", 1)

	l := NewLaunchpadProjectDetailLogic(ctx, svcCtx)

	// 成员视角：项目信息 + 全部应用 + canEdit（owner）
	resp, err := l.LaunchpadProjectDetail(&types.LaunchpadProjectReq{ProjectId: projectID})
	require.NoError(t, err)
	view := respData(t, resp)
	project := view["project"].(map[string]any)
	require.Equal(t, "alpha", project["name"])
	require.Equal(t, int64(1), project["id"])
	require.Equal(t, 2, project["appNums"])
	apps := view["apps"].([]domainlaunchpad.App)
	require.Len(t, apps, 2)
	require.Equal(t, true, view["canEdit"])

	// keyword 过滤应用
	resp, err = l.LaunchpadProjectDetail(&types.LaunchpadProjectReq{ProjectId: projectID, Keyword: "robot"})
	require.NoError(t, err)
	apps = respData(t, resp)["apps"].([]domainlaunchpad.App)
	require.Len(t, apps, 1)
	require.Equal(t, "robot-console", apps[0].Display)

	// 非成员 → 404（user2 是另一项目 owner，非本项目成员）
	mustCreateUser(t, svcCtx, 2, "user2")
	otherProjectID := mustCreateProject(t, svcCtx, ctx, "other", 2)
	user1Ctx := svctest.CtxWithUser(1)
	l2 := NewLaunchpadProjectDetailLogic(user1Ctx, svcCtx)
	_, err = l2.LaunchpadProjectDetail(&types.LaunchpadProjectReq{ProjectId: otherProjectID})
	requireHTTPError(t, err, 404)

	// 项目不存在 → 404
	_, err = l.LaunchpadProjectDetail(&types.LaunchpadProjectReq{ProjectId: 999999})
	requireHTTPError(t, err, 404)
}

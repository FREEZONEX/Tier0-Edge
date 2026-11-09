// TASK-028 LaunchpadAppDetailLogic 单元测试：
//
//	目标：覆盖应用详情（成员+角色可见返回 app、存在但缺角色 403、不存在 404）。
//	正常路径：项目成员且拥有应用绑定角色时返回应用信息。
//	异常情况：应用存在且 active 但用户缺绑定角色 → 403（App access required）；
//	          应用不存在/已删除/项目无权限 → 404（不披露存在性）。
//	说明：场景顺序共享同一测试库（每个场景独立断言，互不冲突）。
package launchpad

import (
	"testing"

	domainlaunchpad "backend/internal/domain/launchpad"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestLaunchpadAppDetail_Behavior(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	projectID := mustCreateProject(t, svcCtx, ctx, "alpha", 1)
	roleID := mustCreateRole(t, svcCtx, ctx, projectID, "viewer", "Viewer")
	appID := mustCreateApp(t, svcCtx, ctx, projectID, "web-app", 1)
	bindAppToRole(t, svcCtx, ctx, projectID, roleID, appID)
	// user1 绑定 viewer 角色（owner 成员默认无角色，绑定后才可见）
	mustAddMember(t, svcCtx, ctx, projectID, 1, []int64{roleID})

	l := NewLaunchpadAppDetailLogic(ctx, svcCtx)

	// 成员+角色可见 → 返回应用详情
	resp, err := l.LaunchpadAppDetail(&types.LaunchpadAppReq{ProjectId: projectID, AppId: appID})
	require.NoError(t, err)
	view := respData(t, resp)
	app := view["app"].(domainlaunchpad.App)
	require.Equal(t, "web-app", app.Display)
	require.Equal(t, appID, app.AppID)
	project := view["project"].(domainlaunchpad.Project)
	require.Equal(t, "alpha", project.DisplayName)

	// user2 是项目成员但没有绑定角色 → 403
	mustCreateUser(t, svcCtx, 2, "user2")
	mustAddMember(t, svcCtx, ctx, projectID, 2, nil)
	user2Ctx := svctest.CtxWithUser(2)
	l2 := NewLaunchpadAppDetailLogic(user2Ctx, svcCtx)
	_, err = l2.LaunchpadAppDetail(&types.LaunchpadAppReq{ProjectId: projectID, AppId: appID})
	requireHTTPError(t, err, 403)

	// 应用不存在 → 404
	_, err = l.LaunchpadAppDetail(&types.LaunchpadAppReq{ProjectId: projectID, AppId: 999999})
	requireHTTPError(t, err, 404)

	// 非成员访问他人项目应用 → 404
	mustCreateUser(t, svcCtx, 3, "user3")
	otherProjectID := mustCreateProject(t, svcCtx, ctx, "other", 3)
	otherAppID := mustCreateApp(t, svcCtx, ctx, otherProjectID, "other-app", 3)
	l3 := NewLaunchpadAppDetailLogic(user2Ctx, svcCtx)
	_, err = l3.LaunchpadAppDetail(&types.LaunchpadAppReq{ProjectId: otherProjectID, AppId: otherAppID})
	requireHTTPError(t, err, 404)
}

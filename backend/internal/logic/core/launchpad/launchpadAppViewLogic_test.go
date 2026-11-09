// TASK-028 LaunchpadAppViewLogic 单元测试：
//
//	目标：覆盖应用查看记录（权限校验通过后记录审计 state 并返回成功）。
//	正常路径：成员且角色可见的应用 → RecordAppView 通过，审计 state 写入 ctx。
//	异常情况：应用不存在/无角色 → 404；审计落库走 HTTP 中间件链路，logic 单测不覆盖。
//	说明：场景顺序共享同一测试库（每个场景独立断言，互不冲突）。
package launchpad

import (
	"testing"

	"backend/internal/domain/audit"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestLaunchpadAppView_Behavior(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	projectID := mustCreateProject(t, svcCtx, ctx, "alpha", 1)
	roleID := mustCreateRole(t, svcCtx, ctx, projectID, "viewer", "Viewer")
	appID := mustCreateApp(t, svcCtx, ctx, projectID, "web-app", 1)
	bindAppToRole(t, svcCtx, ctx, projectID, roleID, appID)
	// user1 绑定 viewer 角色（owner 成员默认无角色，绑定后才可见）
	mustAddMember(t, svcCtx, ctx, projectID, 1, []int64{roleID})

	l := NewLaunchpadAppViewLogic(ctx, svcCtx)

	// 无审计 state 的 ctx：Record 为安全 no-op，仍成功
	resp, err := l.LaunchpadAppView(&types.LaunchpadAppViewReq{AppID: appID})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	// 挂载审计 state：记录 View 审计到 ctx（落库走中间件，这里只验证 state）
	ctxWithState := svcCtx.App.Audit.WithState(ctx)
	l2 := NewLaunchpadAppViewLogic(ctxWithState, svcCtx)
	resp, err = l2.LaunchpadAppView(&types.LaunchpadAppViewReq{AppID: appID})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)
	state := audit.StateFromContext(ctxWithState)
	require.NotNil(t, state)
	require.NotNil(t, state.Record)
	require.Equal(t, audit.ResTypeProjectApp, state.Record.ResType)
	require.Equal(t, audit.BizView, state.Record.BusinessType)
	require.Equal(t, audit.ShowInRecentYes, state.Record.IsShowInRecent)

	// 应用不存在 → 404
	_, err = l.LaunchpadAppView(&types.LaunchpadAppViewReq{AppID: 999999})
	requireHTTPError(t, err, 404)

	// user2 是成员但缺绑定角色 → 404（RecordAppView 走 requireAppVisible 返回 ErrNotFound）
	mustCreateUser(t, svcCtx, 2, "user2")
	mustAddMember(t, svcCtx, ctx, projectID, 2, nil)
	user2Ctx := svctest.CtxWithUser(2)
	l3 := NewLaunchpadAppViewLogic(user2Ctx, svcCtx)
	_, err = l3.LaunchpadAppView(&types.LaunchpadAppViewReq{AppID: appID})
	requireHTTPError(t, err, 404)
}

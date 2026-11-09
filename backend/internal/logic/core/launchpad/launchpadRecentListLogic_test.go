// TASK-028 LaunchpadRecentListLogic 单元测试：
//
//	目标：覆盖最近应用列表（查看审计 → 去重 → 收藏优先 + 时间倒序 → 上限 8 条）。
//	正常路径：成员可见的应用出现在最近列表；被角色过滤的应用不出现；收藏优先排序。
//	异常情况：无查看记录返回空列表；查看记录指向不存在/不可见应用时安全跳过。
//	说明：场景顺序共享同一测试库（每个场景独立断言，互不冲突）。
package launchpad

import (
	"strconv"
	"testing"
	"time"

	domainlaunchpad "backend/internal/domain/launchpad"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/svc/svctest"

	"github.com/stretchr/testify/require"
)

// mustRecordAppView 直接写一条 ProjectApp View 审计记录（与中间件落库同一表/格式）。
func mustRecordAppView(t *testing.T, svcCtx *svc.ServiceContext, userID, appID int64, viewedAt time.Time) {
	t.Helper()
	db := svcCtx.App.Store.CommonDB()
	require.NoError(t, db.Create(&repo.AuditLog{
		UserID:           userID,
		ResType:          "ProjectApp",
		ResID:            strconv.FormatInt(appID, 10),
		BusinessTypeCode: repo.AuditBusinessTypeCode("View"),
		DetailJSON:       `{"appId":` + strconv.FormatInt(appID, 10) + `}`,
		Code:             200,
		IsShowInRecent:   1,
		CreatedTime:      viewedAt,
	}).Error)
}

func TestLaunchpadRecentList_Behavior(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewLaunchpadRecentListLogic(ctx, svcCtx)

	// 无查看记录 → 空列表
	resp, err := l.LaunchpadRecentList()
	require.NoError(t, err)
	require.Empty(t, respData(t, resp)["list"].([]domainlaunchpad.App))

	// 有查看记录 → 可见应用返回，按时间倒序
	projectID := mustCreateProject(t, svcCtx, ctx, "alpha", 1)
	roleID := mustCreateRole(t, svcCtx, ctx, projectID, "viewer", "Viewer")
	appID := mustCreateApp(t, svcCtx, ctx, projectID, "web-app", 1)
	appID2 := mustCreateApp(t, svcCtx, ctx, projectID, "robot-console", 1)
	// appID2 绑定 viewer 角色：user1 绑定角色可见，user2 无角色不可见（用于末场景隔离）
	bindAppToRole(t, svcCtx, ctx, projectID, roleID, appID2)
	// web-app 也绑定 viewer：user2 无角色时两个应用都不可见
	bindAppToRole(t, svcCtx, ctx, projectID, roleID, appID)
	mustAddMember(t, svcCtx, ctx, projectID, 1, []int64{roleID})
	mustRecordAppView(t, svcCtx, 1, appID, time.Now().Add(-time.Hour))
	mustRecordAppView(t, svcCtx, 1, appID2, time.Now().Add(-30*time.Minute))
	resp, err = l.LaunchpadRecentList()
	require.NoError(t, err)
	list := respData(t, resp)["list"].([]domainlaunchpad.App)
	require.Len(t, list, 2)
	require.Equal(t, "robot-console", list[0].Display)
	require.Equal(t, "web-app", list[1].Display)

	// 去重 + 收藏优先：web-app 多次查看只保留最近一次，收藏后优先
	mustRecordAppView(t, svcCtx, 1, appID, time.Now().Add(-10*time.Minute))
	require.NoError(t, svcCtx.App.Launchpad.BatchAddFavorites(ctx, 1, []repo.UserFavorite{
		{ResType: domainlaunchpad.PinnedResTypeApp, ResID: strconv.FormatInt(appID, 10), ResName: "web-app"},
	}))
	resp, err = l.LaunchpadRecentList()
	require.NoError(t, err)
	list = respData(t, resp)["list"].([]domainlaunchpad.App)
	require.Len(t, list, 2)
	require.Equal(t, "web-app", list[0].Display)
	require.Equal(t, true, list[0].IsFavorite)
	require.Equal(t, false, list[1].IsFavorite)

	// 不可见/不存在应用被安全跳过（user2 是成员但缺角色；999999 不存在）
	mustCreateUser(t, svcCtx, 2, "user2")
	mustAddMember(t, svcCtx, ctx, projectID, 2, nil)
	mustRecordAppView(t, svcCtx, 2, appID2, time.Now().Add(-time.Hour))
	mustRecordAppView(t, svcCtx, 2, 999999, time.Now().Add(-30*time.Minute))
	user2Ctx := svctest.CtxWithUser(2)
	l2 := NewLaunchpadRecentListLogic(user2Ctx, svcCtx)
	resp, err = l2.LaunchpadRecentList()
	require.NoError(t, err)
	require.Empty(t, respData(t, resp)["list"].([]domainlaunchpad.App))
}

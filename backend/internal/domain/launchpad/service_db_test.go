package launchpad

// 测试目标：launchpad 领域服务（service.go）的 DB 集成路径。
// 覆盖的正常路径：Projects/Project/App/Members/Favorites/BatchAddFavorites/
// BatchDeleteFavorites/RecordAppView/RecentApps；用户组成员访问 Project/App 及权限撤销。
// 覆盖的异常情况：无权限访问项目（ErrNotFound）、App 不存在（ErrNotFound）、
// 缺少角色（ErrAppRoleRequired）、项目名必填、非法 userID。
// 说明：svctest 依赖 domain 包（import cycle），此处用 testkit 装配；
// 项目/成员/角色/应用均通过 repo 直接落库构造。

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func newTestLaunchpad(t *testing.T) (*Service, context.Context) {
	t.Helper()
	svc, _, ctx := newTestLaunchpadWithStore(t)
	return svc, ctx
}

func newTestLaunchpadWithStore(t *testing.T) (*Service, *repo.Store, context.Context) {
	t.Helper()
	store, _ := testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, UserName: "tier0"})
	return New(ctx, store), store, ctx
}

// setupProject 构造一个项目 + 应用 + 角色 + 成员，返回项目信息。
func setupProject(t *testing.T, ctx context.Context, userID int64) (int64, int64) {
	t.Helper()
	projects := repo.NewProjectRepo(ctx)
	project, err := projects.CreateProject(ctx, repo.Project{
		Name:        "Proj A",
		Description: "desc",
		OwnerUserID: userID,
	})
	require.NoError(t, err)
	app, err := projects.CreateProjectApp(ctx, repo.ProjectApp{
		ProjectID: project.ID,
		Name:      "App 1",
		Status:    "active",
	})
	require.NoError(t, err)
	role, err := projects.UpsertProjectRole(ctx, repo.ProjectRole{
		ProjectID: project.ID,
		RoleKey:   "viewer",
		Name:      "Viewer",
	})
	require.NoError(t, err)
	// 应用绑定到角色
	require.NoError(t, projects.InsertProjectRoleApps(ctx, project.ID, role.ID, []int64{app.ID}))
	// 用户成为成员并挂角色
	_, err = projects.UpsertProjectMember(ctx, repo.ProjectMemberSave{
		ProjectID: project.ID,
		UserID:    userID,
		RoleIDs:   []int64{role.ID},
	})
	require.NoError(t, err)
	return project.ID, app.ID
}

func TestLaunchpadProjects(t *testing.T) {
	svc, ctx := newTestLaunchpad(t)
	projectID, appID := setupProject(t, ctx, 1)

	projects, err := svc.Projects(ctx, 1, "")
	require.NoError(t, err)
	require.Len(t, projects, 1)
	require.Equal(t, projectID, projects[0].ProjectID)
	require.Len(t, projects[0].Apps, 1)
	require.Equal(t, appID, projects[0].Apps[0].AppID)
	require.True(t, projects[0].CanEdit)

	// 关键字过滤
	projects, err = svc.Projects(ctx, 1, "nope")
	require.NoError(t, err)
	require.Empty(t, projects)

	// 非成员看不到
	projects, err = svc.Projects(ctx, 999999, "")
	require.NoError(t, err)
	require.Empty(t, projects)
}

func TestLaunchpadProjectDetail(t *testing.T) {
	svc, ctx := newTestLaunchpad(t)
	projectID, appID := setupProject(t, ctx, 1)

	resp, err := svc.Project(ctx, 1, projectID, "")
	require.NoError(t, err)
	require.Equal(t, true, resp["canEdit"])
	apps := resp["apps"].([]App)
	require.Len(t, apps, 1)
	require.Equal(t, appID, apps[0].AppID)

	// 非成员访问 → ErrNotFound
	_, err = svc.Project(ctx, 999999, projectID, "")
	require.ErrorIs(t, err, ErrNotFound)
	// 项目不存在
	_, err = svc.Project(ctx, 1, 999999, "")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestLaunchpadApp(t *testing.T) {
	svc, ctx := newTestLaunchpad(t)
	projectID, appID := setupProject(t, ctx, 1)

	resp, err := svc.App(ctx, 1, projectID, appID)
	require.NoError(t, err)
	require.Equal(t, appID, resp["app"].(App).AppID)

	// 非成员 → ErrNotFound
	_, err = svc.App(ctx, 999999, projectID, appID)
	require.ErrorIs(t, err, ErrNotFound)
	// App 不存在
	_, err = svc.App(ctx, 1, projectID, 999999)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestLaunchpadAppRoleRequired(t *testing.T) {
	svc, ctx := newTestLaunchpad(t)
	projects := repo.NewProjectRepo(ctx)

	project, err := projects.CreateProject(ctx, repo.Project{Name: "P2", OwnerUserID: 1})
	require.NoError(t, err)
	app, err := projects.CreateProjectApp(ctx, repo.ProjectApp{ProjectID: project.ID, Name: "A2", Status: "active"})
	require.NoError(t, err)
	role, err := projects.UpsertProjectRole(ctx, repo.ProjectRole{ProjectID: project.ID, RoleKey: "admin", Name: "Admin"})
	require.NoError(t, err)
	// 应用绑定到 admin 角色
	require.NoError(t, projects.InsertProjectRoleApps(ctx, project.ID, role.ID, []int64{app.ID}))
	// 成员但无该角色
	_, err = projects.UpsertProjectMember(ctx, repo.ProjectMemberSave{ProjectID: project.ID, UserID: 1, RoleIDs: nil})
	require.NoError(t, err)

	// 成员可见应用列表为空
	apps, err := svc.apps(ctx, project, 1, "")
	require.NoError(t, err)
	require.Empty(t, apps)

	// App 详情 → ErrAppRoleRequired（应用存在且 active 但角色不匹配）
	_, err = svc.App(ctx, 1, project.ID, app.ID)
	require.ErrorIs(t, err, ErrAppRoleRequired)
}

func TestLaunchpadUserGroupAccessLifecycle(t *testing.T) {
	svc, store, ctx := newTestLaunchpadWithStore(t)
	projects := repo.NewProjectRepo(ctx)

	project, err := projects.CreateProject(ctx, repo.Project{Name: "Group Access", OwnerUserID: 1})
	require.NoError(t, err)
	app, err := projects.CreateProjectApp(ctx, repo.ProjectApp{ProjectID: project.ID, Name: "Group App", Status: "active"})
	require.NoError(t, err)
	role, err := projects.UpsertProjectRole(ctx, repo.ProjectRole{ProjectID: project.ID, RoleKey: "viewer", Name: "Viewer"})
	require.NoError(t, err)
	require.NoError(t, projects.InsertProjectRoleApps(ctx, project.ID, role.ID, []int64{app.ID}))

	const groupUserID int64 = 2001
	require.NoError(t, store.CommonDB().Create(&repo.User{
		ID: groupUserID, UserName: "group-user", NickName: "Group User", Password: "x", Status: 1,
	}).Error)
	groups := repo.NewUserGroupRepo(ctx)
	group, err := groups.Create(ctx, "launchpad-access-group", "", 1)
	require.NoError(t, err)
	require.NoError(t, groups.AddMember(ctx, group.ID, groupUserID, 1))
	projectGroup, err := projects.UpsertProjectUserGroup(ctx, repo.ProjectUserGroupSave{
		ProjectID: project.ID, GroupID: group.ID, RoleIDs: []int64{role.ID}, UserID: 1,
	})
	require.NoError(t, err)

	visibleProjects, err := svc.Projects(ctx, groupUserID, "")
	require.NoError(t, err)
	require.Len(t, visibleProjects, 1)
	_, err = svc.Project(ctx, groupUserID, project.ID, "")
	require.NoError(t, err)
	_, err = svc.App(ctx, groupUserID, project.ID, app.ID)
	require.NoError(t, err)
	require.NoError(t, svc.RecordAppView(ctx, groupUserID, app.ID))

	require.NoError(t, groups.ReplaceMembers(ctx, group.ID, nil, 1))
	visibleProjects, err = svc.Projects(ctx, groupUserID, "")
	require.NoError(t, err)
	require.Empty(t, visibleProjects)
	_, err = svc.Project(ctx, groupUserID, project.ID, "")
	require.ErrorIs(t, err, ErrNotFound)
	_, err = svc.App(ctx, groupUserID, project.ID, app.ID)
	require.ErrorIs(t, err, ErrNotFound)
	require.ErrorIs(t, svc.RecordAppView(ctx, groupUserID, app.ID), ErrNotFound)

	require.NoError(t, groups.AddMember(ctx, group.ID, groupUserID, 1))
	_, err = svc.Project(ctx, groupUserID, project.ID, "")
	require.NoError(t, err)
	require.NoError(t, projects.DeleteProjectUserGroup(ctx, project.ID, projectGroup.ID))
	_, err = svc.Project(ctx, groupUserID, project.ID, "")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestLaunchpadMembers(t *testing.T) {
	svc, ctx := newTestLaunchpad(t)
	setupProject(t, ctx, 1)

	// 项目名必填
	_, err := svc.Members(ctx, MembersQuery{})
	require.Error(t, err)

	// 正常查询
	page, err := svc.Members(ctx, MembersQuery{ProjectName: "Proj A"})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Len(t, page.List, 1)
	require.Equal(t, "1", page.List[0].UserID)
	require.NotEmpty(t, page.List[0].Roles)

	// 按角色过滤
	page, err = svc.Members(ctx, MembersQuery{ProjectName: "Proj A", RoleKey: "viewer"})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	page, err = svc.Members(ctx, MembersQuery{ProjectName: "Proj A", RoleKey: "nobody"})
	require.NoError(t, err)
	require.Zero(t, page.Total)

	// 项目不存在
	_, err = svc.Members(ctx, MembersQuery{ProjectName: "No Such"})
	require.ErrorIs(t, err, ErrNotFound)
}

func TestLaunchpadFavorites(t *testing.T) {
	svc, ctx := newTestLaunchpad(t)

	// 非法 userID
	_, err := svc.Favorites(ctx, 0, "app")
	require.Error(t, err)
	require.Error(t, svc.BatchAddFavorites(ctx, 0, nil))
	require.Error(t, svc.BatchDeleteFavorites(ctx, 0, nil))
	require.Error(t, svc.RecordAppView(ctx, 0, 1))

	// 批量添加
	items := []repo.UserFavorite{
		{ResType: "app", ResID: "1", ResName: "App 1"},
		{ResType: "app", ResID: "2", ResName: "App 2"},
	}
	require.NoError(t, svc.BatchAddFavorites(ctx, 1, items))

	// 列表
	favorites, err := svc.Favorites(ctx, 1, "app")
	require.NoError(t, err)
	require.Len(t, favorites, 2)

	// 按类型过滤
	require.Empty(t, mustFavorites(t, svc, ctx, 1, "project"))

	// 批量删除
	require.NoError(t, svc.BatchDeleteFavorites(ctx, 1, items))
	favorites, err = svc.Favorites(ctx, 1, "app")
	require.NoError(t, err)
	require.Empty(t, favorites)
}

func mustFavorites(t *testing.T, svc *Service, ctx context.Context, userID int64, resType string) []UserFavoriteVO {
	t.Helper()
	out, err := svc.Favorites(ctx, userID, resType)
	require.NoError(t, err)
	return out
}

func TestLaunchpadRecordAppView(t *testing.T) {
	svc, ctx := newTestLaunchpad(t)
	projectID, appID := setupProject(t, ctx, 1)

	// 合法访问
	require.NoError(t, svc.RecordAppView(ctx, 1, appID))

	// App 不存在
	require.ErrorIs(t, svc.RecordAppView(ctx, 1, 999999), ErrNotFound)

	// 非成员
	require.ErrorIs(t, svc.RecordAppView(ctx, 999999, appID), ErrNotFound)

	// 停用 App
	projects := repo.NewProjectRepo(ctx)
	require.NoError(t, projects.UpdateProjectAppStatus(ctx, projectID, appID, "disabled", 1))
	require.ErrorIs(t, svc.RecordAppView(ctx, 1, appID), ErrNotFound)
}

func TestLaunchpadRecentApps(t *testing.T) {
	svc, ctx := newTestLaunchpad(t)
	_, appID := setupProject(t, ctx, 1)

	// 无浏览记录 → 空
	apps, err := svc.RecentApps(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, apps)

	// 非法 userID
	_, err = svc.RecentApps(ctx, 0)
	require.Error(t, err)

	// 造一条 App View 审计记录（res_id 为 appID）
	audit := repo.NewAuditRepo(ctx)
	require.NoError(t, audit.InsertAuditLog(ctx, repo.AuditLog{
		UserID:           1,
		ResType:          "ProjectApp",
		ResID:            itoa(appID),
		BusinessTypeCode: repo.AuditBizView,
		Code:             200,
	}))
	apps, err = svc.RecentApps(ctx, 1)
	require.NoError(t, err)
	require.Len(t, apps, 1)
	require.Equal(t, appID, apps[0].AppID)
}

// TestLaunchpadRecentApps_PinnedWithoutView 置顶但从未打开过的 App 也要出现在
// 置顶与最近使用列表中，且排在未置顶的最近打开应用之前。
func TestLaunchpadRecentApps_PinnedWithoutView(t *testing.T) {
	svc, ctx := newTestLaunchpad(t)
	projectID, viewedAppID := setupProject(t, ctx, 1)

	// 同项目再建一个 App:只置顶,不打开
	projects := repo.NewProjectRepo(ctx)
	pinnedApp, err := projects.CreateProjectApp(ctx, repo.ProjectApp{
		ProjectID: projectID,
		AppKey:    "app-pinned-only",
		Name:      "App Pinned Only",
		Status:    "active",
	})
	require.NoError(t, err)

	require.NoError(t, svc.BatchAddFavorites(ctx, 1, []repo.UserFavorite{{
		ResType: PinnedResTypeApp,
		ResID:   itoa(pinnedApp.ID),
		ResName: pinnedApp.Name,
	}}))

	// 另一个 App 只打开,不置顶
	audit := repo.NewAuditRepo(ctx)
	require.NoError(t, audit.InsertAuditLog(ctx, repo.AuditLog{
		UserID:           1,
		ResType:          "ProjectApp",
		ResID:            itoa(viewedAppID),
		BusinessTypeCode: repo.AuditBizView,
		Code:             200,
	}))

	apps, err := svc.RecentApps(ctx, 1)
	require.NoError(t, err)
	require.Len(t, apps, 2)
	require.Equal(t, pinnedApp.ID, apps[0].AppID)
	require.True(t, apps[0].IsFavorite)
	require.Equal(t, viewedAppID, apps[1].AppID)
	require.False(t, apps[1].IsFavorite)
}

func itoa(value int64) string {
	return strconv.FormatInt(value, 10)
}

func TestLaunchpadErrGuard(t *testing.T) {
	_ = errors.Is
	require.NotNil(t, ErrNotFound)
	require.NotNil(t, ErrAppRoleRequired)
}

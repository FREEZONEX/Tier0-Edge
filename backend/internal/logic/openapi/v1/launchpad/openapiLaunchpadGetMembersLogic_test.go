// openapiLaunchpadGetMembersLogic_test.go — OpenapiLaunchpadGetMembersLogic 测试。
//
// 测试目标：OpenAPI 查询 Launchpad 项目成员列表入口。
// 正常路径：项目存在且有成员时返回分页列表；直接成员与用户组成员合并去重并取角色并集；
//
//	无成员项目返回空列表。
//
// 异常情况：项目名缺失返回 400；项目不存在返回 404；
//
//	非法 updatedAtStart 时间串返回 400。
package launchpad

import (
	"testing"

	domainlaunchpad "backend/internal/domain/launchpad"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiLaunchpadGetMembers_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	project := seedLaunchpadProject(t, svcCtx, "demo-project")
	seedLaunchpadMember(t, svcCtx, project.ID, 1001, "alice")

	l := NewOpenapiLaunchpadGetMembersLogic(ctx, svcCtx)
	resp, err := l.OpenapiLaunchpadGetMembers(&types.LaunchpadGetMembersReq{
		ProjectName: "demo-project",
		Page:        1,
		Size:        10,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// 注意：logic 直接 Envelope(domain 返回的 launchpad.MembersPage 结构体)，
	// 不是 map[string]any；UserID 为 string 格式。
	data, ok := resp.Data.(domainlaunchpad.MembersPage)
	require.True(t, ok)
	require.Equal(t, int64(1), data.Total)
	require.Len(t, data.List, 1)
	require.Equal(t, "1001", data.List[0].UserID)
	require.Equal(t, "alice", data.List[0].UserName)
}

func TestOpenapiLaunchpadGetMembers_UserGroupAggregation(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	project := seedLaunchpadProject(t, svcCtx, "group-members-project")
	seedLaunchpadUser(t, svcCtx, 1001, "direct-and-group")
	seedLaunchpadUser(t, svcCtx, 1002, "group-only")
	projects := repo.NewProjectRepo(ctx)
	viewer, err := projects.UpsertProjectRole(ctx, repo.ProjectRole{
		ProjectID: project.ID, RoleKey: "viewer", Name: "Viewer",
	})
	require.NoError(t, err)
	editor, err := projects.UpsertProjectRole(ctx, repo.ProjectRole{
		ProjectID: project.ID, RoleKey: "editor", Name: "Editor",
	})
	require.NoError(t, err)
	_, err = projects.UpsertProjectMember(ctx, repo.ProjectMemberSave{
		ProjectID: project.ID, UserID: 1001, RoleIDs: []int64{viewer.ID},
	})
	require.NoError(t, err)

	groups := repo.NewUserGroupRepo(ctx)
	group, err := groups.Create(ctx, "openapi-members-group", "", 1)
	require.NoError(t, err)
	require.NoError(t, groups.AddMember(ctx, group.ID, 1001, 1))
	require.NoError(t, groups.AddMember(ctx, group.ID, 1002, 1))
	_, err = projects.UpsertProjectUserGroup(ctx, repo.ProjectUserGroupSave{
		ProjectID: project.ID, GroupID: group.ID, RoleIDs: []int64{viewer.ID, editor.ID}, UserID: 1,
	})
	require.NoError(t, err)

	l := NewOpenapiLaunchpadGetMembersLogic(ctx, svcCtx)
	resp, err := l.OpenapiLaunchpadGetMembers(&types.LaunchpadGetMembersReq{
		ProjectName: project.Name, Page: 1, Size: 10,
	})
	require.NoError(t, err)
	page := resp.Data.(domainlaunchpad.MembersPage)
	require.Equal(t, int64(2), page.Total)
	require.Len(t, page.List, 2)

	byUserID := make(map[string]domainlaunchpad.Member, len(page.List))
	for _, member := range page.List {
		byUserID[member.UserID] = member
	}
	require.ElementsMatch(t, []string{"viewer", "editor"}, launchpadMemberRoleKeys(byUserID["1001"].Roles))
	require.ElementsMatch(t, []string{"viewer", "editor"}, launchpadMemberRoleKeys(byUserID["1002"].Roles))
	require.Len(t, byUserID["1001"].Roles, 2, "direct and group roles must be de-duplicated")

	resp, err = l.OpenapiLaunchpadGetMembers(&types.LaunchpadGetMembersReq{
		ProjectName: project.Name, Roles: []string{"editor"}, Page: 1, Size: 1,
	})
	require.NoError(t, err)
	page = resp.Data.(domainlaunchpad.MembersPage)
	require.Equal(t, int64(2), page.Total)
	require.Len(t, page.List, 1)
}

func TestOpenapiLaunchpadGetMembers_EmptyProject(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	seedLaunchpadProject(t, svcCtx, "empty-project")

	l := NewOpenapiLaunchpadGetMembersLogic(ctx, svcCtx)
	resp, err := l.OpenapiLaunchpadGetMembers(&types.LaunchpadGetMembersReq{
		ProjectName: "empty-project",
	})
	require.NoError(t, err)
	data := resp.Data.(domainlaunchpad.MembersPage)
	require.Equal(t, int64(0), data.Total)
	require.Empty(t, data.List)
}

func TestOpenapiLaunchpadGetMembers_MissingProjectName(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewOpenapiLaunchpadGetMembersLogic(ctx, svcCtx)
	_, err := l.OpenapiLaunchpadGetMembers(&types.LaunchpadGetMembersReq{ProjectName: "  "})
	requireHTTPStatus(t, err, 400)
}

func TestOpenapiLaunchpadGetMembers_ProjectNotFound(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewOpenapiLaunchpadGetMembersLogic(ctx, svcCtx)
	_, err := l.OpenapiLaunchpadGetMembers(&types.LaunchpadGetMembersReq{ProjectName: "no-such-project"})
	requireHTTPStatus(t, err, 404)
}

func TestOpenapiLaunchpadGetMembers_InvalidUpdatedAtStart(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	seedLaunchpadProject(t, svcCtx, "time-project")

	l := NewOpenapiLaunchpadGetMembersLogic(ctx, svcCtx)
	_, err := l.OpenapiLaunchpadGetMembers(&types.LaunchpadGetMembersReq{
		ProjectName:    "time-project",
		UpdatedAtStart: "not-a-time",
	})
	requireHTTPStatus(t, err, 400)
}

// seedLaunchpadProject 直插 project 表（launchpad.Members 经 GetProjectByNameOrID 查询）。
func seedLaunchpadProject(t *testing.T, svcCtx *svc.ServiceContext, name string) repo.Project {
	t.Helper()
	project := repo.Project{ProjectKey: "key-" + name, Name: name, Status: "active"}
	require.NoError(t, svcCtx.App.Store.CommonDB().Create(&project).Error)
	return project
}

// seedLaunchpadMember 直插 project_member（launchpad.Members 经 ListProjectMembers 查询，
// user_name 来自 LEFT JOIN sys_user_info，email 留空避免触发联系人密文解密）。
func seedLaunchpadMember(t *testing.T, svcCtx *svc.ServiceContext, projectID, userID int64, userName string) int64 {
	t.Helper()
	seedLaunchpadUser(t, svcCtx, userID, userName)
	store := svcCtx.App.Store.CommonDB()
	var memberID int64
	require.NoError(t, store.Raw(
		`INSERT INTO project_member (project_id, user_id, status, created_time, updated_time)
		 VALUES (?, ?, 'active', 0, 0) RETURNING id`,
		projectID, userID).Scan(&memberID).Error)
	return memberID
}

func seedLaunchpadUser(t *testing.T, svcCtx *svc.ServiceContext, userID int64, userName string) {
	t.Helper()
	require.NoError(t, svcCtx.App.Store.CommonDB().Exec(
		`INSERT INTO sys_user_info (user_id, user_name, nick_name, password, status)
		 VALUES (?, ?, '', 'x', 1)
		 ON CONFLICT (user_id) DO NOTHING`,
		userID, userName).Error)
}

func launchpadMemberRoleKeys(roles []domainlaunchpad.MemberRole) []string {
	keys := make([]string, 0, len(roles))
	for _, role := range roles {
		keys = append(keys, role.RoleKey)
	}
	return keys
}

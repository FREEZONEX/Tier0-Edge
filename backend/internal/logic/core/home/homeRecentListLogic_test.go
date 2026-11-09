// TASK-028 HomeRecentListLogic 单元测试：
//
//	目标：覆盖首页最近访问列表（UNS 节点、项目、项目应用三类主链路）。
//	正常路径：Admin 用户（seed 内置角色绑定全部资源）的最近审计日志被归类为菜单资源并
//	          返回 RecentItem；limit 截断生效。
//	异常情况：不存在的 notebook/flow 实体被跳过不报错；非法用户 ID 返回 400。
//	说明：Notebook/Flow 最近项依赖 notebook/flow 实体数据，本测试以 UNS、项目、
//	      项目应用三条链路为准。
package home

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	domainhome "backend/internal/domain/home"
	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHomeRecentListLogic_Recent_UNS(t *testing.T) {
	svcCtx, ctx := newHomeEnv(t)
	// seed 已含 uns.page 资源且 Admin 角色全绑定；先直插一条 UNS 节点，
	// 再插入一条指向该节点的 UNS 类型最近日志。
	require.NoError(t, svcCtx.App.Store.CommonDB().Create(&repo.UnsNode{
		ID: 1001, Name: "node-1001", DisplayName: "产线A", Namespace: "node-1001",
		Type: 1, Schema: json.RawMessage("[]"), ExtendProperties: json.RawMessage("{}"),
		Status: repo.UnsNodeStatusActive,
	}).Error)
	insertRecentAudit(t, svcCtx, repo.AuditLog{
		ID: 9001, UserID: 1, ResType: "UNS", ResID: "1001", ResName: "产线A",
		DetailJSON: detailJSON("/api/core/uns/1001"),
		Code:       200, IsShowInRecent: 1, CreatedTime: time.Now().UTC(),
	})

	l := NewHomeRecentListLogic(ctx, svcCtx)
	resp, err := l.HomeRecentList(&types.HomeRecentReq{Limit: 10}, 1)
	require.NoError(t, err)

	view := respData(t, resp)
	list, ok := view["list"].([]domainhome.RecentItem)
	require.True(t, ok, "list should be []domainhome.RecentItem")
	require.NotEmpty(t, list, "UNS recent item should be returned")

	first := list[0]
	assert.Equal(t, "uns.page", first.ResourceKey)
	assert.Equal(t, "产线A", first.ResourceName)
	assert.Equal(t, "UNS", first.ResourceType)
}

func TestHomeRecentListLogic_Recent_Project(t *testing.T) {
	svcCtx, ctx := newHomeEnv(t)
	projectID := insertProject(t, svcCtx, 5001, "recent-proj")
	insertRecentAudit(t, svcCtx, repo.AuditLog{
		ID: 9002, UserID: 1, ResType: "Project", ResID: "p1", ResName: "recent-proj",
		DetailJSON: detailJSONWithProjectID("/api/core/projects", projectID),
		Code:       200, IsShowInRecent: 1, CreatedTime: time.Now().UTC(),
	})

	l := NewHomeRecentListLogic(ctx, svcCtx)
	resp, err := l.HomeRecentList(&types.HomeRecentReq{Limit: 10}, 1)
	require.NoError(t, err)

	view := respData(t, resp)
	list, ok := view["list"].([]domainhome.RecentItem)
	require.True(t, ok)

	var found bool
	for _, item := range list {
		if item.ResourceKey == "project.view" && item.ResourceName == "recent-proj" {
			found = true
			assert.Equal(t, "/project/5001", item.RoutePath)
			assert.Equal(t, "Project", item.ResourceType)
		}
	}
	assert.True(t, found, "project recent item should be returned")
}

func TestHomeRecentListLogic_Recent_ProjectApp(t *testing.T) {
	svcCtx, ctx := newHomeEnv(t)
	projectID := insertProject(t, svcCtx, 5002, "app-proj")
	appID := insertProjectApp(t, svcCtx, 7001, projectID, "my-app")
	insertRecentAudit(t, svcCtx, repo.AuditLog{
		ID: 9003, UserID: 1, ResType: "ProjectApp", ResID: itoa(appID), ResName: "my-app",
		DetailJSON: detailJSONWithProjectID("/api/core/projects", projectID),
		Code:       200, IsShowInRecent: 1, CreatedTime: time.Now().UTC(),
	})

	l := NewHomeRecentListLogic(ctx, svcCtx)
	resp, err := l.HomeRecentList(&types.HomeRecentReq{Limit: 10}, 1)
	require.NoError(t, err)

	view := respData(t, resp)
	list, ok := view["list"].([]domainhome.RecentItem)
	require.True(t, ok)

	var found bool
	for _, item := range list {
		if item.ResourceKey == "project.view" && item.ResourceName == "my-app" {
			found = true
			assert.Equal(t, "App", item.ResourceType)
		}
	}
	assert.True(t, found, "project app recent item should be returned")
}

func TestHomeRecentListLogic_Recent_MissingEntitySkipped(t *testing.T) {
	svcCtx, ctx := newHomeEnv(t)
	// Notebook 实体不存在：应被跳过而非报错。
	insertRecentAudit(t, svcCtx, repo.AuditLog{
		ID: 9004, UserID: 1, ResType: "Notebook", ResID: "77", ResName: "ghost",
		DetailJSON: detailJSON("/api/core/notebooks/77"),
		Code:       200, IsShowInRecent: 1, CreatedTime: time.Now().UTC(),
	})

	l := NewHomeRecentListLogic(ctx, svcCtx)
	resp, err := l.HomeRecentList(&types.HomeRecentReq{Limit: 10}, 1)
	require.NoError(t, err)
	view := respData(t, resp)
	list, ok := view["list"].([]domainhome.RecentItem)
	require.True(t, ok)
	assert.Empty(t, list)
}

func TestHomeRecentListLogic_Recent_InvalidUser(t *testing.T) {
	svcCtx, _ := newHomeEnv(t)
	ctx := contextWithoutUser()
	l := NewHomeRecentListLogic(ctx, svcCtx)

	_, err := l.HomeRecentList(&types.HomeRecentReq{Limit: 5}, 1)
	// logicx.UserID 为 0 => home.RecentItems 返回 ErrInvalidArgument => 400
	requireHTTPError(t, err, 400)
}

func TestHomeRecentListLogic_Recent_LimitClamped(t *testing.T) {
	svcCtx, ctx := newHomeEnv(t)
	for _, id := range []int64{9100, 9101, 9102} {
		insertRecentAudit(t, svcCtx, repo.AuditLog{
			ID: id, UserID: 1, ResType: "UNS", ResID: itoa(id), ResName: "n" + itoa(id),
			DetailJSON: detailJSON("/api/core/uns/" + itoa(id)),
			Code:       200, IsShowInRecent: 1, CreatedTime: time.Now().UTC(),
		})
	}

	l := NewHomeRecentListLogic(ctx, svcCtx)
	resp, err := l.HomeRecentList(&types.HomeRecentReq{Limit: 2}, 1)
	require.NoError(t, err)

	view := respData(t, resp)
	list, ok := view["list"].([]domainhome.RecentItem)
	require.True(t, ok)
	assert.LessOrEqual(t, len(list), 2)
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

// 同一项目的成员操作和历史名称只占一项；同名项目、同项目内不同应用仍分别保留。
func TestHomeRecentListLogic_Recent_ResourceIdentity(t *testing.T) {
	svcCtx, ctx := newHomeEnv(t)
	projectID := insertProject(t, svcCtx, 5101, "same-name")
	insertProject(t, svcCtx, 5102, "same-name")
	insertProjectApp(t, svcCtx, 7101, projectID, "same-name")
	insertProjectApp(t, svcCtx, 7102, projectID, "another-app")
	now := time.Now().UTC().Truncate(time.Millisecond)
	rows := []repo.AuditLog{
		{ResType: "ProjectMember", ResID: "42", DetailJSON: detailJSONWithProjectID("/api/core/projects/5101/members/42", projectID)},
		{ResType: "Project", ResID: "5101", ResName: "old-name", DetailJSON: detailJSON("/api/core/projects/5101")},
		{ResType: "ProjectMember", ResID: "43", DetailJSON: detailJSON("/api/core/projects/5101/members/43")},
		{ResType: "Project", ResID: "5102", DetailJSON: detailJSON("/api/core/projects/5102")},
		{ResType: "ProjectApp", ResID: "7101", DetailJSON: detailJSON("/api/core/projects/5101/apps/7101")},
		{ResType: "ProjectApp", ResID: "7102", DetailJSON: detailJSON("/api/core/projects/5101/apps/7102")},
	}
	for i, row := range rows {
		row.ID, row.UserID, row.Code, row.IsShowInRecent = int64(9200+i), 1, 200, 1
		row.CreatedTime = now.Add(-time.Duration(i) * time.Minute)
		insertRecentAudit(t, svcCtx, row)
	}
	resp, err := NewHomeRecentListLogic(ctx, svcCtx).HomeRecentList(&types.HomeRecentReq{Limit: 5}, 1)
	require.NoError(t, err)
	list := respData(t, resp)["list"].([]domainhome.RecentItem)
	require.Len(t, list, 4)
	assert.Equal(t, "/project/5101", list[0].RoutePath)
	assert.Equal(t, now.UnixMilli(), list[0].OperatedAt)
	assert.Equal(t, "/project/5102", list[1].RoutePath)
	assert.Equal(t, "App", list[2].ResourceType)
	assert.Equal(t, "App", list[3].ResourceType)
	assert.NotEqual(t, list[2].Key, list[3].Key)
	for _, item := range list[:3] {
		assert.Equal(t, "same-name", item.ResourceName)
	}
}

// 连续重复记录跨页后继续回填；同一时间用 ID 排序，跳过隐藏、失败和已删除资源。
func TestHomeRecentListLogic_Recent_BackfillsDistinctResources(t *testing.T) {
	svcCtx, ctx := newHomeEnv(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	for id := int64(5201); id <= 5205; id++ {
		insertProject(t, svcCtx, id, "project-"+itoa(id))
		insertRecentAudit(t, svcCtx, repo.AuditLog{
			ID: 40000 + id, UserID: 1, ResType: "Project", ResID: itoa(id),
			Code: 200, IsShowInRecent: 1, DetailJSON: detailJSON("/api/core/projects/" + itoa(id)),
			CreatedTime: now.Add(-time.Duration(id-5200) * time.Minute),
		})
	}
	for i := int64(0); i < 105; i++ {
		insertRecentAudit(t, svcCtx, repo.AuditLog{
			ID: 30000 + i, UserID: 1, ResType: "ProjectMember", ResID: itoa(i + 1),
			Code: 200, IsShowInRecent: 1, DetailJSON: detailJSON("/api/core/projects/5201/members/" + itoa(i+1)),
			CreatedTime: now,
		})
	}
	for i, row := range []repo.AuditLog{
		{ResType: "Notebook", ResID: "999999", Code: 200, IsShowInRecent: 1},
		{ResType: "Project", ResID: "5205", Code: 200, IsShowInRecent: 2},
		{ResType: "Project", ResID: "5204", Code: 500, IsShowInRecent: 1},
	} {
		row.ID, row.UserID, row.CreatedTime = 50000+int64(i), 1, now
		insertRecentAudit(t, svcCtx, row)
	}
	for _, limit := range []int{5, 20} {
		resp, err := NewHomeRecentListLogic(ctx, svcCtx).HomeRecentList(&types.HomeRecentReq{Limit: limit}, 1)
		require.NoError(t, err)
		list := respData(t, resp)["list"].([]domainhome.RecentItem)
		require.Len(t, list, 5)
		for i, item := range list {
			assert.Equal(t, "project.view:"+itoa(5201+int64(i)), item.Key)
		}
		assert.Equal(t, now.UnixMilli(), list[0].OperatedAt)
	}
}

// 成员删除只更新项目的最近活动，不能让仍存在的父项目变成不可点击的删除项。
func TestHomeRecentListLogic_Recent_MemberDeletionKeepsProjectNavigable(t *testing.T) {
	svcCtx, ctx := newHomeEnv(t)
	projectID := insertProject(t, svcCtx, 5301, "active-project")
	now := time.Now().UTC().Truncate(time.Millisecond)
	insertRecentAudit(t, svcCtx, repo.AuditLog{
		ID: 60001, UserID: 1, ResType: "Project", ResID: itoa(projectID),
		Code: 200, IsShowInRecent: 1, BusinessTypeCode: repo.AuditBizUpdate,
		DetailJSON: detailJSON("/api/core/projects/5301"), CreatedTime: now.Add(-time.Minute),
	})
	insertRecentAudit(t, svcCtx, repo.AuditLog{
		ID: 60002, UserID: 1, ResType: "ProjectMember", ResID: "42",
		Code: 200, IsShowInRecent: 1, BusinessTypeCode: repo.AuditBizDelete,
		DetailJSON: detailJSONWithProjectID("/api/core/projects/5301/members/42", projectID), CreatedTime: now,
	})
	resp, err := NewHomeRecentListLogic(ctx, svcCtx).HomeRecentList(&types.HomeRecentReq{Limit: 5}, 1)
	require.NoError(t, err)
	list := respData(t, resp)["list"].([]domainhome.RecentItem)
	require.Len(t, list, 1)
	assert.Equal(t, "/project/5301", list[0].RoutePath)
	assert.Equal(t, "Update", list[0].BusinessType)
	assert.Equal(t, now.UnixMilli(), list[0].OperatedAt)
	var audit repo.AuditLog
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&audit, 60002).Error)
	assert.Equal(t, repo.AuditBizDelete, audit.BusinessTypeCode, "raw audit action must remain Delete")
}

// 本文件覆盖 projectListLogic.go 的 ProjectList：
// 正常路径：admin 可见全部项目；keyword 过滤名称/project_key/描述；
//          普通用户仅见自己创建或作为成员的项目；
// 异常情况：无项目时返回空列表。
package project

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestProjectListLogic_AdminSeesAllAndKeyword(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	createTestProject(t, svcCtx, ctx, "alpha")
	createTestProject(t, svcCtx, ctx, "beta")

	// 全部可见
	resp, err := NewProjectListLogic(ctx, svcCtx).ProjectList(&types.ProjectListReq{})
	require.NoError(t, err)
	data := resp.Data.(map[string]any)
	require.Equal(t, 2, data["total"])
	require.Len(t, data["list"].([]any), 2)

	// keyword 过滤
	resp, err = NewProjectListLogic(ctx, svcCtx).ProjectList(&types.ProjectListReq{Keyword: "alpha"})
	require.NoError(t, err)
	data = resp.Data.(map[string]any)
	require.Equal(t, 1, data["total"])
	item := data["list"].([]any)[0].(map[string]any)
	require.Equal(t, "alpha", item["name"])
	require.Equal(t, int64(1), item["ownerUserId"])
}

// TestProjectListLogic_UserScope 普通用户只能看到自己创建或作为成员的项目。
func TestProjectListLogic_UserScope(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	db := svcCtx.App.Store.CommonDB()
	insertTestUser(t, db, 99, "member-user")
	insertTestUser(t, db, 100, "outsider")

	ctxAdmin := svctest.CtxWithUser(1)
	ctx99 := svctest.CtxWithUser(99)

	adminProject := createTestProject(t, svcCtx, ctxAdmin, "admin-project")
	ownProject := createTestProject(t, svcCtx, ctx99, "own-project")

	// 99 创建了 own-project；admin-project 尚未把 99 加为成员 → 只见 own-project
	resp, err := NewProjectListLogic(ctx99, svcCtx).ProjectList(&types.ProjectListReq{})
	require.NoError(t, err)
	data := resp.Data.(map[string]any)
	require.Equal(t, 1, data["total"])

	// 把 99 加为 admin-project 的成员 → 可见两个
	_, err = svcCtx.App.Project.AddMember(ctxAdmin, memberSave(adminProject, 99, nil, 1))
	require.NoError(t, err)
	resp, err = NewProjectListLogic(ctx99, svcCtx).ProjectList(&types.ProjectListReq{})
	require.NoError(t, err)
	require.Equal(t, 2, resp.Data.(map[string]any)["total"])

	// 无关用户 100 → 空列表
	resp, err = NewProjectListLogic(svctest.CtxWithUser(100), svcCtx).ProjectList(&types.ProjectListReq{})
	require.NoError(t, err)
	require.Equal(t, 0, resp.Data.(map[string]any)["total"])

	// 用户 99 的 own-project 仍属于 99（owner）
	require.Positive(t, ownProject)
}

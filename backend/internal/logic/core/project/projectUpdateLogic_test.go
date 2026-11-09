// 本文件覆盖 projectUpdateLogic.go 的 ProjectUpdate：
// 正常路径：项目创建者（非 admin）可改名/改描述；
// 异常情况：空名 → 400；项目不存在 → 404；平台 admin 与创建者之外的用户（含普通成员）→ 404。
package project

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestProjectUpdateLogic_OwnerCanUpdate(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	db := svcCtx.App.Store.CommonDB()
	insertTestUser(t, db, 99, "normal-user")

	ctx99 := svctest.CtxWithUser(99)
	projectID := createTestProject(t, svcCtx, ctx99, "owned-by-99")

	_, err := NewProjectUpdateLogic(ctx99, svcCtx).ProjectUpdate(&types.ProjectSaveReq{
		Id: projectID, Name: "renamed-by-owner", Description: "new desc",
	})
	require.NoError(t, err)

	var p repo.Project
	require.NoError(t, db.First(&p, projectID).Error)
	require.Equal(t, "renamed-by-owner", p.Name)
	require.Equal(t, "new desc", p.Description)
}

func TestProjectUpdateLogic_EmptyName(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctx, "target")

	for _, name := range []string{"", "   "} {
		_, err := NewProjectUpdateLogic(ctx, svcCtx).ProjectUpdate(&types.ProjectSaveReq{
			Id: projectID, Name: name,
		})
		require.Error(t, err)
		require.Equal(t, 400, httpErrStatus(err), "blank update name should be HTTP 400")
	}
}

func TestProjectUpdateLogic_NotExist(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	_, err := NewProjectUpdateLogic(ctx, svcCtx).ProjectUpdate(&types.ProjectSaveReq{
		Id: 999999, Name: "ghost",
	})
	require.Error(t, err)
	require.Equal(t, 404, httpErrStatus(err))
}

// TestProjectUpdateLogic_NonManagerRejected 权限语义：requireProjectManage 只认平台 admin 或项目创建者。
// 既非 admin 也非创建者的用户（即使已被加为成员）更新项目 → 404。
func TestProjectUpdateLogic_NonManagerRejected(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	db := svcCtx.App.Store.CommonDB()
	insertTestUser(t, db, 99, "member-user")
	insertTestUser(t, db, 100, "outsider")

	ctxAdmin := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctxAdmin, "admin-owned")

	// 99 被加为普通成员（非创建者）
	_, err := svcCtx.App.Project.AddMember(ctxAdmin, memberSave(projectID, 99, nil, 1))
	require.NoError(t, err)

	// 成员 99 与外部用户 100 都不能改名
	for _, userID := range []int64{99, 100} {
		_, err := NewProjectUpdateLogic(svctest.CtxWithUser(userID), svcCtx).ProjectUpdate(&types.ProjectSaveReq{
			Id: projectID, Name: "hijack",
		})
		require.Error(t, err)
		require.Equal(t, 404, httpErrStatus(err), "non-manager update should be HTTP 404")
	}

	// 项目未被改动
	var p repo.Project
	require.NoError(t, db.First(&p, projectID).Error)
	require.Equal(t, "admin-owned", p.Name)
}

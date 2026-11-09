// 本文件覆盖 projectDetailLogic.go 的 ProjectDetail：
// 正常路径：平台 admin 与项目成员可查看详情；
// 异常情况：项目不存在 → 404；非 admin/非创建者/非成员 → 403（requireProjectAccess 语义）。
package project

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestProjectDetailLogic_AdminSeesProject(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	projectID := createTestProject(t, svcCtx, ctx, "detail-me")

	resp, err := NewProjectDetailLogic(ctx, svcCtx).ProjectDetail(&types.IdReq{Id: projectID})
	require.NoError(t, err)
	project, ok := resp.Data.(map[string]any)["project"].(repo.Project)
	require.True(t, ok)
	require.Equal(t, projectID, project.ID)
	require.Equal(t, "detail-me", project.Name)
}

func TestProjectDetailLogic_NotExist(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	_, err := NewProjectDetailLogic(ctx, svcCtx).ProjectDetail(&types.IdReq{Id: 999999})
	require.Error(t, err)
	require.Equal(t, 404, httpErrStatus(err))
}

// TestProjectDetailLogic_MemberAccess 权限语义：非成员 → 403；加为成员后可查看。
func TestProjectDetailLogic_MemberAccess(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	db := svcCtx.App.Store.CommonDB()
	insertTestUser(t, db, 99, "member-user")

	ctxAdmin := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctxAdmin, "member-gated")

	// 加入前：普通用户 99 无访问权 → 403
	_, err := NewProjectDetailLogic(svctest.CtxWithUser(99), svcCtx).ProjectDetail(&types.IdReq{Id: projectID})
	require.Error(t, err)
	require.Equal(t, 403, httpErrStatus(err), "non-member detail should be HTTP 403")

	// admin 把 99 加为成员后：可访问
	_, err = svcCtx.App.Project.AddMember(ctxAdmin, memberSave(projectID, 99, nil, 1))
	require.NoError(t, err)
	_, err = NewProjectDetailLogic(svctest.CtxWithUser(99), svcCtx).ProjectDetail(&types.IdReq{Id: projectID})
	require.NoError(t, err)
}

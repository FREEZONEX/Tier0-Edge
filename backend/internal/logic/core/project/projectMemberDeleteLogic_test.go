// 本文件覆盖 projectMemberDeleteLogic.go：
// 现状：该文件是 goctl 脚手架占位（todo 未实现，恒返回 nil,nil），测试锁定当前占位契约；
// 实际成员移除能力在领域层 svcCtx.App.Project.DeleteMember，这里补测其 DB 行为
// （正常路径：删除成员后从列表消失、角色绑定被清；异常：成员不存在/项目不存在 → ErrNotFound、
//  非管理者 → ErrNotFound）。
package project

import (
	"errors"
	"testing"

	domainproject "backend/internal/domain/project"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestProjectMemberDeleteLogic_StubUnimplemented(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	resp, err := NewProjectMemberDeleteLogic(ctx, svcCtx).ProjectMemberDelete(&types.ProjectMemberReq{
		ProjectId: 1, MemberId: 1,
	})
	require.NoError(t, err)
	require.Nil(t, resp)
}

func TestProjectMemberDomain_DeleteMember(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	db := svcCtx.App.Store.CommonDB()
	insertTestUser(t, db, 99, "removed-member")

	ctxAdmin := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctxAdmin, "delete-member")

	role, err := svcCtx.App.Project.UpsertRole(ctxAdmin, projectID, "viewer", "Viewer", "view", 1)
	require.NoError(t, err)
	member, err := svcCtx.App.Project.AddMember(ctxAdmin, memberSave(projectID, 99, []int64{role.ID}, 1))
	require.NoError(t, err)

	require.NoError(t, svcCtx.App.Project.DeleteMember(ctxAdmin, projectID, member.ID, 1))

	members, err := svcCtx.App.Project.ListMembers(ctxAdmin, projectID)
	require.NoError(t, err)
	require.Len(t, members, 1, "only creator should remain")
	// 成员角色绑定一并删除
	var bindingCount int64
	require.NoError(t, db.Table("project_member_role").
		Where("project_id = ? AND member_id = ?", projectID, member.ID).Count(&bindingCount).Error)
	require.Zero(t, bindingCount)

	// 删除不存在的成员 → ErrNotFound
	err = svcCtx.App.Project.DeleteMember(ctxAdmin, projectID, member.ID, 1)
	require.True(t, errors.Is(err, domainproject.ErrNotFound))

	// 项目不存在 → ErrNotFound
	err = svcCtx.App.Project.DeleteMember(ctxAdmin, 999999, 1, 1)
	require.True(t, errors.Is(err, domainproject.ErrNotFound))

	// 非管理者 → ErrNotFound
	err = svcCtx.App.Project.DeleteMember(ctxAdmin, projectID, members[0].ID, 100)
	require.True(t, errors.Is(err, domainproject.ErrNotFound))
}

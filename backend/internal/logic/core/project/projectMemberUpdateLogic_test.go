// 本文件覆盖 projectMemberUpdateLogic.go：
// 现状：该文件是 goctl 脚手架占位（todo 未实现，恒返回 nil,nil），测试锁定当前占位契约；
// 实际成员角色变更能力在领域层 svcCtx.App.Project.UpdateMember，这里补测其 DB 行为
// （正常路径：更新成员角色；异常：MemberID=0 → ErrInvalid、成员不存在/项目不存在 → ErrNotFound、
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

func TestProjectMemberUpdateLogic_StubUnimplemented(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	resp, err := NewProjectMemberUpdateLogic(ctx, svcCtx).ProjectMemberUpdate(&types.ProjectMemberSaveReq{
		ProjectId: 1, MemberId: 1,
	})
	require.NoError(t, err)
	require.Nil(t, resp)
}

func TestProjectMemberDomain_UpdateMemberRoles(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	db := svcCtx.App.Store.CommonDB()
	insertTestUser(t, db, 99, "role-updated-member")

	ctxAdmin := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctxAdmin, "update-member-roles")

	viewer, err := svcCtx.App.Project.UpsertRole(ctxAdmin, projectID, "viewer", "Viewer", "view", 1)
	require.NoError(t, err)
	editor, err := svcCtx.App.Project.UpsertRole(ctxAdmin, projectID, "editor", "Editor", "edit", 1)
	require.NoError(t, err)

	member, err := svcCtx.App.Project.AddMember(ctxAdmin, memberSave(projectID, 99, []int64{viewer.ID}, 1))
	require.NoError(t, err)

	// 角色从 viewer 换成 editor
	updated, err := svcCtx.App.Project.UpdateMember(ctxAdmin, domainproject.MemberSaveCommand{
		ProjectID:      projectID,
		MemberID:       member.ID,
		RoleIDs:        []int64{editor.ID},
		OperatorUserID: 1,
	})
	require.NoError(t, err)
	require.Len(t, updated.Roles, 1)
	require.Equal(t, "editor", updated.Roles[0].RoleKey)

	// MemberID=0 → ErrInvalid
	_, err = svcCtx.App.Project.UpdateMember(ctxAdmin, domainproject.MemberSaveCommand{
		ProjectID: projectID, RoleIDs: nil, OperatorUserID: 1,
	})
	require.True(t, errors.Is(err, domainproject.ErrInvalid))

	// 成员不存在 → ErrNotFound
	_, err = svcCtx.App.Project.UpdateMember(ctxAdmin, domainproject.MemberSaveCommand{
		ProjectID: projectID, MemberID: 999999, RoleIDs: nil, OperatorUserID: 1,
	})
	require.True(t, errors.Is(err, domainproject.ErrNotFound))

	// 非管理者 → ErrNotFound
	_, err = svcCtx.App.Project.UpdateMember(ctxAdmin, domainproject.MemberSaveCommand{
		ProjectID: projectID, MemberID: member.ID, RoleIDs: nil, OperatorUserID: 100,
	})
	require.True(t, errors.Is(err, domainproject.ErrNotFound))
}

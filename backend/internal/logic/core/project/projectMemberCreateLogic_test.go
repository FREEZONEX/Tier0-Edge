// 本文件覆盖 projectMemberCreateLogic.go：
// 现状：该文件是 goctl 脚手架占位（todo 未实现，恒返回 nil,nil），测试锁定当前占位契约；
// 实际成员新增能力在领域层 svcCtx.App.Project.AddMember，这里补测其 DB 行为
// （正常路径：加成员 + 角色绑定；异常：用户不存在/UserID=0 → ErrInvalid、非管理者 → ErrNotFound）。
package project

import (
	"errors"
	"testing"

	domainproject "backend/internal/domain/project"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestProjectMemberCreateLogic_StubUnimplemented(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	// 占位实现：不落库、不报错，返回 (nil, nil)。逻辑层待实现（见报告）。
	resp, err := NewProjectMemberCreateLogic(ctx, svcCtx).ProjectMemberCreate(&types.ProjectMemberSaveReq{
		ProjectId: 1, UserId: "99",
	})
	require.NoError(t, err)
	require.Nil(t, resp)
}

func TestProjectMemberDomain_AddMemberFlow(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	db := svcCtx.App.Store.CommonDB()
	insertTestUser(t, db, 99, "new-member")
	insertTestUser(t, db, 100, "outsider")

	ctxAdmin := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctxAdmin, "add-member")

	// 建角色后加成员（带角色绑定）
	role, err := svcCtx.App.Project.UpsertRole(ctxAdmin, projectID, "viewer", "Viewer", "view", 1)
	require.NoError(t, err)
	member, err := svcCtx.App.Project.AddMember(ctxAdmin, memberSave(projectID, 99, []int64{role.ID}, 1))
	require.NoError(t, err)
	require.Equal(t, int64(99), member.UserID)
	require.Len(t, member.Roles, 1)
	require.Equal(t, "viewer", member.Roles[0].RoleKey)

	// 用户不存在 → ErrNotFound
	_, err = svcCtx.App.Project.AddMember(ctxAdmin, memberSave(projectID, 55555, nil, 1))
	require.Error(t, err)
	require.True(t, errors.Is(err, domainproject.ErrNotFound))

	// UserID=0 → ErrInvalid
	_, err = svcCtx.App.Project.AddMember(ctxAdmin, memberSave(projectID, 0, nil, 1))
	require.True(t, errors.Is(err, domainproject.ErrInvalid))

	// 非管理者（既非 admin 也非创建者）→ ErrNotFound
	_, err = svcCtx.App.Project.AddMember(ctxAdmin, memberSave(projectID, 99, nil, 100))
	require.True(t, errors.Is(err, domainproject.ErrNotFound))
}

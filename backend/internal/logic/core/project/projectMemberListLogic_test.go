// 本文件覆盖 projectMemberListLogic.go：
// 现状：该文件是 goctl 脚手架占位（todo 未实现，恒返回 nil,nil），测试锁定当前占位契约；
// 实际成员列表能力在领域层 svcCtx.App.Project.ListMembers，这里补测其 DB 行为
// （正常路径：创建者 + 新增成员都在列表；异常：项目不存在 → ErrNotFound）。
package project

import (
	"errors"
	"testing"

	domainproject "backend/internal/domain/project"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestProjectMemberListLogic_StubUnimplemented(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	resp, err := NewProjectMemberListLogic(ctx, svcCtx).ProjectMemberList(&types.ProjectAppReq{ProjectId: 1})
	require.NoError(t, err)
	require.Nil(t, resp)
}

func TestProjectMemberDomain_ListMembers(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	db := svcCtx.App.Store.CommonDB()
	insertTestUser(t, db, 99, "listed-member")

	ctxAdmin := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctxAdmin, "list-members")

	// 创建者默认在成员列表（user 1）
	members, err := svcCtx.App.Project.ListMembers(ctxAdmin, projectID)
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, int64(1), members[0].UserID)

	// 新增成员后出现在列表
	_, err = svcCtx.App.Project.AddMember(ctxAdmin, memberSave(projectID, 99, nil, 1))
	require.NoError(t, err)
	members, err = svcCtx.App.Project.ListMembers(ctxAdmin, projectID)
	require.NoError(t, err)
	require.Len(t, members, 2)

	// 项目不存在 → ErrNotFound
	_, err = svcCtx.App.Project.ListMembers(ctxAdmin, 999999)
	require.True(t, errors.Is(err, domainproject.ErrNotFound))
}

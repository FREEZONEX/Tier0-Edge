// 本文件覆盖 projectUnsListLogic.go：
// 现状：该文件是 goctl 脚手架占位（todo 未实现，恒返回 nil,nil），测试锁定当前占位契约；
// 实际 UNS 列表能力在领域层 svcCtx.App.Project.ListUns，这里补测其 DB 行为
// （正常路径：首次查询创建项目 UNS 根节点（alias=project_<id>_root），子树包含根下子节点；
//  项目不存在 → ErrNotFound）。
package project

import (
	"errors"
	"fmt"
	"testing"

	domainproject "backend/internal/domain/project"
	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestProjectUnsListLogic_StubUnimplemented(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	resp, err := NewProjectUnsListLogic(ctx, svcCtx).ProjectUnsList(&types.ProjectAppReq{ProjectId: 1})
	require.NoError(t, err)
	require.Nil(t, resp)
}

func TestProjectUnsDomain_ListUnsCreatesRootAndLists(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	projectID := createTestProject(t, svcCtx, ctx, "uns-project")

	// 首次查询：创建项目 UNS 根节点并返回子树
	nodes, err := svcCtx.App.Project.ListUns(ctx, projectID)
	require.NoError(t, err)

	root, err := repo.NewUnsRepo(ctx).GetUnsNodeByAlias(ctx, fmt.Sprintf("project_%d_root", projectID))
	require.NoError(t, err, "project UNS root should be created on first list")
	require.Equal(t, int16(1), root.Type)

	// 根下挂一个子节点后，子树应包含它（用 folder 子节点，避免 file 节点
	// 需要 topic folder 父级结构的 repo 层校验）。
	child, err := repo.NewUnsRepo(ctx).CreateUnsNode(ctx, repo.UnsNode{
		ParentID:    root.ID,
		Name:        "child-topic",
		DisplayName: "child-topic",
		Namespace:   root.Namespace + "/child-topic",
		Type:        1,
	}, nil, nil)
	require.NoError(t, err)
	require.Positive(t, child.ID)

	nodes, err = svcCtx.App.Project.ListUns(ctx, projectID)
	require.NoError(t, err)
	found := false
	for _, n := range nodes {
		if n.ID == child.ID {
			found = true
		}
	}
	require.True(t, found, "child uns node should be listed under project root")

	// 项目不存在 → ErrNotFound
	_, err = svcCtx.App.Project.ListUns(ctx, 999999)
	require.True(t, errors.Is(err, domainproject.ErrNotFound))
}

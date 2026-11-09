// 本文件覆盖 projectFlowListLogic.go：
// 现状：该文件是 goctl 脚手架占位（todo 未实现，恒返回 nil,nil），测试锁定当前占位契约；
// 实际 Flow 列表能力在领域层 svcCtx.App.Project.ListFlows，这里补测其 DB 行为
// （正常路径：项目 source/event flow 根节点惰性创建，App manifest 引用的 flowIds 被收集返回）。
package project

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/testkit"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestProjectFlowListLogic_StubUnimplemented(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	resp, err := NewProjectFlowListLogic(ctx, svcCtx).ProjectFlowList(&types.ProjectAppReq{ProjectId: 1})
	require.NoError(t, err)
	require.Nil(t, resp)
}

func TestProjectFlowDomain_ListFlowsCollectsManifestIDs(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	db := svcCtx.App.Store.CommonDB()

	projectID := createTestProject(t, svcCtx, ctx, "flow-project")

	// 造两个 Flow（模拟 App 导入的资源），App manifest 引用其 ID
	flow1 := testkit.Insert(t, db, &repo.Flow{ID: 7001, Name: "flow-7001", Status: "draft", FlowType: 1, NodeType: 2})
	flow2 := testkit.Insert(t, db, &repo.Flow{ID: 7002, Name: "flow-7002", Status: "draft", FlowType: 1, NodeType: 2})
	_ = flow1
	_ = flow2
	app := testkit.Insert(t, db, &repo.ProjectApp{
		ProjectID:    projectID,
		AppKey:       "key-flow-app",
		Name:         "flow-app",
		SiteType:     "sameOriginStatic",
		ManifestJSON: jsonRaw(`{"flowIds":[7001,7002]}`),
		Status:       "active",
	})
	require.Positive(t, app.ID)

	flows, err := svcCtx.App.Project.ListFlows(ctx, projectID)
	require.NoError(t, err)
	ids := map[int64]bool{}
	for _, f := range flows {
		ids[f.ID] = true
	}
	require.True(t, ids[7001] && ids[7002], "manifest flowIds should be listed, got %v", ids)

	// 根节点惰性创建：source(1)/event(2) flow 根目录以项目名命名
	roots, err := svcCtx.App.Project.EnsureResourceRoots(ctx, projectID, 1)
	require.NoError(t, err)
	require.Equal(t, "flow-project", roots.SourceFlowRoot.Name)
	require.Equal(t, "flow-project", roots.EventFlowRoot.Name)
}

// TASK-028 app 模块测试共享助手：
//
//	目标：为 5 个 containerApp logic 文件提供统一的场景搭建与断言工具。
//	正常路径：Envelope 解包、HTTP 错误断言、container app 种子数据插入与查询。
//	异常情况：无（纯工具函数，不涉及业务异常分支）。
package app

import (
	"context"
	"testing"

	domainproject "backend/internal/domain/project"
	respx "backend/internal/httpx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

// newSvcEnv 每次调用创建独立测试库 + miniredis 的完整 ServiceContext 与登录 ctx。
// userID 传 1 表示 Seed 内置 tier0 管理员。
func newSvcEnv(t *testing.T) (*svc.ServiceContext, context.Context) {
	t.Helper()
	return svctest.NewTestServiceContext(t), svctest.CtxWithUser(1)
}

// respData 解包成功响应的 Data 为 map[string]any。
func respData(t *testing.T, resp *types.Envelope) map[string]any {
	t.Helper()
	require.NotNil(t, resp)
	require.Equal(t, 200, resp.Code)
	m, ok := resp.Data.(map[string]any)
	require.True(t, ok, "envelope data should be map[string]any")
	return m
}

// requireHTTPError 断言 err 为 logicx.Error 包装出的 HTTP 错误且状态码匹配。
func requireHTTPError(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	var httpErr *respx.HTTPError
	require.ErrorAs(t, err, &httpErr, "logicx.Error should wrap respx.HTTPError")
	require.Equal(t, status, httpErr.Status)
}

// insertContainerApp 直插一条 container app（project_id=0、site_type=externalContainer）记录。
func insertContainerApp(t *testing.T, svcCtx *svc.ServiceContext, name, entryURL, status string) repo.ProjectApp {
	t.Helper()
	item := repo.ProjectApp{
		ProjectID:    0,
		AppKey:       "container_" + repo.RandomHexForApp(8),
		Name:         name,
		SiteType:     "externalContainer",
		EntryURL:     entryURL,
		ManifestJSON: []byte(`{"description":"","openInPlatform":true}`),
		Status:       status,
	}
	item.CreatedBy = 1
	return *testkitInsert(t, svcCtx, &item)
}

// testkitInsert 写入一条记录并回填主键。
func testkitInsert[T any](t *testing.T, svcCtx *svc.ServiceContext, model *T) *T {
	t.Helper()
	require.NoError(t, svcCtx.App.Store.CommonDB().Create(model).Error)
	return model
}

// mustCreateProject 经领域层创建 project，返回 project ID。
func mustCreateProject(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, name string) int64 {
	t.Helper()
	view, err := svcCtx.App.Project.Create(ctx, domainproject.SaveCommand{Name: name, UserID: 1})
	require.NoError(t, err)
	p, ok := view["project"].(repo.Project)
	require.True(t, ok, "create view project should be repo.Project")
	require.Positive(t, p.ID)
	return p.ID
}

// insertProjectApp 直插一条 project 作用域的普通 app 记录（site_type=sameOriginStatic）。
func insertProjectApp(t *testing.T, svcCtx *svc.ServiceContext, projectID int64, name string) repo.ProjectApp {
	t.Helper()
	item := repo.ProjectApp{
		ProjectID:    projectID,
		AppKey:       "app_" + repo.RandomHexForApp(8),
		Name:         name,
		SiteType:     "sameOriginStatic",
		ManifestJSON: []byte(`{}`),
		Status:       "active",
	}
	item.CreatedBy = 1
	return *testkitInsert(t, svcCtx, &item)
}

// 本文件覆盖 projectAppListLogic.go：
// 现状：该文件是 goctl 脚手架占位（todo 未实现，恒返回 nil,nil），测试锁定当前占位契约；
// 实际应用列表能力在领域层 svcCtx.App.Project.ListApps，这里补测其 DB 行为
// （正常路径：应用出现在列表且字段完整；异常：项目不存在 → ErrNotFound）。
package project

import (
	"errors"
	"testing"

	domainproject "backend/internal/domain/project"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestProjectAppListLogic_StubUnimplemented(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	resp, err := NewProjectAppListLogic(ctx, svcCtx).ProjectAppList(&types.ProjectAppReq{ProjectId: 1})
	require.NoError(t, err)
	require.Nil(t, resp)
}

func TestProjectAppDomain_ListApps(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	projectID := createTestProject(t, svcCtx, ctx, "list-apps")
	appID := createTestManualApp(t, svcCtx, ctx, projectID, "listed-app")

	apps, err := svcCtx.App.Project.ListApps(ctx, projectID)
	require.NoError(t, err)
	require.Len(t, apps, 1)
	item := apps[0]
	require.Equal(t, appID, item["appId"])
	require.Equal(t, "listed-app", item["name"])
	require.Equal(t, "manualExternal", item["siteType"])
	require.Equal(t, "success", item["deployStatus"])

	// 项目不存在 → ErrNotFound
	_, err = svcCtx.App.Project.ListApps(ctx, 999999)
	require.True(t, errors.Is(err, domainproject.ErrNotFound))
}

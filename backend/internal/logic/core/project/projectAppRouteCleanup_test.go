package project

import (
	"encoding/json"
	"fmt"
	"testing"

	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/svc/svctest"

	"github.com/stretchr/testify/require"
)

// 首次导入失败时删除新建动态应用的网关路由；Replace 失败时保留旧版本仍在使用的路由。
// 半成品部署（无 app_id）跳过，静态应用（无 app_id）也跳过。
func TestCleanupImportedResources_NewDynamicAppRemovesRoute(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctx, "route-cleanup-dyn")
	t.Cleanup(func() { _ = svcCtx.App.TenantPGLifecycle.DropDatabase(ctx, fmt.Sprintf("project_%d", projectID)) })

	// 动态应用：manifest 带 app_id
	manifest := map[string]any{"app_id": "d1e4b2a0-1234-4f1e-8b8e-12ab34cd56ef", "container_name": "app-dyn-app-0001-dynapp"}
	manifestRaw, err := json.Marshal(manifest)
	require.NoError(t, err)
	app := repo.ProjectApp{
		ProjectID:    projectID,
		AppKey:       "app_routecleanup",
		Name:         "route-cleanup-dyn",
		SiteType:     "dynamic",
		ManifestJSON: json.RawMessage(manifestRaw),
		Status:       "failed",
	}
	createdApp := testkitInsertProjectApp(t, svcCtx, app)

	// 注册一条网关路由
	routeRepo := repo.NewAppGatewayRouteRepo(ctx)
	route := &repo.AppGatewayRoute{
		AppID:         "d1e4b2a0-1234-4f1e-8b8e-12ab34cd56ef",
		ProjectID:     projectID,
		AccessMode:    "port",
		Port:          30100,
		ContainerHost: "app-dyn-app-0001-dynapp",
		ContainerPort: 5173,
	}
	require.NoError(t, routeRepo.Insert(ctx, route))

	// 首次导入失败补偿应清理新应用的残留路由。
	cleanupImportedResources(ctx, importCreatedResources{}, projectID, createdApp.ID, 1)

	// 路由应被删除（软删后 FindByAppID 查不到）
	_, err = routeRepo.FindByAppID(ctx, route.AppID)
	require.Error(t, err, "route should be removed after cleanup")
	require.NotNil(t, err)
}

func TestCleanupReplacedAppResources_PreservesExistingRoute(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctx, "route-cleanup-replace")
	t.Cleanup(func() { _ = svcCtx.App.TenantPGLifecycle.DropDatabase(ctx, fmt.Sprintf("project_%d", projectID)) })

	manifestRaw, err := json.Marshal(map[string]any{
		"app_id":         "d1e4b2a0-1234-4f1e-8b8e-12ab34cd56f0",
		"container_name": "app-dyn-app-0002-dynapp",
	})
	require.NoError(t, err)
	app := testkitInsertProjectApp(t, svcCtx, repo.ProjectApp{
		ProjectID:    projectID,
		AppKey:       "app_routecleanupreplace",
		Name:         "route-cleanup-replace",
		SiteType:     "dynamic",
		ManifestJSON: json.RawMessage(manifestRaw),
		Status:       "active",
	})

	routeRepo := repo.NewAppGatewayRouteRepo(ctx)
	route := &repo.AppGatewayRoute{
		AppID:         "d1e4b2a0-1234-4f1e-8b8e-12ab34cd56f0",
		ProjectID:     projectID,
		AccessMode:    "port",
		Port:          30101,
		ContainerHost: "app-dyn-app-0002-dynapp",
		ContainerPort: 5173,
	}
	require.NoError(t, routeRepo.Insert(ctx, route))

	cleanupReplacedAppResources(ctx, importCreatedResources{}, projectID, app.ID, 1)

	preserved, err := routeRepo.FindByAppID(ctx, route.AppID)
	require.NoError(t, err)
	require.Equal(t, route.AppID, preserved.AppID)
}

// 半成品部署：manifest 无 app_id，清理应跳过（不报错，也无路由可删）。
func TestCleanupImportedAppGatewayRoutes_SemiApp(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	projectID := createTestProject(t, svcCtx, ctx, "route-cleanup-semi")
	t.Cleanup(func() { _ = svcCtx.App.TenantPGLifecycle.DropDatabase(ctx, fmt.Sprintf("project_%d", projectID)) })

	manifestRaw, _ := json.Marshal(map[string]any{"deploy_error": "interrupted"})
	app := repo.ProjectApp{
		ProjectID:    projectID,
		AppKey:       "app_routecleanupsemi",
		Name:         "route-cleanup-semi",
		SiteType:     "dynamic",
		ManifestJSON: json.RawMessage(manifestRaw),
		Status:       "failed",
	}
	createdApp := testkitInsertProjectApp(t, svcCtx, app)

	// 无 app_id：应直接返回不报错
	require.NotPanics(t, func() {
		cleanupImportedAppGatewayRoutes(ctx, projectID, createdApp.ID, nil)
	})
}

// testkitInsertProjectApp 通过 gorm 插入 project_app 并回填主键。
func testkitInsertProjectApp(t *testing.T, svcCtx *svc.ServiceContext, app repo.ProjectApp) repo.ProjectApp {
	t.Helper()
	db := svcCtx.App.Store.CommonDB()
	require.NoError(t, db.Create(&app).Error)
	require.Positive(t, app.ID)
	return app
}

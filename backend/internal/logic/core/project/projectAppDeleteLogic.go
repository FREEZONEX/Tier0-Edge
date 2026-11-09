// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package project

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"backend/internal/repo"
	"backend/internal/svc"

	"github.com/zeromicro/go-zero/core/logx"
)

type ProjectAppDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectAppDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectAppDeleteLogic {
	return &ProjectAppDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ProjectAppDelete 删除应用
func (l *ProjectAppDeleteLogic) ProjectAppDelete(projectID, appID, userID int64) error {
	if projectID <= 0 || appID <= 0 {
		return fmt.Errorf("project ID and app ID are required")
	}

	projectRepo := repo.NewProjectRepo(l.ctx)

	// 获取应用信息
	apps, err := projectRepo.ListProjectApps(l.ctx, projectID)
	if err != nil {
		return fmt.Errorf("failed to list apps: %w", err)
	}

	var app *repo.ProjectApp
	for _, a := range apps {
		if a.ID == appID {
			app = &a
			break
		}
	}
	if app == nil {
		return fmt.Errorf("app not found")
	}
	// 同一 backend 进程内仍在运行的异步部署必须先取消并等待退出，
	// 避免软删除后后台任务又创建容器、schema 或回写成功状态。
	if err := cancelArtifactDeployAndWait(projectID, appID); err != nil {
		return fmt.Errorf("cancel active app deployment: %w", err)
	}

	// 如果是动态应用（Docker 部署），需要清理容器和网关路由
	if app.SiteType == "dynamic" {
		l.cleanupDynamicApp(*app, userID)
	}
	if strings.TrimSpace(app.GatewayRouteKey) != "" {
		routeRepo := repo.NewGatewayRepo(l.ctx)
		if err := routeRepo.DeleteGatewayRoute(l.ctx, app.GatewayRouteKey, 0); err != nil {
			logx.Errorf("[ProjectAppDelete] 清理手动应用转发路由失败: %v", err)
		}
	}

	// 软删除应用记录
	if err := projectRepo.DeleteProjectApp(l.ctx, projectID, appID, 0); err != nil {
		return fmt.Errorf("failed to delete app: %w", err)
	}

	cleanupAppRoleBindings(l.ctx, projectRepo, projectID, appID)

	// 清理该 App 导入的 Flow/UNS 节点；仍被其他存活 App 引用的共享资源不删
	if err := l.svcCtx.App.Project.CleanupAppImportedResources(l.ctx, *app, 0); err != nil {
		logx.Errorf("[ProjectAppDelete] 清理导入资源失败: projectID=%d appID=%d err=%v", projectID, appID, err)
	}

	logx.Infof("[ProjectAppDelete] 应用删除成功: projectID=%d, appID=%d", projectID, appID)
	return nil
}

func cleanupAppRoleBindings(ctx context.Context, projectRepo *repo.ProjectRepo, projectID, appID int64) {
	roleApps, err := projectRepo.ListProjectRoleAppIDsByAppIDs(ctx, projectID, []int64{appID})
	if err != nil {
		logx.Errorf("[cleanupAppRoleBindings] 查询角色应用绑定失败: projectID=%d appID=%d err=%v", projectID, appID, err)
		return
	}
	if err := projectRepo.DeleteProjectRoleAppsByAppID(ctx, projectID, appID); err != nil {
		logx.Errorf("[cleanupAppRoleBindings] 删除角色应用绑定失败: projectID=%d appID=%d err=%v", projectID, appID, err)
		return
	}
	if len(roleApps) == 0 {
		return
	}
	roleIDs := make([]int64, 0, len(roleApps))
	seen := make(map[int64]struct{}, len(roleApps))
	for _, ra := range roleApps {
		if _, ok := seen[ra.RoleID]; ok {
			continue
		}
		seen[ra.RoleID] = struct{}{}
		roleIDs = append(roleIDs, ra.RoleID)
	}
	if _, err := projectRepo.DeleteProjectRolesIfNoApps(ctx, projectID, roleIDs); err != nil {
		logx.Errorf("[cleanupAppRoleBindings] 清理孤儿角色失败: projectID=%d appID=%d err=%v", projectID, appID, err)
	}
}

// cleanupDynamicApp 清理动态应用的容器和网关路由
func (l *ProjectAppDeleteLogic) cleanupDynamicApp(app repo.ProjectApp, userID int64) {
	// 从 manifest 中获取 docker app ID
	var meta map[string]any
	if len(app.ManifestJSON) > 0 {
		if err := json.Unmarshal(app.ManifestJSON, &meta); err != nil {
			logx.Errorf("[ProjectAppDelete] 解析 manifest 失败: %v", err)
			return
		}
	}

	dockerAppID, _ := meta["app_id"].(string)
	if dockerAppID == "" {
		// 半成品部署（从未成功回写 app_id，例如部署被中断/超时）：没有可清理的容器，
		// 但仍需继续执行下方的应用 schema 清理，避免遗留 app_<id> schema。
		logx.Infof("[ProjectAppDelete] 动态应用无 app_id（半成品部署），跳过容器清理: projectID=%d appID=%d", app.ProjectID, app.ID)
		if err := l.svcCtx.App.Project.PatchAppManifest(l.ctx, app.ProjectID, app.ID, map[string]any{
			"deploying":    false,
			"deploy_error": "deployment was interrupted before container start",
		}, userID); err != nil {
			logx.Errorf("[ProjectAppDelete] 回写半成品应用 manifest 失败: projectID=%d appID=%d err=%v", app.ProjectID, app.ID, err)
		}
	}

	appDir := ""
	appImage := dockerImageSnapshot{}
	defer func() {
		removeDockerImageIfUnused(appImage, "删除动态 App")
	}()
	if dockerAppID != "" {
		// 1. 停止并删除 Docker 容器
		appDir = filepath.Join("/app/data/apps", dockerAppID)
		composePath := filepath.Join(appDir, "docker-compose.yml")
		containerName, _ := meta["container_name"].(string)
		if strings.TrimSpace(containerName) == "" {
			containerName = appContainerName(app.Name, dockerAppID)
		}
		if snapshot, err := inspectAppDockerImage(l.ctx, containerName, composePath); err != nil {
			logx.Errorf("[ProjectAppDelete] 读取应用镜像失败，删除后将无法自动清理: projectID=%d appID=%d err=%v", app.ProjectID, app.ID, err)
		} else {
			appImage = snapshot
		}
		if _, err := os.Stat(composePath); err == nil {
			cmd := appComposeCommand(l.ctx, composePath, "down", "--volumes", "--remove-orphans")
			if output, err := cmd.CombinedOutput(); err != nil {
				logx.Errorf("[ProjectAppDelete] docker compose down 失败: %v, output: %s", err, string(output))
			}
		}

		// 1.1 兜底清理：容器、默认网络、命名卷可能在 compose down 失败后残留，强制再删一次。
		// 按 fleet service label 过滤，兼容旧格式 app-<shortID> 与新格式 app-<slug>-<shortID> 的容器。
		shortAppID := shortID(dockerAppID)
		if output, err := exec.Command("sh", "-c",
			fmt.Sprintf("docker rm -f $(docker ps -aq --filter label=com.tier0.fleet.service=app-%s)", shortAppID)).CombinedOutput(); err != nil {
			logx.Infof("[ProjectAppDelete] 强制删除容器失败或不存在: %v, output: %s", err, string(output))
		}
		if output, err := exec.Command("docker", "network", "rm", "-f", dockerAppID+"_default").CombinedOutput(); err != nil {
			logx.Infof("[ProjectAppDelete] 强制删除网络 %s_default 失败或不存在: %v, output: %s", dockerAppID, err, string(output))
		}
		if output, err := exec.Command("docker", "volume", "rm", "-f", dockerAppID+"_node_modules").CombinedOutput(); err != nil {
			logx.Infof("[ProjectAppDelete] 强制删除卷 %s_node_modules 失败或不存在: %v, output: %s", dockerAppID, err, string(output))
		}

		// 2. 清理网关路由表
		routeRepo := repo.NewAppGatewayRouteRepo(l.ctx)
		if err := routeRepo.DeleteByAppID(l.ctx, dockerAppID); err != nil {
			logx.Errorf("[ProjectAppDelete] 清理网关路由失败: %v", err)
		}

		// 3. 刷新网关路由
		if err := reloadGatewayRoutes(l.ctx); err != nil {
			logx.Errorf("[ProjectAppDelete] 刷新网关路由失败: %v", err)
		}
	}

	// 4. 删除应用级 schema
	// 校验 schema 所属：schema 必须是当前 app 自己的（project_app.schema_name 记录值），
	// 不允许用外部传入/推断的名称删除其他 App 的 schema（唯一索引 (project_id, schema_name) 保证
	// 预占后同 project 内不冲突；此处再做一次记录级校验，防御 schema_name 被异常改坏的情况）。
	schemaName := app.SchemaName
	if schemaName == "" {
		schemaName = appSchemaName(app.ID)
	}
	if err := validateSchemaOwnedByApp(l.ctx, app.ProjectID, app.ID, schemaName); err != nil {
		logx.Errorf("[ProjectAppDelete] schema 所属校验失败，跳过删除: projectID=%d appID=%d schema=%s err=%v", app.ProjectID, app.ID, schemaName, err)
		return
	}
	projectDB := ""
	if project, err := repo.NewProjectRepo(l.ctx).GetProject(l.ctx, app.ProjectID); err == nil {
		projectDB = project.DatabaseName
	} else {
		logx.Errorf("[ProjectAppDelete] 获取项目失败: projectID=%d err=%v", app.ProjectID, err)
	}
	if projectDB != "" && l.svcCtx.App.TenantPGLifecycle != nil {
		if err := l.svcCtx.App.TenantPGLifecycle.DropSchema(l.ctx, projectDB, schemaName); err != nil {
			logx.Errorf("[ProjectAppDelete] 删除应用 schema 失败: projectDB=%s schema=%s err=%v", projectDB, schemaName, err)
		}
	} else {
		// 降级：旧项目无 project database，删除 common DB 中的 schema
		if err := dropLegacyAppSchema(schemaName); err != nil {
			logx.Errorf("[ProjectAppDelete] 删除旧版应用 schema 失败: %s, err=%v", schemaName, err)
		}
	}

	// 5. 清理应用目录
	if appDir != "" {
		if err := os.RemoveAll(appDir); err != nil {
			logx.Errorf("[ProjectAppDelete] 清理应用目录失败: %v", err)
		}
	}

	logx.Infof("[ProjectAppDelete] 动态应用清理完成: %s", dockerAppID)
}

// dropLegacyAppSchema 使用后端高权限连接删除旧版 common DB 中的应用 schema
func dropLegacyAppSchema(schemaName string) error {
	ctx := context.Background()
	db := repo.GetCommonConn(ctx)
	if db == nil {
		return fmt.Errorf("common database connection not available")
	}
	return db.Exec(fmt.Sprintf("DROP SCHEMA IF EXISTS \"%s\" CASCADE", schemaName)).Error
}

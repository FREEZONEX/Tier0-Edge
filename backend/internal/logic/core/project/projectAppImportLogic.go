// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package project

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"backend/internal/appgateway"
	"backend/internal/contextx"
	domainlicense "backend/internal/domain/license"
	domainproject "backend/internal/domain/project"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/zeromicro/go-zero/core/logx"
)

type ProjectAppImportLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectAppImportLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectAppImportLogic {
	return &ProjectAppImportLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProjectAppImportLogic) checkAppNameDuplicate(projectID int64, appName string) error {
	if strings.TrimSpace(appName) == "" {
		return nil
	}
	_, err := repo.NewProjectRepo(l.ctx).GetProjectAppByName(l.ctx, projectID, appName)
	if err == nil {
		return domainproject.ErrAppNameDuplicate
	}
	if errors.Is(err, repo.ErrNotFound) {
		return nil
	}
	return err
}

// findAppForUpdate 查找 artifact 包更新导入要复用的已有应用记录。
// 只按导出包 meta.json 的来源应用 ID（manifest.sourceAppId）匹配，应用重命名后仍能命中原记录；
// 包内未携带 appId 或未命中时一律返回 nil,nil（走新建），不再按名称兜底。其他错误原样返回。
func (l *ProjectAppImportLogic) findAppForUpdate(projectID int64, appName string, meta projectAppMeta) (*repo.ProjectApp, error) {
	if meta.AppID == "" {
		return nil, nil
	}
	appRepo := repo.NewProjectRepo(l.ctx)
	app, err := appRepo.GetProjectAppBySourceAppID(l.ctx, projectID, meta.AppID)
	if err == nil {
		return &app, nil
	}
	if errors.Is(err, repo.ErrNotFound) {
		return nil, nil
	}
	return nil, err
}

// appReplaceRequiredError 普通 Import 命中同 Project 相同 APP ID 的决策哨兵：
// 不直接触发覆盖部署，由 API 层转为 replaceRequired 决策响应（确认前零副作用，EIMP-062）。
type appReplaceRequiredError struct {
	AppID     int64
	ProjectID int64
	Name      string
}

func (e *appReplaceRequiredError) Error() string { return "project.appReplaceRequired" }

// staticDuplicateError 把 appReplaceRequiredError 决策哨兵转为静态包的阻断错误：
// Replace 仅接受含 artifact.toml 的 Export Bundle，静态包走覆盖流程会在 Replace 处被拒，
// 因此静态路径的同 Project 重复命中必须保持为错误，不能转 replaceRequired 决策。
func staticDuplicateError(err error) error {
	var replaceErr *appReplaceRequiredError
	if errors.As(err, &replaceErr) {
		return fmt.Errorf("该 App 已在当前 Project 中存在，静态应用包不支持覆盖导入，请先删除原应用或修改包内 appId 后重试")
	}
	return err
}

// checkAppSourceDuplicate 导入为新 App 时按「目标 Project + 包内来源 appId（meta.AppID）」查重：
// 该 App 已在当前 Project 存在时返回 appReplaceRequiredError 决策（由调用方转 replaceRequired 响应，
// 引导走 Replace 确认流）；其他 Project 已有相同 sourceAppId 不再阻断（同一包可导入多个 Project，
// 各 Project 使用各自本地 database，数据隔离）。
// 包内未携带 appId（meta.json 缺失或为空）时跳过查重，不阻断导入。
// 须在创建任何 DB 记录之前调用。
func checkAppSourceDuplicate(ctx context.Context, projectID int64, meta projectAppMeta) error {
	if strings.TrimSpace(meta.AppID) == "" {
		return nil
	}
	appRepo := repo.NewProjectRepo(ctx)
	existing, err := appRepo.GetProjectAppBySourceAppID(ctx, projectID, meta.AppID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil
		}
		return err
	}
	if existing.ProjectID == projectID {
		return &appReplaceRequiredError{AppID: existing.ID, ProjectID: existing.ProjectID, Name: existing.Name}
	}
	// 理论上 GetProjectAppBySourceAppID 已按 project_id 过滤，此处不会到达；
	// 保留防御性分支：若命中其他 Project 记录（数据异常），按当前 Project 无冲突处理，不阻断。
	return nil
}

// wrapProjectAppCreateError 包装创建应用记录的错误：schema 预占唯一索引冲突转为可读的
// ErrSchemaNameTaken（用户「高级」区指定的 schema 已被同 Project 其他应用占用）。
func wrapProjectAppCreateError(err error) error {
	if errors.Is(err, repo.ErrAppNetworkAliasTaken) {
		return respx.NewHTTPError(409, err.Error())
	}
	if errors.Is(err, repo.ErrDuplicate) {
		return fmt.Errorf("%w: %v", ErrSchemaNameTaken, err)
	}
	return fmt.Errorf("failed to create app: %w", err)
}

// wrapProjectAppStaticCreateError 包装静态应用创建错误：名称/app_key 唯一索引冲突转为
// domainproject.ErrAppNameDuplicate，避免用户看到 raw common.duplicateResource。
func wrapProjectAppStaticCreateError(err error) error {
	if errors.Is(err, repo.ErrDuplicate) {
		return domainproject.ErrAppNameDuplicate
	}
	return fmt.Errorf("failed to create app: %w", err)
}

// ProjectAppImport 处理应用导入请求
// description 为用户在导入弹窗提交的应用描述（trim 后非空才覆盖包内 meta 描述，与 appName 规则一致）。
// cleanupPath 用于在异步路径中释放 caller 创建的临时 zip；本地文件可传 nil。
// opts 承载用户「高级」区提交的 schema 名与环境变量（零值保持现状行为）。
func (l *ProjectAppImportLogic) ProjectAppImport(projectID int64, attachmentPath string, appName string, description string, cleanupPath func(), opts artifactDeployOptions) (*repo.ProjectApp, error) {
	if projectID <= 0 {
		return nil, fmt.Errorf("project ID is required")
	}
	if strings.TrimSpace(attachmentPath) == "" {
		return nil, fmt.Errorf("attachment path is required")
	}

	projectRepo := repo.NewProjectRepo(l.ctx)
	project, err := projectRepo.GetProject(l.ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}

	// 解析导入文件
	importPath, cleanupFile, err := l.resolveImportFile(attachmentPath)
	if err != nil {
		return nil, err
	}
	defer cleanupFile()

	// 先通过 zip 目录快速判断是否为 artifact 包，避免同步解压大文件
	artifactInfo, err := readArtifactFromZip(importPath)
	if err == nil && artifactInfo.Manifest != nil {
		artifactAppName := artifactInfo.AppName
		if strings.TrimSpace(appName) != "" {
			artifactAppName = strings.TrimSpace(appName)
		}
		// 服务端 rune 长度校验：name≤64、description≤200，超限 400；描述校验用户提交生效值
		if err := validateProjectAppMetaLength(artifactAppName, resolveImportAppDescription(description, parsePackageMetaOrEmpty(artifactInfo.MetaRaw).Description)); err != nil {
			if cleanupPath != nil {
				cleanupPath()
			}
			return nil, err
		}
		// 同名/同来源 artifact 包命中同 Project 已有应用：普通 Import 不直接覆盖，
		// 返回 replaceRequired 决策（确认前零副作用）；显式 Replace 走 ProjectAppImportReplace
		existing, findErr := l.findAppForUpdate(projectID, artifactAppName, parsePackageMetaOrEmpty(artifactInfo.MetaRaw))
		if findErr != nil {
			if cleanupPath != nil {
				cleanupPath()
			}
			return nil, findErr
		}
		if existing != nil {
			if cleanupPath != nil {
				cleanupPath()
			}
			return nil, &appReplaceRequiredError{AppID: existing.ID, ProjectID: existing.ProjectID, Name: existing.Name}
		}
		// 快速路径同样要做 name 查重，避免同名但不同来源 appId 的应用重复落库。
		if err := l.checkAppNameDuplicate(projectID, artifactAppName); err != nil {
			if cleanupPath != nil {
				cleanupPath()
			}
			return nil, err
		}
		// 快速路径下 readArtifactFromZip 未透出 meta 三 ID，按包根 meta.json 查重；
		// meta.json 缺失或解析失败时不阻断导入（与 ParsePackageFromPath 的容忍行为一致）
		if pkgMeta, metaErr := readPackageMetaFromZip(importPath); metaErr == nil {
			if err := checkAppSourceDuplicate(l.ctx, projectID, pkgMeta); err != nil {
				if cleanupPath != nil {
					cleanupPath()
				}
				return nil, err
			}
		}
		return l.importArtifactAppAsync(project, importPath, artifactAppName, description, artifactInfo, cleanupPath, opts)
	}

	// 解析包
	pkg, cleanup, err := ParsePackageFromPath(importPath)
	if err != nil {
		return nil, err
	}

	// 用户自定义应用名优先于包内默认名称
	if strings.TrimSpace(appName) != "" {
		pkg.AppName = strings.TrimSpace(appName)
	}
	// 用户提交描述优先于包内 meta 描述（与 appName 覆盖规则一致），校验与 manifest 落库均用生效值
	pkg.Meta.Description = resolveImportAppDescription(description, pkg.Meta.Description)
	// 服务端 rune 长度校验：name≤64、description≤200，超限 400
	if err := validateProjectAppMetaLength(pkg.AppName, pkg.Meta.Description); err != nil {
		cleanup()
		if cleanupPath != nil {
			cleanupPath()
		}
		return nil, err
	}

	// 如果是 artifact 格式（Docker 部署）
	if pkg.ArtifactManifest != nil {
		// 同名/同来源 artifact 包命中同 Project 已有应用：普通 Import 不直接覆盖，
		// 返回 replaceRequired 决策（确认前零副作用）；
		// 本次同步解压出的 pkg 不再使用，立即释放其临时目录
		existing, findErr := l.findAppForUpdate(projectID, pkg.AppName, pkg.Meta)
		if findErr != nil {
			cleanup()
			if cleanupPath != nil {
				cleanupPath()
			}
			return nil, findErr
		}
		if existing != nil {
			cleanup()
			if cleanupPath != nil {
				cleanupPath()
			}
			return nil, &appReplaceRequiredError{AppID: existing.ID, ProjectID: existing.ProjectID, Name: existing.Name}
		}
		if cleanupPath != nil {
			defer cleanupPath()
		}
		if err := l.checkAppNameDuplicate(projectID, pkg.AppName); err != nil {
			return nil, err
		}
		if err := checkAppSourceDuplicate(l.ctx, projectID, pkg.Meta); err != nil {
			return nil, err
		}
		return l.importArtifactApp(project, pkg, cleanup, opts)
	}
	if cleanupPath != nil {
		defer cleanupPath()
	}
	defer cleanup()

	// 静态应用导入（非 Docker）
	appName = pkg.AppName

	if err := l.checkAppNameDuplicate(projectID, appName); err != nil {
		return nil, err
	}
	// 静态包不支持 Replace 覆盖：同 Project 重复命中保持阻断错误，不转 replaceRequired 决策
	if err := checkAppSourceDuplicate(l.ctx, projectID, pkg.Meta); err != nil {
		return nil, staticDuplicateError(err)
	}

	appRepo := repo.NewProjectRepo(l.ctx)

	if err := l.checkAppQuota(); err != nil {
		return nil, err
	}

	// 创建应用记录
	newApp := repo.ProjectApp{
		ProjectID: projectID,
		AppKey:    "app_" + randomHex(8),
		Name:      appName,
		SiteType:  "sameOriginStatic",
		Status:    "active",
	}
	if len(pkg.MetaRaw) > 0 {
		newApp.ManifestJSON = json.RawMessage(pkg.MetaRaw)
	}

	if opts.networkAlias != "" {
		return nil, respx.NewHTTPError(400, "project.networkAliasDynamic")
	}
	created, err := appRepo.CreateProjectApp(l.ctx, newApp)
	if err != nil {
		return nil, wrapProjectAppStaticCreateError(err)
	}

	return &created, nil
}

// maxArtifactDeployTimeout 动态应用后台部署的整体超时。
// 镜像拉取 / npm install / docker compose up / 健康检查可能长时间阻塞，
// 超时后按失败回写状态，避免应用永久停留在 deploying 且无法删除。
const maxArtifactDeployTimeout = 30 * time.Minute

const artifactDeployCancelWait = 3 * time.Minute

type artifactDeployExecution struct {
	cancel context.CancelFunc
	done   chan struct{}
}

var artifactDeployExecutions sync.Map

func artifactDeployExecutionKey(projectID, appID int64) string {
	return fmt.Sprintf("%d:%d", projectID, appID)
}

func beginArtifactDeploy(parent context.Context, projectID, appID int64) (context.Context, *artifactDeployExecution) {
	ctx, cancel := context.WithTimeout(parent, maxArtifactDeployTimeout)
	execution := &artifactDeployExecution{cancel: cancel, done: make(chan struct{})}
	key := artifactDeployExecutionKey(projectID, appID)
	if previous, loaded := artifactDeployExecutions.Swap(key, execution); loaded {
		previous.(*artifactDeployExecution).cancel()
	}
	return ctx, execution
}

func finishArtifactDeploy(projectID, appID int64, execution *artifactDeployExecution) {
	execution.cancel()
	close(execution.done)
	artifactDeployExecutions.CompareAndDelete(artifactDeployExecutionKey(projectID, appID), execution)
}

func cancelArtifactDeployAndWait(projectID, appID int64) error {
	value, ok := artifactDeployExecutions.Load(artifactDeployExecutionKey(projectID, appID))
	if !ok {
		return nil
	}
	execution := value.(*artifactDeployExecution)
	execution.cancel()
	timer := time.NewTimer(artifactDeployCancelWait)
	defer timer.Stop()
	select {
	case <-execution.done:
		return nil
	case <-timer.C:
		return fmt.Errorf("timed out waiting for active deployment to stop")
	}
}

// checkAppQuota 检查 Enterprise License 的 max_apps 配额
func (l *ProjectAppImportLogic) checkAppQuota() error {
	licenseSvc := domainlicense.Default()
	if licenseSvc == nil {
		return nil
	}
	count, err := repo.NewProjectRepo(l.ctx).CountProjectApps(l.ctx)
	if err != nil {
		return fmt.Errorf("failed to count apps: %w", err)
	}
	return licenseSvc.CheckQuota("max_apps", count)
}

// artifactRedeployTargetFromApp 校验同名更新导入的目标应用是否为已部署的动态应用（manifest 含 docker app_id），
// 并提取可复用的部署信息（docker app_id / host_port / access_mode / container_name）。
// 目标应用正在部署中时拒绝并发更新；非动态应用或缺少 docker app_id 时无法复用容器/端口原地重建，明确报错。
// 纯函数，便于单测。
func artifactRedeployTargetFromApp(app repo.ProjectApp, defaultAccessMode string) (dockerAppID string, hostPort int, accessMode, containerName string, err error) {
	// 同一 App 的更新导入串行：目标 App 正在部署中（含进行中的更新导入）时直接拒绝
	if app.Status == "deploying" {
		return "", 0, "", "", domainproject.ErrAppReplaceInProgress
	}
	// 部署失败的 App 不允许覆盖更新，与 static/manual 同等拒绝
	if app.Status == "failed" {
		return "", 0, "", "", domainproject.ErrAppReplaceInvalidTarget
	}
	manifest := map[string]any{}
	if len(app.ManifestJSON) > 0 {
		_ = json.Unmarshal(app.ManifestJSON, &manifest)
	}
	dockerAppID, _ = manifest["app_id"].(string)
	hostPort = int(int64Value(manifest["host_port"]))
	// 目标必须是已部署的动态应用（manifest 含 docker app_id），否则没有可复用的容器/端口/路由
	if app.SiteType != "dynamic" || strings.TrimSpace(dockerAppID) == "" {
		return "", 0, "", "", domainproject.ErrAppReplaceInvalidTarget
	}
	accessMode = manifestStringValue(manifest, "access_mode")
	if accessMode == "" {
		accessMode = defaultAccessMode
	}
	// 复用首次部署时的容器名，避免应用重命名后路由指向旧容器
	containerName, _ = manifest["container_name"].(string)
	if strings.TrimSpace(containerName) == "" {
		containerName = appContainerName(app.Name, dockerAppID)
	}
	return dockerAppID, hostPort, accessMode, containerName, nil
}

// importArtifactApp 处理 _current.zip 格式（artifact.toml）的应用导入
func (l *ProjectAppImportLogic) importArtifactApp(
	project repo.Project,
	pkg *Package,
	cleanupPkg func(),
	opts artifactDeployOptions,
) (*repo.ProjectApp, error) {
	appName := pkg.AppName

	if err := l.checkAppQuota(); err != nil {
		return nil, err
	}

	// 解析包内角色，若格式非法则提前失败，避免创建应用记录后再回滚
	roles, err := parseProjectAppRoles(pkg.RolesRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse roles: %w", err)
	}

	// 同步校验 artifact 配置，非法包直接拒绝（不建记录、不标部署中）
	if err := validateArtifactManifest(pkg.ArtifactManifest); err != nil {
		cleanupPkg()
		return nil, err
	}
	if err := validateArtifactBuiltOutput(pkg.ArtifactManifest, func(entry string) bool {
		_, err := os.Stat(filepath.Join(pkg.TempDir, entry))
		return err == nil
	}); err != nil {
		cleanupPkg()
		return nil, err
	}
	if err := validateArtifactRunEntry(pkg.ArtifactManifest, func(entry string) bool {
		rel, ok := artifactEntryRelPath("", entry)
		if !ok {
			return false
		}
		_, err := os.Stat(filepath.Join(pkg.TempDir, rel))
		return err == nil
	}); err != nil {
		cleanupPkg()
		return nil, err
	}

	// 1. 先创建 DB 记录 (status=deploying)
	artifactTomlRaw, _ := json.Marshal(pkg.ArtifactManifest)
	meta := map[string]any{
		"app_type":      "dynamic",
		"deploying":     true,
		"deploy_config": pkg.ArtifactManifest,
	}
	if len(artifactTomlRaw) > 0 {
		meta["artifact_toml"] = string(artifactTomlRaw)
	}
	// meta.json 新字段：version/schemaAlias/changelog/来源三 ID 等，空值不写入
	applyPackageMetaToManifest(meta, pkg.Meta, true)
	metaJSONBytes, _ := json.Marshal(meta)

	appRepo := repo.NewProjectRepo(l.ctx)
	newApp := repo.ProjectApp{
		ProjectID:    project.ID,
		AppKey:       "app_" + randomHex(8),
		Name:         appName,
		SiteType:     "dynamic",
		Status:       "deploying",
		SchemaName:   resolveImportSchemaName(l.ctx, opts.schemaName, pkg.Meta.AppID, project.ID),
		ManifestJSON: json.RawMessage(metaJSONBytes),
	}
	created, err := appRepo.CreateProjectAppWithNetworkAlias(l.ctx, newApp, opts.networkAlias)
	if err != nil {
		return nil, wrapProjectAppCreateError(err)
	}

	// 2. 同步导入角色并回写 manifest，用户可立即在项目角色列表中查看
	roleIDs, err := l.importArtifactRoles(project.ID, created.ID, roles)
	if err != nil {
		return nil, fmt.Errorf("failed to import roles: %w", err)
	}
	if len(roleIDs) > 0 {
		if err := l.svcCtx.App.Project.PatchAppManifest(l.ctx, project.ID, created.ID, map[string]any{
			"roleIds": roleIDs,
		}, logicx.UserID(l.ctx)); err != nil {
			return nil, fmt.Errorf("failed to patch app manifest: %w", err)
		}
	}

	// 3. 后台 goroutine 执行 Docker 部署
	deployConf := l.svcCtx.Config
	deployProjectName := project.Name
	deployProjectDB := project.DatabaseName
	deployManifest := pkg.ArtifactManifest
	deployAppName := appName
	deployTempDir := pkg.TempDir
	deployAppID := created.ID
	deployProjectID := project.ID
	deployUserID := logicx.UserID(l.ctx)
	deployBaseCtx := context.Background()
	if subject, ok := contextx.SubjectFrom(l.ctx); ok {
		deployBaseCtx = contextx.WithSubject(deployBaseCtx, subject)
	}
	deployCtx, deployExecution := beginArtifactDeploy(deployBaseCtx, deployProjectID, deployAppID)

	go func() {
		defer cleanupPkg()
		defer finishArtifactDeploy(deployProjectID, deployAppID, deployExecution)

		ctx := deployBaseCtx
		logger := logx.WithContext(ctx)
		appRepo := repo.NewProjectRepo(ctx)

		// 本次新建的资源 ID 集合，panic 和普通失败都按此集合逆序清理。
		var created importCreatedResources
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[importArtifactApp] 异步部署 panic: appID=%d, panic=%v", deployAppID, r)
				cleanupImportedResources(ctx, created, deployProjectID, deployAppID, deployUserID)
				if updateErr := appRepo.UpdateProjectAppManifest(ctx, deployProjectID, deployAppID, map[string]any{
					"deploying":            false,
					"deploy_error":         fmt.Sprintf("deploy panic: %v", r),
					"deploy_error_summary": buildDeployErrorSummary(fmt.Errorf("deploy panic: %v", r)),
				}, deployUserID); updateErr != nil {
					logger.Errorf("[importArtifactApp] panic 回写失败: appID=%d err=%v", deployAppID, updateErr)
				}
				_ = appRepo.UpdateProjectAppStatus(ctx, deployProjectID, deployAppID, "failed", deployUserID)
				cleanupAppRoleBindings(ctx, appRepo, deployProjectID, deployAppID)
			}
		}()

		// 后台导入包内附加资源（UNS/Flow/icon/cover），角色已在同步阶段导入，这里幂等重放并补齐其余资源
		appWorker := &ProjectAppLogic{ctx: ctx, svcCtx: l.svcCtx, Logger: logger}
		appPkg, err := buildProjectAppPackage(pkg, roles)
		if err != nil {
			logger.Errorf("[importArtifactApp] 包内资源解析失败: appID=%d, err=%v", deployAppID, err)
			if updateErr := appRepo.UpdateProjectAppManifest(ctx, deployProjectID, deployAppID, map[string]any{
				"deploying":            false,
				"deploy_error":         formatDeployError(fmt.Errorf("resource import failed: %w", err), ""),
				"deploy_error_summary": buildDeployErrorSummary(fmt.Errorf("resource import failed: %w", err)),
			}, deployUserID); updateErr != nil {
				logger.Errorf("[importArtifactApp] 回写资源错误失败: appID=%d err=%v", deployAppID, updateErr)
			}
			_ = appRepo.UpdateProjectAppStatus(ctx, deployProjectID, deployAppID, "failed", deployUserID)
			cleanupAppRoleBindings(ctx, appRepo, deployProjectID, deployAppID)
			return
		}
		resourcePatch, resourceCreated, err := appWorker.importPackageResources(deployProjectID, deployAppID, appPkg, false)
		created.merge(resourceCreated)
		if err != nil {
			logger.Errorf("[importArtifactApp] 资源导入失败: appID=%d, err=%v", deployAppID, err)
			cleanupImportedResources(ctx, created, deployProjectID, deployAppID, deployUserID)
			if updateErr := appRepo.UpdateProjectAppManifest(ctx, deployProjectID, deployAppID, map[string]any{
				"deploying":            false,
				"deploy_error":         formatDeployError(fmt.Errorf("resource import failed: %w", err), ""),
				"deploy_error_summary": buildDeployErrorSummary(fmt.Errorf("resource import failed: %w", err)),
			}, deployUserID); updateErr != nil {
				logger.Errorf("[importArtifactApp] 回写资源错误失败: appID=%d err=%v", deployAppID, updateErr)
			}
			_ = appRepo.UpdateProjectAppStatus(ctx, deployProjectID, deployAppID, "failed", deployUserID)
			cleanupAppRoleBindings(ctx, appRepo, deployProjectID, deployAppID)
			return
		}
		assetPatch, assetIDs, err := appWorker.importPackageAssets(deployProjectID, deployAppID, appPkg, deployUserID)
		created.AssetIDs = append(created.AssetIDs, assetIDs...)
		if err != nil {
			logger.Errorf("[importArtifactApp] 图标/封面上传失败: appID=%d, err=%v", deployAppID, err)
			cleanupImportedResources(ctx, created, deployProjectID, deployAppID, deployUserID)
			if updateErr := appRepo.UpdateProjectAppManifest(ctx, deployProjectID, deployAppID, map[string]any{
				"deploying":            false,
				"deploy_error":         formatDeployError(fmt.Errorf("asset import failed: %w", err), ""),
				"deploy_error_summary": buildDeployErrorSummary(fmt.Errorf("asset import failed: %w", err)),
			}, deployUserID); updateErr != nil {
				logger.Errorf("[importArtifactApp] 回写资源错误失败: appID=%d err=%v", deployAppID, updateErr)
			}
			_ = appRepo.UpdateProjectAppStatus(ctx, deployProjectID, deployAppID, "failed", deployUserID)
			return
		}

		// 后台导入 UNS（若包内存在 uns.json）
		if len(pkg.UnsRaw) > 0 {
			unsNodeIDs, skippedUns, createdUnsIDs, unsErr := importProjectAppUns(ctx, l.svcCtx, deployProjectID, deployAppID, pkg.UnsRaw, deployAppName, deployUserID)
			created.UnsNodeIDs = append(created.UnsNodeIDs, createdUnsIDs...)
			if unsErr != nil {
				logger.Errorf("[importArtifactApp] 异步 UNS 导入失败: appID=%d, err=%v", deployAppID, unsErr)
				cleanupImportedResources(ctx, created, deployProjectID, deployAppID, deployUserID)
				if updateErr := appRepo.UpdateProjectAppManifest(ctx, deployProjectID, deployAppID, map[string]any{
					"deploying":            false,
					"deploy_error":         formatDeployError(fmt.Errorf("uns import failed: %w", unsErr), ""),
					"deploy_error_summary": buildDeployErrorSummary(fmt.Errorf("uns import failed: %w", unsErr)),
				}, deployUserID); updateErr != nil {
					logger.Errorf("[importArtifactApp] 回写 UNS 错误失败: appID=%d err=%v", deployAppID, updateErr)
				}
				_ = appRepo.UpdateProjectAppStatus(ctx, deployProjectID, deployAppID, "failed", deployUserID)
				cleanupAppRoleBindings(ctx, appRepo, deployProjectID, deployAppID)
				return
			}
			if len(unsNodeIDs) > 0 || len(skippedUns) > 0 {
				// created 集合与全量引用一起写入（空数组表示本 App 未新建节点）：
				// 删 App 清理只删本 App 新建的节点，命中匹配的既有节点不误删
				if err := l.svcCtx.App.Project.PatchAppManifest(ctx, deployProjectID, deployAppID, map[string]any{
					"unsNodeIds":           unsNodeIDs,
					"createdUnsNodeIds":    created.UnsNodeIDs,
					"skippedUnsNamespaces": skippedUns,
				}, deployUserID); err != nil {
					logger.Errorf("[importArtifactApp] 回写 UNS manifest 失败: appID=%d err=%v", deployAppID, err)
				}
			}
		}

		appID, hostPort, deployLogs, deployErr := artifactDeploy(deployCtx, deployConf, l.svcCtx.App.TenantPGLifecycle, deployProjectDB, deployProjectName, deployManifest, deployAppName, deployTempDir, deployProjectID, deployAppID, opts)

		if deployErr != nil {
			logx.Errorf("[importArtifactApp] 异步部署失败: appID=%d, err=%v", deployAppID, deployErr)
			cleanupImportedResources(ctx, created, deployProjectID, deployAppID, deployUserID)
			deployError := formatDeployError(deployErr, deployLogs)
			if err := appRepo.UpdateProjectAppManifest(ctx, deployProjectID, deployAppID, map[string]any{
				"deploying":            false,
				"deploy_error":         deployError,
				"deploy_error_summary": buildDeployErrorSummary(fmt.Errorf("deploy failed: %w", deployErr)),
			}, deployUserID); err != nil {
				logx.Errorf("[importArtifactApp] 回写部署错误失败: appID=%d err=%v", deployAppID, err)
			}
			if err := appRepo.UpdateProjectAppStatus(ctx, deployProjectID, deployAppID, "failed", deployUserID); err != nil {
				logx.Errorf("[importArtifactApp] 更新应用状态失败: appID=%d err=%v", deployAppID, err)
			}
			cleanupAppRoleBindings(ctx, appRepo, deployProjectID, deployAppID)
			return
		}

		extraPatch := map[string]any{}
		for key, value := range resourcePatch {
			extraPatch[key] = value
		}
		for key, value := range assetPatch {
			extraPatch[key] = value
		}
		// 持久化生效环境变量（用户「高级」区替换后的最终列表），Replace 时读取作默认；
		// 不含平台强制变量（DATABASE_URL/TIER0_* 由部署层强制注入，不落库避免敏感信息扩散）
		extraPatch["container_port"] = parseArtifactConfig(deployManifest).ContainerPort
		extraPatch["network_alias"] = opts.networkAlias
		extraPatch["network_alias_pending"] = ""
		extraPatch["env"] = resolveArtifactEnv(parseArtifactConfig(deployManifest).Env, opts.envPolicy, opts.envVars)
		// 持久化生效磁盘映射（首次导入无既有值，opts.volumes 即最终列表）
		extraPatch[volumeManifestKey] = volumesOrEmpty(opts.volumes)
		entryURL := buildStaticAppURL(ctx, deployConf, appID, deployConf.AppGateway.DefaultAccessMode)
		if err := mergeArtifactAppSuccess(ctx, deployProjectID, deployAppID, deployAppName, appID, hostPort, deployConf.AppGateway.DefaultAccessMode, entryURL, extraPatch, deployUserID, ""); err != nil {
			logger.Errorf("[importArtifactApp] 回写部署结果失败: appID=%d err=%v", deployAppID, err)
		}

		logx.Infof("[importArtifactApp] 异步部署成功: appID=%d, dockerAppID=%s, hostPort=%d, entryURL=%s", deployAppID, appID, hostPort, entryURL)
	}()

	return &created, nil
}

// artifactZipInfo 直接从 zip 目录读取的 artifact 包概要信息（不解压整个包）
type artifactZipInfo struct {
	Manifest    map[string]any // artifact.toml 解析结果
	RolesRaw    []byte         // 与 artifact.toml 同目录的 role(s).json 原文
	MetaRaw     []byte         // 与 artifact.toml 同目录的 meta.json 原文（可选）
	AppName     string
	ArtifactDir string // artifact.toml 所在目录（空表示在 zip 根），用于定位运行入口文件
}

// readArtifactFromZip 直接读取 zip 包中的 artifact.toml 与角色/meta 文件，不解压整个包。
// 只读取 artifact.toml 所在目录下的角色/meta 文件，避免误读依赖中的同名文件。
// 包内可能存在多个 artifact.toml（如 .template-inspect 模板目录），取路径深度最浅的，
// 避免嵌套模板目录盖过真正的应用根。
func readArtifactFromZip(path string) (artifactZipInfo, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return artifactZipInfo{}, err
	}
	defer zr.Close()

	if len(zr.File) > maxPackageEntries {
		return artifactZipInfo{}, fmt.Errorf("zip contains %d entries, limit is %d", len(zr.File), maxPackageEntries)
	}

	var artifactFile *zip.File
	artifactDepth := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		base := strings.ToLower(filepath.Base(f.Name))
		if base != "artifact.toml" {
			continue
		}
		depth := strings.Count(f.Name, "/")
		if artifactFile == nil || depth < artifactDepth {
			artifactFile = f
			artifactDepth = depth
		}
	}
	if artifactFile == nil {
		return artifactZipInfo{}, fmt.Errorf("artifact.toml not found")
	}

	raw, readErr := readZipEntryLimited(artifactFile, maxPackageMetaFileSize)
	if readErr != nil {
		return artifactZipInfo{}, fmt.Errorf("failed to read artifact.toml: %w", readErr)
	}
	manifest := make(map[string]any)
	if err := toml.Unmarshal(raw, &manifest); err != nil {
		return artifactZipInfo{}, fmt.Errorf("failed to parse artifact.toml: %w", err)
	}
	artifactDir := filepath.Dir(artifactFile.Name)
	var appName string
	if name, ok := manifest["name"].(string); ok {
		appName = name
	}

	var rolesRaw, metaRaw []byte
	if artifactDir != "" {
		for _, f := range zr.File {
			if f.FileInfo().IsDir() {
				continue
			}
			base := strings.ToLower(filepath.Base(f.Name))
			if base != "role.json" && base != "roles.json" && base != "meta.json" {
				continue
			}
			if filepath.Dir(f.Name) != artifactDir {
				continue
			}
			raw, readErr := readZipEntryLimited(f, maxPackageMetaFileSize)
			if readErr != nil {
				continue
			}
			if base == "meta.json" {
				metaRaw = raw
			} else if rolesRaw == nil {
				rolesRaw = raw
			}
		}
	}

	// 名称兜底链：artifact.toml name → 包内 meta.json name → 包文件名 → app-时间戳
	if appName == "" {
		appName = parsePackageMetaOrEmpty(metaRaw).Name
	}
	appName = resolveAppNameFallback(appName, path)
	return artifactZipInfo{
		Manifest:    manifest,
		RolesRaw:    rolesRaw,
		MetaRaw:     metaRaw,
		AppName:     appName,
		ArtifactDir: artifactDir,
	}, nil
}

// readPackageMetaFromZip 读取 zip 包根的 meta.json（取路径深度最浅者），用于覆盖导入的身份校验。
// 包内无 meta.json 时返回空 meta，不报错；meta.json 存在但解析失败时返回错误。
func readPackageMetaFromZip(path string) (projectAppMeta, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return projectAppMeta{}, err
	}
	defer zr.Close()

	if len(zr.File) > maxPackageEntries {
		return projectAppMeta{}, fmt.Errorf("zip contains %d entries, limit is %d", len(zr.File), maxPackageEntries)
	}

	var metaFile *zip.File
	metaDepth := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || shouldIgnoreProjectZipEntry(f.Name) {
			continue
		}
		if strings.ToLower(filepath.Base(f.Name)) != "meta.json" {
			continue
		}
		depth := strings.Count(f.Name, "/")
		if metaFile == nil || depth < metaDepth {
			metaFile = f
			metaDepth = depth
		}
	}
	if metaFile == nil {
		return projectAppMeta{}, nil
	}
	raw, readErr := readZipEntryLimited(metaFile, maxPackageMetaFileSize)
	if readErr != nil {
		return projectAppMeta{}, fmt.Errorf("failed to read meta.json: %w", readErr)
	}
	meta, err := parseProjectAppMeta(raw)
	if err != nil {
		return projectAppMeta{}, fmt.Errorf("failed to parse meta.json: %w", err)
	}
	return meta, nil
}

// importArtifactAppAsync 与 importArtifactApp 类似，但把解压、资源导入、Docker 部署全部放到后台 goroutine。
// description 为用户在导入弹窗提交的应用描述，非空时覆盖 manifest 中的包内描述（与 appName 规则一致）。
func (l *ProjectAppImportLogic) importArtifactAppAsync(
	project repo.Project,
	importPath string,
	appName string,
	description string,
	artifactInfo artifactZipInfo,
	cleanupPath func(),
	opts artifactDeployOptions,
) (*repo.ProjectApp, error) {
	if err := l.checkAppQuota(); err != nil {
		return nil, err
	}

	// 同步解析角色，格式非法直接失败，避免创建空记录
	roles, err := parseProjectAppRoles(artifactInfo.RolesRaw)
	if err != nil {
		return nil, fmt.Errorf("failed to parse roles: %w", err)
	}

	// 同步校验 artifact 配置，非法包直接拒绝（不建记录、不标部署中）
	if err := validateArtifactManifest(artifactInfo.Manifest); err != nil {
		if cleanupPath != nil {
			cleanupPath()
		}
		return nil, err
	}
	if err := validateArtifactBuiltOutput(artifactInfo.Manifest, func(entry string) bool {
		exists, _ := zipDirOrFileExists(importPath, entry)
		return exists
	}); err != nil {
		if cleanupPath != nil {
			cleanupPath()
		}
		return nil, err
	}
	if err := validateArtifactRunEntry(artifactInfo.Manifest, func(entry string) bool {
		rel, ok := artifactEntryRelPath(artifactInfo.ArtifactDir, entry)
		if !ok {
			return false
		}
		exists, _ := zipEntryExists(importPath, rel)
		return exists
	}); err != nil {
		if cleanupPath != nil {
			cleanupPath()
		}
		return nil, err
	}

	artifactTomlRaw, _ := json.Marshal(artifactInfo.Manifest)
	meta := map[string]any{
		"app_type":      "dynamic",
		"deploying":     true,
		"deploy_config": artifactInfo.Manifest,
	}
	if len(artifactTomlRaw) > 0 {
		meta["artifact_toml"] = string(artifactTomlRaw)
	}
	// meta.json 新字段：version/schemaAlias/changelog/来源三 ID 等，空值不写入
	pkgMeta := parsePackageMetaOrEmpty(artifactInfo.MetaRaw)
	applyPackageMetaToManifest(meta, pkgMeta, true)
	// 用户提交描述优先于包内 meta 描述（trim 后非空才覆盖，与 appName 规则一致）
	if d := strings.TrimSpace(description); d != "" {
		meta["description"] = d
	}
	metaJSONBytes, _ := json.Marshal(meta)

	appRepo := repo.NewProjectRepo(l.ctx)
	newApp := repo.ProjectApp{
		ProjectID:    project.ID,
		AppKey:       "app_" + randomHex(8),
		Name:         appName,
		SiteType:     "dynamic",
		Status:       "deploying",
		SchemaName:   resolveImportSchemaName(l.ctx, opts.schemaName, pkgMeta.AppID, project.ID),
		ManifestJSON: json.RawMessage(metaJSONBytes),
	}
	created, err := appRepo.CreateProjectAppWithNetworkAlias(l.ctx, newApp, opts.networkAlias)
	if err != nil {
		return nil, wrapProjectAppCreateError(err)
	}

	deployConf := l.svcCtx.Config
	deployProjectName := project.Name
	deployProjectDB := project.DatabaseName
	deployAppName := appName
	deployAppID := created.ID
	deployProjectID := project.ID
	deployUserID := logicx.UserID(l.ctx)
	deployBaseCtx := context.Background()
	if subject, ok := contextx.SubjectFrom(l.ctx); ok {
		deployBaseCtx = contextx.WithSubject(deployBaseCtx, subject)
	}
	deployCtx, deployExecution := beginArtifactDeploy(deployBaseCtx, deployProjectID, deployAppID)

	go func() {
		defer finishArtifactDeploy(deployProjectID, deployAppID, deployExecution)

		ctx := deployBaseCtx
		logger := logx.WithContext(ctx)
		appRepo := repo.NewProjectRepo(ctx)

		// 本次新建的资源 ID 集合，任何失败分支据此逆序清理（只删 INSERT 的，不含命中更新的既有资源）
		var created importCreatedResources

		markFailed := func(stage string, cause error) {
			logger.Errorf("[importArtifactAppAsync] %s: appID=%d, err=%v", stage, deployAppID, cause)
			cleanupImportedResources(ctx, created, deployProjectID, deployAppID, deployUserID)
			if err := appRepo.UpdateProjectAppManifest(ctx, deployProjectID, deployAppID, map[string]any{
				"deploying":            false,
				"deploy_error":         formatDeployError(fmt.Errorf("%s: %w", stage, cause), ""),
				"deploy_error_summary": buildDeployErrorSummary(fmt.Errorf("%s: %w", stage, cause)),
			}, deployUserID); err != nil {
				logger.Errorf("[importArtifactAppAsync] 回写错误失败: appID=%d err=%v", deployAppID, err)
			}
			_ = appRepo.UpdateProjectAppStatus(ctx, deployProjectID, deployAppID, "failed", deployUserID)
		}

		// panic 兜底：任何未捕获异常都回写失败状态，避免应用永久停留在 deploying 无法删除
		defer func() {
			if r := recover(); r != nil {
				logger.Errorf("[importArtifactAppAsync] 异步部署 panic: appID=%d, panic=%v", deployAppID, r)
				markFailed("deploy panic", fmt.Errorf("panic: %v", r))
				cleanupAppRoleBindings(ctx, appRepo, deployProjectID, deployAppID)
			}
		}()

		// 后台解压
		pkg, cleanup, err := ParsePackageFromPath(importPath)
		if cleanupPath != nil {
			cleanupPath()
		}
		if err != nil {
			markFailed("unpack failed", err)
			return
		}
		defer cleanup()

		// 后台导入包内资源（角色/UNS/Flow），复用静态包导入逻辑
		appWorker := &ProjectAppLogic{ctx: ctx, svcCtx: l.svcCtx, Logger: logger}
		appPkg, err := buildProjectAppPackage(pkg, roles)
		if err != nil {
			markFailed("resource import failed", err)
			cleanupAppRoleBindings(ctx, appRepo, deployProjectID, deployAppID)
			return
		}
		resourcePatch, resourceCreated, err := appWorker.importPackageResources(deployProjectID, deployAppID, appPkg, false)
		created.merge(resourceCreated)
		if err != nil {
			markFailed("resource import failed", err)
			cleanupAppRoleBindings(ctx, appRepo, deployProjectID, deployAppID)
			return
		}

		// 包根 icon/cover 上传 asset
		assetPatch, assetIDs, err := appWorker.importPackageAssets(deployProjectID, deployAppID, appPkg, deployUserID)
		created.AssetIDs = append(created.AssetIDs, assetIDs...)
		if err != nil {
			markFailed("asset import failed", err)
			return
		}

		// 后台导入 UNS（若包内存在 uns.json）
		if len(pkg.UnsRaw) > 0 {
			unsNodeIDs, skippedUns, createdUnsIDs, unsErr := importProjectAppUns(ctx, l.svcCtx, deployProjectID, deployAppID, pkg.UnsRaw, deployAppName, deployUserID)
			created.UnsNodeIDs = append(created.UnsNodeIDs, createdUnsIDs...)
			if unsErr != nil {
				logger.Errorf("[importArtifactAppAsync] 异步 UNS 导入失败: appID=%d, err=%v", deployAppID, unsErr)
				cleanupImportedResources(ctx, created, deployProjectID, deployAppID, deployUserID)
				if updateErr := appRepo.UpdateProjectAppManifest(ctx, deployProjectID, deployAppID, map[string]any{
					"deploying":            false,
					"deploy_error":         formatDeployError(fmt.Errorf("uns import failed: %w", unsErr), ""),
					"deploy_error_summary": buildDeployErrorSummary(fmt.Errorf("uns import failed: %w", unsErr)),
				}, deployUserID); updateErr != nil {
					logger.Errorf("[importArtifactAppAsync] 回写 UNS 错误失败: appID=%d err=%v", deployAppID, updateErr)
				}
				_ = appRepo.UpdateProjectAppStatus(ctx, deployProjectID, deployAppID, "failed", deployUserID)
				cleanupAppRoleBindings(ctx, appRepo, deployProjectID, deployAppID)
				return
			}
			if len(unsNodeIDs) > 0 || len(skippedUns) > 0 {
				// created 集合与全量引用一起写入（空数组表示本 App 未新建节点）：
				// 删 App 清理只删本 App 新建的节点，命中匹配的既有节点不误删
				if err := l.svcCtx.App.Project.PatchAppManifest(ctx, deployProjectID, deployAppID, map[string]any{
					"unsNodeIds":           unsNodeIDs,
					"createdUnsNodeIds":    created.UnsNodeIDs,
					"skippedUnsNamespaces": skippedUns,
				}, deployUserID); err != nil {
					logger.Errorf("[importArtifactAppAsync] 回写 UNS manifest 失败: appID=%d err=%v", deployAppID, err)
				}
			}
		}

		// Docker 部署
		appID, hostPort, deployLogs, deployErr := artifactDeploy(deployCtx, deployConf, l.svcCtx.App.TenantPGLifecycle, deployProjectDB, deployProjectName, pkg.ArtifactManifest, deployAppName, pkg.TempDir, deployProjectID, deployAppID, opts)
		if deployErr != nil {
			deployError := formatDeployError(deployErr, deployLogs)
			if err := appRepo.UpdateProjectAppManifest(ctx, deployProjectID, deployAppID, map[string]any{
				"deploying":            false,
				"deploy_error":         deployError,
				"deploy_error_summary": buildDeployErrorSummary(fmt.Errorf("deploy failed: %w", deployErr)),
			}, deployUserID); err != nil {
				logger.Errorf("[importArtifactAppAsync] 回写部署错误失败: appID=%d err=%v", deployAppID, err)
			}
			if err := appRepo.UpdateProjectAppStatus(ctx, deployProjectID, deployAppID, "failed", deployUserID); err != nil {
				logger.Errorf("[importArtifactAppAsync] 更新应用状态失败: appID=%d err=%v", deployAppID, err)
			}
			cleanupAppRoleBindings(ctx, appRepo, deployProjectID, deployAppID)
			return
		}

		extraPatch := map[string]any{}
		for key, value := range resourcePatch {
			extraPatch[key] = value
		}
		for key, value := range assetPatch {
			extraPatch[key] = value
		}
		// 持久化生效环境变量（用户「高级」区替换后的最终列表），Replace 时读取作默认；
		// 不含平台强制变量（DATABASE_URL/TIER0_* 由部署层强制注入，不落库避免敏感信息扩散）
		extraPatch["network_alias"] = opts.networkAlias
		extraPatch["network_alias_pending"] = ""
		extraPatch["container_port"] = parseArtifactConfig(pkg.ArtifactManifest).ContainerPort
		extraPatch["env"] = resolveArtifactEnv(parseArtifactConfig(pkg.ArtifactManifest).Env, opts.envPolicy, opts.envVars)
		// 持久化生效磁盘映射（首次导入无既有值，opts.volumes 即最终列表）
		extraPatch[volumeManifestKey] = volumesOrEmpty(opts.volumes)
		entryURL := buildStaticAppURL(ctx, deployConf, appID, deployConf.AppGateway.DefaultAccessMode)
		if err := mergeArtifactAppSuccess(ctx, deployProjectID, deployAppID, deployAppName, appID, hostPort, deployConf.AppGateway.DefaultAccessMode, entryURL, extraPatch, deployUserID, ""); err != nil {
			logger.Errorf("[importArtifactAppAsync] 回写部署结果失败: appID=%d err=%v", deployAppID, err)
		}
		logger.Infof("[importArtifactAppAsync] 异步部署成功: appID=%d, dockerAppID=%s, hostPort=%d, entryURL=%s", deployAppID, appID, hostPort, entryURL)
	}()

	return &created, nil
}

// buildProjectAppPackage 把解压后的应用包转换为静态包结构，复用 importPackageResources 等导入逻辑。
// roles 由调用方同步解析后传入，避免在后台 goroutine 重复解析。
// 若包内存在 uns.json，会在此先做解析校验，失败时直接返回错误，避免后续资源导入产生副作用。
func buildProjectAppPackage(pkg *Package, roles []projectPackageRole) (projectAppPackage, error) {
	appPkg := projectAppPackage{
		Meta:      pkg.Meta,
		Roles:     roles,
		FlowRaw:   pkg.FlowRaw,
		IconRaw:   pkg.IconRaw,
		IconName:  pkg.IconName,
		CoverRaw:  pkg.CoverRaw,
		CoverName: pkg.CoverName,
	}
	if len(pkg.UnsRaw) > 0 {
		if err := json.Unmarshal(pkg.UnsRaw, &appPkg.Uns); err != nil {
			return projectAppPackage{}, fmt.Errorf("invalid uns.json: %w", err)
		}
	}
	// artifact 包收集 flow.json/flows.json/sourceflow.json/eventflow.json 以及 flows/ 子目录下的所有 flow 文件
	appPkg.FlowFiles = collectArtifactFlowFiles(pkg.TempDir)
	return appPkg, nil
}

// parsePackageMetaOrEmpty 解析 meta.json 原文；缺失或解析失败时返回零值，不阻断导入
func parsePackageMetaOrEmpty(metaRaw []byte) projectAppMeta {
	if len(metaRaw) == 0 {
		return projectAppMeta{}
	}
	meta, err := parseProjectAppMeta(metaRaw)
	if err != nil {
		return projectAppMeta{}
	}
	return meta
}

// mergeArtifactAppSuccess 部署成功后更新应用记录：在现有 manifest 基础上合并部署结果，
// 保留 artifact_toml、deploy_config、来源身份、角色绑定等既有键，避免整体覆盖丢数据。
// appName 为空时保留应用现有名称；containerName 为空时按 appName+dockerAppID 派生（首次部署），
// 覆盖再部署时传入实际复用的容器名，保证 manifest 记录的容器名与真实容器一致、路由不悬空。
func mergeArtifactAppSuccess(ctx context.Context, projectID, appRecordID int64, appName, dockerAppID string, hostPort int, accessMode, entryURL string, extraPatch map[string]any, userID int64, containerName string) error {
	appRepo := repo.NewProjectRepo(ctx)
	app, err := appRepo.GetProjectApp(ctx, projectID, appRecordID)
	if err != nil {
		return fmt.Errorf("load app for manifest merge: %w", err)
	}
	manifest := map[string]any{}
	if len(app.ManifestJSON) > 0 {
		_ = json.Unmarshal(app.ManifestJSON, &manifest)
	}
	manifest["app_type"] = "dynamic"
	manifest["app_id"] = dockerAppID
	manifest["host_port"] = hostPort
	manifest["access_mode"] = accessMode
	manifest["deploying"] = false
	// 记录部署实际使用的容器名，覆盖再部署时复用该容器名，避免路由指向旧容器
	if strings.TrimSpace(containerName) == "" {
		containerName = appContainerName(appName, dockerAppID)
	}
	manifest["container_name"] = containerName
	delete(manifest, "deploy_error")
	for key, value := range extraPatch {
		manifest[key] = value
	}
	manifestRaw, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal merged manifest: %w", err)
	}
	// 改名预检：目标名与项目内其他存活应用重名时保留现名，只完成其余回写。
	// 直接改名会撞 (project_id, name) 唯一索引，让一场成功的部署卡在 deploying（2026-09-24 SIB 事故）。
	// GetProjectAppByName 按 LOWER(name) 匹配：目标名只是现名的大小写变体时不做预检
	//（唯一索引区分大小写，改回自己名字的大小写变体不会冲突）。
	originalName := app.Name
	if trimmed := strings.TrimSpace(appName); trimmed != "" {
		switch {
		case trimmed == app.Name:
			// 同名，无需处理
		case strings.EqualFold(trimmed, app.Name):
			// 仅大小写变体：唯一索引区分大小写，直接改名不会冲突，
			// 也无需预检（GetProjectAppByName 按 LOWER(name) 匹配只会命中自己）
			app.Name = trimmed
		default:
			if _, err := appRepo.GetProjectAppByName(ctx, projectID, trimmed); err == nil {
				logx.Errorf("[mergeArtifactAppSuccess] 应用改名跳过: appID=%d 目标名 %q 已被同项目其他应用占用，保留现名 %q", appRecordID, trimmed, app.Name)
			} else if errors.Is(err, repo.ErrNotFound) {
				app.Name = trimmed
			} else {
				// 查询失败（如 DB 瞬时抖动）无法确认占用情况：保守跳过改名，
				// deploying=false/status=active 等其余回写必须继续完成（Codex 复审意见）
				logx.Errorf("[mergeArtifactAppSuccess] 改名预检查询失败，保守跳过改名: appID=%d 目标名 %q err=%v", appRecordID, trimmed, err)
			}
		}
	}
	app.SiteType = "dynamic"
	app.Status = "active"
	app.ManifestJSON = manifestRaw
	if entryURL != "" {
		app.EntryURL = entryURL
	}
	app.UpdatedBy = userID
	// 预检与更新之间存在并发窗口（同项目其他导入/改名可能抢走同名）：UpdateProjectApp 撞
	// ErrDuplicate 时退回原名重试一次，保证 deploying=false/status=active 的回写必定完成。
	if _, err := appRepo.UpdateProjectApp(ctx, appRecordID, app); err != nil {
		if !errors.Is(err, repo.ErrDuplicate) || app.Name == originalName {
			return fmt.Errorf("update app after deploy: %w", err)
		}
		logx.Errorf("[mergeArtifactAppSuccess] 改名被并发占用，退回原名重试回写: appID=%d 目标名 %q", appRecordID, app.Name)
		app.Name = originalName
		if _, err := appRepo.UpdateProjectApp(ctx, appRecordID, app); err != nil {
			return fmt.Errorf("update app after deploy: %w", err)
		}
	}
	return nil
}

func (l *ProjectAppImportLogic) importArtifactRoles(projectID, appID int64, roles []projectPackageRole) ([]int64, error) {
	var ids []int64
	appRepo := repo.NewProjectRepo(l.ctx)
	for _, role := range roles {
		roleKey := strings.TrimSpace(role.Role)
		if roleKey == "" {
			continue
		}
		name := strings.TrimSpace(role.DisplayName)
		if name == "" {
			name = roleKey
		}
		item, err := l.svcCtx.App.Project.UpsertRole(l.ctx, projectID, roleKey, name, role.Description, logicx.UserID(l.ctx))
		if err != nil {
			return nil, err
		}
		if err := appRepo.InsertProjectRoleApps(l.ctx, projectID, item.ID, []int64{appID}); err != nil {
			return nil, err
		}
		ids = appendUniqueInt64(ids, item.ID)
	}
	return ids, nil
}

// resolveImportFile 解析导入文件路径
func (l *ProjectAppImportLogic) resolveImportFile(filePath string) (string, func(), error) {
	trimmedPath := strings.TrimSpace(filePath)
	if trimmedPath == "" {
		return "", nil, fmt.Errorf("file path is required")
	}

	if stat, err := os.Stat(trimmedPath); err == nil {
		if stat.IsDir() {
			return "", nil, fmt.Errorf("file path is a directory")
		}
		return trimmedPath, func() {}, nil
	}

	tmpZip, err := os.CreateTemp("", "project-app-import-*.zip")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpZipPath := tmpZip.Name()
	_ = tmpZip.Close()
	cleanup := func() { _ = os.Remove(tmpZipPath) }

	// 文件不存在
	cleanup()
	return "", nil, fmt.Errorf("file not found: %s", trimmedPath)
}

// Package 解析后的应用包
type Package struct {
	AppName          string
	DisplayName      string
	ArtifactManifest map[string]any
	MetaRaw          []byte // artifact 包为 artifact.toml 原文；静态包为 meta.json 原文
	RolesRaw         []byte
	Meta             projectAppMeta // 包根 meta.json 解析结果（可选，缺失为零值）
	UnsRaw           []byte         // 包根 uns.json 原文（可选）
	FlowRaw          []byte         // 包根 flow.json（优先）或 flows.json 原文（可选）
	IconRaw          []byte         // 包根 icon.* 内容（可选）
	IconName         string
	CoverRaw         []byte // 包根 cover.* 内容（可选）
	CoverName        string
	TempDir          string
}

const (
	maxPackageEntries         = 200_000
	maxPackageFileSize  int64 = 2 << 30
	maxPackageTotalSize int64 = 10 << 30
	// maxPackageMetaFileSize 快速读路径下单个元数据文件（meta.json/artifact.toml/roles.json 等）的上限
	maxPackageMetaFileSize int64 = 8 << 20
)

// readPackageRolesRaw 读取包内的 roles.json / role.json
func readPackageRolesRaw(appRoot string) []byte {
	for _, name := range []string{"roles.json", "role.json"} {
		path := filepath.Join(appRoot, name)
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return data
		}
	}
	return nil
}

// readPackageFlowRaw 读取包内的 flow.json（优先），兼容旧版 flows.json
func readPackageFlowRaw(appRoot string) []byte {
	for _, name := range []string{"flow.json", "flows.json"} {
		path := filepath.Join(appRoot, name)
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			return data
		}
	}
	return nil
}

// collectArtifactFlowFiles 收集 artifact 包内的所有 flow 文件：
// 包根 flow.json/flows.json/sourceflow.json/eventflow.json 以及 flows/ 子目录下的 .json 文件。
func collectArtifactFlowFiles(appRoot string) []projectFlowFile {
	var files []projectFlowFile
	rootNames := []string{"flow.json", "flows.json", "sourceflow.json", "eventflow.json"}
	for _, name := range rootNames {
		path := filepath.Join(appRoot, name)
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			continue
		}
		flowType := 1
		if strings.Contains(strings.ToLower(name), "event") {
			flowType = 2
		}
		files = append(files, projectFlowFile{
			Path:     name,
			Name:     strings.TrimSuffix(name, ".json"),
			Raw:      data,
			FlowType: flowType,
		})
	}
	flowsDir := filepath.Join(appRoot, "flows")
	entries, err := os.ReadDir(flowsDir)
	if err != nil {
		return files
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.EqualFold(filepath.Ext(name), ".json") {
			continue
		}
		path := filepath.Join(flowsDir, name)
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			continue
		}
		flowType := 1
		if strings.Contains(strings.ToLower(name), "event") {
			flowType = 2
		}
		files = append(files, projectFlowFile{
			Path:     filepath.ToSlash(filepath.Join("flows", name)),
			Name:     strings.TrimSuffix(name, ".json"),
			Raw:      data,
			FlowType: flowType,
		})
	}
	return files
}

// readPackageFileRaw 读取包根目录下的单个文件，缺失时返回 nil
func readPackageFileRaw(appRoot, name string) []byte {
	if data, err := os.ReadFile(filepath.Join(appRoot, name)); err == nil && len(data) > 0 {
		return data
	}
	return nil
}

// readPackageUnsRaw 读取包内的 uns.json。
func readPackageUnsRaw(appRoot string) []byte {
	path := filepath.Join(appRoot, "uns.json")
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		return data
	}
	return nil
}

// readPackageAssetRaw 按 public/ 优先、包根回退的顺序读取媒体资源，缺失时返回 nil。
// 读取前先校验文件大小，避免 artifact 路径在内容校验前就分配超大内存。
func readPackageAssetRaw(appRoot, prefix string) ([]byte, string, error) {
	for _, relativePath := range packageAssetRelativeCandidates(prefix) {
		path := filepath.Join(appRoot, filepath.FromSlash(relativePath))
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, "", err
		}
		if info.Size() == 0 {
			continue
		}
		if info.Size() > maxPackageMediaSize {
			return nil, "", fmt.Errorf("media asset %s exceeds the 20MiB limit", relativePath)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, "", err
		}
		return data, filepath.Base(path), nil
	}
	return nil, "", nil
}

// ParsePackageFromPath 从路径解析应用包
func ParsePackageFromPath(importPath string) (*Package, func(), error) {
	// 创建临时目录
	tmpDir, err := os.MkdirTemp("", "app-pkg-*")
	if err != nil {
		return nil, func() {}, fmt.Errorf("failed to create temp dir: %w", err)
	}
	cleanup := func() { os.RemoveAll(tmpDir) }

	// 解压 zip
	if err := unzip(importPath, tmpDir); err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("%w: %v", domainproject.ErrUnsupportedPackageFormat, err)
	}

	// 支持 zip 内只有一层包裹目录的情况，自动定位到包含 artifact.toml / meta.json 的目录
	appRoot, err := findPackageRoot(tmpDir)
	if err != nil {
		cleanup()
		return nil, func() {}, domainproject.ErrUnsupportedPackageFormat
	}

	pkg := &Package{TempDir: appRoot}
	pkg.RolesRaw = readPackageRolesRaw(appRoot)
	// 包根目录的可选附加文件：meta.json / uns.json / flow.json；
	// icon/cover 同时支持 public/ 优先与包根回退。
	pkg.UnsRaw = readPackageFileRaw(appRoot, "uns.json")
	pkg.FlowRaw = readPackageFlowRaw(appRoot)
	pkg.IconRaw, pkg.IconName, err = readPackageAssetRaw(appRoot, "icon")
	if err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("%w: %v", domainproject.ErrUnsupportedPackageFormat, err)
	}
	pkg.CoverRaw, pkg.CoverName, err = readPackageAssetRaw(appRoot, "cover")
	if err != nil {
		cleanup()
		return nil, func() {}, fmt.Errorf("%w: %v", domainproject.ErrUnsupportedPackageFormat, err)
	}

	// 检查是否有 artifact.toml（Docker 部署格式）
	artifactPath := filepath.Join(appRoot, "artifact.toml")
	if _, err := os.Stat(artifactPath); err == nil {
		data, err := os.ReadFile(artifactPath)
		if err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("failed to read artifact.toml: %w", err)
		}
		manifest := make(map[string]any)
		if err := toml.Unmarshal(data, &manifest); err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("failed to parse artifact.toml: %w", err)
		}
		pkg.ArtifactManifest = manifest
		pkg.MetaRaw = data

		// artifact 包的 meta.json 为可选附加文件，解析失败不阻断导入
		if metaRaw := readPackageFileRaw(appRoot, "meta.json"); len(metaRaw) > 0 {
			if meta, err := parseProjectAppMeta(metaRaw); err == nil {
				pkg.Meta = meta
			}
		}
		// 名称兜底链：artifact.toml name → 包内 meta.json name → 包文件名 → app-时间戳
		if name, ok := manifest["name"].(string); ok {
			pkg.AppName = name
		}
		if strings.TrimSpace(pkg.AppName) == "" {
			pkg.AppName = pkg.Meta.Name
		}
		pkg.AppName = resolveAppNameFallback(pkg.AppName, importPath)
		return pkg, cleanup, nil
	}

	// 检查是否有 meta.json（静态应用格式）
	metaPath := filepath.Join(appRoot, "meta.json")
	if _, err := os.Stat(metaPath); err == nil {
		data, err := os.ReadFile(metaPath)
		if err != nil {
			cleanup()
			return nil, func() {}, fmt.Errorf("failed to read meta.json: %w", err)
		}
		pkg.MetaRaw = data

		if meta, err := parseProjectAppMeta(data); err == nil {
			pkg.Meta = meta
			pkg.AppName = meta.Name
			pkg.DisplayName = meta.DisplayName
		}
		// 名称兜底链：meta.json name → 包文件名 → app-时间戳
		pkg.AppName = resolveAppNameFallback(pkg.AppName, importPath)
		return pkg, cleanup, nil
	}

	cleanup()
	return nil, func() {}, domainproject.ErrUnsupportedPackageFormat
}

// findPackageRoot 在解压目录中查找包含 artifact.toml 或 meta.json 的应用根目录。
// 包内可能存在多个同名文件（如 .template-inspect 模板目录），取路径深度最浅的候选目录，
// 避免嵌套模板目录盖过真正的应用根；深度相同时保持先遇到者。
func findPackageRoot(root string) (string, error) {
	var appRoot string
	appRootDepth := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		base := strings.ToLower(filepath.Base(path))
		if base != "artifact.toml" && base != "meta.json" {
			return nil
		}
		dir := filepath.Dir(path)
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return nil
		}
		depth := 0
		if rel != "." {
			depth = strings.Count(rel, string(filepath.Separator)) + 1
		}
		if appRoot == "" || depth < appRootDepth {
			appRoot = dir
			appRootDepth = depth
			if depth == 0 {
				// 根目录已是最浅候选，无需继续遍历
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if appRoot == "" {
		return "", domainproject.ErrUnsupportedPackageFormat
	}
	return appRoot, nil
}

// unzip 解压 zip 文件
func unzip(src, dst string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zr.Close()

	if len(zr.File) > maxPackageEntries {
		return fmt.Errorf("zip contains %d entries, limit is %d", len(zr.File), maxPackageEntries)
	}

	var totalSize int64
	for _, file := range zr.File {
		if err := validateZipEntry(file, dst); err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			continue
		}
		fileSize := file.UncompressedSize64
		if fileSize > uint64(maxPackageFileSize) {
			return fmt.Errorf("zip entry %q is %d bytes, per-file limit is %d", file.Name, fileSize, maxPackageFileSize)
		}
		if fileSize > uint64(maxPackageTotalSize) || totalSize > maxPackageTotalSize-int64(fileSize) {
			return fmt.Errorf("zip uncompressed size exceeds %d bytes", maxPackageTotalSize)
		}
		totalSize += int64(fileSize)
	}

	for _, file := range zr.File {
		if err := extractZipEntry(file, dst); err != nil {
			return err
		}
	}
	return nil
}

func validateZipEntry(file *zip.File, dst string) error {
	name := strings.ReplaceAll(file.Name, `\`, "/")
	if name == "" || strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") {
		return fmt.Errorf("unsafe zip entry path %q", file.Name)
	}
	cleanName := filepath.Clean(filepath.FromSlash(name))
	if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) || filepath.IsAbs(cleanName) {
		return fmt.Errorf("unsafe zip entry path %q", file.Name)
	}
	target := filepath.Join(dst, cleanName)
	rel, err := filepath.Rel(dst, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("unsafe zip entry path %q", file.Name)
	}
	mode := file.Mode()
	if !file.FileInfo().IsDir() && mode&os.ModeType != 0 {
		return fmt.Errorf("unsupported zip entry type %q", file.Name)
	}
	// file.Mode() 仅在 creator 为 Unix/macOS 时才把 external attrs 的 S_IFMT 映射为类型位，
	// 非 Unix creator（如 Python zipfile 默认 create_system=0）会把 symlink/device 编码成普通文件绕过。
	// 直接查 raw external attrs 高 16 位的 S_IFMT：仅放行未知(0)/目录(IFDIR)/常规文件(IFREG)。
	const sIfmt = 0xF000
	switch unixMode := file.ExternalAttrs >> 16; unixMode & sIfmt {
	case 0, 0x4000, 0x8000: // unknown / S_IFDIR / S_IFREG
	default: // S_IFLNK(0xA000)/S_IFIFO(0x1000)/S_IFCHR(0x2000)/S_IFBLK(0x6000)/S_IFSOCK(0xC000)
		return fmt.Errorf("unsupported zip entry type %q", file.Name)
	}
	return nil
}

func extractZipEntry(file *zip.File, dst string) error {
	name := strings.ReplaceAll(file.Name, `\`, "/")
	target := filepath.Join(dst, filepath.Clean(filepath.FromSlash(name)))
	if file.FileInfo().IsDir() {
		if err := os.MkdirAll(target, 0o755); err != nil {
			return fmt.Errorf("create zip directory %q: %w", file.Name, err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create parent directory for %q: %w", file.Name, err)
	}

	rc, err := file.Open()
	if err != nil {
		return fmt.Errorf("open zip entry %q: %w", file.Name, err)
	}
	defer rc.Close()

	perm := file.Mode().Perm()
	if perm == 0 {
		perm = 0o644
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("create zip entry %q: %w", file.Name, err)
	}
	written, copyErr := io.Copy(out, io.LimitReader(rc, maxPackageFileSize+1))
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("extract zip entry %q: %w", file.Name, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close zip entry %q: %w", file.Name, closeErr)
	}
	if written > maxPackageFileSize {
		return fmt.Errorf("zip entry %q exceeds per-file limit of %d bytes", file.Name, maxPackageFileSize)
	}
	if uint64(written) != file.UncompressedSize64 {
		return fmt.Errorf("zip entry %q size mismatch: expected %d bytes, extracted %d", file.Name, file.UncompressedSize64, written)
	}
	return nil
}

// readZipEntryLimited 读取单个 zip 条目内容，超过 limit 字节时报错而非静默截断
// （与 extractZipEntry 的限量读取同思路，供快速读路径复用）。
func readZipEntryLimited(file *zip.File, limit int64) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("zip entry %q exceeds limit of %d bytes", file.Name, limit)
	}
	return raw, nil
}

// resolveAppNameFallback 应用名兜底链（用户指定名在调用方优先于本链）：
// 包内名称（artifact.toml/meta.json）→ 包文件名去扩展名 → app-时间戳。
func resolveAppNameFallback(packageName, packagePath string) string {
	if name := strings.TrimSpace(packageName); name != "" {
		return name
	}
	base := strings.TrimSuffix(filepath.Base(packagePath), filepath.Ext(packagePath))
	// filepath.Base("") 返回 "."，空路径同样落到时间戳
	if name := strings.TrimSpace(base); name != "" && name != "." {
		return name
	}
	return "app-" + time.Now().Format("20060102150405")
}

// mapDeployCASError 把 repo 层 CAS 标记 deploying 的并发冲突错误映射为领域层可读错误
// （复用 project.appReplaceInProgress i18n：该应用正在部署中，请等待当前部署完成后再试）。
func mapDeployCASError(err error) error {
	if errors.Is(err, repo.ErrAppDeployInProgress) {
		return domainproject.ErrAppReplaceInProgress
	}
	return err
}

// markAppReplaceDeploying 以 CAS 语义标记应用进入覆盖/更新导入部署（并发互斥）：
// 先 CAS 把 status 置为 deploying（仅 status<>deploying 时命中），成功后才写 manifest 的
// deploying 标记；CAS 失败（并发胜者已在部署）时不改写 manifest——此时 manifest 的 deploying
// 标记属于胜者，直接返回可读错误。manifest 写入失败时回滚 CAS 状态到 prevStatus，
// 避免应用卡在 deploying。
func markAppReplaceDeploying(ctx context.Context, projectRepo *repo.ProjectRepo, projectID, appID int64, prevStatus string, userID int64, networkAlias ...string) error {
	if len(networkAlias) > 0 && networkAlias[0] != "" {
		err := projectRepo.MarkAppDeployingWithNetworkAlias(ctx, projectID, appID, userID, networkAlias[0])
		if errors.Is(err, repo.ErrAppNetworkAliasTaken) {
			return respx.NewHTTPError(409, err.Error())
		}
		return mapDeployCASError(err)
	}
	if err := projectRepo.UpdateProjectAppStatusDeployingCAS(ctx, projectID, appID, userID); err != nil {
		return mapDeployCASError(err)
	}
	if err := projectRepo.UpdateProjectAppManifest(ctx, projectID, appID, map[string]any{
		"deploying":    true,
		"deploy_error": "",
	}, userID); err != nil {
		if rollbackErr := projectRepo.UpdateProjectAppStatus(ctx, projectID, appID, prevStatus, userID); rollbackErr != nil {
			logx.WithContext(ctx).Errorf("rollback deploy status after manifest mark failed: projectID=%d appID=%d err=%v", projectID, appID, rollbackErr)
		}
		return err
	}
	return nil
}

// formatDeployError 把部署错误与容器日志合并为前端可展示的摘要，限制长度避免 JSON 过大。
// 容器日志中可能包含 DATABASE_URL、TIER0_API_KEY 等敏感信息，写入 deploy_error 前先脱敏。
func formatDeployError(deployErr error, deployLogs string) string {
	msg := deployErr.Error()
	if deployLogs != "" {
		msg += "\n--- container logs ---\n" + redactDeployLogs(deployLogs)
	}
	const maxLen = 4000
	if len(msg) > maxLen {
		msg = msg[:maxLen] + "\n...(truncated)"
	}
	return msg
}

// redactDeployLogs 对容器日志中的敏感字段做脱敏处理。
func redactDeployLogs(logs string) string {
	// DATABASE_URL=... / "DATABASE_URL": "..." / export DATABASE_URL=...
	logs = regexp.MustCompile(`(?i)(DATABASE_URL\s*[=:]\s*)[^\s]+`).ReplaceAllString(logs, `${1}***REDACTED***`)
	// TIER0_API_KEY=... / "TIER0_API_KEY": "..."
	logs = regexp.MustCompile(`(?i)(TIER0_API_KEY\s*[=:]\s*)[^\s]+`).ReplaceAllString(logs, `${1}***REDACTED***`)
	// 通用连接串密码：postgresql://user:pass@host -> postgresql://user:***@host
	logs = regexp.MustCompile(`(://[^:]+:)[^@]+(@)`).ReplaceAllString(logs, `${1}***REDACTED***$2`)
	return logs
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ProjectAppImportReplace 处理 Import to replace 模式的覆盖导入：
// 仅接受含 artifact.toml 的 Export Bundle，校验包内 meta.json 身份与目标应用记录的来源身份一致后，
// 保留应用记录 ID/appKey/docker app_id/host_port/EntryURL，走可回退的原地重建部署。
// 身份只按 appId 判定：不一致或无法校验（缺/空 appId）一律硬阻断。
func (l *ProjectAppImportLogic) ProjectAppImportReplace(projectID, appID int64, importPath string, description string, cleanupPath func(), opts artifactDeployOptions) (*repo.ProjectApp, error) {
	if projectID <= 0 || appID <= 0 {
		return nil, fmt.Errorf("project ID and app ID are required")
	}
	projectRepo := repo.NewProjectRepo(l.ctx)
	project, err := projectRepo.GetProject(l.ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	// 目标应用必须存在且属于当前 project（repo 查询带 project_id 条件）
	app, err := projectRepo.GetProjectApp(l.ctx, projectID, appID)
	if err != nil {
		// 目标 App 缺失（如已被删除）与 Project 缺失区分开，返回 App 级错误
		return nil, domainproject.ErrAppNotFound
	}
	// Replace 不提交 schema（前端忽略）：强制沿用首次导入时预占/部署落库的 schema 名，
	// 避免回退 app_{id} 导致重建容器连到新的空 schema、丢失既有数据。
	if stored := strings.TrimSpace(app.SchemaName); stored != "" {
		opts.schemaName = stored
	}
	// 同一 App 的覆盖导入串行：目标 App 正在部署中（含进行中的覆盖导入）时直接拒绝
	if app.Status == "deploying" {
		if cleanupPath != nil {
			cleanupPath()
		}
		return nil, domainproject.ErrAppReplaceInProgress
	}
	// 部署失败的 App 不允许覆盖更新，与 static/manual 同等拒绝
	if app.Status == "failed" {
		if cleanupPath != nil {
			cleanupPath()
		}
		return nil, domainproject.ErrAppReplaceInvalidTarget
	}

	// 目标必须是已部署的动态应用（manifest 含 docker app_id），否则无法原地重建
	targetManifest := map[string]any{}
	if len(app.ManifestJSON) > 0 {
		_ = json.Unmarshal(app.ManifestJSON, &targetManifest)
	}
	if opts.networkAlias == "" {
		opts.networkAlias = manifestStringValue(targetManifest, "network_alias")
	}
	dockerAppID, _ := targetManifest["app_id"].(string)
	hostPort := int(int64Value(targetManifest["host_port"]))
	if app.SiteType != "dynamic" || strings.TrimSpace(dockerAppID) == "" {
		if cleanupPath != nil {
			cleanupPath()
		}
		return nil, domainproject.ErrAppReplaceInvalidTarget
	}
	accessMode := manifestStringValue(targetManifest, "access_mode")
	if accessMode == "" {
		accessMode = l.svcCtx.Config.AppGateway.DefaultAccessMode
	}

	// 磁盘映射归一：inherit 沿用 manifest 已持久化列表，replace 用用户提交列表（buildImportOptions 已校验）；
	// 归一后部署层只看 opts.volumes
	opts.volumes = resolveArtifactVolumes(opts.volumePolicy, opts.volumes, manifestVolumes(targetManifest))
	opts.volumePolicy = "replace"

	// 覆盖导入仅接受含 artifact.toml 的 Export Bundle
	artifactInfo, err := readArtifactFromZip(importPath)
	if err != nil || artifactInfo.Manifest == nil {
		if cleanupPath != nil {
			cleanupPath()
		}
		return nil, domainproject.ErrUnsupportedPackageFormat
	}

	// 身份校验：目标应用已记录 sourceAppId 且包内 meta.json 携带 appId 时才可校验，否则拒绝
	pkgMeta, err := readPackageMetaFromZip(importPath)
	if err != nil {
		if cleanupPath != nil {
			cleanupPath()
		}
		// meta.json 格式错误属于包格式问题，按 400 返回而非透传 500
		return nil, fmt.Errorf("%w: %v", domainproject.ErrUnsupportedPackageFormat, err)
	}
	// 覆盖导入不更改应用名称；描述与首次导入一致走"请求侧优先、空回退包内"，仅校验生效描述长度（rune≤200，超限 400）
	resolvedDesc := resolveImportAppDescription(description, pkgMeta.Description)
	if err := validateProjectAppMetaLength("", resolvedDesc); err != nil {
		if cleanupPath != nil {
			cleanupPath()
		}
		return nil, err
	}
	if err := validateReplaceIdentity(targetManifest, pkgMeta); err != nil {
		// 身份不一致或无法校验一律硬阻断，不再提供前端确认放行
		if cleanupPath != nil {
			cleanupPath()
		}
		return nil, err
	}

	// 同步解析角色，格式非法直接失败
	roles, err := parseProjectAppRoles(artifactInfo.RolesRaw)
	if err != nil {
		if cleanupPath != nil {
			cleanupPath()
		}
		return nil, fmt.Errorf("failed to parse roles: %w", err)
	}

	// 同步校验 artifact 配置，非法包直接拒绝（不标部署中）
	if err := validateArtifactManifest(artifactInfo.Manifest); err != nil {
		if cleanupPath != nil {
			cleanupPath()
		}
		return nil, err
	}
	// 覆盖导入同样在标记部署中之前校验运行入口文件，避免空转到异步替换流程
	if err := validateArtifactBuiltOutput(artifactInfo.Manifest, func(entry string) bool {
		exists, _ := zipDirOrFileExists(importPath, entry)
		return exists
	}); err != nil {
		if cleanupPath != nil {
			cleanupPath()
		}
		return nil, err
	}
	if err := validateArtifactRunEntry(artifactInfo.Manifest, func(entry string) bool {
		rel, ok := artifactEntryRelPath(artifactInfo.ArtifactDir, entry)
		if !ok {
			return false
		}
		exists, _ := zipEntryExists(importPath, rel)
		return exists
	}); err != nil {
		if cleanupPath != nil {
			cleanupPath()
		}
		return nil, err
	}

	// 覆盖导入收尾会把应用改名为包内名称（meta.name）：包名与项目内其他存活应用重名时，
	// 部署必然在最后一步回写失败并卡死 deploying，这里提前 409 阻断，避免整场部署白跑。
	// 目标名仅为现名的大小写变体时跳过检查（GetProjectAppByName 按 LOWER(name) 匹配会命中自己，
	// 唯一索引区分大小写，改回自己名字的大小写变体安全）。
	if finalName := strings.TrimSpace(pkgMeta.Name); finalName != "" && !strings.EqualFold(finalName, app.Name) {
		if err := l.checkAppNameDuplicate(projectID, finalName); err != nil {
			if cleanupPath != nil {
				cleanupPath()
			}
			return nil, err
		}
	}

	// 覆盖导入不新建应用，跳过重名检查与配额检查；标记部署中。
	// 标记前快照当前 manifest 与 icon：后台 goroutine 会写入新包资源 ID/icon，任何失败分支需先还原快照。
	userID := logicx.UserID(l.ctx)
	manifestSnapshot := append(json.RawMessage(nil), app.ManifestJSON...)
	iconAssetIDSnapshot := app.IconAssetID
	hasRolesFile := len(artifactInfo.RolesRaw) > 0
	// 先 CAS 标记 deploying 再写 manifest：CAS 失败（并发胜者已在部署）时不改写 manifest，
	// 避免清掉胜者的部署标记（旧实现先写 manifest 再 CAS，失败回滚会把胜者标记清掉）
	if err := markAppReplaceDeploying(l.ctx, projectRepo, projectID, appID, app.Status, userID, opts.networkAlias); err != nil {
		if cleanupPath != nil {
			cleanupPath()
		}
		return nil, err
	}

	// 复用首次部署时的容器名，避免应用重命名后路由指向旧容器
	containerName, _ := targetManifest["container_name"].(string)
	if strings.TrimSpace(containerName) == "" {
		containerName = appContainerName(app.Name, dockerAppID)
	}

	go l.runArtifactReplaceAsync(project, app, importPath, roles, hasRolesFile, dockerAppID, hostPort, accessMode, containerName, manifestSnapshot, iconAssetIDSnapshot, resolvedDesc, cleanupPath, opts)
	app.Status = "deploying"
	return &app, nil
}

// runArtifactReplaceAsync 在后台 goroutine 执行覆盖导入：解压新包、导入包内资源、可回退地原地重建部署。
// 失败时先把 manifest 与 icon_asset_id 还原到 replace 前快照再标记 failed（不删除应用）；
// 部署层的回退机制保证旧版本继续可用。resolvedDesc 为请求侧优先解析后的生效描述，落库前覆写包内 meta。
func (l *ProjectAppImportLogic) runArtifactReplaceAsync(project repo.Project, app repo.ProjectApp, importPath string, roles []projectPackageRole, hasRolesFile bool, dockerAppID string, hostPort int, accessMode, containerName string, manifestSnapshot json.RawMessage, iconAssetIDSnapshot int64, resolvedDesc string, cleanupPath func(), opts artifactDeployOptions) {
	ctx := context.Background()
	if subject, ok := contextx.SubjectFrom(l.ctx); ok {
		ctx = contextx.WithSubject(ctx, subject)
	}
	logger := logx.WithContext(ctx)
	appRepo := repo.NewProjectRepo(ctx)
	projectID := project.ID
	appID := app.ID
	userID := logicx.UserID(l.ctx)

	// 本次新建的资源 ID 集合，失败分支据此逆序清理（只删 INSERT 的，不含命中更新的既有资源）
	var created importCreatedResources

	// 快照还原成功说明旧版本部署仍可用，状态恢复到 replace/update 前（通常 active），
	// 避免应用在 Launchpad 中消失；还原失败才落 failed。deploy_error 已写入 manifest 供前端提示
	prevStatus := app.Status
	if prevStatus == "" || prevStatus == "deploying" {
		prevStatus = "failed"
	}
	markFailed := func(stage, deployError string) {
		logger.Errorf("[artifactReplace] %s: appID=%d, err=%s", stage, appID, deployError)
		if err := restoreReplaceSnapshot(ctx, appRepo, projectID, appID, manifestSnapshot, iconAssetIDSnapshot, deployError, userID); err != nil {
			logger.Errorf("[artifactReplace] 还原应用快照失败: appID=%d err=%v", appID, err)
			_ = appRepo.UpdateProjectAppStatus(ctx, projectID, appID, "failed", userID)
			return
		}
		_ = appRepo.UpdateProjectAppStatus(ctx, projectID, appID, prevStatus, userID)
	}

	// 后台解压新包
	pkg, cleanup, err := ParsePackageFromPath(importPath)
	if cleanupPath != nil {
		cleanupPath()
	}
	if err != nil {
		markFailed("unpack failed", formatDeployError(err, ""))
		return
	}
	defer cleanup()

	// 描述以请求侧优先解析结果为准（resolvedDesc 已在同步阶段按"请求值非空优先、空回退包内 meta"解析），
	// 提前覆写包内 meta，使资源导入与最终 manifest 回写使用同一份生效描述（与首次导入路径一致）
	pkg.Meta.Description = resolvedDesc

	// 新包包含 roles 文件时采用替换语义：导入新角色前先删除该 App 现有角色绑定；
	// 新包无 roles 文件时不动现有绑定。绑定快照用于任何失败分支恢复 replace 前状态。
	var staleRoleIDs []int64
	var roleBindingSnapshot []int64
	if hasRolesFile {
		roleApps, err := appRepo.ListProjectRoleAppIDsByAppIDs(ctx, projectID, []int64{appID})
		if err != nil {
			markFailed("role binding cleanup failed", formatDeployError(err, ""))
			return
		}
		roleBindingSnapshot = roleIDsFromBindings(roleApps)
		if err := appRepo.DeleteProjectRoleAppsByAppID(ctx, projectID, appID); err != nil {
			markFailed("role binding cleanup failed", formatDeployError(err, ""))
			return
		}
		staleRoleIDs = roleBindingSnapshot
	}
	// 失败时把角色绑定恢复到 replace 前快照，并清理本次失败导入遗留的孤儿角色
	restoreRoleBindings := func() {
		if !hasRolesFile {
			return
		}
		current, err := appRepo.ListProjectRoleAppIDsByAppIDs(ctx, projectID, []int64{appID})
		if err != nil {
			logger.Errorf("[artifactReplace] 读取当前角色绑定失败: appID=%d err=%v", appID, err)
			return
		}
		if err := appRepo.DeleteProjectRoleAppsByAppID(ctx, projectID, appID); err != nil {
			logger.Errorf("[artifactReplace] 清理失败后角色绑定失败: appID=%d err=%v", appID, err)
			return
		}
		for _, roleID := range roleBindingSnapshot {
			if err := appRepo.InsertProjectRoleApps(ctx, projectID, roleID, []int64{appID}); err != nil {
				logger.Errorf("[artifactReplace] 恢复角色绑定失败: appID=%d roleID=%d err=%v", appID, roleID, err)
			}
		}
		// 本次失败导入新绑定的角色在恢复快照后成为孤儿，顺带清理（含其成员绑定）
		if ids := roleIDsFromBindings(current); len(ids) > 0 {
			if _, err := appRepo.DeleteProjectRolesIfNoApps(ctx, projectID, ids); err != nil {
				logger.Errorf("[artifactReplace] 清理失败导入遗留角色失败: appID=%d err=%v", appID, err)
			}
		}
		appgateway.InvalidateRoleCache()
	}
	markFailedWithRoleRestore := func(stage, deployError string) {
		restoreRoleBindings()
		// 失败补偿：保留旧版本仍在使用的网关路由，逆序清理本次新建的资源。
		cleanupReplacedAppResources(ctx, created, projectID, appID, userID)
		markFailed(stage, deployError)
	}

	// 包内资源（角色/UNS/Flow），复用静态包导入逻辑
	appWorker := &ProjectAppLogic{ctx: ctx, svcCtx: l.svcCtx, Logger: logger}
	appPkg, err := buildProjectAppPackage(pkg, roles)
	if err != nil {
		markFailedWithRoleRestore("resource import failed", formatDeployError(err, ""))
		return
	}
	resourcePatch, resourceCreated, err := appWorker.importPackageResources(projectID, appID, appPkg, true)
	created.merge(resourceCreated)
	if err != nil {
		markFailedWithRoleRestore("resource import failed", formatDeployError(err, ""))
		return
	}

	// 包根 icon/cover 上传 asset
	assetPatch, assetIDs, err := appWorker.importPackageAssets(projectID, appID, appPkg, userID)
	created.AssetIDs = append(created.AssetIDs, assetIDs...)
	if err != nil {
		markFailedWithRoleRestore("asset import failed", formatDeployError(err, ""))
		return
	}

	// 可回退的原地重建部署：复用同一 docker app_id、容器名与 host_port，跳过网关路由注册
	// schema 不可变：opts.schemaName 为空（Replace 不接受 schema 提交），deploy 层沿用 app.SchemaName 回退
	_, _, deployLogs, deployErr := artifactRedeploy(ctx, l.svcCtx.Config, l.svcCtx.App.TenantPGLifecycle, project.DatabaseName, project.Name, pkg.ArtifactManifest, app.Name, pkg.TempDir, projectID, appID, dockerAppID, hostPort, containerName, opts)
	if deployErr != nil {
		markFailedWithRoleRestore("deploy failed", formatDeployError(deployErr, deployLogs))
		return
	}

	// 部署成功后才清理仅绑定过本 App 且本次未再绑定的孤儿角色：
	// 提前删除会导致失败分支无法把旧角色绑定恢复到仍然存在的角色上
	if len(staleRoleIDs) > 0 {
		if _, err := appRepo.DeleteProjectRolesIfNoApps(ctx, projectID, staleRoleIDs); err != nil {
			logger.Errorf("[artifactReplace] 清理孤儿角色失败: projectID=%d appID=%d err=%v", projectID, appID, err)
		}
	}

	// 成功：来源身份不变；name/description 非空覆盖、空保留；version/changelog 按新包更新（缺失保留旧值）
	extraPatch := map[string]any{
		"deploy_config": pkg.ArtifactManifest,
	}
	if artifactTomlRaw, err := json.Marshal(pkg.ArtifactManifest); err == nil && len(artifactTomlRaw) > 0 {
		extraPatch["artifact_toml"] = string(artifactTomlRaw)
	}
	for key, value := range resourcePatch {
		extraPatch[key] = value
	}
	for key, value := range assetPatch {
		extraPatch[key] = value
	}
	// Replace 持久化生效环境变量（读取既有 manifest.env 作默认基础，用户可编辑；不含平台强制变量）
	extraPatch["network_alias"] = opts.networkAlias
	extraPatch["network_alias_pending"] = ""
	extraPatch["container_port"] = parseArtifactConfig(pkg.ArtifactManifest).ContainerPort
	extraPatch["env"] = resolveArtifactEnv(parseArtifactConfig(pkg.ArtifactManifest).Env, opts.envPolicy, opts.envVars)
	// 持久化生效磁盘映射（部署前已按 volumePolicy 归一为最终列表）
	extraPatch[volumeManifestKey] = volumesOrEmpty(opts.volumes)
	applyPackageMetaToManifest(extraPatch, pkg.Meta, false)
	if err := mergeArtifactAppSuccess(ctx, projectID, appID, pkg.Meta.Name, dockerAppID, hostPort, accessMode, app.EntryURL, extraPatch, userID, containerName); err != nil {
		logger.Errorf("[artifactReplace] 回写部署结果失败: appID=%d err=%v", appID, err)
	}
	logger.Infof("[artifactReplace] 覆盖部署成功: appID=%d, dockerAppID=%s, hostPort=%d", appID, dockerAppID, hostPort)
}

// restoreReplaceSnapshot 覆盖导入失败时把 manifest 与 icon_asset_id 还原到 replace 前的快照，
// 并在快照 manifest 上写入 deploying=false 与 deploy_error。
func restoreReplaceSnapshot(ctx context.Context, appRepo *repo.ProjectRepo, projectID, appID int64, manifestSnapshot json.RawMessage, iconAssetID int64, deployError string, userID int64) error {
	app, err := appRepo.GetProjectApp(ctx, projectID, appID)
	if err != nil {
		return fmt.Errorf("load app for snapshot restore: %w", err)
	}
	app.ManifestJSON = buildRestoredManifest(manifestSnapshot, deployError)
	app.IconAssetID = iconAssetID
	app.UpdatedBy = userID
	if _, err := appRepo.UpdateProjectApp(ctx, appID, app); err != nil {
		return fmt.Errorf("restore app snapshot: %w", err)
	}
	return nil
}

// buildRestoredManifest 在 manifest 快照上写入失败标记（纯函数，便于单测）
func buildRestoredManifest(snapshot json.RawMessage, deployError string) json.RawMessage {
	manifest := map[string]any{}
	if len(snapshot) > 0 {
		_ = json.Unmarshal(snapshot, &manifest)
	}
	manifest["deploying"] = false
	if deployError != "" {
		manifest["deploy_error"] = deployError
	} else {
		delete(manifest, "deploy_error")
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return snapshot
	}
	return raw
}

// roleIDsFromBindings 从角色应用绑定记录中提取去重后的角色 ID 列表
func roleIDsFromBindings(roleApps []repo.ProjectRoleApp) []int64 {
	roleIDs := make([]int64, 0, len(roleApps))
	seen := make(map[int64]struct{}, len(roleApps))
	for _, ra := range roleApps {
		if _, ok := seen[ra.RoleID]; ok {
			continue
		}
		seen[ra.RoleID] = struct{}{}
		roleIDs = append(roleIDs, ra.RoleID)
	}
	return roleIDs
}

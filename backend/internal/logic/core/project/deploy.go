package project

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"backend/internal/appgateway"
	"backend/internal/config"
	"backend/internal/contextx"
	domaincommon "backend/internal/domain/common"
	domainproject "backend/internal/domain/project"
	"backend/internal/repo"
	"backend/internal/service/tenantpg"

	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

// appSchemaName 根据 project_app 自增 ID 生成应用独立 schema 名
func appSchemaName(appRecordID int64) string {
	return fmt.Sprintf("app_%d", appRecordID)
}

const artifactRecoveryTimeout = 5 * time.Minute

const appRoleSecretRequired = "TENANT_PG_ROLE_SECRET is required for managed App database credentials"

// artifactDeployTarget 描述一次部署的目标身份。
// 零值为全新部署（新建 docker UUID、分配新端口、注册网关路由）；
// 覆盖导入（Import to replace）时复用既有 docker app 身份、容器名与 host_port 做可回退的原地重建，网关路由已存在无需重复注册。
type artifactDeployTarget struct {
	dockerAppID   string // 复用的 docker 应用 UUID；空表示新建
	hostPort      int    // 复用的宿主机端口；0 表示重新分配
	skipRouteReg  bool   // true 时跳过网关路由注册（覆盖导入时路由已存在）
	rollback      bool   // true 时启用可回退的原地重建（旧目录/旧镜像先备份，失败自动恢复）
	containerName string // 复用的容器名；空表示按 appName+appID 重新派生
}

// artifactDeploy 部署应用（全新安装）。opts 可指定 schema 名与用户环境变量（零值保持现状行为）。
func artifactDeploy(
	ctx context.Context,
	conf config.Config,
	lifecycle *tenantpg.Lifecycle,
	projectDB string,
	projectName string,
	manifest map[string]any,
	appName string,
	zipTempDir string,
	projectID int64,
	appRecordID int64,
	opts artifactDeployOptions,
) (appID string, hostPort int, deployLogs string, retErr error) {
	return artifactDeployWithTarget(ctx, conf, lifecycle, projectDB, projectName, manifest, appName, zipTempDir, projectID, appRecordID, opts, artifactDeployTarget{})
}

// artifactRedeploy 覆盖导入（Import to replace）的部署入口：
// 复用既有 docker app_id、容器名与 host_port 做可回退的原地重建，网关路由已存在无需重复注册。
// opts 仅用于环境变量（schema 不可变，沿用 project_app.schema_name）。
func artifactRedeploy(
	ctx context.Context,
	conf config.Config,
	lifecycle *tenantpg.Lifecycle,
	projectDB string,
	projectName string,
	manifest map[string]any,
	appName string,
	zipTempDir string,
	projectID int64,
	appRecordID int64,
	dockerAppID string,
	hostPort int,
	containerName string,
	opts artifactDeployOptions,
) (appID string, outHostPort int, deployLogs string, retErr error) {
	if strings.TrimSpace(dockerAppID) == "" || hostPort <= 0 {
		return "", 0, "", fmt.Errorf("redeploy requires existing docker app id and host port")
	}
	return artifactDeployWithTarget(ctx, conf, lifecycle, projectDB, projectName, manifest, appName, zipTempDir, projectID, appRecordID, opts, artifactDeployTarget{
		dockerAppID:   dockerAppID,
		hostPort:      hostPort,
		skipRouteReg:  true,
		rollback:      true,
		containerName: containerName,
	})
}

// artifactDeployWithTarget 执行 Docker 部署主流程。
// 整体分准备、切换两个阶段：准备阶段（文件/镜像/schema/迁移）失败直接报错，不影响线上旧版本；
// 切换阶段仅覆盖导入（target.rollback=true）时启用回退——旧目录改名备份、旧镜像打 rollback tag，
// 新容器启动或健康检查失败时自动恢复旧版本。
func artifactDeployWithTarget(
	ctx context.Context,
	conf config.Config,
	lifecycle *tenantpg.Lifecycle,
	projectDB string,
	projectName string,
	manifest map[string]any,
	appName string,
	zipTempDir string,
	projectID int64,
	appRecordID int64,
	opts artifactDeployOptions,
	target artifactDeployTarget,
) (appID string, hostPort int, deployLogs string, retErr error) {
	installationID := strings.TrimSpace(os.Getenv("TIER0_INSTALLATION_ID"))
	if installationID == "" {
		return "", 0, "", errors.New("TIER0_INSTALLATION_ID is required to deploy a managed app container")
	}
	if strings.TrimSpace(conf.TenantPG.RoleSecret) == "" {
		return "", 0, "", errors.New(appRoleSecretRequired)
	}
	newAppID := target.dockerAppID
	if newAppID == "" {
		newAppID = uuid.New().String()
	}
	containerName := target.containerName
	if containerName == "" {
		containerName = appContainerName(appName, newAppID)
	}
	if err := validateAppNetworkAlias(opts.networkAlias); err != nil {
		return "", 0, "", err
	}
	if err := checkDockerNetworkAlias(ctx, opts.networkAlias, containerName); err != nil {
		return "", 0, "", err
	}
	appDir := filepath.Join("/app/data/apps", newAppID)
	oldAppImage := dockerImageSnapshot{}
	if target.rollback {
		composePath := filepath.Join(appDir, "docker-compose.yml")
		if snapshot, err := inspectAppDockerImage(ctx, containerName, composePath); err != nil {
			return "", 0, "", fmt.Errorf("inspect old app image before redeploy: %w", err)
		} else {
			oldAppImage = snapshot
		}
	}
	// 覆盖导入时先在旁路目录准备新文件，避免准备阶段失败影响正在运行的旧版本
	workDir := appDir
	if target.rollback {
		workDir = appDir + ".new"
		_ = os.RemoveAll(workDir)
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		return "", 0, "", fmt.Errorf("failed to create app dir: %w", err)
	}

	switched := false
	// oldStopped 标记旧容器已停止但目录交换尚未完成：此阶段失败需仅用旧 compose 重启旧版本
	oldStopped := false
	rollbackImage := ""
	image := ""
	targetImageBeforePullID := ""
	targetImageAfterPullID := ""
	// cfg/healthPath 在 defer 之后的准备阶段才赋值，这里提前声明供失败回退引用
	var cfg artifactConfig
	healthPath := ""
	// 覆盖导入时在 drizzle push 前备份应用 schema；schemaDirty 标记迁移已执行（schema 可能已被改动）
	schemaBackupPath := ""
	schemaDirty := false
	defer func() {
		if retErr == nil {
			// 部署成功：删除 schema 备份文件
			if schemaBackupPath != "" {
				if err := os.Remove(schemaBackupPath); err != nil {
					logx.Errorf("[artifactDeploy] 删除 schema 备份文件失败: %s err=%v", schemaBackupPath, err)
				}
			}
			return
		}
		if containerName != "" {
			deployLogs = captureContainerLogs(containerName)
		}
		// pull 或 pull 后 inspect 报错时再读取一次，确保已落到本地的失败镜像也进入清理路径。
		if targetImageAfterPullID == "" && strings.TrimSpace(image) != "" {
			if imageID, inspectErr := dockerImageID(image); inspectErr == nil {
				targetImageAfterPullID = imageID
			}
		}
		imageReferenceRestored := targetImageBeforePullID == "" || !dockerImageChanged(targetImageBeforePullID, targetImageAfterPullID)
		if targetImageBeforePullID != "" && dockerImageChanged(targetImageBeforePullID, targetImageAfterPullID) {
			if rbErr := restoreDockerImageReference(image, rollbackImage, targetImageBeforePullID, targetImageAfterPullID); rbErr != nil {
				logx.Errorf("[artifactDeploy] 恢复 pull 前镜像引用失败: image=%s err=%v", image, rbErr)
				retErr = fmt.Errorf("%w（恢复旧镜像引用失败: %v）", retErr, rbErr)
			} else {
				imageReferenceRestored = true
			}
		}
		if !target.rollback {
			if err := cleanupApp(appDir, containerName); err != nil {
				logx.Errorf("[artifactDeploy] cleanup failed after deploy error: appDir=%s err=%v", appDir, err)
			}
			cleanupPulledImageAfterFailure(image, targetImageBeforePullID, targetImageAfterPullID, imageReferenceRestored)
			removeRollbackImageTag(rollbackImage)
			return
		}
		// 部署失败时原 ctx 可能已超时或被取消；回滚必须使用独立、受限的新 context，
		// 否则 compose down/up、健康检查和 schema 恢复会立刻返回 deadline exceeded。
		recoveryCtx, cancelRecovery := context.WithTimeout(context.Background(), artifactRecoveryTimeout)
		defer cancelRecovery()
		switch resolveDeployFailureAction(switched, oldStopped) {
		case deployFailureRollback:
			// 切换后失败：回退到旧版本应用目录与旧镜像，保证旧版本继续可用
			if rbErr := rollbackReplacedApp(recoveryCtx, appDir, containerName, cfg.ContainerPort, healthPath); rbErr != nil {
				logx.Errorf("[artifactDeploy] 覆盖部署失败后回退旧版本也失败: appDir=%s err=%v", appDir, rbErr)
				retErr = fmt.Errorf("%w（回退旧版本失败: %v）", retErr, rbErr)
			} else {
				retErr = fmt.Errorf("%w（已回退到旧版本）", retErr)
			}
		case deployFailureRestartOld:
			// 旧容器已停止但目录交换未完成：不动目录，仅用旧 compose 重启旧版本
			if rbErr := restartStoppedOldVersion(recoveryCtx, appDir, containerName, cfg.ContainerPort, healthPath); rbErr != nil {
				logx.Errorf("[artifactDeploy] 覆盖部署失败后重启旧版本也失败: appDir=%s err=%v", appDir, rbErr)
				retErr = fmt.Errorf("%w（重启旧版本失败: %v）", retErr, rbErr)
			} else {
				retErr = fmt.Errorf("%w（已重启旧版本）", retErr)
			}
			_ = os.RemoveAll(workDir)
		default:
			// 准备阶段失败：仅清理旁路目录，线上旧版本未受影响
			_ = os.RemoveAll(workDir)
		}
		cleanupPulledImageAfterFailure(image, targetImageBeforePullID, targetImageAfterPullID, imageReferenceRestored)
		removeRollbackImageTag(rollbackImage)
		// 迁移执行过后失败：先恢复 schema 备份；迁移前就失败（schema 未动）只删备份文件
		if schemaBackupPath != "" {
			if schemaDirty {
				if rbErr := restoreAppSchema(recoveryCtx, conf, lifecycle, projectDB, appSchemaName(appRecordID), schemaBackupPath); rbErr != nil {
					logx.Errorf("[artifactDeploy] 恢复应用 schema 备份失败: projectDB=%s schema=%s err=%v", projectDB, appSchemaName(appRecordID), rbErr)
					retErr = fmt.Errorf("%w（schema 备份恢复失败: %v）", retErr, rbErr)
				} else {
					retErr = fmt.Errorf("%w（schema 已恢复备份）", retErr)
				}
			}
			if err := os.Remove(schemaBackupPath); err != nil {
				logx.Errorf("[artifactDeploy] 删除 schema 备份文件失败: %s err=%v", schemaBackupPath, err)
			}
		}
	}()

	// 复制解压后的文件到应用目录
	if zipTempDir != "" {
		if err := copyDir(zipTempDir, workDir); err != nil {
			return "", 0, "", fmt.Errorf("failed to copy app files: %w", err)
		}
	}

	// 生成 docker 宿主机可见的应用目录路径（用于 compose volume 挂载）。
	// compose 中的挂载路径必须使用最终 appDir，覆盖导入切换后新目录才会改名就位。
	hostAppDir := resolveHostAppDir(appDir)
	hostWorkDir := resolveHostAppDir(workDir)

	// 解析 manifest 获取镜像、端口、启动命令、环境变量和健康检查路径
	cfg = parseArtifactConfig(manifest)
	image = cfg.Image
	containerPort := cfg.ContainerPort
	if containerPort <= 0 {
		containerPort = 8080
	}
	healthPath = cfg.HealthPath
	if healthPath == "" {
		healthPath = "/"
	}
	if !strings.HasPrefix(healthPath, "/") {
		healthPath = "/" + healthPath
	}

	// 目标 tag 本地已有镜像时，先保存其 pull 前内容，失败时恢复原引用并清理本次 pull 的新镜像。
	targetImageBeforePullID, _ = dockerImageID(image)
	if targetImageBeforePullID != "" {
		rollbackImage = fmt.Sprintf("%s-rollback-%s", image, shortID(uuid.New().String()))
		if output, err := exec.Command("docker", "tag", image, rollbackImage).CombinedOutput(); err != nil {
			return "", 0, "", fmt.Errorf("save rollback image tag failed: %v, output: %s", err, string(output))
		}
	}

	// docker pull 镜像（总是先 pull，失败且本地有则降级用本地）
	if err := dockerPullImage(ctx, image); err != nil {
		return "", 0, "", fmt.Errorf("docker pull failed: %w", err)
	}
	pulledImageID, inspectErr := dockerImageID(image)
	if inspectErr != nil {
		return "", 0, "", fmt.Errorf("inspect pulled image %s: %w", image, inspectErr)
	}
	targetImageAfterPullID = pulledImageID

	// 确保项目级 database 存在；旧项目无 database_name 时自动补建并回写
	if projectDB == "" {
		var err error
		projectDB, err = ensureProjectDatabase(ctx, lifecycle, projectID)
		if err != nil {
			return "", 0, "", fmt.Errorf("ensure project database: %w", err)
		}
	}

	// 创建应用级 Schema 并生成 DATABASE_URL。
	// 覆盖导入复用同一 app 记录 → 同一 app_<id> schema，CreateSchema/占位表/drizzle push 幂等执行，数据保留。
	// 首次导入用户可在「高级」区指定 schemaName（预占已写入 project_app.schema_name）；为空回退 app_{appRecordID}。
	schemaName := strings.TrimSpace(opts.schemaName)
	if schemaName == "" {
		schemaName = appSchemaName(appRecordID)
	}
	var databaseURL string
	var err error
	if lifecycle == nil {
		return "", 0, "", fmt.Errorf("tenantpg lifecycle not available")
	}
	// 全新部署拒绝挂上已有业务表的孤儿 schema：删除应用时 DropSchema 失败会遗留带表 schema，
	// 而包内 appId 派生的 schema 名是确定性的，重导同一包会命中残留 schema，旧数据会泄漏进全新安装。
	// 覆盖导入（replace）复用同一 schema 属预期行为，不做此检查。
	if target.dockerAppID == "" {
		existingTables, tableErr := countAppSchemaTables(conf, projectDB, schemaName)
		if tableErr != nil {
			logx.Errorf("[artifactDeploy] 孤儿 schema 检测失败（不阻断部署）: schema=%s err=%v", schemaName, tableErr)
		} else if existingTables > 0 {
			return "", 0, "", fmt.Errorf("schema %s already exists with %d table(s); it likely belongs to a previous install whose cleanup failed — choose another schema name or clean up the schema manually", schemaName, existingTables)
		}
	}
	if err := lifecycle.CreateSchema(ctx, projectDB, schemaName); err != nil {
		return "", 0, "", fmt.Errorf("create app schema %s: %w", schemaName, err)
	}
	// 回写 schema_name
	if err := repo.NewProjectRepo(ctx).UpdateProjectAppSchemaName(ctx, appRecordID, schemaName); err != nil {
		logx.Errorf("[artifactDeploy] 回写 schema_name 失败: appID=%d err=%v", appRecordID, err)
	}
	databaseURL, err = buildAppDatabaseURL(conf.Database.UnsDbUrl, conf.TenantPG.RoleSecret, projectDB, schemaName)
	if err != nil {
		return "", 0, "", fmt.Errorf("build app database URL: %w", err)
	}

	// 生成额外的环境变量；覆盖导入时 ensureAppApiKey 按 app_id 复用现有 API Key
	extraEnvs, err := generateExtraEnvs(ctx, newAppID, projectID, conf)
	if err != nil {
		return "", 0, "", fmt.Errorf("generate App environment: %w", err)
	}

	// 对于 Node 镜像且没有 node_modules 的应用，在容器启动时通过 npm install 安装依赖，
	// 避免把巨大的 node_modules 从后端容器复制到 NFS 应用目录。
	needsNodeModulesInstall := false
	if strings.HasPrefix(image, "node") {
		if _, err := os.Stat(filepath.Join(workDir, "package.json")); err == nil {
			if _, err := os.Stat(filepath.Join(workDir, "node_modules")); os.IsNotExist(err) {
				needsNodeModulesInstall = true
			}
		}
	}

	// 创建占位表，避免 drizzle-kit push 把 freshly created 的空 schema 误删。
	if err := lifecycle.EnsureSchemaMarkerTable(ctx, projectDB, schemaName); err != nil {
		logx.Errorf("[artifactDeploy] 创建 schema 占位表失败: %v", err)
	}

	// 覆盖导入：drizzle push 会直接改动在用的应用 schema，先在 PG 容器内用 pg_dump 备份到临时文件，
	// 后续任何失败都可用 restoreAppSchema 恢复；备份不可用时直接失败，不在无保护状态下执行迁移
	if target.rollback && appMigrationNeeded(workDir, databaseURL) {
		schemaBackupPath, err = backupAppSchema(conf, projectDB, schemaName)
		if err != nil {
			return "", 0, "", fmt.Errorf("backup app schema %s failed: %w", schemaName, err)
		}
	}

	// drizzle-kit push 在「schema 内仅有占位表」时会把占位表当作新业务表的重命名候选，
	// 弹出交互式确认（create or rename），非 TTY 的部署容器内会静默跳过、表永远建不上。
	// push 前先移除占位表消除歧义，push 后立即重建（含 schema 被误删的兜底重建）。
	migrationNeeded := appMigrationNeeded(workDir, databaseURL)
	if migrationNeeded {
		if err := lifecycle.DropSchemaMarkerTable(ctx, projectDB, schemaName); err != nil {
			logx.Errorf("[artifactDeploy] 移除 schema 占位表失败: %v", err)
		}
	}

	// 如果应用包含 drizzle 配置，先执行 drizzle-kit push 初始化表结构。
	if err := runAppMigrations(ctx, workDir, hostWorkDir, image, databaseURL, schemaName); err != nil {
		logx.Errorf("[artifactDeploy] 应用数据库迁移失败: %v", err)
	}
	if migrationNeeded {
		if err := lifecycle.CreateSchema(ctx, projectDB, schemaName); err != nil {
			logx.Errorf("[artifactDeploy] 迁移后重建 schema 失败: %v", err)
		}
		if err := lifecycle.EnsureSchemaMarkerTable(ctx, projectDB, schemaName); err != nil {
			logx.Errorf("[artifactDeploy] 迁移后重建 schema 占位表失败: %v", err)
		}
	}
	if schemaBackupPath != "" {
		// 迁移已执行（含失败场景，drizzle push --force 可能部分应用），后续失败需恢复备份
		schemaDirty = true
	}

	// 分配应用宿主机端口并启动容器；若 Docker 报端口已被占用，则清理后重试分配新端口。
	// 本地 net.Listen 只能检测当前 network namespace，无法覆盖宿主机上其他进程占用的端口，
	// 因此先用 docker ps 收集宿主机真实发布的端口（重装清库或应用删除后 DB 记录缺失，只能靠它兜底），
	// 再用重试兜底 compose up 时的真实冲突。
	// 覆盖导入时复用既有 host_port（不重新分配，也不参与占用校验），单次 up 不重试。
	allocated := getAllocatedPorts()
	if allocated == nil {
		// DB 不可用或查询失败时 getAllocatedPorts 返回 nil，需先初始化再合并 docker 端口
		allocated = make(map[int]bool)
	}
	for p := range getDockerPublishedPorts() {
		allocated[p] = true
	}
	const maxHostPortRetries = 3
	workComposePath := filepath.Join(workDir, "docker-compose.yml")
	composePath := workComposePath
	var port int
	upAttempts := maxHostPortRetries
	if target.hostPort > 0 {
		port = target.hostPort
		upAttempts = 1
	}
	for i := 0; i < upAttempts; i++ {
		if port == 0 {
			p, err := appgateway.AllocateAppHostPort(ctx, conf, allocated)
			if err != nil {
				return "", 0, "", fmt.Errorf("failed to allocate host port: %w", err)
			}
			port = p
		}

		composeData := generateComposeYAML(appName, newAppID, installationID, image, containerPort, port, hostAppDir, databaseURL, schemaName, extraEnvs, resolveArtifactEnv(cfg.Env, opts.envPolicy, opts.envVars), cfg.Command, healthPath, needsNodeModulesInstall, cfg.StartupTimeout, containerName, opts.volumes, opts.networkAlias)
		if err := os.WriteFile(workComposePath, []byte(composeData), 0644); err != nil {
			return "", 0, "", fmt.Errorf("failed to write compose yaml: %w", err)
		}

		if target.rollback && !switched {
			// 切换：旧容器 down → 旧目录改名备份 → 新目录就位 → 用最终路径启动新容器
			oldComposePath := filepath.Join(appDir, "docker-compose.yml")
			if _, err := os.Stat(oldComposePath); err == nil {
				if output, err := appComposeCommand(ctx, oldComposePath, "down", "--remove-orphans").CombinedOutput(); err != nil {
					return "", 0, "", fmt.Errorf("stop old version failed: %v, output: %s", err, string(output))
				}
				oldStopped = true
			}
			rollbackDir := appDir + ".rollback"
			_ = os.RemoveAll(rollbackDir)
			if _, err := os.Stat(appDir); err == nil {
				if err := os.Rename(appDir, rollbackDir); err != nil {
					return "", 0, "", fmt.Errorf("backup old app dir failed: %w", err)
				}
			}
			if err := os.Rename(workDir, appDir); err != nil {
				return "", 0, "", fmt.Errorf("move new app dir into place failed: %w", err)
			}
			switched = true
			composePath = filepath.Join(appDir, "docker-compose.yml")
		}

		cmd := appComposeCommand(ctx, composePath, "up", "-d")
		output, err := cmd.CombinedOutput()
		if err == nil {
			break
		}
		outputStr := string(output)
		if target.hostPort > 0 {
			return "", 0, "", fmt.Errorf("docker compose up failed: %v, output: %s", err, outputStr)
		}
		if !strings.Contains(strings.ToLower(outputStr), "port is already allocated") {
			return "", 0, "", fmt.Errorf("docker compose up failed: %v, output: %s", err, outputStr)
		}
		logx.Infof("[artifactDeploy] 宿主机端口 %d 已被占用，第 %d 次重试", port, i+1)
		downCmd := appComposeCommand(ctx, composePath, "down", "--remove-orphans")
		if downOutput, downErr := downCmd.CombinedOutput(); downErr != nil {
			logx.Errorf("[artifactDeploy] 端口冲突后清理容器失败: %v, output: %s", downErr, string(downOutput))
		}
		allocated[port] = true
		port = 0
		if i == maxHostPortRetries-1 {
			return "", 0, "", fmt.Errorf("docker compose up failed after %d port retries: %v, output: %s", maxHostPortRetries, err, outputStr)
		}
	}

	// 健康检查：artifact 声明 health.startup.timeout 时按声明值收敛，否则默认 180s（EIMP-057）
	healthTimeoutSec := 180
	if cfg.StartupTimeout > 0 {
		healthTimeoutSec = cfg.StartupTimeout
	}
	healthLogs, err := waitForContainerHealthy(ctx, containerName, healthPath, containerPort, healthTimeoutSec)
	if err != nil {
		logx.Errorf("[artifactDeploy] 容器健康检查失败: %v", err)
		if healthLogs != "" {
			logx.Errorf("[artifactDeploy] 容器健康检查日志:\n%s", healthLogs)
		}
		return "", 0, healthLogs, fmt.Errorf("health check failed: %w", err)
	}

	if target.skipRouteReg {
		// 覆盖导入：网关路由已存在，无需重复注册
		logx.Infof("[artifactDeploy] 跳过网关路由注册（覆盖导入复用既有路由）: appID=%s", newAppID)
	} else {
		// 注册 Go 网关路由，排除本应用已占用的宿主机端口，避免网关监听端口与应用宿主机端口冲突
		accessMode := conf.AppGateway.DefaultAccessMode
		excludeHostPort := map[int]bool{port: true}
		if err := registerAppRoute(ctx, conf, appName, newAppID, accessMode, containerPort, projectID, excludeHostPort); err != nil {
			logx.Errorf("[artifactDeploy] 网关路由注册失败，清理容器: %v", err)
			return newAppID, port, "", fmt.Errorf("register gateway route failed: %w", err)
		}
	}

	// 部署成功后不再需要 pull 前镜像的临时保护 tag。
	removeRollbackImageTag(rollbackImage)
	// 覆盖导入成功：清理旧版本备份目录与不再使用的旧应用镜像。
	if target.rollback {
		if err := os.RemoveAll(appDir + ".rollback"); err != nil {
			logx.Errorf("[artifactDeploy] 清理旧版本备份目录失败: %v", err)
		}
		if oldAppImage.ID != "" && oldAppImage.ID != targetImageAfterPullID {
			removeDockerImageIfUnused(oldAppImage, "覆盖部署成功后的旧版本")
		}
	}

	return newAppID, port, "", nil
}

// deployFailureAction 覆盖部署失败后的恢复动作
type deployFailureAction int

const (
	// deployFailureCleanupWorkDir 准备阶段失败：仅清理旁路目录，旧版本未受影响
	deployFailureCleanupWorkDir deployFailureAction = iota
	// deployFailureRestartOld 旧容器已停止但目录交换未完成：仅用旧 compose 重启旧版本，不动任何目录
	deployFailureRestartOld
	// deployFailureRollback 目录交换完成后失败：完整回退到旧版本目录与旧镜像
	deployFailureRollback
)

// resolveDeployFailureAction 根据切换阶段状态决定失败恢复动作
func resolveDeployFailureAction(switched, oldStopped bool) deployFailureAction {
	if switched {
		return deployFailureRollback
	}
	if oldStopped {
		return deployFailureRestartOld
	}
	return deployFailureCleanupWorkDir
}

// restartStoppedOldVersion 覆盖部署在「旧容器已停止、目录交换未完成」阶段失败时的恢复：
// 目录保持原状（交换可能完成了一半：旧目录已改名但新目录未就位，先把旧目录改回原名），
// 仅用旧 compose 重启旧版本并做健康检查。
func restartStoppedOldVersion(ctx context.Context, appDir, containerName string, containerPort int, healthPath string) error {
	if _, err := os.Stat(appDir); os.IsNotExist(err) {
		if _, rbErr := os.Stat(appDir + ".rollback"); rbErr == nil {
			if err := os.Rename(appDir+".rollback", appDir); err != nil {
				return fmt.Errorf("restore old app dir: %w", err)
			}
		}
	}
	oldComposePath := filepath.Join(appDir, "docker-compose.yml")
	if _, err := os.Stat(oldComposePath); err != nil {
		return fmt.Errorf("old compose file missing: %w", err)
	}
	if output, err := appComposeCommand(ctx, oldComposePath, "up", "-d").CombinedOutput(); err != nil {
		return fmt.Errorf("restart old version: %v, output: %s", err, string(output))
	}
	if _, err := waitForContainerHealthy(ctx, containerName, healthPath, containerPort, 120); err != nil {
		return fmt.Errorf("old version health check after restart: %w", err)
	}
	logx.Infof("[restartStoppedOldVersion] 已重启旧版本: appDir=%s", appDir)
	return nil
}

// rollbackReplacedApp 覆盖部署切换失败后恢复旧版本：
// 停掉新容器 → 删除新目录 → 旧目录改回原名 → 旧 compose 重启 → 健康检查确认恢复。
func rollbackReplacedApp(ctx context.Context, appDir, containerName string, containerPort int, healthPath string) error {
	// 防御：旧版本备份目录不存在时绝不能删除 appDir（此时 appDir 可能是唯一的线上版本）
	if _, err := os.Stat(appDir + ".rollback"); err != nil {
		return fmt.Errorf("rollback backup dir %s missing, refuse to remove app dir: %w", appDir+".rollback", err)
	}
	newComposePath := filepath.Join(appDir, "docker-compose.yml")
	if _, err := os.Stat(newComposePath); err == nil {
		if output, err := appComposeCommand(ctx, newComposePath, "down", "--remove-orphans").CombinedOutput(); err != nil {
			logx.Errorf("[rollbackReplacedApp] 停止新版本容器失败: %v, output: %s", err, string(output))
			if rmErr := forceRemoveContainerIfPresent(ctx, containerName); rmErr != nil {
				return fmt.Errorf("stop new version: %v, output: %s; force remove container: %w", err, string(output), rmErr)
			}
		}
	}
	if err := os.RemoveAll(appDir); err != nil {
		return fmt.Errorf("remove new app dir: %w", err)
	}
	if err := os.Rename(appDir+".rollback", appDir); err != nil {
		return fmt.Errorf("restore old app dir: %w", err)
	}
	oldComposePath := filepath.Join(appDir, "docker-compose.yml")
	if output, err := appComposeCommand(ctx, oldComposePath, "up", "-d").CombinedOutput(); err != nil {
		return fmt.Errorf("restart old version: %v, output: %s", err, string(output))
	}
	if _, err := waitForContainerHealthy(ctx, containerName, healthPath, containerPort, 120); err != nil {
		return fmt.Errorf("old version health check after rollback: %w", err)
	}
	logx.Infof("[rollbackReplacedApp] 已回退到旧版本: appDir=%s", appDir)
	return nil
}

type dockerImageSnapshot struct {
	Ref string
	ID  string
}

// inspectAppDockerImage 优先从现有容器读取其真实镜像；容器缺失时回退到旧 compose 配置。
func inspectAppDockerImage(ctx context.Context, containerName, composePath string) (dockerImageSnapshot, error) {
	var containerErr error
	if strings.TrimSpace(containerName) != "" {
		cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Config.Image}}|{{.Image}}", containerName)
		if output, err := cmd.CombinedOutput(); err == nil {
			parts := strings.SplitN(strings.TrimSpace(string(output)), "|", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) != "" && strings.TrimSpace(parts[1]) != "" {
				return dockerImageSnapshot{Ref: strings.TrimSpace(parts[0]), ID: strings.TrimSpace(parts[1])}, nil
			}
			containerErr = fmt.Errorf("unexpected docker inspect output: %q", strings.TrimSpace(string(output)))
		} else {
			containerErr = fmt.Errorf("inspect container %s: %v, output: %s", containerName, err, strings.TrimSpace(string(output)))
		}
	}

	if strings.TrimSpace(composePath) != "" {
		if _, err := os.Stat(composePath); err == nil {
			output, err := appComposeCommand(ctx, composePath, "config", "--images").CombinedOutput()
			if err == nil {
				for _, line := range strings.Split(string(output), "\n") {
					imageRef := strings.TrimSpace(line)
					if imageRef == "" {
						continue
					}
					imageID, inspectErr := dockerImageID(imageRef)
					if inspectErr != nil {
						return dockerImageSnapshot{}, inspectErr
					}
					return dockerImageSnapshot{Ref: imageRef, ID: imageID}, nil
				}
				return dockerImageSnapshot{}, errors.New("compose config returned no image")
			}
			if containerErr != nil {
				return dockerImageSnapshot{}, fmt.Errorf("%v; inspect compose image: %v, output: %s", containerErr, err, strings.TrimSpace(string(output)))
			}
			return dockerImageSnapshot{}, fmt.Errorf("inspect compose image: %v, output: %s", err, strings.TrimSpace(string(output)))
		}
	}
	if containerErr != nil {
		return dockerImageSnapshot{}, containerErr
	}
	return dockerImageSnapshot{}, errors.New("app image not found")
}

func dockerImageID(image string) (string, error) {
	if strings.TrimSpace(image) == "" {
		return "", errors.New("image reference is empty")
	}
	output, err := exec.Command("docker", "image", "inspect", "--format", "{{.Id}}", image).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker image inspect %s: %v, output: %s", image, err, strings.TrimSpace(string(output)))
	}
	imageID := strings.TrimSpace(string(output))
	if imageID == "" {
		return "", fmt.Errorf("docker image inspect %s returned empty ID", image)
	}
	return imageID, nil
}

// restoreDockerImageReference 把 mutable tag 恢复到 pull 前的镜像，仅在 tag 仍指向本次 pull 结果时操作。
func restoreDockerImageReference(image, rollbackImage, beforeID, pulledID string) error {
	if beforeID == "" || pulledID == "" || beforeID == pulledID {
		return nil
	}
	if rollbackImage == "" {
		return errors.New("rollback image tag is missing")
	}
	backupID, err := dockerImageID(rollbackImage)
	if err != nil {
		return fmt.Errorf("inspect rollback image: %w", err)
	}
	if backupID != beforeID {
		return fmt.Errorf("rollback image ID changed: got %s, want %s", backupID, beforeID)
	}
	currentID, err := dockerImageID(image)
	if err != nil {
		return err
	}
	if currentID == beforeID {
		return nil
	}
	if currentID != pulledID {
		return fmt.Errorf("image reference changed concurrently: got %s, pulled %s", currentID, pulledID)
	}
	if output, err := exec.Command("docker", "tag", rollbackImage, image).CombinedOutput(); err != nil {
		return fmt.Errorf("docker tag %s %s: %v, output: %s", rollbackImage, image, err, strings.TrimSpace(string(output)))
	}
	restoredID, err := dockerImageID(image)
	if err != nil {
		return err
	}
	if restoredID != beforeID {
		return fmt.Errorf("restored image ID mismatch: got %s, want %s", restoredID, beforeID)
	}
	return nil
}

func cleanupPulledImageAfterFailure(image string, beforeID, pulledID string, imageReferenceRestored bool) {
	if strings.TrimSpace(image) == "" || !dockerImageChanged(beforeID, pulledID) {
		return
	}
	if beforeID != "" && !imageReferenceRestored {
		logx.Errorf("[artifactDeploy] pull 前镜像引用未恢复，跳过删除失败部署镜像以避免破坏原 tag: image=%s pulledID=%s", image, pulledID)
		return
	}
	removeDockerImageIfUnused(dockerImageSnapshot{Ref: image, ID: pulledID}, "部署失败后本次拉取的镜像")
}

func dockerImageChanged(beforeID, pulledID string) bool {
	return pulledID != "" && pulledID != beforeID
}

// removeDockerImageIfUnused 只删除没有任何容器引用的镜像；不用 -f，其他 tag 仍引用时由 Docker 保留镜像内容。
func removeDockerImageIfUnused(snapshot dockerImageSnapshot, reason string) {
	if strings.TrimSpace(snapshot.ID) == "" {
		return
	}
	if _, err := dockerImageID(snapshot.ID); err != nil {
		return
	}
	output, err := exec.Command("docker", "ps", "-aq", "--filter", "ancestor="+snapshot.ID).CombinedOutput()
	if err != nil {
		logx.Errorf("[dockerImageCleanup] 查询镜像容器引用失败，跳过清理: imageID=%s reason=%s err=%v output=%s", snapshot.ID, reason, err, strings.TrimSpace(string(output)))
		return
	}
	if strings.TrimSpace(string(output)) != "" {
		logx.Infof("[dockerImageCleanup] 镜像仍被其他容器引用，跳过清理: imageID=%s reason=%s", snapshot.ID, reason)
		return
	}
	if strings.TrimSpace(snapshot.Ref) != "" {
		if currentID, inspectErr := dockerImageID(snapshot.Ref); inspectErr == nil && currentID == snapshot.ID {
			if removeOutput, removeErr := exec.Command("docker", "image", "rm", snapshot.Ref).CombinedOutput(); removeErr != nil {
				logx.Errorf("[dockerImageCleanup] 删除镜像引用失败: image=%s reason=%s err=%v output=%s", snapshot.Ref, reason, removeErr, strings.TrimSpace(string(removeOutput)))
				return
			}
		}
	}
	if _, err := dockerImageID(snapshot.ID); err != nil {
		return
	}
	if removeOutput, removeErr := exec.Command("docker", "image", "rm", snapshot.ID).CombinedOutput(); removeErr != nil {
		logx.Infof("[dockerImageCleanup] 镜像仍有其他 tag 引用，保留共享内容: imageID=%s reason=%s err=%v output=%s", snapshot.ID, reason, removeErr, strings.TrimSpace(string(removeOutput)))
		return
	}
	logx.Infof("[dockerImageCleanup] 已清理镜像: imageID=%s reason=%s", snapshot.ID, reason)
}

func forceRemoveContainerIfPresent(ctx context.Context, containerName string) error {
	if strings.TrimSpace(containerName) == "" {
		return nil
	}
	output, err := exec.CommandContext(ctx, "docker", "container", "inspect", "--format", "{{.Id}}", containerName).CombinedOutput()
	if err != nil {
		message := strings.ToLower(string(output))
		if strings.Contains(message, "no such container") || strings.Contains(message, "no such object") {
			return nil
		}
		return fmt.Errorf("inspect container %s: %v, output: %s", containerName, err, strings.TrimSpace(string(output)))
	}
	if strings.TrimSpace(string(output)) == "" {
		return nil
	}
	if rmOutput, rmErr := exec.CommandContext(ctx, "docker", "rm", "-f", containerName).CombinedOutput(); rmErr != nil {
		return fmt.Errorf("docker rm -f %s: %v, output: %s", containerName, rmErr, strings.TrimSpace(string(rmOutput)))
	}
	return nil
}

// removeRollbackImageTag 清理部署前保存的旧镜像 rollback tag
func removeRollbackImageTag(rollbackImage string) {
	if rollbackImage == "" {
		return
	}
	if output, err := exec.Command("docker", "rmi", rollbackImage).CombinedOutput(); err != nil {
		logx.Errorf("[artifactDeploy] 清理 rollback 镜像 tag 失败: %s, err=%v, output: %s", rollbackImage, err, string(output))
	}
}

// dockerImageExists 判断本地是否已存在指定镜像
func dockerImageExists(image string) bool {
	_, err := dockerImageID(image)
	return err == nil
}

// dockerPullImage 拉取 Docker 镜像。总是先尝试 pull（10 分钟超时）以获取最新镜像；
// pull 失败但本地已存在该镜像时降级使用本地镜像；本地也没有时回退到私有镜像仓库（harbor）。
func dockerPullImage(ctx context.Context, image string) error {
	logx.Infof("[artifactDeploy] docker pull %s", image)
	if ctx == nil {
		ctx = context.Background()
	}
	pullCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	output, err := exec.CommandContext(pullCtx, "docker", "pull", image).CombinedOutput()
	if err == nil {
		return nil
	}
	if dockerImageExists(image) {
		logx.Infof("[artifactDeploy] docker pull %s 失败，降级使用本地已有镜像: %v, output: %s", image, err, string(output))
		return nil
	}

	return fmt.Errorf("docker pull %s failed: %v, output: %s", image, err, string(output))
}

// allocateHostPort 在配置范围内分配应用宿主机端口，并排除已分配的应用端口和网关端口
func allocateHostPort(ctx context.Context, conf config.Config) (int, error) {
	return appgateway.AllocateAppHostPort(ctx, conf, getAllocatedPorts())
}

// getAllocatedPorts 从 DB 查询未删除应用已分配的端口。
// 已删除应用的残留容器可能仍占用端口，DB 查不到，需靠 getDockerPublishedPorts 兜底。
func getAllocatedPorts() map[int]bool {
	ctx := context.Background()
	db := repo.GetCommonConn(ctx)
	if db == nil {
		return nil
	}

	var apps []repo.ProjectApp
	if err := db.WithContext(ctx).Where("deleted_time = 0").Find(&apps).Error; err != nil {
		logx.Errorf("[artifactDeploy] 查询已分配端口失败: %v", err)
		return nil
	}

	ports := make(map[int]bool)
	for _, app := range apps {
		if len(app.ManifestJSON) == 0 {
			continue
		}
		var meta map[string]any
		if err := json.Unmarshal(app.ManifestJSON, &meta); err != nil {
			continue
		}
		if hp, ok := meta["host_port"]; ok {
			switch v := hp.(type) {
			case float64:
				ports[int(v)] = true
			case int:
				ports[v] = true
			}
		}
	}
	return ports
}

// dockerPortMappingPattern 匹配 docker ps --format '{{.Ports}}' 输出中的宿主机端口映射，
// 形如 0.0.0.0:30100->5173/tcp 或 0.0.0.0:30400-30499->30400-30499/tcp。
var dockerPortMappingPattern = regexp.MustCompile(`:(\d+)(?:-(\d+))?->`)

// getDockerPublishedPorts 通过 docker ps 收集宿主机上所有容器实际发布的端口，
// 弥补 net.Listen 在容器内无法感知宿主机端口占用的盲区；失败时仅告警并返回空集合。
func getDockerPublishedPorts() map[int]bool {
	cmd := exec.Command("docker", "ps", "--format", "{{.Ports}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logx.Errorf("[artifactDeploy] 查询 docker 已发布端口失败: %v, output: %s", err, string(output))
		return nil
	}
	return parseDockerPublishedPorts(string(output))
}

// parseDockerPublishedPorts 解析 docker ps --format '{{.Ports}}' 输出，返回宿主机端口集合。
func parseDockerPublishedPorts(output string) map[int]bool {
	ports := make(map[int]bool)
	for _, line := range strings.Split(output, "\n") {
		for _, m := range dockerPortMappingPattern.FindAllStringSubmatch(line, -1) {
			start, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			end := start
			if m[2] != "" {
				e, err := strconv.Atoi(m[2])
				if err != nil {
					continue
				}
				end = e
			}
			// 只校验端口合法性，不能整段丢弃大端口段（如 20000-40000），
			// 否则与应用程序范围重叠的端口会被误判为空闲，仍在 compose up 时撞端口
			if end < start || start < 1 || end > 65535 {
				continue
			}
			for p := start; p <= end; p++ {
				ports[p] = true
			}
		}
	}
	return ports
}

// captureContainerLogs 获取容器最近 200 行日志，用于失败时定位原因。
func captureContainerLogs(containerName string) string {
	cmd := exec.Command("docker", "logs", "--tail", "200", containerName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("(failed to fetch container logs: %v)", err)
	}
	return string(output)
}

// waitForContainerHealthy 等待容器健康检查通过。
// 返回的日志字符串在失败时可用于定位容器启动错误。
func waitForContainerHealthy(ctx context.Context, containerName, healthPath string, containerPort, timeoutSec int) (string, error) {
	if healthPath == "" {
		return "", nil
	}
	start := time.Now()
	for time.Since(start).Seconds() < float64(timeoutSec) {
		select {
		case <-ctx.Done():
			return captureContainerLogs(containerName), fmt.Errorf("health check aborted: %w", ctx.Err())
		default:
		}
		cmd := exec.CommandContext(ctx, "docker", "inspect", "--format={{.State.Health.Status}}", containerName)
		output, err := cmd.CombinedOutput()
		if err == nil {
			status := strings.TrimSpace(string(output))
			if status == "healthy" {
				return "", nil
			}
			if status == "unhealthy" {
				return captureContainerLogs(containerName), fmt.Errorf("container %s is unhealthy", containerName)
			}
		}
		// 手动探测须受 startupTimeout 约束：wget 加 -T 上限（取剩余时间，单次最多 10s），
		// 避免健康端点接受连接后挂起导致单次探测远超声明的启动超时
		remaining := timeoutSec - int(time.Since(start).Seconds())
		if remaining < 1 {
			break
		}
		probeTimeout := remaining
		if probeTimeout > 10 {
			probeTimeout = 10
		}
		cmd2 := exec.CommandContext(ctx, "docker", "exec", containerName, "sh", "-c",
			fmt.Sprintf("wget -qO- -T %d http://127.0.0.1:%d%s > /dev/null", probeTimeout, containerPort, healthPath))
		if err := cmd2.Run(); err == nil {
			return "", nil
		}
		time.Sleep(2 * time.Second)
	}
	return captureContainerLogs(containerName), fmt.Errorf("health check timeout for container %s", containerName)
}

const maxGatewayPortRetries = 5

// registerAppRoute 注册 Go 网关路由
func registerAppRoute(ctx context.Context, conf config.Config, appName, appID, accessMode string, containerPort int, projectID int64, exclude map[int]bool) error {
	routeRepo := repo.NewAppGatewayRouteRepo(ctx)
	shortAppID := shortID(appID)
	containerName := appContainerName(appName, appID)

	route := &repo.AppGatewayRoute{
		AppID:         appID,
		ProjectID:     projectID,
		AccessMode:    accessMode,
		ContainerHost: containerName,
		ContainerPort: containerPort,
	}

	if accessMode == "port" {
		// 端口分配冲突时重试，避免并发导入分配到同一端口
		var gwPort int
		var err error
		for i := 0; i < maxGatewayPortRetries; i++ {
			gwPort, err = allocateGatewayPort(ctx, conf, exclude)
			if err != nil {
				return fmt.Errorf("分配网关端口失败: %w", err)
			}
			route.Port = gwPort
			err = routeRepo.Insert(ctx, route)
			if err == nil {
				break
			}
			if !errors.Is(err, repo.ErrDuplicate) {
				return fmt.Errorf("写入网关路由表失败: %w", err)
			}
			logx.Infof("[registerAppRoute] 网关端口 %d 冲突，第 %d 次重试", gwPort, i+1)
		}
		if err != nil {
			return fmt.Errorf("网关端口分配重试 %d 次后仍冲突: %w", maxGatewayPortRetries, err)
		}
	} else if accessMode == "subdomain" {
		// 通配父域格式：<shortID>.<suffix>（如 a1b2c3d4e5f6.apps.example.com），
		// 与 cloud 的 matchSubdomain 语义一致。旧 app-<shortID> 格式路由在 Router
		// 中通过精确匹配继续兼容。
		domain := fmt.Sprintf("%s.%s", shortAppID, conf.AppGateway.SubdomainSuffix)
		route.Domain = domain
		if err := routeRepo.Insert(ctx, route); err != nil {
			return fmt.Errorf("写入网关路由表失败: %w", err)
		}
	}

	if err := reloadGatewayRoutes(ctx); err != nil {
		return fmt.Errorf("刷新网关路由失败: %w", err)
	}
	return nil
}

// allocateGatewayPort 分配网关端口，优先使用本地可用端口
func allocateGatewayPort(ctx context.Context, conf config.Config, exclude map[int]bool) (int, error) {
	return appgateway.AllocatePort(ctx, conf, exclude)
}

func reloadGatewayRoutes(ctx context.Context) error {
	if err := appgateway.ReloadRoutes(ctx); err != nil {
		logx.Errorf("[deploy] 本进程刷新网关路由失败: %v", err)
		return err
	}
	return nil
}

// ensureProjectDatabase 确保项目级 database 存在；旧项目无 database_name 时幂等创建并回写。
func ensureProjectDatabase(ctx context.Context, lifecycle *tenantpg.Lifecycle, projectID int64) (string, error) {
	if lifecycle == nil {
		return "", fmt.Errorf("tenantpg lifecycle not available")
	}
	databaseName := fmt.Sprintf("project_%d", projectID)
	if err := lifecycle.CreateDatabase(ctx, databaseName); err != nil {
		return "", fmt.Errorf("create project database %s: %w", databaseName, err)
	}
	if err := repo.NewProjectRepo(ctx).UpdateProjectDatabaseName(ctx, projectID, databaseName); err != nil {
		return "", fmt.Errorf("update project database name: %w", err)
	}
	logx.Infof("[ensureProjectDatabase] 为旧项目创建/确认 project database: %s", databaseName)
	return databaseName, nil
}

// createLegacyAppDatabase 为无 project database 的旧项目创建 common DB 内的 app schema。
// Deprecated: 旧项目现在会通过 ensureProjectDatabase 自动补建 project database，本函数不再使用。
func createLegacyAppDatabase(appRecordID int64, baseURL string) (string, error) {
	if strings.TrimSpace(baseURL) == "" {
		return "", fmt.Errorf("base database URL is empty")
	}

	ctx := context.Background()
	db := repo.GetCommonConn(ctx)
	if db == nil {
		return "", fmt.Errorf("common database connection not available")
	}

	schemaName := appSchemaName(appRecordID)
	if err := db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS \"%s\"", schemaName)).Error; err != nil {
		return "", fmt.Errorf("create app schema failed: %w", err)
	}
	logx.Infof("[createLegacyAppDatabase] 创建旧版应用 Schema: %s", schemaName)

	return tenantpg.BuildAppDatabaseURL(baseURL, schemaName), nil
}

// buildAppDatabaseURL 构造 project DB + 受限 role + search_path 的 DATABASE_URL
func buildAppDatabaseURL(baseDSN, roleSecret, projectDB, schemaName string) (string, error) {
	if strings.TrimSpace(roleSecret) == "" {
		return "", errors.New(appRoleSecretRequired)
	}
	user, pass, err := tenantpg.DeriveCredentials(roleSecret, projectDB)
	if err != nil {
		return "", fmt.Errorf("derive credentials: %w", err)
	}
	projectDSN, err := tenantpg.BuildProjectConnDSNWithCreds(baseDSN, projectDB, user, pass)
	if err != nil {
		return "", fmt.Errorf("build project dsn: %w", err)
	}
	return tenantpg.BuildAppDatabaseURL(projectDSN, schemaName), nil
}

// ensureLegacySchemaMarkerTable 在旧版 common DB 的指定 schema 中创建占位表。
// Deprecated: 新实现统一在 project database 中创建占位表，本函数不再使用。
func ensureLegacySchemaMarkerTable(schemaName string) error {
	ctx := context.Background()
	db := repo.GetCommonConn(ctx)
	if db == nil {
		return fmt.Errorf("common database connection not available")
	}
	sql := fmt.Sprintf("CREATE TABLE IF NOT EXISTS \"%s\".\"__schema_marker\" (id int PRIMARY KEY)", schemaName)
	if err := db.Exec(sql).Error; err != nil {
		return fmt.Errorf("create schema marker table failed: %w", err)
	}
	return nil
}

// appMigrationNeeded 判断本次部署是否会执行应用数据库迁移（与 runAppMigrations 的前置条件一致），
// 覆盖导入据此决定是否需要先备份应用 schema
func appMigrationNeeded(appDir, databaseURL string) bool {
	return databaseURL != "" && hasDrizzleConfig(appDir)
}

// hasDrizzleConfig 检测应用目录是否包含 drizzle 配置文件
func hasDrizzleConfig(appDir string) bool {
	configNames := []string{"drizzle.config.ts", "drizzle.config.js", "drizzle.config.mjs"}
	for _, name := range configNames {
		if _, err := os.Stat(filepath.Join(appDir, name)); err == nil {
			return true
		}
	}
	return false
}

// runAppMigrations 检测并执行应用自带的 drizzle-kit push，为动态应用创建初始表结构。
// 由于 drizzle-kit 通常位于 devDependencies，临时容器需要先执行 npm install 才能运行迁移。
func runAppMigrations(ctx context.Context, appDir, hostAppDir, image, databaseURL, schemaName string) error {
	if databaseURL == "" {
		return nil
	}
	if !hasDrizzleConfig(appDir) {
		return nil
	}

	logx.Infof("[artifactDeploy] 检测到 drizzle 配置，执行 drizzle-kit push 初始化 schema: %s", schemaName)

	baseArgs := []string{
		"run", "--rm",
		"-v", hostAppDir + ":/app",
		"-w", "/app",
		"-e", "DATABASE_URL=" + databaseURL,
		"-e", "DB_SCHEMA=" + schemaName,
		"--network", getRuntimeNetworkName(),
		image,
	}

	nodeModulesDir := filepath.Join(appDir, "node_modules")
	if info, err := os.Stat(nodeModulesDir); err == nil && info.IsDir() {
		logx.Infof("[artifactDeploy] 检测到应用已包含 node_modules，跳过 npm install")
	} else {
		installCmd := exec.CommandContext(ctx, "docker", append(baseArgs, "npm", "install")...)
		installOutput, installErr := installCmd.CombinedOutput()
		if installErr != nil {
			return fmt.Errorf("npm install for migrations failed: %w, output: %s", installErr, string(installOutput))
		}
		logx.Infof("[artifactDeploy] npm install for migrations 完成")
	}

	pushCmd := exec.CommandContext(ctx, "docker", append(baseArgs, "npx", "drizzle-kit", "push", "--force")...)
	pushOutput, err := pushCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("drizzle-kit push failed: %w, output: %s", err, string(pushOutput))
	}
	logx.Infof("[artifactDeploy] drizzle-kit push 完成: %s", string(pushOutput))
	return nil
}

// getHostIP 获取宿主机 IP（edge_network 网关）
func getHostIP() string {
	if ip := os.Getenv("HOST_IP"); ip != "" {
		return ip
	}
	return "172.18.0.1"
}

// getBackendAPIHost 返回后端 API 在容器网络中的访问地址。
// 应用容器会被接入 runtime 网络，因此默认使用服务名 backend:8080；
// 可通过 BACKEND_API_HOST 环境变量覆盖。
func getBackendAPIHost() string {
	if h := os.Getenv("BACKEND_API_HOST"); h != "" {
		return h
	}
	return "backend:8080"
}

// getRuntimeNetworkName 返回 backend / tsdb 所在的 runtime 网络名称，
// 供应用容器 compose 以外部网络形式接入。
func getRuntimeNetworkName() string {
	if n := os.Getenv("RUNTIME_NETWORK_NAME"); n != "" {
		return n
	}
	if project := strings.TrimSpace(os.Getenv("COMPOSE_PROJECT_NAME")); project != "" {
		return project + "_runtime"
	}
	return "edge_runtime"
}

// appComposeCommand keeps each dynamic app in the Compose project derived from
// its own compose file directory. The backend needs the installation-level
// COMPOSE_PROJECT_NAME for Fleet/runtime discovery, but passing that variable
// to an app compose command would make the app part of the main Enterprise
// stack and let --remove-orphans target unrelated platform containers.
// ctx 非 nil 时用 CommandContext 构造，便于部署超时中断阻塞中的 compose 命令。
func appComposeCommand(ctx context.Context, composePath string, args ...string) *exec.Cmd {
	composeArgs := []string{"compose", "-f", composePath}
	composeArgs = append(composeArgs, args...)
	var cmd *exec.Cmd
	if ctx != nil {
		cmd = exec.CommandContext(ctx, "docker", composeArgs...)
	} else {
		cmd = exec.Command("docker", composeArgs...)
	}
	cmd.Env = make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if found && strings.EqualFold(key, "COMPOSE_PROJECT_NAME") {
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	return cmd
}

// getAppsVolumeName 返回存放动态应用文件的 Docker named volume 名称。
// Windows Docker Desktop 上 bind mount 复制/读写 node_modules 类小文件极慢，
// 部署脚本会在 Windows 安装时改用 named volume 并注入 HOST_APPS_VOLUME；
// 为空表示沿用宿主机目录 bind mount（Linux 默认行为，保持不变）。
func getAppsVolumeName() string {
	return strings.TrimSpace(os.Getenv("HOST_APPS_VOLUME"))
}

// getAppContainerHost 返回应用容器内部服务应绑定的地址。
// Docker 会默认向容器注入 HOSTNAME 环境变量（值为容器短 ID），而 TanStack Start
// 的 server.mjs 优先读取 HOSTNAME 进行 bind；将其覆盖为 0.0.0.0 才能通过 127.0.0.1
// 的健康检查并被网关访问。
func getAppContainerHost() string {
	if h := os.Getenv("APP_CONTAINER_HOST"); h != "" {
		return h
	}
	return "0.0.0.0"
}

// resolveHostAppDir 将容器内的 /app/data/apps/<id> 映射为宿主机路径，
// 供 docker-compose volume 挂载使用。优先读取 HOST_APPS_ROOT 环境变量；
// 否则通过 /proc/self/mountinfo 解析 /app/data/apps 的宿主机挂载点。
func resolveHostAppDir(appDir string) string {
	if root := strings.TrimSpace(os.Getenv("HOST_APPS_ROOT")); root != "" {
		rel, err := filepath.Rel("/app/data/apps", appDir)
		if err != nil {
			return appDir
		}
		return filepath.Join(root, rel)
	}

	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return appDir
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		mountPoint := fields[4]
		if mountPoint != "/app/data/apps" {
			continue
		}
		// 找到分隔符 '-'，source 在其后第二位：- fs_type source ...
		hostRoot := ""
		for i := 0; i < len(fields)-2; i++ {
			if fields[i] == "-" {
				hostRoot = fields[i+2]
				break
			}
		}
		if hostRoot == "" {
			return appDir
		}
		// NFS 源形如 127.0.0.1:/path；取冒号后路径
		if idx := strings.Index(hostRoot, ":"); idx != -1 {
			hostRoot = hostRoot[idx+1:]
		}
		rel, err := filepath.Rel("/app/data/apps", appDir)
		if err != nil {
			return appDir
		}
		return filepath.Join(hostRoot, rel)
	}
	return appDir
}

// shortID 返回 UUID 前 12 位
func shortID(id string) string {
	if len(id) <= 12 {
		return strings.ReplaceAll(id, "-", "")
	}
	return strings.ReplaceAll(id[:12], "-", "")
}

// containerNameIllegalChars 匹配 docker 容器名中不合法的字符。
// 合法字符为 [a-z0-9_.-]，其余字符需要被替换为 -。
var containerNameIllegalChars = regexp.MustCompile(`[^a-z0-9_.-]+`)

// sanitizeContainerSegment 将 App 名转换为合法 docker 容器名段。
// 规则：小写、非法字符转 -、去头尾非法字符、截断约 33 字符。
// 上限 33 保证 app-<slug>-<shortID> 全长不超过 app_gateway_routes.container_host 的 VARCHAR(50)。
// 纯中文或空结果返回空字符串，由调用方回退到旧格式。
func sanitizeContainerSegment(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = containerNameIllegalChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-_.")
	if s == "" {
		return ""
	}
	const maxLen = 33
	if len(s) > maxLen {
		s = s[:maxLen]
		s = strings.Trim(s, "-_.")
	}
	return s
}

// appContainerName 生成 docker 容器名：app-<规范化后的 App 名>-<ID 后缀>。
// 规范化后为空时回退旧格式 app-<shortID>，保证唯一且 docker 合法。
// App 首次部署后容器名即固定，后续改名不再影响该名称。
func appContainerName(appName, appID string) string {
	short := shortID(appID)
	slug := sanitizeContainerSegment(appName)
	if slug == "" {
		return "app-" + short
	}
	return "app-" + slug + "-" + short
}

// copyDir 递归复制目录。
// 使用 tar 流式复制，默认排除 node_modules（运行时通过容器内 npm install 按需安装），
// 并加上 -o 避免 NFS root_squash 下无法恢复属主导致失败。
// 若源目录已存在 node_modules，则一并复制，支持离线客户导入自包含应用包。
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	exclude := "--exclude=.git"
	if info, err := os.Stat(filepath.Join(src, "node_modules")); err != nil || !info.IsDir() {
		exclude += " --exclude=node_modules"
	}
	cmd := exec.Command("sh", "-c",
		fmt.Sprintf("tar %s -cf - -C %s . | tar -o -xf - -C %s",
			exclude, shellQuote(src), shellQuote(dst)))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copyDir failed: %w, output: %s", err, string(output))
	}
	return nil
}

// shellQuote 给路径加上单引号，防止路径中的特殊字符被 shell 解释。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// resolveAPIKeyOwnerID 解析 API Key 的 ownerID。
// 优先从 ctx 中的 Subject 取当前用户 ID；取不到则查询 tier0 用户。
// 最终还取不到则返回 0，调用方应决定是否继续注册 sys_api_key。
func resolveAPIKeyOwnerID(ctx context.Context) int64 {
	if subject, ok := contextx.SubjectFrom(ctx); ok && subject.UserID != 0 {
		return subject.UserID
	}
	iamRepo := repo.NewIAMRepo(ctx)
	if user, err := iamRepo.GetUserByUsername(ctx, "tier0"); err == nil {
		return user.ID
	}
	return 0
}

// ensureAppApiKey 确保应用有 API Key，并同步注册到 sys_api_key 供 OpenAPI 鉴权。
// 形态策略：
//   - sk-app-（app 型，新形态）→ 纯复用，仅兜底补注册；
//   - sk-svc-（service 型，旧形态）→ 重新部署时换签为 app 型新 key；
//     旧 key 不吊销（正在运行的实例与活跃会话继续可用），其 sys_api_key 注册行
//     （app-<appID>-default）原样保留，由运维经重置通道择机清理；
//   - 其余旧格式（t0_ 等）→ 直接签发 app 型新 key。
func ensureAppApiKey(ctx context.Context, appID string, projectID int64) (string, error) {
	apiKeyRepo := repo.NewAppApiKeyRepo(ctx)

	existing, err := apiKeyRepo.FindByAppID(ctx, appID)
	if err == nil && existing != nil {
		if domaincommon.ParseWorkspaceIDFromAPIKey(existing.ApiKey) == domaincommon.PlatformWorkspaceID {
			switch {
			case strings.HasPrefix(existing.ApiKey, "sk-app-"):
				if err := registerAppAPIKeyInSys(ctx, existing.ApiKey, appID); err != nil {
					return "", fmt.Errorf("补注册应用 API Key: %w", err)
				}
				return existing.ApiKey, nil
			case strings.HasPrefix(existing.ApiKey, "sk-svc-"):
				logx.Infof("[ensureAppApiKey] 应用 %s 使用旧形态 service 型 API Key，重新部署换签为 app 型", appID)
			default:
				logx.Infof("[ensureAppApiKey] 应用 %s 使用旧格式 API Key，轮换为 app 型", appID)
			}
		} else {
			logx.Infof("[ensureAppApiKey] 应用 %s 使用旧格式 API Key，轮换为 Cloud 兼容格式", appID)
		}
	} else if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return "", fmt.Errorf("查询应用 API Key: %w", err)
	}

	newKey, err := repo.GenerateWorkspaceAPIKey("app", domaincommon.PlatformWorkspaceID)
	if err != nil {
		return "", fmt.Errorf("生成应用 API Key: %w", err)
	}
	ownerID := resolveAPIKeyOwnerID(ctx)
	if existing != nil {
		if detail, validateErr := repo.NewAPIKeyRepo(ctx).ValidateAPIKeyDetail(ctx, existing.ApiKey); validateErr == nil && detail.OwnerID > 0 {
			ownerID = detail.OwnerID
		}
	}
	if ownerID == 0 {
		return "", fmt.Errorf("无法解析 API Key ownerID")
	}

	db := repo.GetCommonConn(ctx)
	if db == nil {
		return "", fmt.Errorf("平台数据库未初始化")
	}
	err = db.Transaction(func(tx *gorm.DB) error {
		txAppAPIKeys := repo.NewAppApiKeyRepo(tx)
		txAPIKeys := repo.NewAPIKeyRepo(tx)
		if err := registerAppAPIKeyWithRepo(ctx, txAPIKeys, newKey, appID, ownerID); err != nil {
			return err
		}
		if existing != nil {
			return txAppAPIKeys.UpdateAPIKey(ctx, existing.ID, newKey)
		}
		return txAppAPIKeys.Insert(ctx, &repo.AppApiKey{
			AppID:     appID,
			Name:      "default",
			ApiKey:    newKey,
			ProjectID: projectID,
		})
	})
	if err != nil {
		return "", fmt.Errorf("保存应用 API Key: %w", err)
	}

	logx.Infof("[ensureAppApiKey] 为应用 %s 签发 app 型 API Key", appID)
	return newKey, nil
}

// registerAppAPIKeyInSys 将应用 API Key 注册到 sys_api_key，供 OpenAPI 与 MQTT 鉴权使用。
func registerAppAPIKeyInSys(ctx context.Context, rawKey, appID string) error {
	ownerID := resolveAPIKeyOwnerID(ctx)
	if ownerID == 0 {
		return fmt.Errorf("无法解析 API Key ownerID")
	}
	return registerAppAPIKeyWithRepo(ctx, repo.NewAPIKeyRepo(ctx), rawKey, appID, ownerID)
}

// appAPIKeyRegistryName 返回应用 key 注册到 sys_api_key 使用的名称：
//   - 新形态（sk-app-，keyType=app）→ app-<完整appID>，与 Cloud 新签发格式一致；
//   - 旧形态（sk-svc-，keyType=service）→ 保留历史名 app-<appID>-default，
//     使补注册按名幂等命中旧注册行，且不与旧形态互删（同名注册会原地覆盖 hash，
//     令正在运行的实例持有的旧 key 立即失效）。
func appAPIKeyRegistryName(appID, rawKey string) string {
	if strings.HasPrefix(rawKey, "sk-app-") {
		return "app-" + appID
	}
	return "app-" + appID + "-default"
}

// appAPIKeyType 按 key 明文前缀推导注册到 sys_api_key 的 owner_type。
func appAPIKeyType(rawKey string) string {
	if strings.HasPrefix(rawKey, "sk-app-") {
		return "app"
	}
	return "service"
}

func registerAppAPIKeyWithRepo(ctx context.Context, apiKeyRepo *repo.APIKeyRepo, rawKey, appID string, ownerID int64) error {
	if apiKeyRepo == nil || ownerID == 0 {
		return fmt.Errorf("API Key 注册参数无效")
	}
	permission := repo.APIKeyPermissionFull
	resourceKeys := repo.APIKeyResourceKeysForPermission(permission)

	return apiKeyRepo.RegisterAPIKey(
		ctx,
		rawKey,
		appAPIKeyRegistryName(appID, rawKey),
		appID,
		ownerID,
		appAPIKeyType(rawKey),
		permission,
		"internal",
		resourceKeys,
	)
}

// generateExtraEnvs 生成部署应用时需要注入的额外环境变量
func generateExtraEnvs(ctx context.Context, appID string, projectID int64, conf config.Config) (map[string]string, error) {
	mqttHost, mqttPort, err := conf.SDKMQTTAddress()
	if err != nil {
		return nil, fmt.Errorf("resolve App SDK MQTT endpoint: %w", err)
	}
	envs := make(map[string]string)
	// APP_ID 必须是平台分配的 docker app UUID：SDK getCurrentAppId 与通知 sender.id 依赖它，
	// 包内可能残留来源环境（Cloud）的旧值，故作为平台强制变量注入并拒绝包/用户覆盖。
	envs["APP_ID"] = appID
	envs["TIER0_PROJECT_ID"] = fmt.Sprintf("%d", projectID)

	apiKey, err := ensureAppApiKey(ctx, appID, projectID)
	if err != nil {
		logx.Errorf("[generateExtraEnvs] 确保应用 API Key 失败，本次部署不注入 TIER0_API_KEY: %v", err)
		envs["TIER0_API_KEY"] = ""
	} else {
		envs["TIER0_API_KEY"] = apiKey
	}

	// 应用容器跑在 Docker runtime 网络内部，必须使用内部后端地址，
	// 不能复用给浏览器/外部 SDK 的公网地址（可能指向宿主机或 TLS 入口）。
	envs["TIER0_API_HOST"] = getBackendAPIHost()

	// SDK uses WebSocket; derive the hostname from the internal MQTT broker and
	// allow a separate internal WebSocket listener through TIER0_SDK_MQTT_*.
	envs["TIER0_MQTT_HOST"] = mqttHost
	envs["TIER0_MQTT_PORT"] = mqttPort

	return envs, nil
}

// generateComposeYAML 生成 docker-compose.yml 内容
// containerName 为空时按 appName+appID 派生；覆盖再部署时应传入首次部署的容器名以保持路由一致。
func generateComposeYAML(appName, appID, installationID, image string, containerPort, hostPort int, appDir, databaseURL, schemaName string, extraEnvs, artifactEnvs map[string]string, command []string, healthPath string, needsNodeModulesInstall bool, startupTimeout int, containerName string, customVolumes []artifactVolume, networkAlias ...string) string {
	// 平台自动注入且不允许被覆盖的变量（platformForcedEnvs 为包级定义，导入时校验用户提交复用）
	forcedEnvs := platformForcedEnvs

	envLines := ""
	if databaseURL != "" {
		envLines += fmt.Sprintf("      - DATABASE_URL=%s\n", databaseURL)
	}
	if schemaName != "" {
		envLines += fmt.Sprintf("      - DB_SCHEMA=%s\n", schemaName)
	}
	// 注入容器端口，确保应用监听在 containerPort
	envLines += fmt.Sprintf("      - PORT=%d\n", containerPort)
	// 覆盖 Docker 默认注入的 HOSTNAME，避免 Node/TanStack 优先绑定到容器 hostname 而不是 0.0.0.0
	envLines += fmt.Sprintf("      - HOSTNAME=%s\n", getAppContainerHost())

	// 先写入 artifact.toml 中的环境变量（可被平台默认值补全，但不可覆盖平台强制变量）
	for k, v := range artifactEnvs {
		if forcedEnvs[k] {
			continue
		}
		envLines += fmt.Sprintf("      - %s=%s\n", k, v)
	}

	// 再用平台默认值补全 artifact.toml 未声明的变量（强制变量最后统一写入，避免重复）
	for k, v := range extraEnvs {
		if _, ok := artifactEnvs[k]; ok {
			continue
		}
		if forcedEnvs[k] {
			continue
		}
		envLines += fmt.Sprintf("      - %s=%s\n", k, v)
	}

	// 最后写入平台强制变量，确保关键注入项始终生效且不会被 artifact.toml 覆盖
	for k := range forcedEnvs {
		if v, ok := extraEnvs[k]; ok {
			envLines += fmt.Sprintf("      - %s=%s\n", k, v)
		}
	}

	envBlock := ""
	if envLines != "" {
		envBlock = "    environment:\n" + envLines
	}

	startPeriod := 40
	if startupTimeout > 0 {
		// artifact 声明 health.startup.timeout 时按声明值收敛启动宽限期（EIMP-057）
		startPeriod = startupTimeout
	}
	commandBlock := ""
	if len(command) > 0 {
		quoted := make([]string, len(command))
		for i, c := range command {
			quoted[i] = fmt.Sprintf("%q", c)
		}
		if needsNodeModulesInstall {
			if startupTimeout <= 0 {
				startPeriod = 180
			}
			wrapped := strings.Join(command, " ")
			commandBlock = fmt.Sprintf("    command: [\"sh\", \"-c\", \"npm install --omit=dev && exec %s\"]\n", wrapped)
		} else {
			commandBlock = fmt.Sprintf("    command: [%s]\n", strings.Join(quoted, ", "))
		}
	}

	// 应用文件挂载：默认宿主机目录 bind mount；HOST_APPS_VOLUME 存在时（Windows 部署）
	// 改为挂载共享 named volume 的 subpath（要求 Docker Engine >= 26 / Compose >= 2.26）。
	appsVolume := getAppsVolumeName()
	appDataMount := fmt.Sprintf("      - %s:/app/data\n", appDir)
	if appsVolume != "" {
		appDataMount = fmt.Sprintf(
			"      - type: volume\n        source: apps\n        target: /app/data\n        volume:\n          subpath: %s\n",
			appID)
	}

	nodeModulesVolume := ""
	if needsNodeModulesInstall {
		nodeModulesVolume = "      - node_modules:/app/data/node_modules\n"
	}
	// 用户自定义磁盘映射（导入「高级」区提交，logic 层已归一为最终列表）：
	// 宿主机路径 bind mount 到容器路径，双引号包裹防 YAML 注入；路径合法性由 normalizeVolumes 保证
	customMountLines := ""
	for _, v := range customVolumes {
		if v.ReadOnly {
			customMountLines += fmt.Sprintf("      - %q\n", v.HostPath+":"+v.ContainerPath+":ro")
		} else {
			customMountLines += fmt.Sprintf("      - %q\n", v.HostPath+":"+v.ContainerPath)
		}
	}
	volumesTopEntries := ""
	if appsVolume != "" {
		volumesTopEntries += fmt.Sprintf("  apps:\n    external: true\n    name: %s\n", appsVolume)
	}
	if needsNodeModulesInstall {
		volumesTopEntries += "  node_modules:\n"
	}
	volumesTop := ""
	if volumesTopEntries != "" {
		volumesTop = "volumes:\n" + volumesTopEntries
	}

	// 动态应用只需要接入平台 runtime 网络即可访问 backend/emqx/tsdb。
	// 不再创建 <uuid>_default 网络，避免每个应用额外占用一个 Docker 子网。
	networksBlock := "    networks:\n      - runtime\n"
	if len(networkAlias) > 0 && networkAlias[0] != "" {
		networksBlock = fmt.Sprintf("    networks:\n      runtime:\n        aliases:\n          - %q\n", networkAlias[0])
	}
	networksTop := fmt.Sprintf("networks:\n  runtime:\n    external: true\n    name: %s\n", getRuntimeNetworkName())
	if containerName == "" {
		containerName = appContainerName(appName, appID)
	}
	shortAppID := shortID(appID)
	fleetLabels := fmt.Sprintf(
		"    labels:\n      com.tier0.fleet.managed: \"true\"\n      com.tier0.fleet.installation-id: %q\n      com.tier0.fleet.service: %q\n      com.tier0.ha.group: \"active\"\n      com.tier0.ha.start-order: \"40\"\n      com.tier0.fleet.app-name: %q\n      com.tier0.fleet.restartable: \"true\"\n",
		strings.TrimSpace(installationID),
		"app-"+shortAppID,
		appName,
	)

	return fmt.Sprintf(`version: "3.8"
services:
  %s:
    image: %s
    container_name: %s
    working_dir: /app/data
%s    volumes:
%s%s%s%s%s%s    ports:
      - %d:%d
    restart: ${FLEET_HA_BUSINESS_RESTART_POLICY:-unless-stopped}
    healthcheck:
      test: ["CMD", "sh", "-c", "wget -qO- http://127.0.0.1:%d%s >/dev/null"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: %ds
%s%s`, containerName, image, containerName, fleetLabels, appDataMount, nodeModulesVolume, customMountLines, envBlock, commandBlock, networksBlock, hostPort, containerPort, containerPort, healthPath, startPeriod, volumesTop, networksTop)
}

// artifactConfig 解析后的 artifact.toml 部署配置
type artifactConfig struct {
	Image          string
	ContainerPort  int
	Command        []string
	HealthPath     string
	Env            map[string]string
	StartupTimeout int // health.startup.timeout（秒），0=未声明（走默认 180s）
}

// parseArtifactConfig 兼容简单格式与新版 services 嵌套格式
func parseArtifactConfig(manifest map[string]any) artifactConfig {
	cfg := artifactConfig{
		ContainerPort: 8080,
		HealthPath:    "/",
		Env:           make(map[string]string),
	}

	// 简单格式（旧版占位镜像用）
	if img, ok := manifest["image"].(string); ok && img != "" {
		cfg.Image = img
	}
	// 简单格式 port 兼容 float64/int64/int 三种类型
	if p, ok := getIntValue(manifest["port"]); ok {
		cfg.ContainerPort = p
	}
	if hp, ok := manifest["health_path"].(string); ok && hp != "" {
		cfg.HealthPath = hp
	}

	// 新版 services 嵌套格式
	if svcList, ok := manifest["services"].([]any); ok && len(svcList) > 0 {
		svc, ok := svcList[0].(map[string]any)
		if ok {
			if p, ok := getIntValue(svc["localPort"]); ok {
				cfg.ContainerPort = p
			}
			if prod, ok := svc["production"].(map[string]any); ok {
				if run, ok := prod["run"].(map[string]any); ok {
					if img, ok := run["image"].(string); ok && img != "" {
						cfg.Image = img
					}
					if args, ok := run["args"].([]any); ok {
						for _, a := range args {
							if s, ok := a.(string); ok {
								cfg.Command = append(cfg.Command, s)
							}
						}
					}
					if env, ok := run["env"].(map[string]any); ok {
						for k, v := range env {
							cfg.Env[k] = fmt.Sprintf("%v", v)
						}
					}
				}
				if health, ok := prod["health"].(map[string]any); ok {
					if startup, ok := health["startup"].(map[string]any); ok {
						if p, ok := startup["path"].(string); ok && p != "" {
							cfg.HealthPath = p
						}
						if t, ok := getIntValue(startup["timeout"]); ok && t > 0 {
							cfg.StartupTimeout = t
						}
					}
				}
			}
		}
	}

	if cfg.HealthPath == "" {
		cfg.HealthPath = "/"
	}
	if !strings.HasPrefix(cfg.HealthPath, "/") {
		cfg.HealthPath = "/" + cfg.HealthPath
	}
	return cfg
}

// getIntValue 从 float64/int64/int 中提取 int
func getIntValue(v any) (int, bool) {
	switch val := v.(type) {
	case float64:
		return int(val), true
	case int64:
		return int(val), true
	case int:
		return val, true
	}
	return 0, false
}

// artifactManifestPort 提取 artifact 清单中显式声明的容器端口（简单格式 port / 新版 services[0].localPort），
// 未声明时返回 false（部署时沿用 parseArtifactConfig 的 8080 默认值）。
func artifactManifestPort(manifest map[string]any) (int, bool) {
	if p, ok := getIntValue(manifest["port"]); ok {
		return p, true
	}
	if svcList, ok := manifest["services"].([]any); ok && len(svcList) > 0 {
		if svc, ok := svcList[0].(map[string]any); ok {
			if p, ok := getIntValue(svc["localPort"]); ok {
				return p, true
			}
		}
	}
	return 0, false
}

// artifactManifestRequiresBuild 判断 artifact.toml 是否声明了包内构建步骤。
// SaaS 导出的已编译包通常仍声明 build 且自带构建产物（dist/ 等）；企业版导入不重新构建、直接运行产物，
// 因此声明 build 本身不拒绝，是否可导入由 validateArtifactBuiltOutput 结合包内产物目录判定。
func artifactManifestRequiresBuild(manifest map[string]any) bool {
	if manifest == nil {
		return false
	}
	svcList, ok := manifest["services"].([]any)
	if !ok {
		return false
	}
	for _, item := range svcList {
		svc, ok := item.(map[string]any)
		if !ok {
			continue
		}
		prod, ok := svc["production"].(map[string]any)
		if !ok {
			continue
		}
		build, ok := prod["build"].(map[string]any)
		if !ok {
			continue
		}
		args, ok := build["args"].([]any)
		if !ok {
			continue
		}
		for _, a := range args {
			if s, ok := a.(string); ok && strings.TrimSpace(s) != "" {
				return true
			}
		}
	}
	return false
}

// artifactRunArgs 从 artifact manifest 中提取 services[0].production.run.args。
func artifactRunArgs(manifest map[string]any) []string {
	if manifest == nil {
		return nil
	}
	svcList, ok := manifest["services"].([]any)
	if !ok || len(svcList) == 0 {
		return nil
	}
	svc, ok := svcList[0].(map[string]any)
	if !ok {
		return nil
	}
	prod, ok := svc["production"].(map[string]any)
	if !ok {
		return nil
	}
	run, ok := prod["run"].(map[string]any)
	if !ok {
		return nil
	}
	args, ok := run["args"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, a := range args {
		if s, ok := a.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// artifactRunEntry 从 run.args 中推断需要校验的入口脚本相对路径。
// 仅覆盖 "node <file>" / "python <file>" / "sh <file>" / "bash <file>" 等
// 解释器后跟单个脚本文件的形式；遇到解释器选项（如 -m/--enable-source-maps）会跳过，
// 直到找到不含前导 - 的参数；若该参数明显是文件路径（含 . 或 /）则返回，否则视为不可校验命令。
func artifactRunEntry(manifest map[string]any) string {
	args := artifactRunArgs(manifest)
	if len(args) < 2 {
		return ""
	}
	interpreter := strings.ToLower(filepath.Base(args[0]))
	allowed := map[string]bool{"node": true, "python": true, "python3": true, "python2": true, "sh": true, "bash": true}
	if !allowed[interpreter] {
		return ""
	}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		// 跳过解释器选项（如 node --enable-source-maps、python -m）
		if strings.HasPrefix(arg, "-") {
			continue
		}
		// 只把看起来像文件路径的参数作为入口脚本（如 server.mjs、app.py、./start.sh）
		if strings.Contains(arg, ".") || strings.Contains(arg, "/") {
			return arg
		}
		// 首个非选项参数不是文件路径（如 python -m uvicorn 中的 uvicorn），停止查找
		return ""
	}
	return ""
}

// artifactEntryRelPath 计算入口脚本在 artifact 根目录内的相对路径。
// 规范化（清理 ./、// 等）并拒绝逃出根目录的路径（../、绝对路径）；
// 合法时返回相对路径与 true，否则返回空与 false。
func artifactEntryRelPath(artifactDir, entry string) (string, bool) {
	if !filepath.IsLocal(entry) {
		return "", false
	}
	// artifact.toml 位于 zip 根时无需前缀约束
	if artifactDir == "" || artifactDir == "." {
		return filepath.Clean(entry), true
	}
	base := filepath.Clean(artifactDir)
	rel := filepath.Clean(filepath.Join(base, entry))
	// 解析后必须仍位于 artifactDir 之内，防止 ../ 逃逸到其他目录
	if rel != base && !strings.HasPrefix(rel, base+string(filepath.Separator)) {
		return "", false
	}
	return rel, true
}

// validateArtifactRunEntry 校验 artifact 声明的运行入口文件是否存在。
// 仅对能识别的解释器+脚本路径模式生效；无法识别时静默通过，避免误杀自定义命令。
func validateArtifactRunEntry(manifest map[string]any, entryExists func(string) bool) error {
	entry := artifactRunEntry(manifest)
	if entry == "" {
		return nil
	}
	if !entryExists(entry) {
		return fmt.Errorf("%w: 运行入口文件 %q 不存在", domainproject.ErrUnsupportedPackageFormat, entry)
	}
	return nil
}

// validateArtifactBuiltOutput 校验声明了构建步骤的 artifact 包是否自带构建产物。
// 企业版导入不执行包内构建，因此声明 build.args 的包必须已包含产物目录
// （dist/ 或 node_modules/ 至少其一，SaaS 导出已编译包的标准形态）；否则在导入时同步拒绝（400）。
// entryExists 接收相对路径（如 "dist"）并返回该路径是否在包内存在（文件或目录均可）。
func validateArtifactBuiltOutput(manifest map[string]any, entryExists func(string) bool) error {
	if !artifactManifestRequiresBuild(manifest) {
		return nil
	}
	if entryExists("dist") || entryExists("node_modules") {
		return nil
	}
	return fmt.Errorf("%w: 该包声明了构建步骤（services[].production.build）但缺少构建产物（dist/ 或 node_modules/），请导入已完成构建（含产物目录）的应用包", domainproject.ErrUnsupportedPackageFormat)
}

// zipEntryExists 检查 zip 中是否存在指定相对路径的文件（非目录）。
func zipEntryExists(path string, entry string) (bool, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if f.Name == entry {
			return true, nil
		}
	}
	return false, nil
}

// zipDirOrFileExists 检查 zip 中是否存在指定相对路径的条目（文件或目录）。
// 目录按 "dir/" 前缀匹配 zip 内的普通文件条目，兼容纯文件条目或显式目录条目的打包方式。
func zipDirOrFileExists(path string, entry string) (bool, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false, err
	}
	defer zr.Close()
	prefix := strings.TrimSuffix(entry, "/") + "/"
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			if f.Name == entry || f.Name == entry+"/" {
				return true, nil
			}
			continue
		}
		if f.Name == entry || strings.HasPrefix(f.Name, prefix) {
			return true, nil
		}
	}
	return false, nil
}

// validateArtifactManifest 同步校验 artifact 包的核心部署配置，失败直接返回可读错误，
// 在创建应用记录/标记部署中之前拦截明显无效的包（image 缺失、端口越界等）。
// 包装为 domainproject.ErrUnsupportedPackageFormat（logicx 映射 HTTP 400），
// 保证非法包以 400 返回而非透传 500；错误消息保持中文可读。
// health_path 未以 / 开头时按 parseArtifactConfig 的现状自动补 / 后再校验（不视为错误）。
func validateArtifactManifest(manifest map[string]any) error {
	if manifest == nil {
		return fmt.Errorf("%w: artifact 配置缺失", domainproject.ErrUnsupportedPackageFormat)
	}
	cfg := parseArtifactConfig(manifest)
	if strings.TrimSpace(cfg.Image) == "" {
		return fmt.Errorf("%w: artifact 配置缺少镜像（image）", domainproject.ErrUnsupportedPackageFormat)
	}
	if p, ok := artifactManifestPort(manifest); ok && (p < 1 || p > 65535) {
		return fmt.Errorf("%w: artifact 端口 %d 超出范围 [1,65535]", domainproject.ErrUnsupportedPackageFormat, p)
	}
	if !strings.HasPrefix(cfg.HealthPath, "/") {
		return fmt.Errorf("%w: artifact health_path %q 必须以 / 开头", domainproject.ErrUnsupportedPackageFormat, cfg.HealthPath)
	}
	return nil
}

// cleanupApp 清理应用容器和资源
func cleanupApp(appDir, containerName string) error {
	var cleanupErr error
	composePath := filepath.Join(appDir, "docker-compose.yml")
	if _, err := os.Stat(composePath); err == nil {
		cmd := appComposeCommand(context.Background(), composePath, "down", "--volumes", "--remove-orphans")
		if output, err := cmd.CombinedOutput(); err != nil {
			logx.Errorf("[cleanupApp] docker compose down failed: %v, output: %s", err, string(output))
			cleanupErr = fmt.Errorf("docker compose down: %v, output: %s", err, strings.TrimSpace(string(output)))
		}
	}
	if err := forceRemoveContainerIfPresent(context.Background(), containerName); err != nil {
		cleanupErr = errors.Join(cleanupErr, err)
	}
	return errors.Join(cleanupErr, os.RemoveAll(appDir))
}

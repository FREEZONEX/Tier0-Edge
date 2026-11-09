package project

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"backend/internal/appgateway"
	"backend/internal/config"
	"backend/internal/contextx"
	domainasset "backend/internal/domain/asset"
	auditdomain "backend/internal/domain/audit"
	domaincommon "backend/internal/domain/common"
	domainflow "backend/internal/domain/flow"
	domainproject "backend/internal/domain/project"
	domainuns "backend/internal/domain/uns"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type ProjectAppLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectAppLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectAppLogic {
	return &ProjectAppLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProjectAppLogic) List(req *types.ProjectAppReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Project.ListApps(l.ctx, req.ProjectId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

func (l *ProjectAppLogic) Uns(req *types.ProjectAppReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Project.ListUns(l.ctx, req.ProjectId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"list": data, "total": len(data)}), nil
}

func (l *ProjectAppLogic) Flows(req *types.ProjectAppReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Project.ListFlows(l.ctx, req.ProjectId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"list": data, "total": len(data)}), nil
}

func (l *ProjectAppLogic) Members(req *types.ProjectAppReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Project.ListMembers(l.ctx, req.ProjectId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"list": data, "total": len(data)}), nil
}

func (l *ProjectAppLogic) AddMember(req *types.ProjectMemberSaveReq) (resp *types.Envelope, err error) {
	userID := parseInt64(req.UserId)
	data, err := l.svcCtx.App.Project.AddMember(l.ctx, domainproject.MemberSaveCommand{
		ProjectID:      req.ProjectId,
		UserID:         userID,
		RoleIDs:        req.RoleIds,
		OperatorUserID: logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	memberID := memberIDFromResp(data)
	l.recordProjectMemberAudit(auditdomain.BizCreate, req.ProjectId, memberID, userID)
	return respx.Envelope(map[string]any{"member": data}), nil
}

func (l *ProjectAppLogic) UpdateMember(req *types.ProjectMemberSaveReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Project.UpdateMember(l.ctx, domainproject.MemberSaveCommand{
		ProjectID:      req.ProjectId,
		MemberID:       req.MemberId,
		RoleIDs:        req.RoleIds,
		OperatorUserID: logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	memberID := memberIDFromResp(data)
	userID := userIDFromMemberResp(data)
	l.recordProjectMemberAudit(auditdomain.BizUpdate, req.ProjectId, memberID, userID)
	return respx.Envelope(map[string]any{"member": data}), nil
}

func (l *ProjectAppLogic) DeleteMember(req *types.ProjectMemberReq) (resp *types.Envelope, err error) {
	if err := l.svcCtx.App.Project.DeleteMember(l.ctx, req.ProjectId, req.MemberId, logicx.UserID(l.ctx)); err != nil {
		return nil, logicx.Error(err)
	}
	l.recordProjectMemberAudit(auditdomain.BizDelete, req.ProjectId, req.MemberId, 0)
	return respx.Envelope(map[string]any{"deleted": true}), nil
}

func (l *ProjectAppLogic) Roles(req *types.ProjectAppReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Project.ListRoles(l.ctx, req.ProjectId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"list": data, "total": len(data)}), nil
}

func (l *ProjectAppLogic) Import(req *types.ProjectAppImportReq) (resp *types.Envelope, err error) {
	if err := l.svcCtx.App.Project.CheckManage(l.ctx, req.ProjectId, logicx.UserID(l.ctx)); err != nil {
		return nil, logicx.Error(err)
	}
	fileID := projectImportFileID(req)
	if fileID == 0 {
		l.recordProjectAppImportAudit(req.ProjectId, 0, "", projectAppImportAuditDetail{Decision: "rejected", Reason: "invalid file"})
		return nil, logicx.Error(domainproject.ErrInvalid)
	}
	// 读取前校验包 asset 归属当前 Project（按 asset_binding 绑定关系），越权 403
	if err := checkImportAssetOwned(l.ctx, fileID, req.ProjectId, projectImportAssetOwnerType); err != nil {
		l.recordProjectAppImportAudit(req.ProjectId, 0, "", projectAppImportAuditDetail{Decision: "rejected", Reason: err.Error(), FileID: fileID})
		return nil, logicx.Error(err)
	}
	file, reader, _, err := l.svcCtx.App.Asset.Open(l.ctx, fileID)
	if err != nil {
		l.recordProjectAppImportAudit(req.ProjectId, 0, "", projectAppImportAuditDetail{Decision: "failed", Reason: err.Error(), FileID: fileID})
		return nil, logicx.Error(err)
	}
	defer reader.Close()

	// 本地文件存储时直接复用原始路径，避免同步复制大文件。
	localPath, isLocal := localAssetFilePath(reader)

	importOpts, optsErr := buildImportOptions(req.SchemaName, req.EnvPolicy, req.EnvVars, req.VolumePolicy, req.Volumes, req.NetworkAlias)
	if optsErr != nil {
		l.recordProjectAppImportAudit(req.ProjectId, 0, "", projectAppImportAuditDetail{Decision: "rejected", Reason: optsErr.Error(), FileID: fileID})
		return nil, logicx.Error(optsErr)
	}

	if isLocal && hasArtifactTOML(localPath) {
		app, err := NewProjectAppImportLogic(l.ctx, l.svcCtx).ProjectAppImport(req.ProjectId, localPath, req.AppName, req.Description, nil, importOpts)
		if err != nil {
			if resp := l.replaceRequiredDecision(req.ProjectId, fileID, "", err); resp != nil {
				return resp, nil
			}
			l.recordProjectAppImportAudit(req.ProjectId, 0, "", projectAppImportAuditDetail{Decision: "rejected", Reason: err.Error(), FileID: fileID})
			return nil, logicx.Error(err)
		}
		l.recordProjectAppImportAudit(req.ProjectId, app.ID, app.Name, projectAppImportAuditDetail{Decision: "success", FileID: fileID})
		return respx.Envelope(map[string]any{
			"appId":    app.ID,
			"name":     app.Name,
			"status":   app.Status,
			"siteType": app.SiteType,
		}), nil
	}

	// 非本地存储或静态应用包：先流式落到临时文件。
	tmpPath, cleanup, err := copyAssetToTempFile(reader)
	if err != nil {
		l.recordProjectAppImportAudit(req.ProjectId, 0, "", projectAppImportAuditDetail{Decision: "failed", Reason: err.Error(), FileID: fileID})
		return nil, logicx.Error(err)
	}

	if isLocal {
		// 本地静态包直接复用原路径，不再创建临时副本。
		_ = os.Remove(tmpPath)
		tmpPath = localPath
		cleanup = func() {}
	}

	if hasArtifactTOML(tmpPath) {
		// 非本地存储时，临时 zip 由 ProjectAppImport 的异步 goroutine 负责清理。
		app, err := NewProjectAppImportLogic(l.ctx, l.svcCtx).ProjectAppImport(req.ProjectId, tmpPath, req.AppName, req.Description, cleanup, importOpts)
		if err != nil {
			if resp := l.replaceRequiredDecision(req.ProjectId, fileID, "", err); resp != nil {
				return resp, nil
			}
			l.recordProjectAppImportAudit(req.ProjectId, 0, "", projectAppImportAuditDetail{Decision: "rejected", Reason: err.Error(), FileID: fileID})
			return nil, logicx.Error(err)
		}
		l.recordProjectAppImportAudit(req.ProjectId, app.ID, app.Name, projectAppImportAuditDetail{Decision: "success", FileID: fileID})
		return respx.Envelope(map[string]any{
			"appId":    app.ID,
			"name":     app.Name,
			"status":   app.Status,
			"siteType": app.SiteType,
		}), nil
	}

	if importOpts.networkAlias != "" {
		cleanup()
		return nil, respx.NewHTTPError(400, "project.networkAliasDynamic")
	}
	pkg, err := readProjectAppPackageFromPath(tmpPath, file.OriginalName)
	if err != nil {
		cleanup()
		l.recordProjectAppImportAudit(req.ProjectId, 0, "", projectAppImportAuditDetail{Decision: "rejected", Reason: err.Error(), FileID: fileID})
		return nil, logicx.Error(err)
	}
	appName := pkg.Meta.Name
	if strings.TrimSpace(req.AppName) != "" {
		appName = strings.TrimSpace(req.AppName)
	}
	// 用户提交描述优先于包内 meta 描述（与 appName 覆盖规则一致），校验与 manifest 落库均用生效值
	pkg.Meta.Description = resolveImportAppDescription(req.Description, pkg.Meta.Description)
	// 服务端 rune 长度校验：name≤64、description≤200，超限 400
	if err := validateProjectAppMetaLength(appName, pkg.Meta.Description); err != nil {
		cleanup()
		l.recordProjectAppImportAudit(req.ProjectId, 0, "", projectAppImportAuditDetail{Decision: "rejected", Reason: err.Error(), FileID: fileID})
		return nil, logicx.Error(err)
	}
	manifest := map[string]any{
		"name":         appName,
		"displayName":  pkg.Meta.DisplayName,
		"description":  pkg.Meta.Description,
		"version":      pkg.Meta.Version,
		"fileId":       fileID,
		"originalName": file.OriginalName,
		"deploying":    true,
	}
	// meta.json 新字段：schemaAlias/changelog/来源三 ID 等，空值不写入
	applyPackageMetaToManifest(manifest, pkg.Meta, true)
	// 导入为新 App 前按包内来源 appId 查重：命中其他 Project 阻断；命中同 Project 时
	// 静态包不支持 Replace 覆盖，保持阻断错误，不转 replaceRequired 决策（Replace 仅接受 artifact 包）
	if err := checkAppSourceDuplicate(l.ctx, req.ProjectId, pkg.Meta); err != nil {
		cleanup()
		err = staticDuplicateError(err)
		l.recordProjectAppImportAudit(req.ProjectId, 0, "", projectAppImportAuditDetail{Decision: "rejected", Reason: err.Error(), FileID: fileID, PackageAppID: pkg.Meta.AppID})
		return nil, logicx.Error(err)
	}
	respData, isNew, err := l.svcCtx.App.Project.ImportApp(l.ctx, domainproject.AppImportCommand{
		ProjectID: req.ProjectId,
		AppName:   appName,
		Version:   pkg.Meta.Version,
		Manifest:  manifest,
		FileID:    fileID,
		UserID:    logicx.UserID(l.ctx),
		Status:    "deploying",
	})
	if err != nil {
		cleanup()
		l.recordProjectAppImportAudit(req.ProjectId, 0, "", projectAppImportAuditDetail{Decision: "failed", Reason: err.Error(), FileID: fileID, PackageAppID: pkg.Meta.AppID})
		return nil, logicx.Error(err)
	}
	appID := int64Value(respData["appId"])
	if appID != 0 {
		l.recordProjectAppImportAudit(req.ProjectId, appID, appName, projectAppImportAuditDetail{Decision: "success", FileID: fileID, PackageAppID: pkg.Meta.AppID})
		// 静态应用导入的后续解压、资源导入改为后台 goroutine 执行，避免接口超时。
		// 临时文件由 goroutine 通过 cleanup 负责清理；本地原文件 cleanup 为空操作。
		go runProjectAppImportAsync(l.ctx, l.svcCtx, req.ProjectId, appID, tmpPath, file.OriginalName, isNew, logicx.UserID(l.ctx), cleanup)
	} else {
		cleanup()
	}
	return respx.Envelope(respData), nil
}

// replaceRequiredDecision 把 appReplaceRequiredError 哨兵转为 replaceRequired 决策响应（HTTP 200）。
// 普通 Import 命中同 Project 相同来源 appId 时不直接覆盖（确认前零副作用），
// 由前端据此弹确认后调 Replace 接口完成覆盖（EIMP-062）；非哨兵错误返回 nil 走原错误处理。
func (l *ProjectAppLogic) replaceRequiredDecision(projectID, fileID int64, packageAppID string, err error) *types.Envelope {
	var replaceErr *appReplaceRequiredError
	if !errors.As(err, &replaceErr) {
		return nil
	}
	l.recordProjectAppImportAudit(projectID, replaceErr.AppID, replaceErr.Name, projectAppImportAuditDetail{
		Decision:     "replaceRequired",
		TargetAppID:  replaceErr.AppID,
		PackageAppID: packageAppID,
		FileID:       fileID,
	})
	return respx.Envelope(map[string]any{
		"replaceRequired": true,
		"targetAppId":     replaceErr.AppID,
		"targetProjectId": replaceErr.ProjectID,
		"name":            replaceErr.Name,
	})
}

// Replace 处理覆盖导入（Import to replace）：仅接受含 artifact.toml 的 Export Bundle，
// 保留目标 App 身份并重新部署。包内 appId 与目标 App 来源 appId 不一致时硬阻断。
func (l *ProjectAppLogic) Replace(req *types.ProjectAppReplaceReq) (resp *types.Envelope, err error) {
	if err := l.svcCtx.App.Project.CheckManage(l.ctx, req.ProjectId, logicx.UserID(l.ctx)); err != nil {
		return nil, logicx.Error(err)
	}
	if req.AppId <= 0 {
		l.recordProjectAppImportAudit(req.ProjectId, 0, "", projectAppImportAuditDetail{Decision: "rejected", Reason: "invalid app"})
		return nil, logicx.Error(domainproject.ErrInvalid)
	}
	fileID := projectReplaceFileID(req)
	if fileID == 0 {
		l.recordProjectAppImportAudit(req.ProjectId, 0, "", projectAppImportAuditDetail{Decision: "rejected", Reason: "invalid file"})
		return nil, logicx.Error(domainproject.ErrInvalid)
	}
	// 读取前校验包 asset 归属当前 Project（Replace 弹窗上传的绑定类型为 projectAppReplacement、
	// Import 弹窗为 projectApp，两者 ownerId 均为 projectId），越权 403。
	// 旧版本 Replace 弹窗曾绑定 ownerId=appId，历史文件不在校验范围内，重新上传即可。
	if err := checkImportAssetOwned(l.ctx, fileID, req.ProjectId, projectImportAssetOwnerType, projectReplaceAssetOwnerType); err != nil {
		l.recordProjectAppImportAudit(req.ProjectId, req.AppId, "", projectAppImportAuditDetail{Decision: "rejected", Reason: err.Error(), TargetAppID: req.AppId, FileID: fileID})
		return nil, logicx.Error(err)
	}
	_, reader, _, err := l.svcCtx.App.Asset.Open(l.ctx, fileID)
	if err != nil {
		l.recordProjectAppImportAudit(req.ProjectId, req.AppId, "", projectAppImportAuditDetail{Decision: "failed", Reason: err.Error(), TargetAppID: req.AppId, FileID: fileID})
		return nil, logicx.Error(err)
	}
	defer reader.Close()

	// 本地文件存储时直接复用原始路径，避免同步复制大文件。
	localPath, isLocal := localAssetFilePath(reader)
	tmpPath := localPath
	cleanup := func() {}
	if !isLocal {
		// 非本地存储：先流式落到临时文件；后续异步 goroutine 通过 cleanup 负责清理。
		tmpPath, cleanup, err = copyAssetToTempFile(reader)
		if err != nil {
			l.recordProjectAppImportAudit(req.ProjectId, req.AppId, "", projectAppImportAuditDetail{Decision: "failed", Reason: err.Error(), TargetAppID: req.AppId, FileID: fileID})
			return nil, logicx.Error(err)
		}
	}

	// 覆盖导入仅接受含 artifact.toml 的 Export Bundle
	if !hasArtifactTOML(tmpPath) {
		cleanup()
		l.recordProjectAppImportAudit(req.ProjectId, req.AppId, "", projectAppImportAuditDetail{Decision: "rejected", Reason: domainproject.ErrUnsupportedPackageFormat.Error(), TargetAppID: req.AppId, FileID: fileID})
		return nil, logicx.Error(domainproject.ErrUnsupportedPackageFormat)
	}
	// Replace 只能编辑环境变量与磁盘映射，schema 不可变（沿用 project_app.schema_name，前端忽略 schemaName 提交）
	replaceOpts, optsErr := buildImportOptions("", req.EnvPolicy, req.EnvVars, req.VolumePolicy, req.Volumes, req.NetworkAlias)
	if optsErr != nil {
		l.recordProjectAppImportAudit(req.ProjectId, req.AppId, "", projectAppImportAuditDetail{Decision: "rejected", Reason: optsErr.Error(), TargetAppID: req.AppId, FileID: fileID})
		return nil, logicx.Error(optsErr)
	}
	app, err := NewProjectAppImportLogic(l.ctx, l.svcCtx).ProjectAppImportReplace(req.ProjectId, req.AppId, tmpPath, req.Description, cleanup, replaceOpts)
	if err != nil {
		l.recordProjectAppImportAudit(req.ProjectId, req.AppId, "", projectAppImportAuditDetail{Decision: "rejected", Reason: err.Error(), TargetAppID: req.AppId, FileID: fileID})
		return nil, logicx.Error(err)
	}
	l.recordProjectAppImportAudit(req.ProjectId, app.ID, app.Name, projectAppImportAuditDetail{Decision: "success", TargetAppID: req.AppId, FileID: fileID})
	return respx.Envelope(map[string]any{
		"appId":    app.ID,
		"name":     app.Name,
		"status":   app.Status,
		"siteType": app.SiteType,
	}), nil
}

// runProjectAppImportAsync 在后台 goroutine 中完成静态应用的资源导入。
func runProjectAppImportAsync(parentCtx context.Context, svcCtx *svc.ServiceContext, projectID, appID int64, tmpPath, originalName string, isNew bool, userID int64, cleanup func()) {
	ctx := context.Background()
	if subject, ok := contextx.SubjectFrom(parentCtx); ok {
		ctx = contextx.WithSubject(ctx, subject)
	}
	logger := logx.WithContext(ctx)
	defer cleanup()

	worker := &ProjectAppLogic{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logger,
	}

	// 本次新建的资源 ID 集合，失败分支据此做逆序清理（只删 INSERT 的，不含命中更新的既有资源）
	var created importCreatedResources

	// 失败时清理或标记状态
	markFailed := func(err error) {
		logger.Errorf("project app import async failed: projectID=%d appID=%d err=%v", projectID, appID, err)
		cleanupImportedResources(ctx, created, projectID, appID, userID)
		if isNew {
			if _, delErr := svcCtx.App.Project.DeleteApp(ctx, projectID, appID, userID); delErr != nil {
				logger.Errorf("cleanup project app after import failure: %v", delErr)
			}
			return
		}
		patch := map[string]any{
			"deploying":            false,
			"deploy_error":         formatDeployError(err, ""),
			"deploy_error_summary": buildDeployErrorSummary(err),
		}
		if err := svcCtx.App.Project.PatchAppManifest(ctx, projectID, appID, patch, userID); err != nil {
			logger.Errorf("patch app manifest after import failure: %v", err)
		}
		if _, err := svcCtx.App.Project.UpdateAppStatus(ctx, projectID, appID, "failed", userID); err != nil {
			logger.Errorf("update app status after import failure: %v", err)
		}
	}

	pkg, err := readProjectAppPackageFromPath(tmpPath, originalName)
	if err != nil {
		markFailed(fmt.Errorf("read package: %w", err))
		return
	}

	resourcePatch, resourceCreated, err := worker.importPackageResources(projectID, appID, pkg, false)
	created.merge(resourceCreated)
	if err != nil {
		markFailed(fmt.Errorf("import resources: %w", err))
		return
	}

	// 包根目录的 icon/cover 上传为 asset，并回写 icon_asset_id / coverAssetId
	assetPatch, assetIDs, err := worker.importPackageAssets(projectID, appID, pkg, userID)
	created.AssetIDs = append(created.AssetIDs, assetIDs...)
	if err != nil {
		markFailed(fmt.Errorf("import assets: %w", err))
		return
	}

	// 所有可失败步骤（资源/icon/cover）已成功：发布本次新建 Flow（失败降级不阻断导入）

	// 导入成功：更新 manifest 并置为 active
	patch := map[string]any{
		"deploying": false,
	}
	for key, value := range resourcePatch {
		patch[key] = value
	}
	for key, value := range assetPatch {
		patch[key] = value
	}
	if err := svcCtx.App.Project.PatchAppManifest(ctx, projectID, appID, patch, userID); err != nil {
		logger.Errorf("patch app manifest after import success: %v", err)
	}
	if _, err := svcCtx.App.Project.UpdateAppStatus(ctx, projectID, appID, "active", userID); err != nil {
		logger.Errorf("update app status after import success: %v", err)
	}
	logger.Infof("project app import async success: projectID=%d appID=%d", projectID, appID)
}

func (l *ProjectAppLogic) Status(req *types.ProjectAppStatusReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Project.UpdateAppStatus(l.ctx, req.ProjectId, req.AppId, req.Status, logicx.UserID(l.ctx))
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

func (l *ProjectAppLogic) Delete(req *types.ProjectAppReq) (resp *types.Envelope, err error) {
	userID := logicx.UserID(l.ctx)
	if err := l.svcCtx.App.Project.CheckManage(l.ctx, req.ProjectId, userID); err != nil {
		return nil, logicx.Error(err)
	}
	app, err := repo.NewProjectRepo(l.ctx).GetProjectApp(l.ctx, req.ProjectId, req.AppId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	if app.SiteType == "dynamic" {
		if err := NewProjectAppDeleteLogic(l.ctx, l.svcCtx).ProjectAppDelete(req.ProjectId, req.AppId, userID); err != nil {
			return nil, logicx.Error(err)
		}
		l.recordProjectAppDeleteAudit(req.ProjectId, req.AppId, app.Name)
		return respx.Envelope(map[string]any{"deleted": true}), nil
	}
	data, err := l.svcCtx.App.Project.DeleteApp(l.ctx, req.ProjectId, req.AppId, userID)
	if err != nil {
		return nil, logicx.Error(err)
	}
	l.recordProjectAppDeleteAudit(req.ProjectId, req.AppId, app.Name)
	return respx.Envelope(data), nil
}

const (
	// flowImportNameMaxLen 是导入 Flow 时允许的最大名称长度，超过时需截短原名称以容纳序号后缀。
	flowImportNameMaxLen = domaincommon.NameMaxLen
)

// projectAppMeta 包根 meta.json 的解析结果。
// WorkspaceID/ProjectID/AppID 为导出来源的身份标识（字符串），Import to replace 时用于身份校验；
// 缺失或为空时全部走现有回退，不报错。
type projectAppMeta struct {
	Name        string
	DisplayName string
	Description string
	Version     string
	WorkspaceID string
	ProjectID   string
	AppID       string
	SchemaAlias string
	Changelog   string
}

// projectFlowFile 代表应用包内一个待导入的 flow json 文件。
type projectFlowFile struct {
	Path        string // zip 内原始路径，如 "flows.json" / "flows/sub.json"；云端 wrapper 拆出的条目带 "#序号" 后缀
	Name        string // 推导出的 flow 显示名（默认取文件 basename；云端条目取 flowName）
	Raw         []byte // 文件原始内容（云端条目为 flowData 节点数组）
	FlowType    int    // 1=source, 2=event；默认 1
	IdentityID  int64  // 来源环境平台数据库主键（云端条目 id）；0 表示无身份，按 extractFlowIdentityID 回退
	Description string // flow 描述（云端条目 description）；空时回退包 meta 描述
}

type projectAppPackage struct {
	Meta      projectAppMeta
	Uns       projectUnsPackage
	Roles     []projectPackageRole
	FlowRaw   []byte
	FlowFiles []projectFlowFile
	IconRaw   []byte
	IconName  string
	CoverRaw  []byte
	CoverName string
}

type projectUnsPackage struct {
	Namespace []projectUnsNode `json:"namespace"`
}

type projectUnsNode struct {
	Name             string           `json:"name"`
	Type             string           `json:"type"`
	TopicType        string           `json:"topicType"`
	Description      string           `json:"description"`
	Fields           []map[string]any `json:"fields"`
	ExtendProperties map[string]any   `json:"extendProperties"`
	EnableHistory    json.RawMessage  `json:"enableHistory,omitempty"`
	Children         []projectUnsNode `json:"children"`
}

const (
	// projectImportAssetOwnerType 前端 Import 弹窗上传导入包时 asset 绑定的 ownerType（ownerId=projectId），
	// 与 domain 层 ProjectImportAssetOwnerType 保持一致，供 core multipart 分片上传 complete 绑定复用。
	projectImportAssetOwnerType = domainproject.ProjectImportAssetOwnerType
	// projectReplaceAssetOwnerType 前端 Replace 弹窗上传覆盖包时 asset 绑定的 ownerType（ownerId=projectId）
	projectReplaceAssetOwnerType = "projectAppReplacement"
)

// checkImportAssetOwned 校验导入包 asset 归属当前 Project（按 asset_binding 绑定关系）。
// ownerTypes 为前端上传时可能的绑定类型集合：Import 弹窗绑定 (projectApp, projectId)，
// Replace 弹窗绑定 (projectAppReplacement, projectId)；任一 ownerType 命中即视为归属当前 Project。
// 不匹配统一返回 403 可读错误，不泄露文件名与来源 Project。
func checkImportAssetOwned(ctx context.Context, fileID, projectID int64, ownerTypes ...string) error {
	if fileID <= 0 || projectID <= 0 {
		return domainproject.ErrImportAssetDenied
	}
	for _, ownerType := range ownerTypes {
		if _, err := repo.NewAssetRepo(ctx).GetAssetFileByOwner(ctx, fileID, ownerType, projectID); err == nil {
			return nil
		} else if !errors.Is(err, repo.ErrNotFound) {
			return err
		}
	}
	return domainproject.ErrImportAssetDenied
}

func projectImportFileID(req *types.ProjectAppImportReq) int64 {
	if req == nil {
		return 0
	}
	if req.FileId != 0 {
		return req.FileId
	}
	if req.Id != 0 {
		return req.Id
	}
	id, _ := strconv.ParseInt(strings.TrimSpace(req.AttachmentPath), 10, 64)
	return id
}

// projectReplaceFileID 与 projectImportFileID 同规则：优先 fileId，其次 id，最后 attachmentPath
func projectReplaceFileID(req *types.ProjectAppReplaceReq) int64 {
	if req == nil {
		return 0
	}
	if req.FileId != 0 {
		return req.FileId
	}
	if req.Id != 0 {
		return req.Id
	}
	id, _ := strconv.ParseInt(strings.TrimSpace(req.AttachmentPath), 10, 64)
	return id
}

// localAssetFilePath 尝试获取对象存储 reader 对应的本地文件路径，避免同步复制。
func localAssetFilePath(reader io.Reader) (string, bool) {
	if f, ok := reader.(*os.File); ok {
		return f.Name(), true
	}
	return "", false
}

func copyAssetToTempFile(reader io.Reader) (string, func(), error) {
	tmpFile, err := os.CreateTemp("", "project-app-import-*.zip")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := io.Copy(tmpFile, reader); err != nil {
		_ = tmpFile.Close()
		cleanup()
		return "", nil, fmt.Errorf("failed to copy asset to temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to close temp file: %w", err)
	}
	return tmpPath, cleanup, nil
}

func readProjectAppPackageFromPath(path string, fallbackName string) (projectAppPackage, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return projectAppPackage{}, domainproject.ErrUnsupportedPackageFormat
	}
	defer zr.Close()

	// 与 artifact 解压路径（unzip）统一安全校验：entry 数上限 + 逐 entry 路径/类型校验，任一不通过即拒绝
	if len(zr.File) == 0 {
		return projectAppPackage{}, domainproject.ErrUnsupportedPackageFormat
	}
	if len(zr.File) > maxPackageEntries {
		return projectAppPackage{}, domainproject.ErrUnsupportedPackageFormat
	}
	for _, f := range zr.File {
		if err := validateZipEntry(f, "."); err != nil {
			return projectAppPackage{}, domainproject.ErrUnsupportedPackageFormat
		}
	}

	var pkg projectAppPackage
	var totalSize int64
	// meta.json 所在目录为包根。icon/cover 同时支持包根与其 public/ 目录，
	// public/ 优先；其他嵌套目录中的同名图片不得被误认为应用图标/封面。
	pkgRootDir, pkgRootDepth := "", -1
	type assetCandidate struct {
		file *zip.File
		name string
	}
	assetCandidates := make(map[string]assetCandidate)
	// flows/ 目录下的 flow 候选文件：zip 条目顺序不保证 meta.json 在前，
	// 循环中先记录候选及其所在目录，循环结束后按最终包根目录过滤
	type flowCandidate struct {
		file projectFlowFile
		dir  string
	}
	var flowCandidates []flowCandidate
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || shouldIgnoreProjectZipEntry(f.Name) {
			continue
		}
		// 按声明尺寸做单文件/总量上限校验，避免超大包耗尽内存（与 unzip 同一组上限）
		fileSize := f.UncompressedSize64
		if fileSize > uint64(maxPackageFileSize) {
			return projectAppPackage{}, domainproject.ErrUnsupportedPackageFormat
		}
		if fileSize > uint64(maxPackageTotalSize) || totalSize > maxPackageTotalSize-int64(fileSize) {
			return projectAppPackage{}, domainproject.ErrUnsupportedPackageFormat
		}
		totalSize += int64(fileSize)
		base := strings.ToLower(filepath.Base(f.Name))
		// 媒体候选只保留 zip entry 引用，待包根确定后仅解压最终命中的候选。
		// 避免恶意包用大量嵌套 icon/cover 让解压后字节长时间常驻内存。
		if name, ok := matchPackageAssetCandidateFile(base); ok {
			assetCandidates[normalizePackageEntryPath(f.Name)] = assetCandidate{file: f, name: name}
			continue
		}
		raw, readErr := readZipEntryLimited(f, maxPackageFileSize)
		if readErr != nil {
			return projectAppPackage{}, domainproject.ErrUnsupportedPackageFormat
		}
		if uint64(len(raw)) != f.UncompressedSize64 {
			// 实际解压量与声明量不一致，按非法包拒绝（与 extractZipEntry 的校验一致）
			return projectAppPackage{}, domainproject.ErrUnsupportedPackageFormat
		}
		switch base {
		case "meta.json":
			if depth := strings.Count(f.Name, "/"); pkgRootDepth < 0 || depth < pkgRootDepth {
				pkgRootDir, pkgRootDepth = filepath.Dir(f.Name), depth
			}
			meta, err := parseProjectAppMeta(raw)
			if err != nil {
				return projectAppPackage{}, domainproject.ErrUnsupportedPackageFormat
			}
			pkg.Meta = meta
		case "uns.json":
			if err := json.Unmarshal(raw, &pkg.Uns); err != nil {
				return projectAppPackage{}, fmt.Errorf("invalid uns.json: %w", err)
			}
		case "role.json", "roles.json":
			roles, err := parseProjectAppRoles(raw)
			if err != nil {
				return projectAppPackage{}, domainproject.ErrUnsupportedPackageFormat
			}
			pkg.Roles = roles
		case "flow.json", "flows.json", "sourceflow.json", "eventflow.json":
			// 同时保留 FlowRaw：flow.json 优先，兼容旧版 flows.json，满足既有测试与 artifact 包路径
			if base == "flow.json" || (base == "flows.json" && len(pkg.FlowRaw) == 0) {
				pkg.FlowRaw = raw
			}
			flowType := 1
			if strings.Contains(base, "event") {
				flowType = 2
			}
			pkg.FlowFiles = append(pkg.FlowFiles, projectFlowFile{
				Path:     f.Name,
				Name:     strings.TrimSuffix(base, ".json"),
				Raw:      raw,
				FlowType: flowType,
			})
		default:
			// 同时记录 flows/ 目录下的 flow json 候选（直接位于某 flows 目录下），
			// 是否在包根的 flows/ 由循环结束后的统一过滤判定
			dir := filepath.ToSlash(filepath.Dir(f.Name))
			if strings.EqualFold(filepath.Base(dir), "flows") && strings.EqualFold(filepath.Ext(base), ".json") {
				flowType := 1
				if strings.Contains(base, "event") {
					flowType = 2
				}
				flowCandidates = append(flowCandidates, flowCandidate{
					file: projectFlowFile{
						Path:     f.Name,
						Name:     strings.TrimSuffix(base, ".json"),
						Raw:      raw,
						FlowType: flowType,
					},
					dir: dir,
				})
			}
		}
	}
	// flows/ flow 候选按最终包根目录过滤（兼容带包装目录的 zip 及 meta.json 靠后的条目顺序）
	for _, cand := range flowCandidates {
		relDir := cand.dir
		if pkgRootDepth >= 0 {
			relDir = strings.TrimPrefix(cand.dir, filepath.ToSlash(pkgRootDir)+"/")
		}
		if strings.EqualFold(relDir, "flows") {
			pkg.FlowFiles = append(pkg.FlowFiles, cand.file)
		}
	}
	// 按 public/ 优先、包根回退选择 icon/cover。
	if pkgRootDepth >= 0 {
		selectAsset := func(prefix string) ([]byte, string, error) {
			for _, candidatePath := range packageAssetCandidatePaths(pkgRootDir, prefix) {
				candidate, ok := assetCandidates[candidatePath]
				if !ok || candidate.file.UncompressedSize64 == 0 {
					continue
				}
				if candidate.file.UncompressedSize64 > uint64(maxPackageMediaSize) {
					return nil, "", domainproject.ErrUnsupportedPackageFormat
				}
				raw, readErr := readZipEntryLimited(candidate.file, maxPackageMediaSize)
				if readErr != nil || uint64(len(raw)) != candidate.file.UncompressedSize64 {
					return nil, "", domainproject.ErrUnsupportedPackageFormat
				}
				if len(raw) == 0 {
					continue
				}
				return raw, candidate.name, nil
			}
			return nil, "", nil
		}
		pkg.IconRaw, pkg.IconName, err = selectAsset("icon")
		if err != nil {
			return projectAppPackage{}, err
		}
		pkg.CoverRaw, pkg.CoverName, err = selectAsset("cover")
		if err != nil {
			return projectAppPackage{}, err
		}
	}
	// 无 meta.json 的静态包一律拒绝（不再按文件名回退建 App）；
	// 有 meta.json 但 name 为空时保留文件名回退（名称兜底链的一环），文件名也为空才拒绝
	if pkgRootDepth < 0 {
		return projectAppPackage{}, domainproject.ErrUnsupportedPackageFormat
	}
	if strings.TrimSpace(pkg.Meta.Name) == "" {
		pkg.Meta.Name = strings.TrimSuffix(filepath.Base(fallbackName), filepath.Ext(fallbackName))
	}
	if strings.TrimSpace(pkg.Meta.Name) == "" {
		return projectAppPackage{}, domainproject.ErrUnsupportedPackageFormat
	}
	if len(pkg.FlowFiles) > 0 {
		return projectAppPackage{}, domainproject.ErrUnsupportedPackageFormat
	}
	return pkg, nil
}

func hasArtifactTOML(path string) bool {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || shouldIgnoreProjectZipEntry(f.Name) {
			continue
		}
		if strings.EqualFold(filepath.Base(f.Name), "artifact.toml") {
			return true
		}
	}
	return false
}

func parseProjectAppMeta(raw []byte) (projectAppMeta, error) {
	var wrapper struct {
		App json.RawMessage `json:"app"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return projectAppMeta{}, err
	}
	payload := raw
	if len(wrapper.App) > 0 && string(wrapper.App) != "null" {
		payload = wrapper.App
	}
	var meta struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
		Version     any    `json:"version"`
		WorkspaceID string `json:"workspaceId"`
		ProjectID   string `json:"projectId"`
		AppID       string `json:"appId"`
		SchemaAlias string `json:"schemaAlias"`
		Changelog   string `json:"changelog"`
	}
	if err := json.Unmarshal(payload, &meta); err != nil {
		return projectAppMeta{}, err
	}
	return projectAppMeta{
		Name:        strings.TrimSpace(meta.Name),
		DisplayName: strings.TrimSpace(meta.DisplayName),
		Description: strings.TrimSpace(meta.Description),
		Version:     strings.TrimSpace(versionText(meta.Version)),
		WorkspaceID: strings.TrimSpace(meta.WorkspaceID),
		ProjectID:   strings.TrimSpace(meta.ProjectID),
		AppID:       strings.TrimSpace(meta.AppID),
		SchemaAlias: strings.TrimSpace(meta.SchemaAlias),
		Changelog:   strings.TrimSpace(meta.Changelog),
	}, nil
}

const (
	// maxProjectAppNameRunes 应用名称长度上限（按 rune 计，中英文同权）
	maxProjectAppNameRunes = 64
	// maxProjectAppDescRunes 应用描述长度上限（按 rune 计）
	maxProjectAppDescRunes = 200
)

// validateProjectAppMetaLength 服务端统一校验应用名称/描述长度（rune 计），超限返回 400 可读错误。
// 在导入/覆盖入口调用，覆盖静态与 artifact 两条路径（含用户指定名与包内 meta 名）。
func validateProjectAppMetaLength(name, description string) error {
	if utf8.RuneCountInString(name) > maxProjectAppNameRunes {
		return domainproject.ErrAppNameTooLong
	}
	if utf8.RuneCountInString(description) > maxProjectAppDescRunes {
		return domainproject.ErrAppDescTooLong
	}
	return nil
}

// resolveImportAppDescription 计算导入生效描述：用户提交值（trim 后非空）优先，空则回退包内 meta 描述。
// 与 appName 的覆盖规则一致：UI 弹窗预填包内值、用户可改，后端兜底链面向 API 直调场景。
func resolveImportAppDescription(reqDescription, metaDescription string) string {
	if d := strings.TrimSpace(reqDescription); d != "" {
		return d
	}
	return metaDescription
}

// matchPackageAssetFile 判断 zip 条目名是否为指定的包根资源文件（如 icon.png、cover.svg）。
// 命中时返回原始文件名（含扩展名），用于 asset 的 originalName 与 ContentType 推断。
func matchPackageAssetFile(base, prefix string) (string, bool) {
	name, ext, found := strings.Cut(base, ".")
	if !found || name != prefix {
		return "", false
	}
	switch ext {
	case "png", "jpg", "jpeg", "svg", "webp":
		return base, true
	}
	return "", false
}

var packageAssetExtensions = []string{"png", "jpg", "jpeg", "svg", "webp"}

// packageAssetRelativeCandidates 返回应用包媒体的相对路径优先级。
// public/ 是新版运行时资源位置，包根的同类命名作为旧包兼容回退。
func packageAssetRelativeCandidates(prefix string) []string {
	publicPrefixes := []string{prefix}
	rootPrefixes := []string{prefix}
	switch prefix {
	case "icon":
		publicPrefixes = []string{"app-icon", "app_icon", "icon"}
		rootPrefixes = []string{"icon", "app-icon", "app_icon"}
	case "cover":
		publicPrefixes = []string{"app-cover", "app_cover", "app-cover-image", "app_cover_image", "cover"}
		rootPrefixes = []string{"cover", "app-cover", "app_cover", "app-cover-image", "app_cover_image"}
	}

	paths := make([]string, 0, (len(publicPrefixes)+len(rootPrefixes))*len(packageAssetExtensions))
	for _, publicPrefix := range publicPrefixes {
		for _, ext := range packageAssetExtensions {
			paths = append(paths, filepath.ToSlash(filepath.Join("public", publicPrefix+"."+ext)))
		}
	}
	for _, rootPrefix := range rootPrefixes {
		for _, ext := range packageAssetExtensions {
			paths = append(paths, rootPrefix+"."+ext)
		}
	}
	return paths
}

func packageAssetCandidatePaths(pkgRootDir, prefix string) []string {
	relativePaths := packageAssetRelativeCandidates(prefix)
	paths := make([]string, 0, len(relativePaths))
	for _, relativePath := range relativePaths {
		paths = append(paths, normalizePackageEntryPath(filepath.Join(pkgRootDir, filepath.FromSlash(relativePath))))
	}
	return paths
}

func normalizePackageEntryPath(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.ToSlash(filepath.Clean(name)), "./"))
}

func matchPackageAssetCandidateFile(base string) (string, bool) {
	for _, prefix := range []string{"icon", "cover", "app-icon", "app_icon", "app-cover", "app_cover", "app-cover-image", "app_cover_image"} {
		if name, ok := matchPackageAssetFile(base, prefix); ok {
			return name, true
		}
	}
	return "", false
}

// packageAssetContentType 根据包内资源文件名推断 asset 的 ContentType。
func packageAssetContentType(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".svg":
		return "image/svg+xml"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// applyPackageMetaToManifest 把 meta.json 解析结果写入 manifest patch，仅覆盖非空字段（缺失保留旧值）。
// recordSourceIdentity 为 true 时记录来源三 ID（import-as-new）；覆盖导入时来源身份保持不变，传 false。
func applyPackageMetaToManifest(patch map[string]any, meta projectAppMeta, recordSourceIdentity bool) {
	if meta.Description != "" {
		patch["description"] = meta.Description
	}
	if meta.Version != "" {
		patch["version"] = meta.Version
	}
	if meta.SchemaAlias != "" {
		patch["schemaAlias"] = meta.SchemaAlias
	}
	if meta.Changelog != "" {
		patch["changelog"] = meta.Changelog
	}
	if recordSourceIdentity {
		if meta.WorkspaceID != "" {
			patch["sourceWorkspaceId"] = meta.WorkspaceID
		}
		if meta.ProjectID != "" {
			patch["sourceProjectId"] = meta.ProjectID
		}
		if meta.AppID != "" {
			patch["sourceAppId"] = meta.AppID
		}
	}
}

// validateReplaceIdentity 校验覆盖导入（Import to replace）的包身份与目标应用记录的来源身份是否一致。
// 身份只按 appId 判定：目标应用未记录 sourceAppId 或包内 meta.json 缺 appId 时无法校验，直接拒绝；
// workspaceId/projectId 仅作为来源信息记录（Cloud 来源），不参与校验。
func validateReplaceIdentity(targetManifest map[string]any, meta projectAppMeta) error {
	recordedAppID := manifestStringValue(targetManifest, "sourceAppId")
	if recordedAppID == "" || meta.AppID == "" {
		return domainproject.ErrAppIdentityUnverifiable
	}
	if meta.AppID != recordedAppID {
		return domainproject.ErrAppIdentityNotMatch
	}
	return nil
}

// manifestStringValue 从 manifest 中读取字符串键值；键缺失、为 null 或非字符串时返回空串
func manifestStringValue(manifest map[string]any, key string) string {
	value, ok := manifest[key]
	if !ok || value == nil {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func versionText(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return strings.TrimSpace(strings.Trim(jsonString(v), `"`))
	}
}

func jsonString(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func jsonScalar(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return strings.Trim(strings.TrimSpace(jsonString(v)), `"`)
	}
}

func shouldIgnoreProjectZipEntry(name string) bool {
	for _, part := range strings.Split(filepath.Clean(filepath.FromSlash(name)), string(filepath.Separator)) {
		if strings.EqualFold(part, "__MACOSX") || strings.EqualFold(part, ".DS_Store") || strings.HasPrefix(part, "._") {
			return true
		}
	}
	return false
}

// importCreatedResources 记录一次包导入实际新建（INSERT）的资源 ID 集合，
// 供失败补偿逆序清理。被顶层 id 命中而更新的既有 Flow、被跳过或覆盖更新的既有 UNS 节点
// 不在集合内，绝不在失败补偿中删除。
type importCreatedResources struct {
	UnsNodeIDs []int64 // 本次新建的 UNS 节点（不含项目根），按父先子后的创建顺序
	FlowIDs    []int64 // 本次新建的 Flow
	AssetIDs   []int64 // 本次上传的 icon/cover asset
}

// merge 把另一批新建资源并入当前集合，保持创建顺序
func (c *importCreatedResources) merge(other importCreatedResources) {
	for _, id := range other.UnsNodeIDs {
		c.UnsNodeIDs = appendUniqueInt64(c.UnsNodeIDs, id)
	}
	for _, id := range other.FlowIDs {
		c.FlowIDs = appendUniqueInt64(c.FlowIDs, id)
	}
	for _, id := range other.AssetIDs {
		c.AssetIDs = appendUniqueInt64(c.AssetIDs, id)
	}
}

// newImportFlowRootEnsurer 把按 flowType 获取/创建 Flow 根目录包装成带缓存的延迟函数：
// 同一 flowType 只 ensure 一次；ensure 报错透传且不缓存（下次调用重试）。
func newImportFlowRootEnsurer(ensure func(flowType int) (repo.Flow, error)) func(flowType int) (repo.Flow, error) {
	roots := make(map[int]repo.Flow)
	return func(flowType int) (repo.Flow, error) {
		if root, ok := roots[flowType]; ok {
			return root, nil
		}
		root, err := ensure(flowType)
		if err != nil {
			return repo.Flow{}, err
		}
		roots[flowType] = root
		return root, nil
	}
}

// importPackageResources 导入包内附加资源（UNS/Flow/角色）。
// overwrite=true 时（覆盖导入 Replace 路径）UNS 已存在节点按包内值覆盖更新；
// overwrite=false 时（新 App 导入路径）维持"存在即跳过"。
// 返回的 created 为本次实际新建的资源 ID 集合（失败补偿用）；中途失败时返回已新建部分。
func (l *ProjectAppLogic) importPackageResources(projectID, appID int64, pkg projectAppPackage, overwrite bool) (patch map[string]any, created importCreatedResources, err error) {
	userID := logicx.UserID(l.ctx)
	// UNS 落全局根，只需 source/event flow 根；不创建项目同名 UNS 根目录。
	// Flow 根延迟创建（EIMP-070）：全部 Flow 命中既有记录走更新时，不产生空的 Flow 根目录副作用
	ensureRoot := newImportFlowRootEnsurer(func(flowType int) (repo.Flow, error) {
		return l.svcCtx.App.Project.EnsureFlowRoot(l.ctx, projectID, flowType, userID)
	})
	unsNodeIDs, skippedUns, createdUnsIDs, err := importPackageUns(l.ctx, l.svcCtx, projectID, appID, repo.UnsNode{}, pkg.Uns.Namespace, []string{pkg.Meta.Name, pkg.Meta.DisplayName}, userID, overwrite)
	created.UnsNodeIDs = createdUnsIDs
	if err != nil {
		return nil, created, err
	}
	roleIDs, err := l.importPackageRoles(projectID, pkg.Roles)
	if err != nil {
		return nil, created, err
	}
	if err := l.bindImportedRoles(projectID, appID, roleIDs); err != nil {
		return nil, created, err
	}
	// 应用角色绑定新增，清空网关角色缓存，避免开放 App 旧缓存继续放行
	appgateway.InvalidateRoleCache()
	flowIDs, createdFlowIDs, renamedFlows, err := l.importPackageFlow(projectID, appID, ensureRoot, pkg, unsNodeIDs, userID)
	created.FlowIDs = createdFlowIDs
	if err != nil {
		return nil, created, err
	}
	// created 集合与全量引用一起写入 manifest：删 App 时优先按 created 集合清理（只删本 App 新建的，
	// 命中匹配的既有 Flow/UNS 不误删）；Replace 场景按交集规则保留仍被本次引用的旧 created 归属。
	prevCreatedFlowIDs, prevCreatedUnsNodeIDs := appCreatedResourceIDs(l.ctx, projectID, appID)
	patch = map[string]any{
		"unsNodeIds":           unsNodeIDs,
		"createdUnsNodeIds":    mergeCreatedResourceIDs(prevCreatedUnsNodeIDs, unsNodeIDs, createdUnsIDs),
		"skippedUnsNamespaces": skippedUns,
		"roleIds":              roleIDs,
		"flowIds":              flowIDs,
		"createdFlowIds":       mergeCreatedResourceIDs(prevCreatedFlowIDs, flowIDs, createdFlowIDs),
		"renamedFlows":         renamedFlows,
		"importSummary":        buildImportSummary(pkg, unsNodeIDs, skippedUns, flowIDs, renamedFlows),
	}
	if err := l.svcCtx.App.Project.PatchAppManifest(l.ctx, projectID, appID, patch, userID); err != nil {
		return nil, created, err
	}
	return patch, created, nil
}

// appCreatedResourceIDs 读取应用 manifest 已记录的 createdFlowIds/createdUnsNodeIds（上次导入的
// created 集合），供 Replace 覆盖导入按交集规则合并归属。读取失败或旧 manifest 无 created 字段
// 时返回空集合（按本次新建为准）。
func appCreatedResourceIDs(ctx context.Context, projectID, appID int64) ([]int64, []int64) {
	app, err := repo.NewProjectRepo(ctx).GetProjectApp(ctx, projectID, appID)
	if err != nil {
		return nil, nil
	}
	manifest := map[string]any{}
	if len(app.ManifestJSON) > 0 {
		_ = json.Unmarshal(app.ManifestJSON, &manifest)
	}
	return jsonInt64Slice(manifest["createdFlowIds"]), jsonInt64Slice(manifest["createdUnsNodeIds"])
}

// mergeCreatedResourceIDs 计算本次导入后 manifest 应记录的 created 资源 ID 集合：
// created_new = (created_old ∩ 本次全量引用) ∪ 本次新建。
// 旧的 created 若仍被本次全量引用（命中更新/继续引用）则保留归属；本次不再引用或由其他来源
// 命中（未出现在本次新建）的既有资源不会混入。纯函数，便于单测。
func mergeCreatedResourceIDs(previousCreated, currentAll, newCreated []int64) []int64 {
	inCurrent := make(map[int64]struct{}, len(currentAll))
	for _, id := range currentAll {
		if id > 0 {
			inCurrent[id] = struct{}{}
		}
	}
	seen := make(map[int64]struct{}, len(previousCreated)+len(newCreated))
	out := make([]int64, 0, len(previousCreated)+len(newCreated))
	add := func(id int64) {
		if id <= 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, id := range previousCreated {
		if _, ok := inCurrent[id]; ok {
			add(id)
		}
	}
	for _, id := range newCreated {
		add(id)
	}
	return out
}

// jsonInt64Slice 把 manifest 中的数字/数字数组值归一化为 []int64；缺失或非数组返回 nil。
func jsonInt64Slice(value any) []int64 {
	switch v := value.(type) {
	case []int64:
		return v
	case []any:
		out := make([]int64, 0, len(v))
		for _, item := range v {
			if n := int64Value(item); n > 0 {
				out = append(out, n)
			}
		}
		return out
	default:
		return nil
	}
}

// importPackageAssets 把包根目录的 icon.* / cover.* 上传为 asset：
// Icon 写入 project_app.icon_asset_id 列，Cover 写入 manifest 的 coverAssetId。
// 文件缺失时跳过；返回需要合并进 manifest 的 patch 与本次上传的 asset ID（失败补偿用）。
func (l *ProjectAppLogic) importPackageAssets(projectID, appID int64, pkg projectAppPackage, userID int64) (map[string]any, []int64, error) {
	patch := map[string]any{}
	var assetIDs []int64
	if len(pkg.IconRaw) > 0 {
		iconAssetID, err := l.uploadPackageAsset(pkg.IconName, pkg.IconRaw, userID)
		if err != nil {
			return nil, assetIDs, fmt.Errorf("upload icon asset: %w", err)
		}
		assetIDs = append(assetIDs, iconAssetID)
		if err := repo.NewProjectRepo(l.ctx).UpdateProjectAppIconAsset(l.ctx, projectID, appID, iconAssetID, userID); err != nil {
			return nil, assetIDs, fmt.Errorf("update app icon asset: %w", err)
		}
	}
	if len(pkg.CoverRaw) > 0 {
		coverAssetID, err := l.uploadPackageAsset(pkg.CoverName, pkg.CoverRaw, userID)
		if err != nil {
			return nil, assetIDs, fmt.Errorf("upload cover asset: %w", err)
		}
		assetIDs = append(assetIDs, coverAssetID)
		patch["coverAssetId"] = coverAssetID
	}
	return patch, assetIDs, nil
}

// maxPackageMediaSize 包内单个媒体文件（icon/cover）的大小上限（20MiB）。
const maxPackageMediaSize int64 = 20 << 20

// validatePackageMedia 按扩展名校验包内媒体文件：
// 单文件超过 20MiB 直接拒绝；png/jpeg 走 image.DecodeConfig 解码校验、webp 走魔数比对
// （http.DetectContentType）、svg 走文本内容校验；伪造内容返回可读错误，不静默接受。
func validatePackageMedia(name string, raw []byte) error {
	if int64(len(raw)) > maxPackageMediaSize {
		return fmt.Errorf("media asset %s exceeds the 20MiB limit", name)
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg":
		// image.DecodeConfig 按魔数嗅探格式，需同时校验解码出的格式与扩展名一致，
		// 防止 jpg 内容伪装成 png（反之亦然）绕过校验
		_, format, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			return fmt.Errorf("media asset %s is not a valid image: %v", name, err)
		}
		expected := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
		if expected == "jpg" {
			expected = "jpeg"
		}
		if format != expected {
			return fmt.Errorf("media asset %s content type %q does not match extension %q", name, format, expected)
		}
	case ".webp":
		if got := http.DetectContentType(raw); got != "image/webp" {
			return fmt.Errorf("media asset %s is not a valid webp image: detected content type %q", name, got)
		}
	case ".svg":
		if !isSVGContent(raw) {
			return fmt.Errorf("media asset %s is not a valid svg file", name)
		}
	}
	return nil
}

// isSVGContent 对 svg 做基本文本校验：拒绝含 NUL 的二进制内容，并要求内容包含 <svg 标签。
func isSVGContent(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	head := raw
	if len(head) > 4096 {
		head = head[:4096]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return false
	}
	return strings.Contains(strings.ToLower(string(head)), "<svg")
}

// uploadPackageAsset 把包内单个文件写入 asset 存储，返回 asset ID。
// 写入前做媒体内容校验（大小上限 + 魔数/解码/文本校验），伪造内容拒绝导入。
func (l *ProjectAppLogic) uploadPackageAsset(name string, raw []byte, userID int64) (int64, error) {
	if err := validatePackageMedia(name, raw); err != nil {
		return 0, err
	}
	resp, err := l.svcCtx.App.Asset.UploadContent(l.ctx, domainasset.UploadContentCommand{
		FileName:    name,
		ContentType: packageAssetContentType(name),
		Size:        int64(len(raw)),
		Reader:      bytes.NewReader(raw),
		UserID:      userID,
	})
	if err != nil {
		return 0, err
	}
	assetID := int64Value(resp["id"])
	if assetID == 0 {
		return 0, domainproject.ErrInvalid
	}
	return assetID, nil
}

func (l *ProjectAppLogic) bindImportedRoles(projectID, appID int64, roleIDs []int64) error {
	if len(roleIDs) == 0 || appID == 0 {
		return nil
	}
	appRepo := repo.NewProjectRepo(l.ctx)
	for _, roleID := range roleIDs {
		if err := appRepo.InsertProjectRoleApps(l.ctx, projectID, roleID, []int64{appID}); err != nil {
			return err
		}
	}
	return nil
}

func (l *ProjectAppLogic) importPackageRoles(projectID int64, roles []projectPackageRole) ([]int64, error) {
	var ids []int64
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
		ids = appendUniqueInt64(ids, item.ID)
	}
	return ids, nil
}

// importProjectAppUns 从应用包 uns.json 原始字节中导入 UNS 节点，
// 返回导入节点 ID 列表、被跳过的 namespace 列表、本次新建节点 ID 列表及错误。
// 仅用于新 App 导入路径，已存在节点维持"存在即跳过"。
// 中途失败时也返回已新建节点 ID，供失败补偿清理。
func importProjectAppUns(ctx context.Context, svcCtx *svc.ServiceContext, projectID, appID int64, unsRaw []byte, appName string, userID int64) ([]int64, []string, []int64, error) {
	if len(unsRaw) == 0 {
		return nil, nil, nil, nil
	}
	var uns projectUnsPackage
	if err := json.Unmarshal(unsRaw, &uns); err != nil {
		return nil, nil, nil, fmt.Errorf("invalid uns.json: %w", err)
	}
	return importPackageUns(ctx, svcCtx, projectID, appID, repo.UnsNode{}, uns.Namespace, []string{appName}, userID, false)
}

// importPackageUns 导入包内 UNS 节点树：root 为零值时落 UNS 全局根（App 导入统一行为，
// 与平台 UNS 导入一致，保留包内层级、不再挂项目同名根目录）；root 非零时挂到该根节点下。
// 返回涉及节点 ID（含非零根）、被跳过 namespace、本次实际新建节点 ID（父先子后）及错误；
// 中途失败时新建列表保留已建部分。
func importPackageUns(ctx context.Context, svcCtx *svc.ServiceContext, projectID, appID int64, root repo.UnsNode, nodes []projectUnsNode, appNames []string, userID int64, overwrite bool) ([]int64, []string, []int64, error) {
	globalRoot := root.ID == 0
	if globalRoot != (strings.TrimSpace(root.Namespace) == "") {
		return nil, nil, nil, domainproject.ErrInvalid
	}
	flattenSet := map[string]struct{}{}
	if name := strings.TrimSpace(root.Name); name != "" {
		flattenSet[strings.ToLower(name)] = struct{}{}
	}
	for _, name := range appNames {
		if name = strings.TrimSpace(name); name != "" {
			flattenSet[strings.ToLower(name)] = struct{}{}
		}
	}
	nodes = flattenTopLevelUnsNodes(nodes, flattenSet)
	var ids []int64
	if !globalRoot {
		ids = append(ids, root.ID)
	}
	var skipped []string
	var createdIDs []int64
	for _, node := range nodes {
		nextIDs, nextSkipped, nextCreated, err := importUnsNode(ctx, svcCtx, projectID, appID, root.ID, root.Namespace, "", root.Name, node, userID, overwrite)
		createdIDs = append(createdIDs, nextCreated...)
		if err != nil {
			return nil, nil, createdIDs, err
		}
		for _, id := range nextIDs {
			ids = appendUniqueInt64(ids, id)
		}
		skipped = append(skipped, nextSkipped...)
	}
	return ids, skipped, createdIDs, nil
}

// flattenTopLevelUnsNodes 将顶层与项目根或应用名同名的 folder 节点展开，避免导入后多建一层文件夹。
func flattenTopLevelUnsNodes(nodes []projectUnsNode, flattenSet map[string]struct{}) []projectUnsNode {
	var out []projectUnsNode
	for _, node := range nodes {
		nodeType := "folder"
		if strings.EqualFold(strings.TrimSpace(node.Type), "TOPIC") {
			nodeType = "file"
		}
		name := strings.TrimSpace(node.Name)
		if nodeType == "folder" && name != "" {
			if _, ok := flattenSet[strings.ToLower(name)]; ok {
				out = append(out, node.Children...)
				continue
			}
		}
		out = append(out, node)
	}
	return out
}

func importUnsNode(ctx context.Context, svcCtx *svc.ServiceContext, projectID, appID, parentID int64, parentPath, inheritedTopic, projectRootName string, node projectUnsNode, userID int64, overwrite bool) ([]int64, []string, []int64, error) {
	name := strings.TrimSpace(node.Name)
	if name == "" {
		return nil, nil, nil, nil
	}
	topicType := normalizePackageTopicType(node.TopicType)
	if topicType == "" {
		topicType = inheritedTopic
	}
	nodeType := "folder"
	if strings.EqualFold(strings.TrimSpace(node.Type), "TOPIC") {
		nodeType = "file"
	}
	parentPath = strings.Trim(parentPath, "/")
	if parentPath != "" && strings.EqualFold(name, projectRootName) && nodeType == "folder" && strings.EqualFold(parentPath, projectRootName) {
		ids := []int64{parentID}
		var skipped []string
		var createdIDs []int64
		for _, child := range node.Children {
			nextIDs, nextSkipped, nextCreated, err := importUnsNode(ctx, svcCtx, projectID, appID, parentID, parentPath, topicType, projectRootName, child, userID, overwrite)
			createdIDs = append(createdIDs, nextCreated...)
			if err != nil {
				return nil, nil, createdIDs, err
			}
			for _, id := range nextIDs {
				ids = appendUniqueInt64(ids, id)
			}
			skipped = append(skipped, nextSkipped...)
		}
		return ids, skipped, createdIDs, nil
	}
	path := strings.Trim(parentPath+"/"+name, "/")
	if nodeType == "file" && topicType == "" {
		return nil, nil, nil, fmt.Errorf("UNS file node %q missing topicType", path)
	}
	var enableHistory *bool
	if nodeType == "file" {
		var err error
		enableHistory, err = parseProjectUnsEnableHistory(node.EnableHistory)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("UNS file node %q has invalid enableHistory: %w", path, err)
		}
	}
	extend := map[string]any{}
	for key, value := range node.ExtendProperties {
		extend[key] = value
	}
	extend["projectId"] = projectID
	extend["projectAppId"] = appID
	schema := node.Fields
	// TOPIC 节点创建时领域层会自动补挂一层 topicType 目录（如 .../Metric/<topic>），
	// 幂等查找必须包含该层级，否则重复导入会误判节点不存在并尝试重建，触发命名冲突。
	lookupNamespace := path
	if nodeType == "file" {
		effectiveTopicType := topicType
		if effectiveTopicType == "" {
			// 与领域层 ensureTopicTypeParentForFile 的默认 topicType=1(State) 对齐
			effectiveTopicType = "State"
		}
		parentSeg := parentPath
		if idx := strings.LastIndex(parentPath, "/"); idx >= 0 {
			parentSeg = parentPath[idx+1:]
		}
		if !strings.EqualFold(parentSeg, effectiveTopicType) {
			lookupNamespace = strings.Trim(parentPath+"/"+effectiveTopicType+"/"+name, "/")
		}
	}
	item, skippedNamespace, created, err := ensureImportedUnsNode(ctx, svcCtx, parentID, lookupNamespace, name, nodeType, topicType, node.Description, schema, extend, enableHistory, userID, overwrite)
	if err != nil {
		return nil, nil, nil, err
	}
	var skipped []string
	if skippedNamespace != "" {
		skipped = append(skipped, skippedNamespace)
	}
	// namespace 冲突被跳过的节点不计入 imported，避免计数虚高
	var ids []int64
	if skippedNamespace == "" {
		ids = append(ids, item.ID)
	}
	var createdIDs []int64
	if created {
		createdIDs = append(createdIDs, item.ID)
	}
	for _, child := range node.Children {
		nextTopic := topicType
		nextIDs, nextSkipped, nextCreated, err := importUnsNode(ctx, svcCtx, projectID, appID, item.ID, path, nextTopic, projectRootName, child, userID, overwrite)
		createdIDs = append(createdIDs, nextCreated...)
		if err != nil {
			return nil, nil, createdIDs, err
		}
		for _, id := range nextIDs {
			ids = appendUniqueInt64(ids, id)
		}
		skipped = append(skipped, nextSkipped...)
	}
	return ids, skipped, createdIDs, nil
}

// ensureImportedUnsNode 确保 UNS 节点存在；若 namespace 已存在：
// overwrite=false 时跳过并返回被跳过的 namespace；overwrite=true 时按字段级规则覆盖更新，
// 返回更新后的节点且不记入 skipped，让子节点继续递归。
// created=true 仅当本次实际新建了节点（失败补偿只清理这类节点）。
func ensureImportedUnsNode(ctx context.Context, svcCtx *svc.ServiceContext, parentID int64, namespace, name, nodeType, topicType, description string, schema []map[string]any, extend map[string]any, enableHistory *bool, userID int64, overwrite bool) (repo.UnsNode, string, bool, error) {
	unsRepo := repo.NewUnsRepo(ctx)
	existing, err := unsRepo.GetUnsNodeByNamespace(ctx, namespace)
	if err == nil {
		if overwrite {
			updated, err := overwriteUnsNode(ctx, svcCtx, existing, description, schema, extend, enableHistory, userID)
			if err != nil {
				return repo.UnsNode{}, "", false, err
			}
			return updated, "", false, nil
		}
		return existing, namespace, false, nil
	}
	if !errors.Is(err, repo.ErrNotFound) {
		return repo.UnsNode{}, "", false, err
	}
	schemaRaw, _ := json.Marshal(schema)
	extendRaw, _ := json.Marshal(extend)
	resp, err := svcCtx.App.UNS.Create(ctx, domainuns.SaveCommand{
		ParentID:         parentID,
		Name:             name,
		DisplayName:      name,
		Description:      description,
		NodeType:         nodeType,
		TopicType:        topicType,
		Schema:           string(schemaRaw),
		ExtendProperties: string(extendRaw),
		EnableHistory:    enableHistory,
		UserID:           userID,
	})
	if err != nil {
		return repo.UnsNode{}, "", false, err
	}
	id := int64Value(resp["id"])
	if id == 0 {
		return repo.UnsNode{}, "", false, domainproject.ErrInvalid
	}
	created, err := unsRepo.GetUnsNode(ctx, id, false)
	return created, "", true, err
}

// overwriteUnsNode 覆盖更新已存在的 UNS 节点，字段级规则（2026-08-06 拍板，EIMP-034/050）：
//   - type/topicType：保留原值，不覆盖（包内值不参与）
//   - description/extendProperties：整体覆盖（含空值清空）
//   - schema（包内 fields）：按字段 name 增量合并——同名替换（保持目标原位置）、新增追加、
//     目标有而包内没有的字段保留；包内 fields 整体为空时保留目标 schema
//   - children/父子结构：不动，由外层递归处理，目标端既有子节点不删除
//
// id/namespace/名称保持不变；enableHistory 仅在包内显式携带时覆盖，旧包缺失时沿用节点现有配置；
// mock_data 始终沿用节点现有配置，避免被默认值覆盖。
func overwriteUnsNode(ctx context.Context, svcCtx *svc.ServiceContext, existing repo.UnsNode, description string, packageFields []map[string]any, extend map[string]any, packageEnableHistory *bool, userID int64) (repo.UnsNode, error) {
	schemaRaw, _ := json.Marshal(mergeUnsSchemaFields(existing.Schema, packageFields))
	extendRaw, _ := json.Marshal(extend)
	enableHistory := existing.EnableHistory == 1
	if packageEnableHistory != nil {
		enableHistory = *packageEnableHistory
	}
	if _, err := svcCtx.App.UNS.Update(ctx, domainuns.SaveCommand{
		ID:               existing.ID,
		ParentID:         existing.ParentID,
		Name:             existing.Name,
		DisplayName:      existing.DisplayName,
		Description:      description,
		NodeType:         unsImportNodeTypeName(existing.Type),
		TopicType:        unsImportTopicTypeName(existing.TopicType),
		Schema:           string(schemaRaw),
		ExtendProperties: string(extendRaw),
		EnableHistory:    &enableHistory,
		MockData:         existing.MockData == 1,
		UserID:           userID,
	}); err != nil {
		return repo.UnsNode{}, err
	}
	return repo.NewUnsRepo(ctx).GetUnsNode(ctx, existing.ID, false)
}

// parseProjectUnsEnableHistory 解析云端 UNS 导出协议中的可选 enableHistory。
// 当前云端输出 "TRUE"/"FALSE"；同时兼容直接 JSON 布尔值及历史 1/2 表示法。
// nil 表示旧 App 包未携带该字段，Replace 时必须保留目标节点现有值。
func parseProjectUnsEnableHistory(raw json.RawMessage) (*bool, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}

	var value any
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}

	result := false
	switch typed := value.(type) {
	case bool:
		result = typed
	case json.Number:
		number, err := strconv.ParseFloat(typed.String(), 64)
		if err != nil {
			return nil, err
		}
		switch number {
		case 1:
			result = true
		case 0, 2:
			result = false
		default:
			return nil, fmt.Errorf("unsupported numeric value %s", typed.String())
		}
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "1", "true", "yes", "y", "enable", "enabled", "是":
			result = true
		case "0", "2", "false", "no", "n", "disable", "disabled", "否":
			result = false
		default:
			return nil, fmt.Errorf("unsupported string value %q", typed)
		}
	default:
		return nil, fmt.Errorf("unsupported JSON value %s", string(trimmed))
	}
	return &result, nil
}

// unsImportNodeTypeName 将 UNS 节点 DB type 映射回领域层 SaveCommand 的 nodeType 字符串
// （与 domain/uns 的 nodeTypeName 规则一致：2=file，其余=folder）。
func unsImportNodeTypeName(value int16) string {
	if value == 2 {
		return "file"
	}
	return "folder"
}

// unsImportTopicTypeName 将 UNS 节点 DB topic_type 映射回领域层 SaveCommand 的 topicType 字符串
// （与 domain/uns 的 topicTypeName 规则一致：1=State，2=Action，3=Metric）。
func unsImportTopicTypeName(value int16) string {
	switch value {
	case 1:
		return "State"
	case 2:
		return "Action"
	case 3:
		return "Metric"
	default:
		return ""
	}
}

// mergeUnsSchemaFields 按字段 name 增量合并 schema（2026-08-06 拍板，EIMP-034/050）：
// 包内同名字段替换（保持目标原位置）、包内新增字段追加、目标有而包内没有的字段保留。
// 包内 fields 为空时保留目标 schema（node.Fields 缺失与空数组不可区分，置空会误删）。
func mergeUnsSchemaFields(existingSchema json.RawMessage, packageFields []map[string]any) []map[string]any {
	if len(packageFields) == 0 {
		return unsSchemaFieldList(existingSchema)
	}
	byName := make(map[string]map[string]any, len(packageFields))
	order := make([]string, 0, len(packageFields))
	for _, field := range packageFields {
		name, _ := field["name"].(string)
		if name == "" {
			continue
		}
		if _, ok := byName[name]; !ok {
			order = append(order, name)
		}
		byName[name] = field
	}
	existingFields := unsSchemaFieldList(existingSchema)
	merged := make([]map[string]any, 0, len(existingFields)+len(order))
	used := make(map[string]struct{}, len(existingFields))
	for _, field := range existingFields {
		name, _ := field["name"].(string)
		replacement, ok := byName[name]
		if name == "" || !ok {
			merged = append(merged, field) // 目标有而包内无（或目标字段无 name）：保留
			continue
		}
		merged = append(merged, replacement)
		used[name] = struct{}{}
	}
	for _, name := range order {
		if _, ok := used[name]; ok {
			continue
		}
		merged = append(merged, byName[name])
	}
	return merged
}

// unsSchemaFieldList 从已存 schema JSON 中解析字段列表，兼容数组与 {"fields":[...]} 两种形态。
func unsSchemaFieldList(raw json.RawMessage) []map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	switch typed := value.(type) {
	case []any:
		return unsSchemaFieldMaps(typed)
	case map[string]any:
		if fields, ok := typed["fields"].([]any); ok {
			return unsSchemaFieldMaps(fields)
		}
	}
	return nil
}

func unsSchemaFieldMaps(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// importPackageFlow 导入包内 flow 文件，返回涉及 flow ID、本次新建 flow ID、重命名记录及错误。
// 匹配语义（BUG-014）：flow 文件顶层 id 是来源环境的平台数据库主键，为全局唯一身份键；
// flowID（tab 节点 id）是目标环境 Node-RED 运行时 ID，不作为导入匹配键，也不从导入文件直接覆盖。
//   - 顶层 id 命中既有 Flow（含跨 Project，EIMP-070）：flowName/description 按包内值覆盖
//     （名称与目标 folder 下其他 Flow 冲突时按 (n) 避让），flow_data 走 SaveFlowData 更新 JSON，
//     保留目标 DB id / flow_id / ParentID / FlowType，
//     保存前把包内 tab id 及 z 引用重绑回目标 flowID（包内 flowID 不落库、不报错）。
//   - 顶层 id 为空 / 未命中：新建 Flow，生成新 DB 主键与新 flowID（写回 tab 并重绑 z 引用），
//     即使包内 flowID 与既有 Flow 相同也不复用；Flow 根目录仅在此时延迟 ensure（EIMP-070）。
func (l *ProjectAppLogic) importPackageFlow(projectID, appID int64, ensureRoot func(flowType int) (repo.Flow, error), pkg projectAppPackage, unsNodeIDs []int64, userID int64) (flowIDs []int64, createdFlowIDs []int64, renamedFlows []map[string]string, err error) {
	mqttEndpoint, err := config.ParseMQTTEndpoint(l.svcCtx.Config.DataIngest.MqttBrokers)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("resolve imported Flow MQTT endpoint: %w", err)
	}
	flowRepo := repo.NewFlowRepo(l.ctx)
	// 按 flowType（即 parent root）分组避让名称，source flow 与 event flow 在不同根下允许同名
	usedNames := make(map[int]map[string]struct{})
	// 云端导出的 flows.json 是多 flow 包装（{"flows":[{id,flowName,flowData,...}]}），先拆成单条再逐条导入
	for _, file := range expandCloudFlowFiles(pkg.FlowFiles) {
		raw := strings.TrimSpace(string(file.Raw))
		if raw == "" {
			continue
		}
		baseName, identityID, _ := importedFlowIdentity(pkg.Meta, file.Raw)
		if file.IdentityID > 0 {
			// 云端条目：id 是来源云端 DB 主键（全局身份键），flowName 是真实 flow 名
			identityID = file.IdentityID
			if file.Name != "" {
				baseName = file.Name
			}
		}
		description := strings.TrimSpace(file.Description)
		if description == "" {
			description = pkg.Meta.Description
		}

		// 顶层 id 全局命中 → 覆盖更新目标 Flow：flow_data 走 SaveFlowData（版本/状态语义不变），
		// flowName/description 按包内值覆盖更新；其余身份字段（DB id / flow_id / ParentID / FlowType）保留
		existing, matched, err := matchFlowByIdentityID(flowRepo, l.ctx, identityID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("query flow by identity id %s: %w", file.Path, err)
		}
		if matched {
			// flowName 覆盖：命中 Flow 保持其原 ParentID，去重须在其实际所在 folder 内进行；
			// 与其他 Flow 重名时按 (n) 递增避让，自身当前名视为可用（名称未变时不触发重命名）
			exists := func(name string) bool {
				found, err := flowRepo.GetFlowByName(l.ctx, file.FlowType, existing.ParentID, name)
				if err != nil {
					return false
				}
				return found.ID != existing.ID
			}
			if usedNames[file.FlowType] == nil {
				usedNames[file.FlowType] = make(map[string]struct{})
			}
			finalName, err := makeFlowImportName(baseName, usedNames[file.FlowType], exists, flowImportNameMaxLen)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("allocate flow name for %s: %w", file.Path, err)
			}
			usedNames[file.FlowType][finalName] = struct{}{}
			if finalName != baseName {
				renamedFlows = append(renamedFlows, map[string]string{
					"originalName": baseName,
					"finalName":    finalName,
				})
			}
			if _, err := flowRepo.UpdateFlowNameDescription(l.ctx, existing.ID, finalName, description, userID); err != nil {
				return nil, nil, nil, fmt.Errorf("update flow name/description %s: %w", file.Path, err)
			}
			// 命中更新的 flow 复用其本地 MQTT 凭证；缺失时补建并回写 connect_client_id
			client, created, err := l.ensureImportedFlowConnectClient(existing.ConnectClientID, "Flow "+finalName, userID)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("ensure flow connect client %s: %w", file.Path, err)
			}
			if created {
				if err := flowRepo.UpdateFlowConnectClient(l.ctx, existing.ID, client.ID, userID); err != nil {
					return nil, nil, nil, fmt.Errorf("update flow connect client %s: %w", file.Path, err)
				}
			}
			// 本地化内部/SaaS mqtt-broker 节点并注入本地凭证
			data := string(rewriteImportedMQTTBrokerNodes([]byte(raw), client, mqttEndpoint))
			if targetFlowID := strings.TrimSpace(existing.RuntimeFlowID); targetFlowID != "" {
				withTab, err := ensureFlowTabNode([]byte(data), targetFlowID, finalName)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("inject flow tab node %s: %w", file.Path, err)
				}
				rebound, err := rebindFlowRuntimeID(withTab, targetFlowID)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("rebind flow runtime id %s: %w", file.Path, err)
				}
				data = string(rebound)
			}
			if _, err := flowRepo.SaveFlowData(l.ctx, existing.ID, data, userID); err != nil {
				return nil, nil, nil, fmt.Errorf("save flow data %s: %w", file.Path, err)
			}
			flowIDs = append(flowIDs, existing.ID)
			continue
		}

		// 未命中/空顶层 id → 新建：始终生成新 flowID 写回首个 tab 节点并重绑 z 引用，
		// 不复用包内 tab id（flowID 是目标端运行时 ID，不能作为身份来源）。
		// Flow 根目录延迟到确认新建时才 ensure，全部命中更新的导入不产生空根目录
		root, err := ensureRoot(file.FlowType)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("ensure flow root for type %d: %w", file.FlowType, err)
		}
		if root.ID == 0 {
			return nil, nil, nil, fmt.Errorf("missing flow root for type %d", file.FlowType)
		}
		generatedFlowID := randomHex(8)
		if usedNames[file.FlowType] == nil {
			usedNames[file.FlowType] = make(map[string]struct{})
		}
		finalName, err := l.allocateFlowName(flowRepo, root.ID, file.FlowType, baseName, usedNames[file.FlowType])
		if err != nil {
			return nil, nil, nil, fmt.Errorf("allocate flow name for %s: %w", file.Path, err)
		}
		usedNames[file.FlowType][finalName] = struct{}{}
		if finalName != baseName {
			renamedFlows = append(renamedFlows, map[string]string{
				"originalName": baseName,
				"finalName":    finalName,
			})
		}
		// 为导入 flow 创建本地 MQTT 凭证，并据此本地化内部/SaaS mqtt-broker 节点
		client, _, err := l.ensureImportedFlowConnectClient(0, "Flow "+finalName, userID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("create flow connect client %s: %w", file.Path, err)
		}
		data := string(rewriteImportedMQTTBrokerNodes([]byte(raw), client, mqttEndpoint))
		// 云端 flowData 无 tab 节点：先按最终名注入 tab 并重绑全部 z 引用，再走统一 rebind
		withTab, err := ensureFlowTabNode([]byte(data), generatedFlowID, finalName)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("inject flow tab node %s: %w", file.Path, err)
		}
		rebound, err := rebindFlowRuntimeID(withTab, generatedFlowID)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("assign flow runtime id %s: %w", file.Path, err)
		}
		raw = string(rebound)

		// 包内节点 id 属于来源环境，原样带入会与同运行时其它导入副本撞车（Node-RED 按 id
		// 注册节点、编辑器按 id 收状态，重复 id 互相覆盖导致状态错乱/连接互踢）。
		// 新建导入一律重生成全部节点 id 并重映射 wires/broker 等引用（tab 除外，
		// 其 id 即刚分配的 runtime flow id）；identity 命中的更新路径不动（同一 flow
		// 记录更新不产生新副本）。
		regenerated, err := regenerateImportedFlowNodeIDs([]byte(raw))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("regenerate imported flow node ids %s: %w", file.Path, err)
		}
		raw = string(regenerated)

		flow := repo.Flow{
			RuntimeFlowID:   generatedFlowID,
			ConnectClientID: client.ID,
			Name:            finalName,
			FlowData:        raw,
			Status:          "draft",
			Template:        "projectApp",
			Description:     description,
			FlowType:        file.FlowType,
			NodeType:        2,
			ParentID:        root.ID,
			UnsNodeIDs:      importedFlowUnsNodeIDs(file.FlowType, unsNodeIDs),
		}
		flow.CreatedBy = userID
		flow.UpdatedBy = userID
		// 包内顶层 identityID 非空且已确认不存在时，保留该 id 作为新 Flow 的 DB 主键
		// （ensureID 仅对 0 生成新 id），使同一来源（Cloud 侧 DB id）重复导入幂等命中更新语义；
		// flow_id（运行时 tab id）仍新生成，不参与身份匹配。
		flow.ID = flowCreateDBID(identityID)
		created, err := flowRepo.CreateFlow(l.ctx, flow)
		if err != nil && identityID > 0 && errors.Is(err, repo.ErrDuplicate) {
			// 并发窗口内该主键已被占用：放弃 identityID，回退为 ensureID 生成新 id 重试一次
			logx.WithContext(l.ctx).Infof("flow identity id %d already taken, fallback to generated id: source=%s", identityID, file.Path)
			flow.ID = 0
			created, err = flowRepo.CreateFlow(l.ctx, flow)
		}
		if err != nil {
			return nil, nil, nil, fmt.Errorf("create flow %s: %w", file.Path, err)
		}
		if _, err := flowRepo.SaveFlowData(l.ctx, created.ID, raw, userID); err != nil {
			return nil, nil, nil, fmt.Errorf("save flow data %s: %w", file.Path, err)
		}
		flowIDs = append(flowIDs, created.ID)
		createdFlowIDs = append(createdFlowIDs, created.ID)
	}
	return flowIDs, createdFlowIDs, renamedFlows, nil
}

func importedFlowUnsNodeIDs(flowType int, unsNodeIDs []int64) []int64 {
	if flowType != 1 {
		return nil
	}
	return unsNodeIDs
}

// ensureImportedFlowConnectClient 返回导入 flow 应使用的本地 MQTT 凭证：
// 已有 connectClientID 时直接复用；否则创建 Flow 类型凭证。
// 返回的 created 为 true 表示本次新建（调用方需要回写 connect_client_id）。
func (l *ProjectAppLogic) ensureImportedFlowConnectClient(connectClientID int64, description string, userID int64) (repo.ConnectClient, bool, error) {
	if connectClientID > 0 {
		client, err := repo.NewConnectClientRepo(l.ctx).GetConnectClient(l.ctx, connectClientID)
		if err == nil {
			return client, false, nil
		}
		if !errors.Is(err, repo.ErrNotFound) {
			return repo.ConnectClient{}, false, err
		}
		// 引用已失效则补建新凭证，由调用方回写新 id
	}
	req := repo.ConnectClient{
		Type:        repo.ConnectClientTypeFlow,
		Description: description,
	}
	req.CreatedBy = userID
	client, err := repo.NewConnectClientRepo(l.ctx).CreateConnectClient(l.ctx, req)
	if err != nil {
		return repo.ConnectClient{}, false, err
	}
	return client, true, nil
}

// flowImportPlan 描述单个 flow 文件的导入决策（纯函数结果，便于表驱动测试）。
type flowImportPlan struct {
	update        bool   // true=按顶层 id 命中更新既有 Flow；false=新建
	targetID      int64  // 更新目标的 DB 主键
	keepRuntimeID string // 更新时保留的目标 flowID（包内 JSON 需重绑回该值）
}

// planFlowImport 按 BUG-014 规则决策：仅当顶层 identityID 非空且全局命中非 folder 的既有 Flow 时
// 走更新（保留目标身份，跨 Project 命中同样更新原 Flow，不校验 ParentID）；
// 其余情况（空 id / 未命中 / 命中 folder）一律新建并生成新 flowID，flowID 不参与匹配。
func planFlowImport(identityID int64, existing repo.Flow, matched bool) flowImportPlan {
	if matched && identityID > 0 && existing.NodeType != 1 {
		return flowImportPlan{
			update:        true,
			targetID:      existing.ID,
			keepRuntimeID: existing.RuntimeFlowID,
		}
	}
	return flowImportPlan{}
}

// matchFlowByIdentityID 按顶层 id（平台数据库主键）全局查询既有 Flow，不限定 Project。
// identityID 为空或未命中时返回 matched=false；命中 folder（node_type=1）视为未命中。
func matchFlowByIdentityID(flowRepo *repo.FlowRepo, ctx context.Context, identityID int64) (repo.Flow, bool, error) {
	if identityID <= 0 {
		return repo.Flow{}, false, nil
	}
	existing, err := flowRepo.GetFlow(ctx, identityID)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return repo.Flow{}, false, nil
		}
		return repo.Flow{}, false, err
	}
	plan := planFlowImport(identityID, existing, true)
	if !plan.update {
		return repo.Flow{}, false, nil
	}
	return existing, true, nil
}

// flowCreateDBID 返回新建 Flow 应使用的 DB 主键：包内顶层 identityID 非空（且已确认不存在）时
// 保留该 id，使同一来源重复导入幂等命中更新语义；否则返回 0 交由 repo 的 ensureID 生成新 id。
// 纯函数，便于单测。
func flowCreateDBID(identityID int64) int64 {
	if identityID > 0 {
		return identityID
	}
	return 0
}

// allocateFlowName 在目标 Flow folder 中为 base 分配一个未被占用的名称。
// 若 base 已被目标 folder 或本次同包其他 flow 占用，则按 base(n) 递增重命名；
// 重命名后超过长度限制时，截短 base 以保留完整序号后缀。
func (l *ProjectAppLogic) allocateFlowName(flowRepo *repo.FlowRepo, parentID int64, flowType int, base string, usedNames map[string]struct{}) (string, error) {
	exists := func(name string) bool {
		_, err := flowRepo.GetFlowByName(l.ctx, flowType, parentID, name)
		return err == nil
	}
	return makeFlowImportName(base, usedNames, exists, flowImportNameMaxLen)
}

// makeFlowImportName 为 flow 生成不重复的名称：base 被占用时按 base(n) 递增，并在超长时截短原名称。
func makeFlowImportName(base string, used map[string]struct{}, exists func(string) bool, maxLen int) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "project-flow"
	}
	if !flowNameCandidateExists(base, used, exists) {
		return base, nil
	}
	for n := 1; ; n++ {
		suffix := fmt.Sprintf("(%d)", n)
		allowedBaseLen := maxLen - utf8.RuneCountInString(suffix)
		if allowedBaseLen < 1 {
			return "", fmt.Errorf("flow name suffix exceeds max length %d", maxLen)
		}
		candidate := truncateRunes(base, allowedBaseLen) + suffix
		if !flowNameCandidateExists(candidate, used, exists) {
			return candidate, nil
		}
	}
}

// flowNameCandidateExists 判断名称是否已被同包其他 flow 或 exists 回调判定为占用。
func flowNameCandidateExists(name string, used map[string]struct{}, exists func(string) bool) bool {
	if _, ok := used[name]; ok {
		return true
	}
	if exists != nil {
		return exists(name)
	}
	return false
}

// truncateRunes 按 rune 截断字符串，保证结果不超过 maxRunes 个字符。
func truncateRunes(value string, maxRunes int) string {
	if utf8.RuneCountInString(value) <= maxRunes {
		return value
	}
	count := 0
	for idx := range value {
		if count == maxRunes {
			return value[:idx]
		}
		count++
	}
	return value
}

// importedFlowIdentity 从 flow JSON 提取导入身份：显示名、顶层 id（平台数据库主键，全局身份键）、
// 首个 tab 节点 id（来源环境 Node-RED 运行时 ID，仅用于展示/判断，不作匹配键）。
// 裸数组形态没有顶层 id，identityID 返回 0（按新建处理）。
func importedFlowIdentity(meta projectAppMeta, raw []byte) (string, int64, string) {
	name := strings.TrimSpace(meta.DisplayName)
	if name == "" {
		name = strings.TrimSpace(meta.Name)
	}
	identityID := extractFlowIdentityID(raw)
	runtimeID := ""
	nodes := extractFlowNodes(raw)
	for _, node := range nodes {
		if strings.TrimSpace(jsonScalar(node["type"])) != "tab" {
			continue
		}
		if label := strings.TrimSpace(jsonScalar(node["label"])); label != "" {
			name = label
		}
		runtimeID = strings.TrimSpace(jsonScalar(node["id"]))
		break
	}
	if name == "" {
		name = "project-flow"
	}
	return name, identityID, runtimeID
}

// extractFlowIdentityID 读取包装形态 flow JSON（{"id":..., "nodes"/"flows":[...]}）的顶层 id，
// 即来源环境的平台数据库主键；裸 tab 数组或缺 id 时返回 0。
func extractFlowIdentityID(raw []byte) int64 {
	var wrapper struct {
		ID any `json:"id"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return 0
	}
	return int64Value(wrapper.ID)
}

func extractFlowNodes(raw []byte) []map[string]any {
	var nodes []map[string]any
	if err := json.Unmarshal(raw, &nodes); err == nil {
		return nodes
	}
	var wrapper struct {
		Nodes []map[string]any `json:"nodes"`
		Flows []map[string]any `json:"flows"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil {
		if len(wrapper.Nodes) > 0 {
			return wrapper.Nodes
		}
		return wrapper.Flows
	}
	return nil
}

// rebindFlowRuntimeID 把 flow JSON 中首个 type=="tab" 节点的 id 改写为 newID，
// 并把其他节点中引用旧 tab id 的 z 字段同步重绑；无 tab 节点时原样返回。
// 用于两条路径：新建 flow 时写入目标端新生成的 flowID；顶层 id 命中更新时把包内
// tab id 重绑回目标 flow 保留的 flowID（包内 flowID 不落库）。
func rebindFlowRuntimeID(raw []byte, newID string) ([]byte, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	nodes, ok := flowNodeList(doc)
	if !ok {
		return raw, nil
	}
	oldID := ""
	rebound := false
	for _, node := range nodes {
		if strings.TrimSpace(jsonScalar(node["type"])) != "tab" {
			continue
		}
		oldID = strings.TrimSpace(jsonScalar(node["id"]))
		node["id"] = newID
		rebound = true
		break
	}
	if !rebound {
		return raw, nil
	}
	if oldID != "" && oldID != newID {
		for _, node := range nodes {
			if strings.TrimSpace(jsonScalar(node["z"])) == oldID {
				node["z"] = newID
			}
		}
	}
	return json.Marshal(doc)
}

// localizableImportedMQTTBroker 判断导入 flow 中的 mqtt-broker 节点是否需要本地化。
// 仅识别内部 / SaaS / 本地回环 broker，避免误改客户自有外部 MQTT broker。
//   - 空 / emqx：本地内部 broker
//   - localhost / 127.0.0.1 / ::1：本地回环示例节点
//   - *.tier0.dev：SaaS 导出地址（如 mqtt.pre.tier0.dev）
func localizableImportedMQTTBroker(node map[string]any) bool {
	if domainflow.IsInternalMQTTBrokerNode(node) {
		return true
	}
	if strings.TrimSpace(jsonScalar(node["type"])) != "mqtt-broker" {
		return false
	}
	broker := nodeStringField(node, "broker")
	if broker == "" {
		broker = nodeStringField(node, "host")
	}
	broker = strings.ToLower(strings.TrimSpace(broker))
	switch broker {
	case "", "emqx", "localhost", "127.0.0.1", "::1":
		return true
	}
	return strings.HasSuffix(broker, ".tier0.dev")
}

// nodeStringField 读取节点中的字符串字段；缺失或非字符串时返回空串，避免把 nil 误判为 "null"。
func nodeStringField(node map[string]any, key string) string {
	v, ok := node[key].(string)
	if !ok {
		return ""
	}
	return v
}

// rewriteImportedMQTTBrokerNodes 通过 JSON 解析把导入 flow 中「需要本地化」的
// mqtt-broker 节点重写为本地 EMQX：地址（broker/port/tls）+ 本地 MQTT 凭证
// （clientid / credentials），并把 flow 内所有可本地化 mqtt-broker 节点统一收敛为
// 单个 canonical 节点：canonical 生成唯一新 id，其余命中节点删掉，仅重绑引用这些
// 旧 broker id 的「broker 引用字段」（mqtt in/out 节点的 broker 字段、节点 env 数组中
// type=="mqtt-broker" 的 value），避免同一 flow 内残留多个同地址、同 clientid 的
// broker 配置节点——Node-RED 为每个 broker 配置各建一个 MQTT 客户端，EMQX 按 clientid
// 做 session takeover，多个同 clientid 连接会互相挤占、反复上下线，导致节点一直停在
// connecting。mqtt-broker 是 flow 级全局 config 节点，subflow 实例经 env/z 引用主 tab
// 的 config 是标准用法，故跨 scope 收敛到同一 canonical 可正常解析。canonical 优先取
// 无 z 的全局节点；若命中者全部带 z，则取第一个并清掉其 z——部署时 splitGlobalNodes
// 只把无 z 的 config 节点当全局节点，canonical 必须落在全局 scope，否则主 tab 的
// mqtt 节点重绑过来会解析不到 broker。外部 broker 节点与无 mqtt-broker 节点原样保留；
// 解析失败时原样返回。不用字符串替换。
func rewriteImportedMQTTBrokerNodes(raw []byte, client repo.ConnectClient, endpoints ...config.MQTTEndpoint) []byte {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return raw
	}
	nodes, ok := flowNodeList(doc)
	if !ok {
		return raw
	}

	// 收集可本地化 broker 节点：优先选无 z 的全局节点作为 canonical，其余为冗余
	// （不区分 scope，全 flow 收敛为单一 canonical，消除同 clientid 多连接互踢）。
	canonicalIdx := -1
	firstHitIdx := -1
	redundantIdx := []int{}
	for i := range nodes {
		if !localizableImportedMQTTBroker(nodes[i]) {
			continue
		}
		if firstHitIdx < 0 {
			firstHitIdx = i
		}
		_, hasZ := nodes[i]["z"]
		if canonicalIdx < 0 && (!hasZ || strings.TrimSpace(jsonScalar(nodes[i]["z"])) == "") {
			canonicalIdx = i
			continue
		}
		redundantIdx = append(redundantIdx, i)
	}
	if canonicalIdx < 0 {
		// 无全局候选：canonical 取第一个命中者，清掉 z 使其成为全局 config 节点。
		canonicalIdx = firstHitIdx
		if canonicalIdx < 0 {
			out, err := json.Marshal(doc)
			if err != nil {
				return raw
			}
			return out
		}
		redundantIdx = redundantIdx[1:]
		delete(nodes[canonicalIdx], "z")
	}

	// 为 canonical 生成唯一新 id，并建立 旧id→canonical新id 映射。
	usedIDs := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if id := strings.TrimSpace(jsonScalar(node["id"])); id != "" {
			usedIDs[id] = struct{}{}
		}
	}
	var newID string
	for {
		candidate := randomHex(8)
		if _, exists := usedIDs[candidate]; exists {
			continue
		}
		newID = candidate
		break
	}
	replacements := make(map[string]string)
	if oldID := strings.TrimSpace(jsonScalar(nodes[canonicalIdx]["id"])); oldID != "" {
		replacements[oldID] = newID
	}
	for _, idx := range redundantIdx {
		if oldID := strings.TrimSpace(jsonScalar(nodes[idx]["id"])); oldID != "" {
			replacements[oldID] = newID
		}
	}

	// 第一遍：canonical 与冗余 broker 的 id 统一替换为新 id。
	nodes[canonicalIdx]["id"] = newID
	for _, idx := range redundantIdx {
		nodes[idx]["id"] = newID
	}

	// 第二遍：仅重绑「broker 引用字段」（mqtt in/out 节点的 broker 字段、env 里的
	// mqtt-broker 引用），不替换任何其他字符串值（payload/topic 等用户数据原样保留）。
	for _, node := range nodes {
		rewriteBrokerReferenceFields(node, replacements)
	}

	// 第三遍：只对 canonical 写入本地连接信息。
	localWrite(&nodes[canonicalIdx], client, endpoints...)

	// 移除冗余 broker 节点（引用已统一到 canonical，删除无悬挂）。
	redundant := make(map[int]bool, len(redundantIdx))
	for _, idx := range redundantIdx {
		redundant[idx] = true
	}
	keep := make([]map[string]any, 0, len(nodes))
	for i, node := range nodes {
		if redundant[i] {
			continue
		}
		keep = append(keep, node)
	}

	out, err := marshalFlowNodes(doc, keep)
	if err != nil {
		return raw
	}
	return out
}

// rewriteBrokerReferenceFields 仅重绑节点中引用 MQTT broker config 的字段：
//   - mqtt in / mqtt out 节点的 broker 字段（值为旧 broker id）
//   - 节点 env 数组中 type=="mqtt-broker" 的 value 字段（subflow/flow 通过环境属性
//     传递 broker config 引用的场景）
//
// 其余字段（payload、topic 等用户数据）不做任何替换。
func rewriteBrokerReferenceFields(node map[string]any, replacements map[string]string) {
	typ := strings.TrimSpace(jsonScalar(node["type"]))
	if typ == "mqtt in" || typ == "mqtt out" {
		if ref := strings.TrimSpace(jsonScalar(node["broker"])); ref != "" {
			if replacement, ok := replacements[ref]; ok {
				node["broker"] = replacement
			}
		}
	}
	// env 数组里携带 mqtt-broker config 引用（subflow 实例化时按 value 解析 broker）
	if envs, ok := node["env"].([]any); ok {
		for _, envItem := range envs {
			envMap, ok := envItem.(map[string]any)
			if !ok {
				continue
			}
			if strings.TrimSpace(jsonScalar(envMap["type"])) != "mqtt-broker" {
				continue
			}
			value := strings.TrimSpace(jsonScalar(envMap["value"]))
			if value == "" {
				continue
			}
			if replacement, ok := replacements[value]; ok {
				envMap["value"] = replacement
			}
		}
	}
}

// regenerateImportedFlowNodeIDs 重生成导入 flow JSON 中全部节点的 id（tab 除外——
// 其 id 是刚分配的 runtime flow id），并递归重映射 wires/broker/subflow 等引用。
// name 是展示字段而非引用，重映射前快照、完成后恢复，避免 id 与 name 同值的节点
// （如 id/name 同为 "emqx" 的 broker）name 被误改成新 id。
func regenerateImportedFlowNodeIDs(raw []byte) ([]byte, error) {
	var nodes []map[string]any
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return nil, err
	}
	usedIDs := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if id := strings.TrimSpace(jsonScalar(node["id"])); id != "" {
			usedIDs[id] = struct{}{}
		}
	}
	replacements := make(map[string]string, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(jsonScalar(node["type"])) == "tab" {
			continue
		}
		oldID := strings.TrimSpace(jsonScalar(node["id"]))
		if oldID == "" {
			continue
		}
		for {
			newID := randomHex(8)
			if _, exists := usedIDs[newID]; exists {
				continue
			}
			replacements[oldID] = newID
			usedIDs[newID] = struct{}{}
			node["id"] = newID
			break
		}
	}
	originalNames := make([]string, len(nodes))
	hasName := make([]bool, len(nodes))
	for i, node := range nodes {
		if rawName, ok := node["name"]; ok {
			if name, isStr := rawName.(string); isStr {
				originalNames[i] = name
				hasName[i] = true
			}
		}
	}
	for _, node := range nodes {
		rewriteImportedNodeReferences(node, replacements)
	}
	for i, node := range nodes {
		if hasName[i] {
			node["name"] = originalNames[i]
		}
	}
	return json.Marshal(nodes)
}

// rewriteImportedNodeReferences 递归替换节点中与旧 id 精确匹配的字符串值
// （wires/broker/z/env 等引用字段），并处理 "subflow:<id>" 前缀形式。
func rewriteImportedNodeReferences(value any, replacements map[string]string) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			typed[key] = rewriteImportedNodeReferences(item, replacements)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = rewriteImportedNodeReferences(item, replacements)
		}
		return typed
	case []map[string]any:
		for _, item := range typed {
			rewriteImportedNodeReferences(item, replacements)
		}
		return typed
	case string:
		if replacement, ok := replacements[typed]; ok {
			return replacement
		}
		if strings.HasPrefix(typed, "subflow:") {
			oldID := strings.TrimSpace(strings.TrimPrefix(typed, "subflow:"))
			if replacement, ok := replacements[oldID]; ok {
				return "subflow:" + replacement
			}
		}
	}
	return value
}

// localWrite 把本地 EMQX 连接信息写入指定的 mqtt-broker 节点。
func localWrite(node *map[string]any, client repo.ConnectClient, endpoints ...config.MQTTEndpoint) {
	var endpoint config.MQTTEndpoint
	if len(endpoints) > 0 {
		endpoint = endpoints[0]
	}
	// Imported TLS config belongs to the source environment.
	delete(*node, "tls")
	domainflow.ApplyMQTTEndpoint(*node, endpoint)
	if client.ID > 0 {
		(*node)["clientid"] = client.ClientID
		creds, _ := (*node)["credentials"].(map[string]any)
		if creds == nil {
			creds = map[string]any{}
		}
		creds["user"] = client.Username
		creds["password"] = client.Password
		(*node)["credentials"] = creds
	} else {
		// 无本地凭证时清掉 SaaS 凭证，避免指向本地 EMQX 却携带远端账号
		delete(*node, "credentials")
		delete(*node, "clientid")
	}
}

// marshalFlowNodes 把更新后的节点列表回写文档：裸数组直接替换，map 形态回写 nodes/flows 键。
func marshalFlowNodes(doc any, nodes []map[string]any) ([]byte, error) {
	if m, ok := doc.(map[string]any); ok {
		if _, ok := m["nodes"].([]any); ok {
			m["nodes"] = nodes
			return json.Marshal(m)
		}
		if _, ok := m["flows"].([]any); ok {
			m["flows"] = nodes
			return json.Marshal(m)
		}
	}
	return json.Marshal(nodes)
}

// cloudFlowEntry 云端导出 flows.json 中的单条 flow 记录：
// {"id":云端DB主键,"flowID":"运行时tab id","flowName":"...","flowData":"节点数组JSON字符串（无tab节点）","flowType":1|2,"description":"..."}
// flowID 是来源环境 Node-RED 运行时 ID，不参与导入匹配，仅解析保留。
type cloudFlowEntry struct {
	ID          any    `json:"id"`
	FlowID      string `json:"flowID"`
	FlowName    string `json:"flowName"`
	FlowData    string `json:"flowData"`
	FlowType    int    `json:"flowType"`
	Description string `json:"description"`
}

// expandCloudFlowFiles 把云端导出的 flows.json 多 flow 包装拆成单条 projectFlowFile；
// 非云端包装的文件原样保留。
func expandCloudFlowFiles(files []projectFlowFile) []projectFlowFile {
	var out []projectFlowFile
	for _, file := range files {
		entries, ok := parseCloudFlowEntries(file.Raw)
		if !ok {
			out = append(out, file)
			continue
		}
		for i, entry := range entries {
			flowType := entry.FlowType
			if flowType != 2 {
				flowType = 1
			}
			out = append(out, projectFlowFile{
				Path:        fmt.Sprintf("%s#%d", file.Path, i),
				Name:        strings.TrimSpace(entry.FlowName),
				Raw:         []byte(strings.TrimSpace(entry.FlowData)),
				FlowType:    flowType,
				IdentityID:  int64Value(entry.ID),
				Description: entry.Description,
			})
		}
	}
	return out
}

// parseCloudFlowEntries 识别云端 flow 包装的两种形态：
//  1. 多 flow 包装：{"flows":[{"flowName":..,"flowData":..,...}]}，要求每条都带非空 flowData；
//  2. 单 flow 对象：{"id","flowID","flowName","flowData",...}（update 包顶层单条形态），
//     要求带非空 flowData 且有 flowName 或 id 键。
//
// BUG-014 的 {"id","flows":[Node-RED 节点]} 包装元素是节点对象（带 type 键、无 flowData），不会误判。
func parseCloudFlowEntries(raw []byte) ([]cloudFlowEntry, bool) {
	validEntries := func(entries []cloudFlowEntry) bool {
		for _, entry := range entries {
			if strings.TrimSpace(entry.FlowData) == "" {
				return false
			}
		}
		return true
	}
	var wrapper struct {
		Flows []cloudFlowEntry `json:"flows"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil && len(wrapper.Flows) > 0 {
		if !validEntries(wrapper.Flows) {
			return nil, false
		}
		return wrapper.Flows, true
	}
	// 含 nodes/flows 键的对象是 Node-RED 节点包装而非云端单 flow 条目：
	// 不能仅凭顶层 id + flowData 元数据误判，否则真实节点会被 flowData 字符串整体替换而丢失
	var probe struct {
		Nodes json.RawMessage `json:"nodes"`
		Flows json.RawMessage `json:"flows"`
	}
	if err := json.Unmarshal(raw, &probe); err == nil {
		keyPresent := func(b json.RawMessage) bool {
			s := strings.TrimSpace(string(b))
			return s != "" && s != "null" && s != "[]"
		}
		if keyPresent(probe.Nodes) || keyPresent(probe.Flows) {
			return nil, false
		}
	}
	var single cloudFlowEntry
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, false
	}
	if strings.TrimSpace(single.FlowData) == "" || (single.ID == nil && strings.TrimSpace(single.FlowName) == "") {
		return nil, false
	}
	return []cloudFlowEntry{single}, true
}

// ensureFlowTabNode 确保 flow JSON 含 tab 节点：无 tab 时（云端 flowData 形态）在裸数组头部
// 注入 {"id":flowID,"type":"tab","label":label}，并把全部节点的 z 引用统一指向 flowID；
// 已有 tab 或非裸数组形态时原样返回（由 rebindFlowRuntimeID 处理）。
func ensureFlowTabNode(raw []byte, flowID, label string) ([]byte, error) {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	nodes, ok := flowNodeList(doc)
	if !ok {
		return raw, nil
	}
	for _, node := range nodes {
		if strings.TrimSpace(jsonScalar(node["type"])) == "tab" {
			return raw, nil
		}
	}
	arr, ok := doc.([]any)
	if !ok {
		return raw, nil
	}
	for _, node := range nodes {
		node["z"] = flowID
	}
	tab := map[string]any{"id": flowID, "type": "tab", "label": label, "disabled": false, "info": ""}
	return json.Marshal(append([]any{tab}, arr...))
}

// flowNodeList 取出 flow JSON 的节点列表：支持 Node-RED 裸数组与 {"nodes"/"flows":[...]} 包装形态。
// 返回的 map 与 doc 共享底层引用，就地修改后重新序列化 doc 即可。
func flowNodeList(doc any) ([]map[string]any, bool) {
	toMaps := func(arr []any) ([]map[string]any, bool) {
		nodes := make([]map[string]any, 0, len(arr))
		for _, item := range arr {
			node, ok := item.(map[string]any)
			if !ok {
				return nil, false
			}
			nodes = append(nodes, node)
		}
		return nodes, true
	}
	switch v := doc.(type) {
	case []any:
		return toMaps(v)
	case map[string]any:
		for _, key := range []string{"nodes", "flows"} {
			if arr, ok := v[key].([]any); ok {
				return toMaps(arr)
			}
		}
	}
	return nil, false
}

// cleanupImportedResources 首次导入失败补偿：删除可能已注册的网关路由和本次实际新建的资源。
func cleanupImportedResources(ctx context.Context, created importCreatedResources, projectID int64, appID int64, userID int64) {
	cleanupImportedResourcesWithRoutePolicy(ctx, created, projectID, appID, userID, true)
}

// cleanupReplacedAppResources Replace 失败补偿：保留既有网关路由，只清理本次实际新建的资源。
// Replace 部署复用原 docker app_id、容器名和路由；部署层已恢复旧容器，删除路由会使已回退的应用无法访问。
func cleanupReplacedAppResources(ctx context.Context, created importCreatedResources, projectID int64, appID int64, userID int64) {
	cleanupImportedResourcesWithRoutePolicy(ctx, created, projectID, appID, userID, false)
}

// cleanupImportedResourcesWithRoutePolicy 导入失败补偿：删除本次实际新建的资源，只删 INSERT 的记录；
// 被顶层 id 命中更新的既有 Flow、被跳过/覆盖更新的既有 UNS 节点不在集合内，绝不删除。
// 清理顺序与创建顺序相反：asset → Flow → UNS。UNS 按父先子后逐个 DeleteUnsNode（子树级联，
// 可覆盖领域层自动补挂的 topicType 目录）；已被祖先级联删除的节点再删会命中 not-found，直接跳过。
func cleanupImportedResourcesWithRoutePolicy(ctx context.Context, created importCreatedResources, projectID int64, appID int64, userID int64, removeGatewayRoute bool) {
	logger := logx.WithContext(ctx)
	// projectID>0 时清理动态应用可能已注册的网关路由（部署中途失败的残留路由否则
	// 永不清理，访问会 502；删除应用路径已有独立清理，此处覆盖失败补偿路径）。
	if removeGatewayRoute && appID > 0 && projectID > 0 {
		cleanupImportedAppGatewayRoutes(ctx, projectID, appID, logger)
	}
	if len(created.AssetIDs) > 0 {
		assetRepo := repo.NewAssetRepo(ctx)
		for i := len(created.AssetIDs) - 1; i >= 0; i-- {
			if err := assetRepo.SoftDeleteAssetFile(ctx, created.AssetIDs[i]); err != nil && !errors.Is(err, repo.ErrNotFound) {
				logger.Errorf("cleanup imported asset %d failed: %v", created.AssetIDs[i], err)
			}
		}
	}
	if len(created.FlowIDs) > 0 {
		flowRepo := repo.NewFlowRepo(ctx)
		for i := len(created.FlowIDs) - 1; i >= 0; i-- {
			if err := flowRepo.DeleteFlow(ctx, created.FlowIDs[i], userID); err != nil && !errors.Is(err, repo.ErrNotFound) {
				logger.Errorf("cleanup imported flow %d failed: %v", created.FlowIDs[i], err)
			}
		}
	}
	if len(created.UnsNodeIDs) > 0 {
		unsRepo := repo.NewUnsRepo(ctx)
		for _, id := range created.UnsNodeIDs {
			if _, err := unsRepo.DeleteUnsNode(ctx, id, userID); err != nil && !errors.Is(err, repo.ErrNotFound) {
				logger.Errorf("cleanup imported uns node %d failed: %v", id, err)
			}
		}
	}
}

// cleanupImportedAppGatewayRoutes 清理动态应用失败的网关路由残留：
// 从 App manifest 读 docker app_id（若部署已回写），有则删路由并刷新网关内存路由表。
// 半成品部署（app_id 为空）直接跳过；清理失败只记日志不阻断整体补偿。
func cleanupImportedAppGatewayRoutes(ctx context.Context, projectID, appRecordID int64, logger logx.Logger) {
	if logger == nil {
		logger = logx.WithContext(ctx)
	}
	app, err := repo.NewProjectRepo(ctx).GetProjectApp(ctx, projectID, appRecordID)
	if err != nil {
		logger.Errorf("cleanup imported gateway routes: load app %d failed: %v", appRecordID, err)
		return
	}
	var meta map[string]any
	if len(app.ManifestJSON) > 0 {
		if err := json.Unmarshal(app.ManifestJSON, &meta); err != nil {
			logger.Errorf("cleanup imported gateway routes: parse manifest app %d failed: %v", appRecordID, err)
			return
		}
	}
	dockerAppID, _ := meta["app_id"].(string)
	if strings.TrimSpace(dockerAppID) == "" {
		return
	}
	routeRepo := repo.NewAppGatewayRouteRepo(ctx)
	if err := routeRepo.DeleteByAppID(ctx, dockerAppID); err != nil {
		logger.Errorf("cleanup imported gateway routes: delete route for app %d failed: %v", appRecordID, err)
		return
	}
	if err := reloadGatewayRoutes(ctx); err != nil {
		logger.Errorf("cleanup imported gateway routes: reload failed: %v", err)
	}
}

// isSystemTopicFolder 判断文件夹名是否为 UNS 系统 topic 文件夹（State/Action/Metric）。
func isSystemTopicFolder(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "state", "action", "metric":
		return true
	default:
		return false
	}
}

// countUnsNodes 递归统计包内 UNS 节点总数（不含根）。
func countUnsNodes(nodes []projectUnsNode) int {
	total := 0
	for _, node := range nodes {
		total++
		total += countUnsNodes(node.Children)
	}
	return total
}

// buildImportSummary 构造导入结果摘要，供前端汇总展示。
func buildImportSummary(pkg projectAppPackage, unsNodeIDs []int64, skippedUns []string, flowIDs []int64, renamedFlows []map[string]string) map[string]any {
	unsTotal := countUnsNodes(pkg.Uns.Namespace)
	unsImported := len(unsNodeIDs)
	if unsImported > 0 {
		// 根节点固定存在，统计新增时减去根
		unsImported--
	}
	return map[string]any{
		"uns": map[string]any{
			"total":             unsTotal,
			"imported":          unsImported,
			"skipped":           len(skippedUns),
			"skippedNamespaces": skippedUns,
		},
		"flows": map[string]any{
			"total":        len(pkg.FlowFiles),
			"imported":     len(flowIDs),
			"renamed":      len(renamedFlows),
			"renamedFlows": renamedFlows,
		},
	}
}

// buildDeployErrorSummary 构造异步导入失败的结构化摘要，写入 manifest 供前端轮询。
func buildDeployErrorSummary(err error) map[string]any {
	stage := "deploy"
	msg := err.Error()
	switch {
	case strings.Contains(msg, "read package"):
		stage = "readPackage"
	case strings.Contains(msg, "invalid uns.json"):
		stage = "unsParse"
	case strings.Contains(msg, "UNS "):
		stage = "unsImport"
	case strings.Contains(msg, "flow"):
		stage = "flowImport"
	case strings.Contains(msg, "role"):
		stage = "roleImport"
	}
	return map[string]any{
		"failed":  true,
		"stage":   stage,
		"message": msg,
	}
}

func normalizePackageTopicType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "state", "1":
		return "State"
	case "action", "2":
		return "Action"
	case "metric", "3":
		return "Metric"
	default:
		return ""
	}
}

func appendUniqueInt64(ids []int64, id int64) []int64 {
	if id == 0 {
		return ids
	}
	for _, current := range ids {
		if current == id {
			return ids
		}
	}
	return append(ids, id)
}

func int64Value(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		id, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return id
	default:
		return 0
	}
}

func parseInt64(value string) int64 {
	id, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return id
}

func memberIDFromResp(data repo.ProjectMember) int64 {
	return data.ID
}

func userIDFromMemberResp(data repo.ProjectMember) int64 {
	return data.UserID
}

func (l *ProjectAppLogic) recordProjectMemberAudit(businessType string, projectID, memberID, userID int64) {
	// 成员变更需要出现在项目 Recent 中；Recent 路由使用 ResID 生成 /project/:id，因此这里用项目 ID。
	l.svcCtx.App.Audit.Record(l.ctx, auditdomain.RecordInput{
		ScopeType:    auditdomain.ScopeTypePlatform,
		ResType:      auditdomain.ResTypeProject,
		ResID:        strconv.FormatInt(projectID, 10),
		BusinessType: businessType,
		Detail: map[string]any{
			"projectId": projectID,
			"memberId":  memberID,
			"userId":    userID,
		},
	})
}

// projectAppImportAuditDetail 导入审计的结构化明细。
// decision 为 success（成功）/rejected（业务拒绝：配额/身份/格式/状态/并发冲突等）/failed（读取或落库失败）；
// reason 在拒绝或失败时记录可读原因；targetAppId 为 Replace 场景的目标应用，packageAppId 为包内来源 appId。
type projectAppImportAuditDetail struct {
	Decision     string
	Reason       string
	TargetAppID  int64
	PackageAppID string
	FileID       int64
}

func (l *ProjectAppLogic) recordProjectAppImportAudit(projectID, appID int64, appName string, detail projectAppImportAuditDetail) {
	fields := map[string]any{
		"projectId": projectID,
		"appId":     appID,
		"operator":  logicx.UserID(l.ctx),
	}
	if detail.Decision != "" {
		fields["decision"] = detail.Decision
	}
	if detail.Reason != "" {
		fields["reason"] = detail.Reason
	}
	if detail.TargetAppID > 0 {
		fields["targetAppId"] = detail.TargetAppID
	}
	if detail.PackageAppID != "" {
		fields["packageAppId"] = detail.PackageAppID
	}
	if detail.FileID > 0 {
		fields["fileId"] = detail.FileID
	}
	showInRecent := auditdomain.ShowInRecentYes
	if detail.Decision != "" && detail.Decision != "success" {
		showInRecent = auditdomain.ShowInRecentNo
	}
	l.svcCtx.App.Audit.Record(l.ctx, auditdomain.RecordInput{
		ScopeType:      auditdomain.ScopeTypeProject,
		ScopeID:        projectID,
		ResType:        auditdomain.ResTypeProjectApp,
		ResID:          strconv.FormatInt(appID, 10),
		ResName:        appName,
		BusinessType:   auditdomain.BizImport,
		IsShowInRecent: showInRecent,
		Detail:         fields,
	})
}

func (l *ProjectAppLogic) recordProjectAppDeleteAudit(projectID, appID int64, appName string) {
	if appID <= 0 {
		return
	}
	l.svcCtx.App.Audit.Record(l.ctx, auditdomain.RecordInput{
		ScopeType:    auditdomain.ScopeTypeProject,
		ScopeID:      projectID,
		ResType:      auditdomain.ResTypeProjectApp,
		ResID:        strconv.FormatInt(appID, 10),
		ResName:      appName,
		BusinessType: auditdomain.BizDelete,
		Detail: map[string]any{
			"projectId": projectID,
			"appId":     appID,
			"name":      appName,
		},
	})
}

// Code scaffolded by goctl. Safe to edit.

package project

import (
	"context"

	domainproject "backend/internal/domain/project"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ProjectAppImportPreviewLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectAppImportPreviewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectAppImportPreviewLogic {
	return &ProjectAppImportPreviewLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// ProjectAppImportPreview 只读 zip 头部（artifact.toml + meta.json），返回 schema 默认值与包内可编辑环境变量，
// 供导入弹窗「高级」区预填。要求 project.manage 权限（包内 env 可能含敏感值，不能落到 project.view）。
// 平台强制变量（APP_ID / API Key / DATABASE_URL 等）不会进入预览；正式导入时后端会重新解析包，
// 不信任前端回传的身份/环境变量，并由平台重新注入强制变量。
func (l *ProjectAppImportPreviewLogic) ProjectAppImportPreview(req *types.ProjectAppImportPreviewReq) (*types.Envelope, error) {
	userID := logicx.UserID(l.ctx)
	if err := l.svcCtx.App.Project.CheckManage(l.ctx, req.ProjectId, userID); err != nil {
		return nil, logicx.Error(err)
	}
	if req.FileId <= 0 {
		return nil, logicx.Error(domainproject.ErrInvalid)
	}
	// 包 asset 归属当前 Project（按 asset_binding 绑定关系），越权 403。
	// Import 弹窗绑定 (projectApp, projectId)，Replace 弹窗绑定 (projectAppReplacement, projectId)，
	// 两者都可调预览接口（与 Replace 端点的 ownerTypes 一致），保证 Replace 弹窗 env 预填不 403。
	if err := checkImportAssetOwned(l.ctx, req.FileId, req.ProjectId, projectImportAssetOwnerType, projectReplaceAssetOwnerType); err != nil {
		return nil, logicx.Error(err)
	}

	_, reader, _, err := l.svcCtx.App.Asset.Open(l.ctx, req.FileId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	defer reader.Close()

	localPath, isLocal := localAssetFilePath(reader)
	var tmpPath string
	var cleanup func()
	if isLocal {
		tmpPath = localPath
		cleanup = func() {}
	} else {
		tmpPath, cleanup, err = copyAssetToTempFile(reader)
		if err != nil {
			return nil, logicx.Error(err)
		}
	}
	defer cleanup()

	info, readErr := readArtifactFromZip(tmpPath)
	if readErr != nil {
		// 非 artifact 包（静态包）：无 schema/env 概念，返回空（前端隐藏高级区）
		logx.Infof("[ProjectAppImportPreview] 非 artifact 包，返回空预览: fileId=%d err=%v", req.FileId, readErr)
		return respx.Envelope(map[string]any{
			"appId":         "",
			"projectId":     "",
			"schemaDefault": "",
			"envVars":       []types.EnvVarItem{},
		}), nil
	}

	meta := parsePackageMetaOrEmpty(info.MetaRaw)
	cfg := parseArtifactConfig(info.Manifest)
	envVars := buildEditableEnvVarItems(cfg.Env)

	return respx.Envelope(map[string]any{
		"appId":         meta.AppID,
		"projectId":     meta.ProjectID,
		"schemaDefault": normalizeSchemaName(meta.AppID),
		"envVars":       envVars,
	}), nil
}

// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package project

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"backend/internal/domain/project"
	"backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ProjectAppDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectAppDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectAppDetailLogic {
	return &ProjectAppDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProjectAppDetailLogic) ProjectAppDetail(req *types.ProjectAppReq) (resp *types.Envelope, err error) {
	app, err := repo.NewProjectRepo(l.ctx).GetProjectApp(l.ctx, req.ProjectId, req.AppId)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, logicx.Error(project.ErrNotFound)
		}
		return nil, err
	}

	manifest := map[string]any{}
	if len(app.ManifestJSON) > 0 {
		_ = json.Unmarshal(app.ManifestJSON, &manifest)
	}

	deploying, _ := manifest["deploying"].(bool)
	deployError, _ := manifest["deploy_error"].(string)
	description, _ := manifest["description"].(string)
	appType, _ := manifest["app_type"].(string)
	openInPlatform, _ := manifest["openInPlatform"].(bool)
	coverAssetID := manifestAssetID(manifest, "coverAssetId")
	// 已持久化的自定义磁盘映射（导入「高级」区提交），Replace 弹窗预填用；无则返回空数组
	volumes := manifestVolumes(manifest)
	manual := app.SiteType == "manualExternal" || appType == "manual"
	deployStatus := deployStatusForDetail(app.Status, deploying, deployError)
	if manual {
		deployStatus = "success"
	}
	// 入口地址按当前请求 Host 动态重建：仅处理生成型 URL；手工外部地址可能
	// 落在网关端口段的任意域名上，必须原样透传。iconUrl/coverUrl 为站内相对
	// 路径，无需重建
	entryURL := app.EntryURL
	if !manual {
		entryURL = httpx.RebuildGeneratedEntryURL(l.ctx, l.svcCtx.Config.AppGateway, app.EntryURL)
	}

	data := map[string]any{
		"appId":             app.ID,
		"appPrincipalId":    app.AppPrincipalID,
		"projectId":         app.ProjectID,
		"name":              app.Name,
		"displayName":       app.Name,
		"description":       description,
		"status":            project.AppStatusForFrontend(app.Status),
		"siteType":          app.SiteType,
		"appType":           appType,
		"manual":            manual,
		"openInPlatform":    openInPlatform,
		"deployStatus":      deployStatus,
		"deployError":       deployError,
		"sourceAppId":       manifestStringValue(manifest, "sourceAppId"),
		"sourceProjectId":   manifestStringValue(manifest, "sourceProjectId"),
		"sourceWorkspaceId": manifestStringValue(manifest, "sourceWorkspaceId"),
		"url":               entryURL,
		"networkAlias":      manifestStringValue(manifest, "network_alias"),
		"internalUrl":       project.AppInternalURL(manifest),
		"iconAssetId":       app.IconAssetID,
		"iconUrl":           projectAppDetailIconURL(app.IconAssetID),
		"coverAssetId":      coverAssetID,
		"coverUrl":          projectAppDetailIconURL(coverAssetID),
		"volumes":           volumes,
	}
	return &types.Envelope{Code: 0, Msg: "ok", Data: data}, nil
}

func manifestAssetID(manifest map[string]any, key string) int64 {
	switch value := manifest[key].(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	case string:
		id, _ := strconv.ParseInt(value, 10, 64)
		return id
	default:
		return 0
	}
}

func projectAppDetailIconURL(iconAssetID int64) string {
	if iconAssetID <= 0 {
		return ""
	}
	return "/api/core/assets/" + strconv.FormatInt(iconAssetID, 10) + "/download"
}

func deployStatusForDetail(status string, deploying bool, deployError string) string {
	if deploying {
		return "deploying"
	}
	if deployError != "" {
		return "failed"
	}
	switch project.NormalizeAppStatus(status) {
	case "stopped":
		return "stopped"
	case "failed":
		return "failed"
	case "active", "deploying":
		return "success"
	default:
		return "pending"
	}
}

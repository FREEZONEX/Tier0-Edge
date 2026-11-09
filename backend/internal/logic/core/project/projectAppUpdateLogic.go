// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package project

import (
	"context"
	"strconv"

	auditdomain "backend/internal/domain/audit"
	domainproject "backend/internal/domain/project"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ProjectAppUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectAppUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectAppUpdateLogic {
	return &ProjectAppUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProjectAppUpdateLogic) ProjectAppUpdate(req *types.ProjectAppUpdateReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Project.UpdateAppMetadata(l.ctx, domainproject.AppMetadataCommand{
		ProjectID:    req.ProjectId,
		AppID:        req.AppId,
		Name:         req.Name,
		Description:  req.Description,
		IconAssetID:  req.IconAssetId,
		CoverAssetID: req.CoverAssetId,
		UserID:       logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	l.svcCtx.App.Audit.Record(l.ctx, auditdomain.RecordInput{
		ScopeType:      auditdomain.ScopeTypeProject,
		ScopeID:        req.ProjectId,
		ResType:        auditdomain.ResTypeProjectApp,
		ResID:          strconv.FormatInt(req.AppId, 10),
		ResName:        req.Name,
		BusinessType:   auditdomain.BizUpdate,
		IsShowInRecent: auditdomain.ShowInRecentYes,
		Detail: map[string]any{
			"projectId":    req.ProjectId,
			"appId":        req.AppId,
			"name":         req.Name,
			"description":  req.Description,
			"iconAssetId":  req.IconAssetId,
			"coverAssetId": req.CoverAssetId,
		},
	})
	return respx.Envelope(data), nil
}

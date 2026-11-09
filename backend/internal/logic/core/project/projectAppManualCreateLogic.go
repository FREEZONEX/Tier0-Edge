// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

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

type ProjectAppManualCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectAppManualCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectAppManualCreateLogic {
	return &ProjectAppManualCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProjectAppManualCreateLogic) ProjectAppManualCreate(req *types.ProjectAppManualSaveReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Project.CreateManualApp(l.ctx, domainproject.ManualAppCommand{
		ProjectID:      req.ProjectId,
		Name:           req.Name,
		Description:    req.Description,
		AccessMode:     req.AccessMode,
		ContainerName:  req.ContainerName,
		ContainerPort:  req.ContainerPort,
		TargetPath:     req.TargetPath,
		EntryURL:       req.EntryUrl,
		OpenInPlatform: req.OpenInPlatform,
		IconAssetID:    req.IconAssetId,
		UserID:         logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

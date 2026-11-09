// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package app

import (
	"context"
	"strings"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ContainerAppCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewContainerAppCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ContainerAppCreateLogic {
	return &ContainerAppCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ContainerAppCreateLogic) ContainerAppCreate(req *types.ContainerAppSaveReq) (resp *types.Envelope, err error) {
	if err := validateContainerAppSave(req); err != nil {
		return nil, err
	}
	manifest, err := containerAppManifest(req.Description, req.OpenInPlatform)
	if err != nil {
		return nil, err
	}
	item := repo.ProjectApp{
		ProjectID:    0,
		AppKey:       "container_" + repo.RandomHexForApp(8),
		Name:         strings.TrimSpace(req.Name),
		SiteType:     containerAppSiteType,
		EntryURL:     strings.TrimSpace(req.EntryUrl),
		IconAssetID:  req.IconAssetId,
		ManifestJSON: manifest,
		Status:       "active",
	}
	created, err := repo.NewProjectRepo(l.ctx).CreateProjectApp(l.ctx, item)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(containerAppData(created)), nil
}

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

type ContainerAppUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewContainerAppUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ContainerAppUpdateLogic {
	return &ContainerAppUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ContainerAppUpdateLogic) ContainerAppUpdate(req *types.ContainerAppSaveReq) (resp *types.Envelope, err error) {
	if req.AppId <= 0 {
		return nil, logicx.Error(repo.ErrInvalidArgument)
	}
	if err := validateContainerAppSave(req); err != nil {
		return nil, err
	}
	existing, err := repo.NewProjectRepo(l.ctx).GetContainerApp(l.ctx, req.AppId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	manifest, err := containerAppManifest(req.Description, req.OpenInPlatform)
	if err != nil {
		return nil, err
	}
	existing.Name = strings.TrimSpace(req.Name)
	existing.EntryURL = strings.TrimSpace(req.EntryUrl)
	existing.IconAssetID = req.IconAssetId
	existing.ManifestJSON = manifest
	updated, err := repo.NewProjectRepo(l.ctx).UpdateProjectApp(l.ctx, req.AppId, existing)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(containerAppData(updated)), nil
}

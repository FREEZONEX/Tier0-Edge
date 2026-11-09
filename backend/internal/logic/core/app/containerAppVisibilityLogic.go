// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package app

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ContainerAppVisibilityLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewContainerAppVisibilityLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ContainerAppVisibilityLogic {
	return &ContainerAppVisibilityLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ContainerAppVisibilityLogic) ContainerAppVisibility(req *types.ContainerAppVisibilityReq) (resp *types.Envelope, err error) {
	status := "stopped"
	if req.Enabled {
		status = "active"
	}
	if err := repo.NewProjectRepo(l.ctx).UpdateContainerAppStatus(l.ctx, req.AppId, status, logicx.UserID(l.ctx)); err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"appId": req.AppId, "enabled": req.Enabled}), nil
}

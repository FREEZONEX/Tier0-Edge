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

type ContainerAppDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewContainerAppDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ContainerAppDeleteLogic {
	return &ContainerAppDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ContainerAppDeleteLogic) ContainerAppDelete(req *types.ContainerAppIdReq) (resp *types.Envelope, err error) {
	if err := repo.NewProjectRepo(l.ctx).DeleteContainerApp(l.ctx, req.AppId, logicx.UserID(l.ctx)); err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"appId": req.AppId, "deleted": true}), nil
}

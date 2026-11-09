// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package app

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ContainerAppListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewContainerAppListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ContainerAppListLogic {
	return &ContainerAppListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ContainerAppListLogic) ContainerAppList() (resp *types.Envelope, err error) {
	apps, err := repo.NewProjectRepo(l.ctx).ListContainerApps(l.ctx)
	if err != nil {
		return nil, err
	}
	list := make([]map[string]any, 0, len(apps))
	for _, item := range apps {
		list = append(list, containerAppData(item))
	}
	return respx.Envelope(map[string]any{"list": list, "total": len(list)}), nil
}

// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package gateway

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GatewayRouteListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGatewayRouteListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GatewayRouteListLogic {
	return &GatewayRouteListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GatewayRouteListLogic) GatewayRouteList() (resp *types.Envelope, err error) {
	routes, err := l.svcCtx.App.Gateway.Routes(l.ctx)
	if err != nil {
		return nil, err
	}
	return respx.Envelope(map[string]any{"list": routes, "total": len(routes)}), nil
}

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

type GatewayRouteTestLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGatewayRouteTestLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GatewayRouteTestLogic {
	return &GatewayRouteTestLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GatewayRouteTestLogic) GatewayRouteTest(req *types.RouteKeyReq) (resp *types.Envelope, err error) {
	return respx.Envelope(map[string]any{"routeKey": req.RouteKey, "reachable": true, "note": "phase1 config test only"}), nil
}

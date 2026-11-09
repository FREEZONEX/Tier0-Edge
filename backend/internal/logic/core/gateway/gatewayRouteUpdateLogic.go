// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package gateway

import (
	"context"
	"errors"

	gatewaydomain "backend/internal/domain/gateway"
	respx "backend/internal/httpx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GatewayRouteUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGatewayRouteUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GatewayRouteUpdateLogic {
	return &GatewayRouteUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GatewayRouteUpdateLogic) GatewayRouteUpdate(req *types.GatewayRouteReq) (resp *types.Envelope, err error) {
	if req.RouteKey == "" || req.PathPattern == "" || req.TargetType == "" || req.AuthPolicy == "" {
		return nil, respx.NewHTTPError(400, "routeKey/pathPattern/targetType/authPolicy are required")
	}
	if err := l.svcCtx.App.Gateway.UpdateRoute(l.ctx, toRepoRoute(req)); err != nil {
		if errors.Is(err, gatewaydomain.ErrSystemRouteReadonly) {
			return nil, respx.NewHTTPError(403, "system gateway route is read-only")
		}
		if errors.Is(err, gatewaydomain.ErrRouteNotFound) {
			return nil, respx.NewHTTPError(404, "gateway route not found")
		}
		return nil, err
	}
	return respx.Empty(), nil
}

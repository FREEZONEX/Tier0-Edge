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

type GatewayRouteCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGatewayRouteCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GatewayRouteCreateLogic {
	return &GatewayRouteCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GatewayRouteCreateLogic) GatewayRouteCreate(req *types.GatewayRouteReq) (resp *types.Envelope, err error) {
	if req.RouteKey == "" || req.PathPattern == "" || req.TargetType == "" || req.AuthPolicy == "" {
		return nil, respx.NewHTTPError(400, "routeKey/pathPattern/targetType/authPolicy are required")
	}
	if err := l.svcCtx.App.Gateway.CreateRoute(l.ctx, toRepoRoute(req)); err != nil {
		if errors.Is(err, gatewaydomain.ErrSystemRouteReadonly) {
			return nil, respx.NewHTTPError(403, "system gateway route is read-only")
		}
		if errors.Is(err, gatewaydomain.ErrRouteExists) {
			return nil, respx.NewHTTPError(409, "gateway route already exists")
		}
		return nil, err
	}
	return respx.Empty(), nil
}

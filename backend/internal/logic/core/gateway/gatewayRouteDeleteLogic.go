// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package gateway

import (
	"context"
	"errors"

	"backend/internal/contextx"
	gatewaydomain "backend/internal/domain/gateway"
	respx "backend/internal/httpx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type GatewayRouteDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGatewayRouteDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GatewayRouteDeleteLogic {
	return &GatewayRouteDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GatewayRouteDeleteLogic) GatewayRouteDelete(req *types.RouteKeyReq) (resp *types.Envelope, err error) {
	subject, ok := contextx.SubjectFrom(l.ctx)
	if !ok {
		return nil, respx.NewHTTPError(401, "unauthorized")
	}
	if err := l.svcCtx.App.Gateway.DeleteRoute(l.ctx, req.RouteKey, subject.UserID); err != nil {
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

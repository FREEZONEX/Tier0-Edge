// Code scaffolded by goctl. Safe to edit.

package launchpad

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type LaunchpadFavoriteListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLaunchpadFavoriteListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LaunchpadFavoriteListLogic {
	return &LaunchpadFavoriteListLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *LaunchpadFavoriteListLogic) LaunchpadFavoriteList(req *types.LaunchpadFavoriteListReq) (resp *types.Envelope, err error) {
	list, err := l.svcCtx.App.Launchpad.Favorites(l.ctx, logicx.UserID(l.ctx), req.ResType)
	if err != nil {
		return nil, respx.NewHTTPError(500, err.Error())
	}
	return respx.Envelope(map[string]any{"list": list}), nil
}

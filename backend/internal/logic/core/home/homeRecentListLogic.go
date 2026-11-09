// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package home

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type HomeRecentListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewHomeRecentListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *HomeRecentListLogic {
	return &HomeRecentListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *HomeRecentListLogic) HomeRecentList(req *types.HomeRecentReq, isShowInRecent int64) (resp *types.Envelope, err error) {
	items, err := l.svcCtx.App.Home.RecentItems(l.ctx, logicx.UserID(l.ctx), req.Limit, isShowInRecent)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"list": items}), nil
}

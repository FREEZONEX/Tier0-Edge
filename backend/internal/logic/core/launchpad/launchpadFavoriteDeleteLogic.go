// Code scaffolded by goctl. Safe to edit.

package launchpad

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type LaunchpadFavoriteDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLaunchpadFavoriteDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LaunchpadFavoriteDeleteLogic {
	return &LaunchpadFavoriteDeleteLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *LaunchpadFavoriteDeleteLogic) LaunchpadFavoriteDelete(req *types.LaunchpadFavoriteBatchReq) (resp *types.Envelope, err error) {
	items := make([]repo.UserFavorite, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, repo.UserFavorite{
			ResType: it.ResType,
			ResID:   it.ResID,
		})
	}
	if err := l.svcCtx.App.Launchpad.BatchDeleteFavorites(l.ctx, logicx.UserID(l.ctx), items); err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(nil), nil
}

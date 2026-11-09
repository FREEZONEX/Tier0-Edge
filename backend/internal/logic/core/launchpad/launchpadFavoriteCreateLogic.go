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

type LaunchpadFavoriteCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLaunchpadFavoriteCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LaunchpadFavoriteCreateLogic {
	return &LaunchpadFavoriteCreateLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *LaunchpadFavoriteCreateLogic) LaunchpadFavoriteCreate(req *types.LaunchpadFavoriteBatchReq) (resp *types.Envelope, err error) {
	items := make([]repo.UserFavorite, 0, len(req.Items))
	for _, it := range req.Items {
		items = append(items, repo.UserFavorite{
			ResType: it.ResType,
			ResID:   it.ResID,
			ResName: it.ResName,
		})
	}
	if err := l.svcCtx.App.Launchpad.BatchAddFavorites(l.ctx, logicx.UserID(l.ctx), items); err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(nil), nil
}

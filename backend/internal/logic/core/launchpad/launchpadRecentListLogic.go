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

type LaunchpadRecentListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLaunchpadRecentListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LaunchpadRecentListLogic {
	return &LaunchpadRecentListLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *LaunchpadRecentListLogic) LaunchpadRecentList() (resp *types.Envelope, err error) {
	list, err := l.svcCtx.App.Launchpad.RecentApps(l.ctx, logicx.UserID(l.ctx))
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"list": list}), nil
}

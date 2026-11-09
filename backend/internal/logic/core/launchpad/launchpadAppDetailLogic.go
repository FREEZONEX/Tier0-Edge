// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package launchpad

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type LaunchpadAppDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLaunchpadAppDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LaunchpadAppDetailLogic {
	return &LaunchpadAppDetailLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *LaunchpadAppDetailLogic) LaunchpadAppDetail(req *types.LaunchpadAppReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Launchpad.App(l.ctx, logicx.UserID(l.ctx), req.ProjectId, req.AppId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

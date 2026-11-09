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

type LaunchpadProjectDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLaunchpadProjectDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LaunchpadProjectDetailLogic {
	return &LaunchpadProjectDetailLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *LaunchpadProjectDetailLogic) LaunchpadProjectDetail(req *types.LaunchpadProjectReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Launchpad.Project(l.ctx, logicx.UserID(l.ctx), req.ProjectId, req.Keyword)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

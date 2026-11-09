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

type LaunchpadProjectListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLaunchpadProjectListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LaunchpadProjectListLogic {
	return &LaunchpadProjectListLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *LaunchpadProjectListLogic) LaunchpadProjectList(req *types.LaunchpadListReq) (resp *types.Envelope, err error) {
	requesterID := logicx.UserID(l.ctx)
	targetUserID := requesterID
	if req.UserId > 0 && req.UserId != requesterID {
		if !l.svcCtx.App.IAM.IsAdmin(l.ctx, requesterID) {
			return nil, respx.NewHTTPError(403, "forbidden")
		}
		targetUserID = req.UserId
	}
	data, err := l.svcCtx.App.Launchpad.Projects(l.ctx, targetUserID, req.Keyword)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

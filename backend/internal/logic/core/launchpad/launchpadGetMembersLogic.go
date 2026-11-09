// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package launchpad

import (
	"context"

	domainlaunchpad "backend/internal/domain/launchpad"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type LaunchpadGetMembersLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLaunchpadGetMembersLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LaunchpadGetMembersLogic {
	return &LaunchpadGetMembersLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *LaunchpadGetMembersLogic) LaunchpadGetMembers(req *types.LaunchpadGetMembersReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Launchpad.Members(l.ctx, domainlaunchpad.MembersQuery{
		ProjectName:    req.ProjectName,
		RoleKey:        req.RoleKey,
		Roles:          req.Roles,
		UpdatedAtStart: req.UpdatedAtStart,
		UpdatedAtEnd:   req.UpdatedAtEnd,
		Page:           req.Page,
		Size:           req.Size,
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

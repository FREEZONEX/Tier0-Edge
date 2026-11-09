// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package auth

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type MicrosoftSSOLinkLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMicrosoftSSOLinkLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MicrosoftSSOLinkLogic {
	return &MicrosoftSSOLinkLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MicrosoftSSOLinkLogic) MicrosoftSSOLink(req *types.ExternalSSOLinkReq, binding string) (resp *types.Envelope, err error) {
	result, err := l.svcCtx.App.Auth.LinkMicrosoftIdentity(l.ctx, req.Ticket, binding, req.Username, req.Password)
	if err != nil {
		return nil, microsoftHTTPError(err)
	}
	return microsoftLoginEnvelope(l.ctx, l.svcCtx, result), nil
}

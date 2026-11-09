// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package auth

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type MicrosoftSSOPendingLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMicrosoftSSOPendingLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MicrosoftSSOPendingLogic {
	return &MicrosoftSSOPendingLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MicrosoftSSOPendingLogic) MicrosoftSSOPending(req *types.ExternalSSOPendingReq, binding string) (resp *types.Envelope, err error) {
	pending, err := l.svcCtx.App.Auth.MicrosoftPending(l.ctx, req.Ticket, binding)
	if err != nil {
		return nil, microsoftHTTPError(err)
	}
	return respx.Envelope(map[string]any{
		"displayName": pending.DisplayName, "email": pending.Email,
		"suggestedUsername": pending.SuggestedUsername, "mfaAuthenticated": pending.MFAAuthenticated,
		"redirectUri": pending.RedirectURI,
	}), nil
}

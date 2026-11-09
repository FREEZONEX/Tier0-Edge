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

type MicrosoftSSOUsernameAvailabilityLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMicrosoftSSOUsernameAvailabilityLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MicrosoftSSOUsernameAvailabilityLogic {
	return &MicrosoftSSOUsernameAvailabilityLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MicrosoftSSOUsernameAvailabilityLogic) MicrosoftSSOUsernameAvailability(req *types.ExternalSSOUsernameAvailabilityReq, binding string) (resp *types.Envelope, err error) {
	available, err := l.svcCtx.App.Auth.MicrosoftUsernameAvailable(l.ctx, req.Ticket, binding, req.Username)
	if err != nil {
		return nil, microsoftHTTPError(err)
	}
	return respx.Envelope(map[string]any{"available": available}), nil
}

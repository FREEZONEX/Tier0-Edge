// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package auth

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type MicrosoftSSOCompleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMicrosoftSSOCompleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MicrosoftSSOCompleteLogic {
	return &MicrosoftSSOCompleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MicrosoftSSOCompleteLogic) MicrosoftSSOComplete(req *types.ExternalSSOCompleteReq, binding string) (resp *types.Envelope, err error) {
	result, err := l.svcCtx.App.Auth.CompleteMicrosoftRegistration(l.ctx, req.Ticket, binding, req.Username)
	if err != nil {
		return nil, microsoftHTTPError(err)
	}
	return microsoftLoginEnvelope(l.ctx, l.svcCtx, result), nil
}

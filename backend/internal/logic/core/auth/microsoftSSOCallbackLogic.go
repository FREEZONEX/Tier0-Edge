// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package auth

import (
	"context"

	domainauth "backend/internal/domain/auth"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type MicrosoftSSOCallbackLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMicrosoftSSOCallbackLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MicrosoftSSOCallbackLogic {
	return &MicrosoftSSOCallbackLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MicrosoftSSOCallbackLogic) MicrosoftSSOCallback(req *types.ExternalSSOCallbackReq, binding string) (domainauth.MicrosoftCallbackResult, error) {
	result, err := l.svcCtx.App.Auth.MicrosoftCallback(l.ctx, req.State, req.Code, req.Error, binding)
	if err != nil {
		return domainauth.MicrosoftCallbackResult{}, err
	}
	if result.Login != nil {
		recordMicrosoftLogin(l.ctx, l.svcCtx, *result.Login)
	}
	return result, nil
}

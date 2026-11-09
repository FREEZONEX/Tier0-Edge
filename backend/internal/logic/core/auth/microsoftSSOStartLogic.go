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

type MicrosoftSSOStartLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMicrosoftSSOStartLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MicrosoftSSOStartLogic {
	return &MicrosoftSSOStartLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MicrosoftSSOStartLogic) MicrosoftSSOStart(req *types.ExternalSSOStartReq) (domainauth.MicrosoftAuthorizationResult, error) {
	result, err := l.svcCtx.App.Auth.MicrosoftAuthorizationURL(l.ctx, req.RedirectUri)
	if err != nil {
		return domainauth.MicrosoftAuthorizationResult{}, err
	}
	return result, nil
}

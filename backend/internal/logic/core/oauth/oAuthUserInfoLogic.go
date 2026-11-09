// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package oauth

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OAuthUserInfoLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOAuthUserInfoLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OAuthUserInfoLogic {
	return &OAuthUserInfoLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OAuthUserInfoLogic) OAuthUserInfo() (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.OAuth.UserInfo(l.ctx, logicx.UserID(l.ctx))
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

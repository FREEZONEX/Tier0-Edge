// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package oauth

import (
	"context"

	domainoauth "backend/internal/domain/oauth"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OAuthTokenLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOAuthTokenLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OAuthTokenLogic {
	return &OAuthTokenLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OAuthTokenLogic) OAuthToken(req *types.OAuthTokenReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.OAuth.Token(l.ctx, domainoauth.TokenCommand{
		GrantType: req.GrantType, Code: req.Code, RedirectURI: req.RedirectUri, ClientID: req.ClientId,
		CodeVerifier: req.CodeVerifier,
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

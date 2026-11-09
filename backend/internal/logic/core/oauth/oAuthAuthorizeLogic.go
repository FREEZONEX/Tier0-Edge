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

type OAuthAuthorizeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOAuthAuthorizeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OAuthAuthorizeLogic {
	return &OAuthAuthorizeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OAuthAuthorizeLogic) OAuthAuthorize(req *types.OAuthAuthorizeReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.OAuth.Authorize(l.ctx, domainoauth.AuthorizeCommand{
		ResponseType: req.ResponseType, ClientID: req.ClientId, RedirectURI: req.RedirectUri, Scope: req.Scope,
		State: req.State, CodeChallenge: req.CodeChallenge, CodeChallengeMethod: req.CodeChallengeMethod,
		UserID: logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

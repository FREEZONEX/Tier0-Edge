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

type OAuthClientCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOAuthClientCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OAuthClientCreateLogic {
	return &OAuthClientCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OAuthClientCreateLogic) OAuthClientCreate(req *types.OAuthClientReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.OAuth.CreateClient(l.ctx, domainoauth.ClientCommand{
		ClientName: req.ClientName, RedirectURIs: req.RedirectUris, AllowedOrigins: req.AllowedOrigins,
		AppID: req.AppId, UserID: logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

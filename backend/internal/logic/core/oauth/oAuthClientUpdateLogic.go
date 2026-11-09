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

type OAuthClientUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOAuthClientUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OAuthClientUpdateLogic {
	return &OAuthClientUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OAuthClientUpdateLogic) OAuthClientUpdate(req *types.OAuthClientReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.OAuth.UpdateClient(l.ctx, domainoauth.ClientCommand{
		ID: req.Id, ClientName: req.ClientName, RedirectURIs: req.RedirectUris, AllowedOrigins: req.AllowedOrigins,
		AppID: req.AppId, UserID: logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

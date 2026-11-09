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

type OAuthClientListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOAuthClientListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OAuthClientListLogic {
	return &OAuthClientListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OAuthClientListLogic) OAuthClientList(req *types.PageReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.OAuth.ListClients(l.ctx)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

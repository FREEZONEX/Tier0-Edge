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

type OAuthClientDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOAuthClientDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OAuthClientDeleteLogic {
	return &OAuthClientDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *OAuthClientDeleteLogic) OAuthClientDelete(req *types.IdReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.OAuth.DeleteClient(l.ctx, req.Id)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

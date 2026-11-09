// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package uns

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UnsRecycleListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUnsRecycleListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UnsRecycleListLogic {
	return &UnsRecycleListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UnsRecycleListLogic) UnsRecycleList(req *types.PageReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.UNS.Recycle(l.ctx)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

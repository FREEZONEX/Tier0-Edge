// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package project

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ProjectDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectDetailLogic {
	return &ProjectDetailLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProjectDetailLogic) ProjectDetail(req *types.IdReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Project.Detail(l.ctx, req.Id, logicx.UserID(l.ctx))
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

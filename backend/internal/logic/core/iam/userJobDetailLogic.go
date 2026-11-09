package iam

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UserJobDetailLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserJobDetailLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserJobDetailLogic {
	return &UserJobDetailLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *UserJobDetailLogic) UserJobDetail(req *types.UserJobReq) (*types.Envelope, error) {
	data, err := l.svcCtx.App.IAM.GetUserJob(l.ctx, req.JobKey, logicx.UserID(l.ctx))
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

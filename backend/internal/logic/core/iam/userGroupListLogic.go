package iam

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UserGroupListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserGroupListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserGroupListLogic {
	return &UserGroupListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserGroupListLogic) UserGroupList(req *types.PageReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.IAM.ListUserGroups(l.ctx, req.Keyword, req.PageNo, req.PageSize)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

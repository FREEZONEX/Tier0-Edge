package iam

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UserGroupDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserGroupDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserGroupDeleteLogic {
	return &UserGroupDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserGroupDeleteLogic) UserGroupDelete(req *types.UserGroupIdReq) (resp *types.Envelope, err error) {
	if err := l.svcCtx.App.IAM.DeleteUserGroup(l.ctx, req.GroupId, logicx.UserID(l.ctx)); err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"deleted": true}), nil
}

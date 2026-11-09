package iam

import (
	"context"

	domainiam "backend/internal/domain/iam"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UserGroupCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserGroupCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserGroupCreateLogic {
	return &UserGroupCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserGroupCreateLogic) UserGroupCreate(req *types.UserGroupSaveReq) (resp *types.Envelope, err error) {
	item, err := l.svcCtx.App.IAM.CreateUserGroup(l.ctx, domainiam.UserGroupSaveCommand{GroupName: req.GroupName, Description: req.Description, UserID: logicx.UserID(l.ctx)})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"group": item}), nil
}

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

type UserGroupUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserGroupUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserGroupUpdateLogic {
	return &UserGroupUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserGroupUpdateLogic) UserGroupUpdate(req *types.UserGroupSaveReq) (resp *types.Envelope, err error) {
	item, err := l.svcCtx.App.IAM.UpdateUserGroup(l.ctx, domainiam.UserGroupSaveCommand{GroupID: req.GroupId, GroupName: req.GroupName, Description: req.Description, UserID: logicx.UserID(l.ctx)})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"group": item}), nil
}

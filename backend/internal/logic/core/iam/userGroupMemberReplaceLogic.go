package iam

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UserGroupMemberReplaceLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserGroupMemberReplaceLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserGroupMemberReplaceLogic {
	return &UserGroupMemberReplaceLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *UserGroupMemberReplaceLogic) UserGroupMemberReplace(req *types.UserGroupMembersSaveReq) (resp *types.Envelope, err error) {
	if err := l.svcCtx.App.IAM.ReplaceUserGroupMembers(l.ctx, req.GroupId, req.UserIds, logicx.UserID(l.ctx)); err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"updated": true}), nil
}

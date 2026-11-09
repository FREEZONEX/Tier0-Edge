package project

import (
	"context"

	domainproject "backend/internal/domain/project"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ProjectMemberGroupCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectMemberGroupCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectMemberGroupCreateLogic {
	return &ProjectMemberGroupCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProjectMemberGroupCreateLogic) ProjectMemberGroupCreate(req *types.ProjectMemberGroupSaveReq) (resp *types.Envelope, err error) {
	item, err := l.svcCtx.App.Project.AddMemberGroup(l.ctx, domainproject.MemberGroupSaveCommand{ProjectID: req.ProjectId, GroupID: req.GroupId, RoleIDs: req.RoleIds, OperatorUserID: logicx.UserID(l.ctx)})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"group": item}), nil
}

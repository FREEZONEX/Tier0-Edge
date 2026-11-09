package project

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ProjectMemberGroupDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectMemberGroupDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectMemberGroupDeleteLogic {
	return &ProjectMemberGroupDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProjectMemberGroupDeleteLogic) ProjectMemberGroupDelete(req *types.ProjectMemberGroupReq) (resp *types.Envelope, err error) {
	if err := l.svcCtx.App.Project.DeleteMemberGroup(l.ctx, req.ProjectId, req.ProjectGroupId, logicx.UserID(l.ctx)); err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"deleted": true}), nil
}

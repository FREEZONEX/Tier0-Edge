package project

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ProjectMemberGroupListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectMemberGroupListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectMemberGroupListLogic {
	return &ProjectMemberGroupListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProjectMemberGroupListLogic) ProjectMemberGroupList(req *types.ProjectAppReq) (resp *types.Envelope, err error) {
	items, err := l.svcCtx.App.Project.ListMemberGroups(l.ctx, req.ProjectId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"list": items, "total": len(items)}), nil
}

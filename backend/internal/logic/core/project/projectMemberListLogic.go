// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package project

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ProjectMemberListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectMemberListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectMemberListLogic {
	return &ProjectMemberListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProjectMemberListLogic) ProjectMemberList(req *types.ProjectAppReq) (resp *types.Envelope, err error) {
	// todo: add your logic here and delete this line

	return
}

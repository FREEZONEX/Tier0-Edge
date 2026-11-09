// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package project

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ProjectMemberUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectMemberUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectMemberUpdateLogic {
	return &ProjectMemberUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProjectMemberUpdateLogic) ProjectMemberUpdate(req *types.ProjectMemberSaveReq) (resp *types.Envelope, err error) {
	// todo: add your logic here and delete this line

	return
}

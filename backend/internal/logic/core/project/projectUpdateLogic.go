// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

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

type ProjectUpdateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectUpdateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectUpdateLogic {
	return &ProjectUpdateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProjectUpdateLogic) ProjectUpdate(req *types.ProjectSaveReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Project.Update(l.ctx, domainproject.SaveCommand{
		ID: req.Id, Name: req.Name, Description: req.Description, UserID: logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

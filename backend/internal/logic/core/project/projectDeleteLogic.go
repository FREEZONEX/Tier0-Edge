// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package project

import (
	"context"
	"strconv"

	auditdomain "backend/internal/domain/audit"
	domainproject "backend/internal/domain/project"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/repo"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ProjectDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectDeleteLogic {
	return &ProjectDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProjectDeleteLogic) ProjectDelete(req *types.IdReq) (resp *types.Envelope, err error) {
	userID := logicx.UserID(l.ctx)
	if err := l.svcCtx.App.Project.CheckManage(l.ctx, req.Id, userID); err != nil {
		return nil, logicx.Error(err)
	}

	// 项目下仍有应用时不允许删除
	apps, err := repo.NewProjectRepo(l.ctx).ListProjectApps(l.ctx, req.Id)
	if err != nil {
		return nil, logicx.Error(err)
	}
	if len(apps) > 0 {
		return nil, logicx.Error(domainproject.ErrProjectHasApps)
	}

	data, err := l.svcCtx.App.Project.Delete(l.ctx, req.Id, userID)
	if err != nil {
		return nil, logicx.Error(err)
	}
	projectName, _ := data["name"].(string)
	l.svcCtx.App.Audit.Record(l.ctx, auditdomain.RecordInput{
		ScopeType:    auditdomain.ScopeTypePlatform,
		ResType:      auditdomain.ResTypeProject,
		ResID:        strconv.FormatInt(req.Id, 10),
		ResName:      projectName,
		BusinessType: auditdomain.BizDelete,
		Detail:       map[string]any{"projectId": req.Id, "projectName": projectName},
	})
	return respx.Envelope(data), nil
}

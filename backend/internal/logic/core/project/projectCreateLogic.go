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

type ProjectCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewProjectCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ProjectCreateLogic {
	return &ProjectCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ProjectCreateLogic) ProjectCreate(req *types.ProjectSaveReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.Project.Create(l.ctx, domainproject.SaveCommand{
		Name: req.Name, Description: req.Description, UserID: logicx.UserID(l.ctx),
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	if projectItem, ok := data["project"].(repo.Project); ok {
		if projectItem.ID > 0 {
			l.svcCtx.App.Audit.Record(l.ctx, auditdomain.RecordInput{
				ScopeType:      auditdomain.ScopeTypePlatform,
				ResType:        auditdomain.ResTypeProject,
				ResID:          strconv.FormatInt(projectItem.ID, 10),
				ResName:        projectItem.Name,
				BusinessType:   auditdomain.BizCreate,
				IsShowInRecent: auditdomain.ShowInRecentYes,
				Detail:         map[string]any{"projectId": projectItem.ID},
			})
		}
	}
	return respx.Envelope(data), nil
}

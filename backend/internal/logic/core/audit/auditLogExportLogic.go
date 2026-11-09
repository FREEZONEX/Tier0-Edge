// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package audit

import (
	"backend/internal/contextx"
	domainaudit "backend/internal/domain/audit"
	respx "backend/internal/httpx"
	"context"
	"net/http"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AuditLogExportLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAuditLogExportLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AuditLogExportLogic {
	return &AuditLogExportLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AuditLogExportLogic) AuditLogExport(req *types.AuditLogPageReq) (resp *types.Envelope, err error) {
	subject, _ := contextx.SubjectFrom(l.ctx)
	if !contextx.HasResource(subject, "system.audit.log") {
		return nil, respx.NewHTTPError(http.StatusForbidden, "permission denied")
	}
	data, err := l.svcCtx.App.Audit.Export(l.ctx, toPageQuery(req))
	if err != nil {
		return nil, queryError(err)
	}
	l.svcCtx.App.Audit.Record(l.ctx, domainaudit.RecordInput{ResType: "AuditLog", BusinessType: domainaudit.BizExport, ResName: "AuditLog", Detail: map[string]any{"count": data["total"]}})
	return respx.Envelope(data), nil
}

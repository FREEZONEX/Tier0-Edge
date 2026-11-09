// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package auth

import (
	"context"

	"backend/internal/contextx"
	auditdomain "backend/internal/domain/audit"
	loginlogdomain "backend/internal/domain/loginlog"
	respx "backend/internal/httpx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type LogoutLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLogoutLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LogoutLogic {
	return &LogoutLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *LogoutLogic) Logout() (resp *types.Envelope, err error) {
	// 登出事件与登录一样只进登录日志（event_type=logout），不写操作审计。
	if subject, ok := contextx.SubjectFrom(l.ctx); ok {
		ip, userAgent, _ := auditdomain.RequestInfoFrom(l.ctx)
		l.svcCtx.App.LoginLog.Record(loginlogdomain.RecordInput{
			EventType: loginlogdomain.EventLogout,
			UserID:    subject.UserID,
			UserName:  subject.UserName,
			IP:        ip,
			UserAgent: userAgent,
			Code:      200,
		})
	}
	return respx.Empty(), nil
}

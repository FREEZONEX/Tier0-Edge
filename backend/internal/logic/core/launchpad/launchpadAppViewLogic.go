// Code scaffolded by goctl. Safe to edit.

package launchpad

import (
	"context"
	"strconv"

	respx "backend/internal/httpx"
	auditdomain "backend/internal/domain/audit"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type LaunchpadAppViewLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLaunchpadAppViewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LaunchpadAppViewLogic {
	return &LaunchpadAppViewLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *LaunchpadAppViewLogic) LaunchpadAppView(req *types.LaunchpadAppViewReq) (resp *types.Envelope, err error) {
	userID := logicx.UserID(l.ctx)
	if err := l.svcCtx.App.Launchpad.RecordAppView(l.ctx, userID, req.AppID); err != nil {
		return nil, logicx.Error(err)
	}
	l.svcCtx.App.Audit.Record(l.ctx, auditdomain.RecordInput{
		ScopeType:      auditdomain.ScopeTypePlatform,
		ResType:        auditdomain.ResTypeProjectApp,
		ResID:          strconv.FormatInt(req.AppID, 10),
		BusinessType:   auditdomain.BizView,
		IsShowInRecent: auditdomain.ShowInRecentYes,
		Detail:         map[string]any{"appId": req.AppID},
	})
	return respx.Envelope(nil), nil
}

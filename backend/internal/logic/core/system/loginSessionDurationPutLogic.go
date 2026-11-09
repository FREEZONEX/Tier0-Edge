// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package system

import (
	"context"

	domainaudit "backend/internal/domain/audit"
	domainauth "backend/internal/domain/auth"
	respx "backend/internal/httpx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type LoginSessionDurationPutLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLoginSessionDurationPutLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginSessionDurationPutLogic {
	return &LoginSessionDurationPutLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// LoginSessionDurationPut 保存登录有效期设置;仅影响新签发的登录会话。
func (l *LoginSessionDurationPutLogic) LoginSessionDurationPut(req *types.SystemLoginSessionDurationReq) (resp *types.Envelope, err error) {
	if !domainauth.IsLoginSessionDuration(req.Duration) {
		return nil, respx.NewHTTPError(400, "invalid login session duration")
	}
	settingRepo := repo.NewSystemSettingRepo(l.ctx)
	if err := settingRepo.PutSetting(l.ctx, domainauth.SystemSettingKeyLoginSessionDuration, req.Duration); err != nil {
		l.Errorf("save login session duration %s failed: %v", req.Duration, err)
		return nil, err
	}
	l.svcCtx.App.Audit.Record(l.ctx, domainaudit.RecordInput{
		ScopeType:    domainaudit.ScopeTypePlatform,
		ResType:      domainaudit.ResTypePlatform,
		ResName:      domainauth.SystemSettingKeyLoginSessionDuration,
		BusinessType: domainaudit.BizSave,
		Detail:       map[string]any{"duration": req.Duration},
	})
	return respx.Envelope(map[string]any{
		"duration": req.Duration,
	}), nil
}

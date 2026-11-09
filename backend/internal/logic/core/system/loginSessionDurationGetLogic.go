// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package system

import (
	"context"
	"errors"

	domainauth "backend/internal/domain/auth"
	respx "backend/internal/httpx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type LoginSessionDurationGetLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLoginSessionDurationGetLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginSessionDurationGetLogic {
	return &LoginSessionDurationGetLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// LoginSessionDurationGet 返回登录有效期设置;从未写入过时返回默认值(12h)。
func (l *LoginSessionDurationGetLogic) LoginSessionDurationGet() (resp *types.Envelope, err error) {
	settingRepo := repo.NewSystemSettingRepo(l.ctx)
	value, err := settingRepo.GetSetting(l.ctx, domainauth.SystemSettingKeyLoginSessionDuration)
	if err != nil {
		if !errors.Is(err, repo.ErrNotFound) {
			return nil, err
		}
		value = domainauth.DefaultLoginSessionDuration
	}
	return respx.Envelope(map[string]any{
		"duration": value,
	}), nil
}

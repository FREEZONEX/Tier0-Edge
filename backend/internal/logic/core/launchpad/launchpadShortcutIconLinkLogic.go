// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package launchpad

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type LaunchpadShortcutIconLinkLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLaunchpadShortcutIconLinkLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LaunchpadShortcutIconLinkLogic {
	return &LaunchpadShortcutIconLinkLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// LaunchpadShortcutIconLink 给当前用户签发一条短期免登录取图地址，供移动端
// 「添加到主屏幕」的引导页当 apple-touch-icon 用（见 domain 层 shortcuticon.go）。
func (l *LaunchpadShortcutIconLinkLogic) LaunchpadShortcutIconLink(req *types.LaunchpadAppViewReq) (*types.Envelope, error) {
	url, err := l.svcCtx.App.Launchpad.ShortcutIconLink(l.ctx, logicx.UserID(l.ctx), req.AppID)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"url": url}), nil
}

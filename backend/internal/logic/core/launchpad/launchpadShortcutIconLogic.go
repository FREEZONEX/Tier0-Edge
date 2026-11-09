// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package launchpad

import (
	"context"

	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type LaunchpadShortcutIconLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLaunchpadShortcutIconLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LaunchpadShortcutIconLogic {
	return &LaunchpadShortcutIconLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// LaunchpadShortcutIcon 校验签名后返回图标资产 id；这条路径没有登录态，授权全靠
// 签名（见 domain 层 shortcuticon.go）。handler 拿到 id 后直接流式回图片。
func (l *LaunchpadShortcutIconLogic) LaunchpadShortcutIcon(req *types.LaunchpadShortcutIconReq) (int64, error) {
	assetID, err := l.svcCtx.App.Launchpad.ShortcutIconAssetID(l.ctx, req.AppID, req.Expires, req.Signature)
	if err != nil {
		return 0, logicx.Error(err)
	}
	return assetID, nil
}

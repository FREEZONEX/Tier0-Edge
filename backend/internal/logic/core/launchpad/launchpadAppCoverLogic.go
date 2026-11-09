// Code scaffolded by goctl. Safe to edit.
// goctl 1.10.1

package launchpad

import (
	"context"

	"backend/internal/domain/launchpad"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type LaunchpadAppCoverLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLaunchpadAppCoverLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LaunchpadAppCoverLogic {
	return &LaunchpadAppCoverLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// LaunchpadAppCover 返回该 App 的封面资产 id；鉴权与归属校验在 domain 层完成，
// handler 拿到 id 后直接流式回图片（见 launchpadAppCoverHandler.go）。
func (l *LaunchpadAppCoverLogic) LaunchpadAppCover(req *types.LaunchpadAppViewReq) (int64, error) {
	assetID, err := l.svcCtx.App.Launchpad.AppMediaAssetID(l.ctx, logicx.UserID(l.ctx), req.AppID, launchpad.MediaKindCover)
	if err != nil {
		return 0, logicx.Error(err)
	}
	return assetID, nil
}

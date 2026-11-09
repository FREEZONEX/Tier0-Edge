// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package asset

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type AssetMultipartAbortLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewAssetMultipartAbortLogic(ctx context.Context, svcCtx *svc.ServiceContext) *AssetMultipartAbortLogic {
	return &AssetMultipartAbortLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *AssetMultipartAbortLogic) AssetMultipartAbort(req *types.AssetMultipartAbortReq) (resp *types.Envelope, err error) {
	// 先取 temp 记录拿到所属项目，复检会话用户对该项目的管理权限（与 init 一致），
	// 避免用户失去项目权限后仍能中止/清理该项目作用域的分片会话。
	item, err := l.svcCtx.App.Asset.ResolveMultipartTemp(l.ctx, req.FileKey, req.UploadId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	if err := l.svcCtx.App.Project.CheckManage(l.ctx, item.ProjectID, logicx.UserID(l.ctx)); err != nil {
		return nil, logicx.Error(err)
	}
	aborted, err := l.svcCtx.App.Asset.MultipartAbort(l.ctx, req.FileKey, req.UploadId)
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(map[string]any{"aborted": aborted}), nil
}

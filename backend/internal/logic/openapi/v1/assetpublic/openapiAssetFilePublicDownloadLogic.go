// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package assetpublic

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiAssetFilePublicDownloadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiAssetFilePublicDownloadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiAssetFilePublicDownloadLogic {
	return &OpenapiAssetFilePublicDownloadLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// OpenapiAssetFilePublicDownload 保留脚手架桩：公开下载由自定义 handler 直接流式输出，
// 不经过 go-zero 默认的 JSON 响应流程。
func (l *OpenapiAssetFilePublicDownloadLogic) OpenapiAssetFilePublicDownload(req *types.OpenapiAssetFileDownloadReq) error {
	return nil
}

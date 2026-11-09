// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package asset

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type OpenapiAssetFileDownloadLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewOpenapiAssetFileDownloadLogic(ctx context.Context, svcCtx *svc.ServiceContext) *OpenapiAssetFileDownloadLogic {
	return &OpenapiAssetFileDownloadLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// OpenapiAssetFileDownload 保留脚手架桩：实际下载由自定义 handler 直接流式输出（302/代理流），
// 不经过 go-zero 默认的 JSON 响应流程。
func (l *OpenapiAssetFileDownloadLogic) OpenapiAssetFileDownload(req *types.OpenapiAssetFileDownloadReq) error {
	return nil
}

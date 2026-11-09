package iam

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type UserExportPreviewLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewUserExportPreviewLogic(ctx context.Context, svcCtx *svc.ServiceContext) *UserExportPreviewLogic {
	return &UserExportPreviewLogic{Logger: logx.WithContext(ctx), ctx: ctx, svcCtx: svcCtx}
}

func (l *UserExportPreviewLogic) UserExportPreview(req *types.UserExportReq) (*types.Envelope, error) {
	data, err := l.svcCtx.App.IAM.PreviewUserExport(l.ctx, toUserExportRequest(req), logicx.UserID(l.ctx))
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

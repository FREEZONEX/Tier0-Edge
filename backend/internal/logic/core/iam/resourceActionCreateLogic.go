// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package iam

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ResourceActionCreateLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewResourceActionCreateLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResourceActionCreateLogic {
	return &ResourceActionCreateLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResourceActionCreateLogic) ResourceActionCreate(req *types.ResourceActionSaveReq) (resp *types.Envelope, err error) {
	// todo: add your logic here and delete this line

	return
}

// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package iam

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ResourceActionDeleteLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewResourceActionDeleteLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResourceActionDeleteLogic {
	return &ResourceActionDeleteLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResourceActionDeleteLogic) ResourceActionDelete(req *types.ResourceActionIdReq) (resp *types.Envelope, err error) {
	// todo: add your logic here and delete this line

	return
}

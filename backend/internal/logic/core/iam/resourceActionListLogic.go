// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package iam

import (
	"context"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ResourceActionListLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewResourceActionListLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ResourceActionListLogic {
	return &ResourceActionListLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ResourceActionListLogic) ResourceActionList(req *types.ResourceIdReq) (resp *types.Envelope, err error) {
	// todo: add your logic here and delete this line

	return
}

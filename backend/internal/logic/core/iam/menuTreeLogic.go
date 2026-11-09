// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package iam

import (
	"context"

	respx "backend/internal/httpx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type MenuTreeLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewMenuTreeLogic(ctx context.Context, svcCtx *svc.ServiceContext) *MenuTreeLogic {
	return &MenuTreeLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *MenuTreeLogic) MenuTree() (resp *types.Envelope, err error) {
	menus, err := l.svcCtx.App.IAM.Menus(l.ctx)
	if err != nil {
		return nil, err
	}
	menus = filterResourcesByLicense(menus)
	return respx.Envelope(menus), nil
}

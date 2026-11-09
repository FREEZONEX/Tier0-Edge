// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package loginlog

import (
	"context"

	domainloginlog "backend/internal/domain/loginlog"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type LoginLogPageLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLoginLogPageLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginLogPageLogic {
	return &LoginLogPageLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *LoginLogPageLogic) LoginLogPage(req *types.LoginLogPageReq) (resp *types.Envelope, err error) {
	data, err := l.svcCtx.App.LoginLog.Page(l.ctx, domainloginlog.PageQuery{
		PageNo:    req.PageNo,
		PageSize:  req.PageSize,
		Keyword:   req.Keyword,
		UserName:  req.UserName,
		IpAddr:    req.IpAddr,
		EventType: req.EventType,
		Code:      req.Code,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	})
	if err != nil {
		return nil, logicx.Error(err)
	}
	return respx.Envelope(data), nil
}

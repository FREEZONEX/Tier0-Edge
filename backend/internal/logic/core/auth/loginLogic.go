// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2-1

package auth

import (
	"context"

	auditdomain "backend/internal/domain/audit"
	loginlogdomain "backend/internal/domain/loginlog"
	respx "backend/internal/httpx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type LoginLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewLoginLogic(ctx context.Context, svcCtx *svc.ServiceContext) *LoginLogic {
	return &LoginLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// 登录事件只进登录日志（sys_user_login_log，含 IP/浏览器/OS 明细），不写操作审计。
func (l *LoginLogic) Login(req *types.LoginReq) (resp *types.Envelope, err error) {
	result, err := l.svcCtx.App.Auth.Login(l.ctx, req.Username, req.Password)
	ip, userAgent, _ := auditdomain.RequestInfoFrom(l.ctx)
	if err != nil {
		l.svcCtx.App.LoginLog.Record(loginlogdomain.RecordInput{
			EventType: loginlogdomain.EventLogin,
			UserName:  req.Username,
			IP:        ip,
			UserAgent: userAgent,
			Code:      401,
			Msg:       "invalid username or password",
		})
		return nil, respx.NewHTTPError(401, "invalid username or password")
	}
	l.svcCtx.App.LoginLog.Record(loginlogdomain.RecordInput{
		EventType: loginlogdomain.EventLogin,
		UserID:    result.User.ID,
		UserName:  result.User.UserName,
		IP:        ip,
		UserAgent: userAgent,
		Code:      200,
	})
	return respx.Envelope(types.LoginResp{
		Token:               result.Token,
		UserId:              result.User.ID,
		UserName:            result.User.UserName,
		NickName:            result.User.NickName,
		Email:               result.User.Email,
		Phone:               result.User.Phone,
		RoleCode:            l.svcCtx.App.IAM.UserRoleCode(l.ctx, result.User.ID),
		IsRandomPwd:         result.User.IsRandomPwd,
		ForceChangePassword: result.ForceChangePassword,
		ResourceKeys:        result.ResourceKeys,
		UiActions:           result.UIActions,
		SessionExpireAt:     result.SessionExpireAt,
	}), nil
}

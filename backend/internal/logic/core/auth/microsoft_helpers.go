package auth

import (
	"context"
	"errors"

	auditdomain "backend/internal/domain/audit"
	domainauth "backend/internal/domain/auth"
	loginlogdomain "backend/internal/domain/loginlog"
	respx "backend/internal/httpx"
	"backend/internal/logic/logicx"
	"backend/internal/svc"
	"backend/internal/types"
)

func microsoftLoginEnvelope(ctx context.Context, svcCtx *svc.ServiceContext, result domainauth.LoginResult) *types.Envelope {
	recordMicrosoftLogin(ctx, svcCtx, result)
	return respx.Envelope(types.LoginResp{
		Token: result.Token, UserId: result.User.ID, UserName: result.User.UserName,
		NickName: result.User.NickName, Email: result.User.Email, Phone: result.User.Phone,
		RoleCode: svcCtx.App.IAM.UserRoleCode(ctx, result.User.ID), IsRandomPwd: result.User.IsRandomPwd,
		ForceChangePassword: result.ForceChangePassword, ResourceKeys: result.ResourceKeys,
		UiActions: result.UIActions, SessionExpireAt: result.SessionExpireAt,
	})
}

// recordMicrosoftLogin 把 Microsoft SSO 登录写入登录日志（与密码登录同口径），不写操作审计。
func recordMicrosoftLogin(ctx context.Context, svcCtx *svc.ServiceContext, result domainauth.LoginResult) {
	ip, userAgent, _ := auditdomain.RequestInfoFrom(ctx)
	svcCtx.App.LoginLog.Record(loginlogdomain.RecordInput{
		EventType: loginlogdomain.EventLogin,
		UserID:    result.User.ID,
		UserName:  result.User.UserName,
		IP:        ip,
		UserAgent: userAgent,
		Code:      200,
	})
}

func microsoftHTTPError(err error) error {
	switch {
	case errors.Is(err, domainauth.ErrMicrosoftSSODisabled):
		return respx.NewHTTPError(404, err.Error())
	case errors.Is(err, domainauth.ErrMicrosoftStateInvalid),
		errors.Is(err, domainauth.ErrMicrosoftIdentityInvalid),
		errors.Is(err, domainauth.ErrMicrosoftPendingInvalid),
		errors.Is(err, domainauth.ErrMicrosoftUsernameInvalid):
		return respx.NewHTTPError(400, err.Error())
	case errors.Is(err, domainauth.ErrMicrosoftLinkUnauthorized):
		return respx.NewHTTPError(401, err.Error())
	case errors.Is(err, domainauth.ErrMicrosoftLinkAttempts):
		return respx.NewHTTPError(429, err.Error())
	case errors.Is(err, domainauth.ErrMicrosoftUsernameTaken),
		errors.Is(err, domainauth.ErrMicrosoftIdentityConflict),
		errors.Is(err, domainauth.ErrMicrosoftRegistrationBusy):
		return respx.NewHTTPError(409, err.Error())
	case errors.Is(err, domainauth.ErrMicrosoftSSOUnavailable):
		return respx.NewHTTPError(503, domainauth.ErrMicrosoftSSOUnavailable.Error())
	default:
		return logicx.Error(err)
	}
}

func MicrosoftBrowserErrorCode(err error) string {
	switch {
	case errors.Is(err, domainauth.ErrMicrosoftSSODisabled):
		return "disabled"
	case errors.Is(err, domainauth.ErrMicrosoftStateInvalid):
		return "invalid_state"
	case errors.Is(err, domainauth.ErrMicrosoftIdentityInvalid):
		return "identity_rejected"
	case errors.Is(err, domainauth.ErrMicrosoftIdentityConflict):
		return "identity_conflict"
	default:
		return "unavailable"
	}
}

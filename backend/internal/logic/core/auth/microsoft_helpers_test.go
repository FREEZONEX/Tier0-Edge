// recordMicrosoftLogin 单元测试：
//
//	目标：Microsoft SSO 登录与密码登录同口径，写入登录日志（event_type=login），
//	      不再写操作审计（审计断言由中间件链路覆盖，此处验证落库结果）。
//	正常路径：给定 LoginResult 后异步落库，user_id/user_name/event_type 正确。
//	异常情况：无 audit state 时 IP/UA 为空仍应正常落库。
package auth

import (
	"testing"
	"time"

	domainauth "backend/internal/domain/auth"
	"backend/internal/repo"
	"backend/internal/svc/svctest"

	"github.com/stretchr/testify/require"
)

func TestRecordMicrosoftLogin_WritesLoginLog(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	recordMicrosoftLogin(ctx, svcCtx, domainauth.LoginResult{
		User:  repo.User{ID: 1, UserName: "tier0"},
		Token: "test-token",
	})

	loginLogRepo := repo.NewUserLoginLogRepo(ctx)
	require.Eventually(t, func() bool {
		items, total, err := loginLogRepo.ListUserLoginLogs(ctx, repo.UserLoginLogFilter{
			EventType: repo.UserLoginEventLogin,
			PageNo:    1,
			PageSize:  10,
		})
		if err != nil || total == 0 {
			return false
		}
		for _, item := range items {
			if item.UserID == 1 && item.UserName == "tier0" {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "expected a microsoft sso login-log row")
}

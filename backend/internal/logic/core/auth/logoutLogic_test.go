// TASK-028 LogoutLogic 单元测试：
//
//	目标：覆盖登出接口（有/无登录 Subject 均成功返回空 Envelope）。
//	正常路径：登录状态下登出返回成功空响应；未登录 ctx 调用同样返回成功（登出幂等）。
//	异常情况：无（该接口不返回错误）。
//
// 登录日志补充用例：带 Subject 登出应落一条 event_type=logout 的登录日志。
package auth

import (
	"context"
	"testing"

	"backend/internal/repo"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLogout_WithSubject(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewLogoutLogic(ctx, svcCtx)

	resp, err := l.Logout()
	require.NoError(t, err)
	requireOK(t, resp)
}

func TestLogout_WithoutSubject(t *testing.T) {
	svcCtx, _ := newSvcEnv(t)
	l := NewLogoutLogic(context.Background(), svcCtx)

	resp, err := l.Logout()
	require.NoError(t, err)
	requireOK(t, resp)
}

// 带 Subject 的登出应异步落一条 event_type=logout 的登录日志。
func TestLogout_LoginLogLogoutEvent(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewLogoutLogic(ctx, svcCtx)

	resp, err := l.Logout()
	require.NoError(t, err)
	requireOK(t, resp)

	loginLogRepo := repo.NewUserLoginLogRepo(ctx)
	require.Eventually(t, func() bool {
		items, total, listErr := loginLogRepo.ListUserLoginLogs(ctx, repo.UserLoginLogFilter{
			EventType: repo.UserLoginEventLogout,
			PageNo:    1,
			PageSize:  10,
		})
		if listErr != nil || total == 0 {
			return false
		}
		for _, item := range items {
			if item.EventType == repo.UserLoginEventLogout && item.UserID == 1 {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "expected a logout login-log row")
}

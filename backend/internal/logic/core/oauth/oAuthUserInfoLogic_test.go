// TASK-028 OAuthUserInfoLogic 单元测试：
//
//	目标：覆盖当前登录用户的 OIDC 风格用户信息接口。
//	正常路径：seed 内置 tier0 用户（id=1）返回 userId/userName/nickName 等字段，
//	          且补全 OIDC 标准 sub 字段（等于 userId）。
//	异常情况：不存在的用户 → 404。
package oauth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOAuthUserInfo_SeedAdmin(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewOAuthUserInfoLogic(ctx, svcCtx)

	resp, err := l.OAuthUserInfo()
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, int64(1), view["userId"])
	require.Equal(t, "tier0", view["userName"])
	require.NotEmpty(t, view["nickName"])
	require.Equal(t, view["userId"], view["sub"])
	require.NotNil(t, view["roleList"])
}

func TestOAuthUserInfo_NotFound(t *testing.T) {
	svcCtx := newSvcEnvNoUser(t)
	l := NewOAuthUserInfoLogic(contextWithUser(t, 999999), svcCtx)

	_, err := l.OAuthUserInfo()
	requireHTTPError(t, err, 404)
}

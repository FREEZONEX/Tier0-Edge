// TASK-028 CurrentUserConfigGetLogic 单元测试：
//   目标：覆盖当前用户个性化配置查询。
//   正常路径：登录用户返回 userId/homePage/mainLanguage；用户从未配置过时 homePage 回退默认值。
//   异常情况：ctx 无登录 Subject 时返回 401。
package auth

import (
	"context"
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestCurrentUserConfigGet_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)

	// 预置配置：直接经 repo 落库，模拟用户已设置过主页与语言
	home := "/home"
	lang := "en-US"
	_, err := repo.NewUserConfigRepo(ctx).UpdateUserConfig(ctx, 1, &home, &lang)
	require.NoError(t, err)

	l := NewCurrentUserConfigGetLogic(ctx, svcCtx)
	resp, err := l.CurrentUserConfigGet()
	require.NoError(t, err)
	data, ok := resp.Data.(types.CurrentUserConfigResp)
	require.True(t, ok, "data should be CurrentUserConfigResp")
	require.Equal(t, int64(1), data.UserId)
	require.Equal(t, "/home", data.HomePage)
	require.Equal(t, "en-US", data.MainLanguage)
}

func TestCurrentUserConfigGet_DefaultHomePageWhenUnset(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewCurrentUserConfigGetLogic(ctx, svcCtx)
	resp, err := l.CurrentUserConfigGet()
	require.NoError(t, err)
	requireOK(t, resp)
	data, ok := resp.Data.(types.CurrentUserConfigResp)
	require.True(t, ok)
	require.Equal(t, int64(1), data.UserId)
	require.NotEmpty(t, data.HomePage)
}

func TestCurrentUserConfigGet_Unauthorized(t *testing.T) {
	svcCtx, _ := newSvcEnv(t)
	l := NewCurrentUserConfigGetLogic(context.Background(), svcCtx)
	_, err := l.CurrentUserConfigGet()
	requireHTTPError(t, err, 401)
}

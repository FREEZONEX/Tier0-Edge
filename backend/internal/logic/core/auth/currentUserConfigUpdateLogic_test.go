// TASK-028 CurrentUserConfigUpdateLogic 单元测试：
//   目标：覆盖当前用户个性化配置的 upsert 更新（homePage/mainLanguage 均可选，逐字段生效）。
//   正常路径：仅更新主页、仅更新语言、两者同时更新，均落库 sys_user_config 且响应为最新配置。
//   异常情况：ctx 无登录 Subject 时返回 401。
package auth

import (
	"context"
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestCurrentUserConfigUpdate_SetHomePageAndLanguage(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewCurrentUserConfigUpdateLogic(ctx, svcCtx)

	resp, err := l.CurrentUserConfigUpdate(&types.CurrentUserConfigReq{
		HomePage:     "/home",
		MainLanguage: "zh-CN",
	})
	require.NoError(t, err)
	data, ok := resp.Data.(types.CurrentUserConfigResp)
	require.True(t, ok, "data should be CurrentUserConfigResp")
	require.Equal(t, "/home", data.HomePage)
	require.Equal(t, "zh-CN", data.MainLanguage)

	var row repo.UserConfig
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("user_id = ?", 1).First(&row).Error)
	require.Equal(t, "/home", row.HomePage)
	require.Equal(t, "zh-CN", row.MainLanguage)
}

func TestCurrentUserConfigUpdate_OnlyHomePageKeepsLanguage(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)

	// 先落库一份含语言的配置
	lang := "en-US"
	_, err := repo.NewUserConfigRepo(ctx).UpdateUserConfig(ctx, 1, nil, &lang)
	require.NoError(t, err)

	l := NewCurrentUserConfigUpdateLogic(ctx, svcCtx)
	resp, err := l.CurrentUserConfigUpdate(&types.CurrentUserConfigReq{HomePage: "/launchpad"})
	require.NoError(t, err)
	data, ok := resp.Data.(types.CurrentUserConfigResp)
	require.True(t, ok)
	require.Equal(t, "/launchpad", data.HomePage)
	// mainLanguage 未传，保持原值
	require.Equal(t, "en-US", data.MainLanguage)
}

func TestCurrentUserConfigUpdate_EmptyReqDefaults(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewCurrentUserConfigUpdateLogic(ctx, svcCtx)

	resp, err := l.CurrentUserConfigUpdate(&types.CurrentUserConfigReq{})
	require.NoError(t, err)
	data, ok := resp.Data.(types.CurrentUserConfigResp)
	require.True(t, ok)
	require.Equal(t, "/home", data.HomePage)
	require.Empty(t, data.MainLanguage)
}

func TestCurrentUserConfigUpdate_Unauthorized(t *testing.T) {
	svcCtx, _ := newSvcEnv(t)
	l := NewCurrentUserConfigUpdateLogic(context.Background(), svcCtx)
	_, err := l.CurrentUserConfigUpdate(&types.CurrentUserConfigReq{HomePage: "/home"})
	requireHTTPError(t, err, 401)
}

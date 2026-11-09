// TASK-028 CurrentUserConfigPatchLogic 单元测试：
//   目标：验证 PATCH 语义的配置更新只是委托给 CurrentUserConfigUpdateLogic，行为一致。
//   正常路径：合法请求经委托成功更新配置并返回最新值。
//   异常情况：ctx 无登录 Subject 时返回 401（委托路径透传）。
package auth

import (
	"context"
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestCurrentUserConfigPatch_DelegatesToUpdate(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewCurrentUserConfigPatchLogic(ctx, svcCtx)

	resp, err := l.CurrentUserConfigPatch(&types.CurrentUserConfigReq{MainLanguage: "ja-JP"})
	require.NoError(t, err)
	data, ok := resp.Data.(types.CurrentUserConfigResp)
	require.True(t, ok, "data should be CurrentUserConfigResp")
	require.Equal(t, "ja-JP", data.MainLanguage)
}

func TestCurrentUserConfigPatch_Unauthorized(t *testing.T) {
	svcCtx, _ := newSvcEnv(t)
	l := NewCurrentUserConfigPatchLogic(context.Background(), svcCtx)
	_, err := l.CurrentUserConfigPatch(&types.CurrentUserConfigReq{MainLanguage: "ja-JP"})
	requireHTTPError(t, err, 401)
}

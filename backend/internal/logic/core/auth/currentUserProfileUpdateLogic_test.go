// TASK-028 CurrentUserProfileUpdateLogic 单元测试：
//   目标：覆盖当前用户自助更新昵称/邮箱/手机号（自我更新路径，不触碰角色）。
//   正常路径：合法请求更新成功，sys_user_info 落库且响应返回最新用户信息。
//   异常情况：ctx 无登录 Subject（UserID<=0）时返回 401。
package auth

import (
	"context"
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestCurrentUserProfileUpdate_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewCurrentUserProfileUpdateLogic(ctx, svcCtx)

	resp, err := l.CurrentUserProfileUpdate(&types.CurrentUserProfileReq{
		FirstName: "Tier Zero",
		Email:     "tier0@example.com",
		Phone:     "13800000000",
	})
	require.NoError(t, err)
	requireOK(t, resp)
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok, "data should be map[string]any")
	require.Equal(t, "Tier Zero", data["nickName"])
	require.Equal(t, "tier0@example.com", data["email"])
	require.Equal(t, "13800000000", data["phone"])

	// 查库验证落库
	var row repo.User
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("user_id = ? AND deleted_time = 0", 1).First(&row).Error)
	require.Equal(t, "Tier Zero", row.NickName)
}

func TestCurrentUserProfileUpdate_EmptyFieldsKeepsExisting(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewCurrentUserProfileUpdateLogic(ctx, svcCtx)

	resp, err := l.CurrentUserProfileUpdate(&types.CurrentUserProfileReq{})
	require.NoError(t, err)
	requireOK(t, resp)
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	// 空字段不覆盖原有昵称
	require.Equal(t, "Admin", data["nickName"])
}

func TestCurrentUserProfileUpdate_Unauthorized(t *testing.T) {
	svcCtx, _ := newSvcEnv(t)
	l := NewCurrentUserProfileUpdateLogic(context.Background(), svcCtx)
	_, err := l.CurrentUserProfileUpdate(&types.CurrentUserProfileReq{FirstName: "X"})
	requireHTTPError(t, err, 401)
}

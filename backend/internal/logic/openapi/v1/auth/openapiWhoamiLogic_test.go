// openapiWhoamiLogic 单测：测试目标为 OpenapiWhoami（当前登录用户信息，含权限与
// 角色合并）与纯函数 compactStrings（去空白、去重、保序）。覆盖正常路径（完整
// Subject 字段回显、ResourceKeys 排序、AuthType/KeyType 合并）与异常情况
// （无登录 Subject 时零值回显）。
package auth

import (
	"context"
	"sort"
	"testing"

	"backend/internal/contextx"
	"backend/internal/domain/common"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

// whoamiCtx 构造携带指定 Subject 的 ctx。
func whoamiCtx(subject contextx.Subject) context.Context {
	return contextx.WithSubject(context.Background(), subject)
}

func TestOpenapiWhoami_Behavior(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)

	subject := contextx.Subject{
		UserID:       42,
		UserName:     "api-user",
		Email:        "u@example.com",
		AuthType:     "apikey",
		APIKeyName:   "my-key",
		APIKeyPrefix: "sk-abc",
		KeyType:      "platform",
		ResourceKeys: map[string]struct{}{
			"flow.view": {},
			"asset.upload": {},
			"asset.view": {},
		},
	}
	l := NewOpenapiWhoamiLogic(whoamiCtx(subject), svcCtx)

	resp, err := l.OpenapiWhoami()
	require.NoError(t, err)
	data, ok := resp.Data.(types.AuthWhoamiResp)
	require.True(t, ok)
	require.Equal(t, int64(42), data.UserID)
	require.Equal(t, "api-user", data.UserName)
	require.Equal(t, "u@example.com", data.Email)
	require.Equal(t, common.PlatformWorkspaceID, data.WorkspaceID)
	require.Equal(t, "", data.WorkspaceName)
	require.Equal(t, "my-key", data.ApiKeyName)
	require.Equal(t, "sk-abc", data.KeyPrefix)
	require.Equal(t, "platform", data.KeyType)

	// ResourceKeys 无序，输出必须排序
	require.True(t, sort.StringsAreSorted(data.Permissions))
	require.Equal(t, []string{"asset.upload", "asset.view", "flow.view"}, data.Permissions)

	// AuthType 与 KeyType 去重合并进 Roles；两者不同 -> 两项
	require.Equal(t, []string{"apikey", "platform"}, data.Roles)
}

func TestOpenapiWhoami_NoSubject(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	l := NewOpenapiWhoamiLogic(context.Background(), svcCtx)

	resp, err := l.OpenapiWhoami()
	require.NoError(t, err)
	data, ok := resp.Data.(types.AuthWhoamiResp)
	require.True(t, ok)
	require.Equal(t, int64(0), data.UserID)
	require.Empty(t, data.Permissions)
	require.Empty(t, data.Roles)
}

func TestOpenapiWhoami_DeduplicatedRoles(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	// AuthType 与 KeyType 相同（session 会话无 key）-> Roles 仅一项
	subject := contextx.Subject{
		UserID:   7,
		UserName: "tier0",
		AuthType: "session",
		KeyType:  "session",
	}
	l := NewOpenapiWhoamiLogic(whoamiCtx(subject), svcCtx)
	resp, err := l.OpenapiWhoami()
	require.NoError(t, err)
	data := resp.Data.(types.AuthWhoamiResp)
	require.Equal(t, []string{"session"}, data.Roles)
}

func TestCompactStrings(t *testing.T) {
	// 正常路径：空白裁剪 + 去重 + 保序
	require.Equal(t, []string{"a", "b"}, compactStrings(" a ", "b", "a", "  ", ""))
	// 全部空白/空 -> 空切片
	require.Empty(t, compactStrings("", "  ", "\t"))
	// 单值
	require.Equal(t, []string{"x"}, compactStrings("x"))
}

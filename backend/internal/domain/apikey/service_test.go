package apikey

// 测试目标：API Key 服务（service.go）。
// 覆盖的正常路径：keyResp 字段契约（含 raw 透传）；List/Create/Update/Delete 的 DB 路径。
// 覆盖的异常情况：非法名称、超长名称、不存在的 key（ErrNotFound）。
// 说明：svctest 依赖 domain 包（import cycle），apikey 测试使用 testkit 等价装配；
// owner 使用 seed 内置 tier0（userID=1）。

import (
	"context"
	"testing"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func newTestAPIKey(t *testing.T) (*Service, context.Context) {
	t.Helper()
	testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, UserName: "tier0"})
	return New(ctx), ctx
}

func TestKeyResp(t *testing.T) {
	item := repo.APIKey{
		ID:           1,
		Name:         "k",
		KeyPrefix:    "sk-per-ws1_abcd",
		KeySuffix:    "wxyz",
		OwnerID:      1,
		OwnerType:    "personal",
		UsageType:    "external",
		KeyType:      "personal",
		Permission:   "read_only",
		ResourceKeys: "uns.read,flow.read",
		Status:       1,
	}
	resp := keyResp(item, "raw-secret")
	require.Equal(t, "sk-per-ws1_abcd...wxyz", resp["maskedKey"])
	require.Equal(t, "raw-secret", resp["apiKey"])
	require.Equal(t, int64(1), resp["ownerId"])
	require.Equal(t, int64(1), resp["ownerID"])

	// 无 raw 时不输出 apiKey 字段
	resp = keyResp(item, "")
	_, ok := resp["apiKey"]
	require.False(t, ok)
}

func TestAPIKeyLifecycle(t *testing.T) {
	svc, ctx := newTestAPIKey(t)

	// 创建（默认 permission 归一化为 read_only）
	created, err := svc.Create(ctx, "my-key", 1, "", "external", "personal", nil)
	require.NoError(t, err)
	raw, ok := created["apiKey"].(string)
	require.True(t, ok && raw != "")
	require.Equal(t, "read_only", created["permission"])
	keyID, ok := created["id"].(int64)
	require.True(t, ok && keyID > 0)

	// 非法名称
	_, err = svc.Create(ctx, "", 1, "", "external", "personal", nil)
	require.ErrorIs(t, err, ErrInvalid)
	longName := make([]byte, 101)
	for i := range longName {
		longName[i] = 'a'
	}
	_, err = svc.Create(ctx, string(longName), 1, "", "external", "personal", nil)
	require.ErrorIs(t, err, ErrInvalid)

	// 列表
	list, err := svc.List(ctx, 1, "", "")
	require.NoError(t, err)
	require.Equal(t, 1, list["total"])

	// 更新（改名 + 升级权限）
	updated, err := svc.Update(ctx, keyID, 1, "my-key-2", "full")
	require.NoError(t, err)
	require.Equal(t, "my-key-2", updated["name"])
	require.Equal(t, "full_access", updated["permission"])

	// 更新不存在的 key
	_, err = svc.Update(ctx, 999999, 1, "x", "read")
	require.ErrorIs(t, err, ErrNotFound)

	// 更新他人 key（owner 不匹配 → ErrNotFound）
	_, err = svc.Update(ctx, keyID, 999999, "x", "read")
	require.ErrorIs(t, err, ErrNotFound)

	// 删除
	deleted, err := svc.Delete(ctx, keyID, 1)
	require.NoError(t, err)
	require.Equal(t, true, deleted["deleted"])

	// 删除不存在的 key
	_, err = svc.Delete(ctx, keyID, 1)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestAPIKeyPermissionVariants(t *testing.T) {
	svc, ctx := newTestAPIKey(t)

	// 各种 permission 别名归一化
	cases := []struct {
		in   string
		want string
	}{
		{"read", "read_only"},
		{"data_writer", "data_writer"},
		{"fullaccess", "full_access"},
		{"manage", "full_access"},
	}
	for _, tc := range cases {
		created, err := svc.Create(ctx, "key-"+tc.in, 1, tc.in, "external", "personal", nil)
		require.NoError(t, err)
		require.Equal(t, tc.want, created["permission"])
	}
}

// TASK-028 container_app_helpers 单元测试（纯函数，无需 DB）：
//
//	目标：覆盖 container app 的纯函数：入参校验（validateContainerAppSave）、
//	      manifest 生成（containerAppManifest）、响应组装（containerAppData）。
//	正常路径：合法 URL（http/https、带端口、带路径）通过；manifest 序列化正确；
//	          containerAppData 正确解析 manifest 与状态映射。
//	异常情况：空 name/entryUrl、非 URL、非 http(s) 协议、仅路径 URL 均返回
//	          repo.ErrInvalidArgument（经 logicx.Error 包装为 400）。
package app

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestValidateContainerAppSave_ValidURLs(t *testing.T) {
	valid := []string{
		"https://example.com",
		"http://example.com:8080/path",
		"  https://example.com  ",
	}
	for _, u := range valid {
		err := validateContainerAppSave(&types.ContainerAppSaveReq{Name: "app", EntryUrl: u})
		require.NoError(t, err, "url %q should pass", u)
	}
}

func TestValidateContainerAppSave_InvalidInputs(t *testing.T) {
	cases := []types.ContainerAppSaveReq{
		{Name: "", EntryUrl: "https://example.com"},
		{Name: "  ", EntryUrl: "https://example.com"},
		{Name: "app", EntryUrl: ""},
		{Name: "app", EntryUrl: "not-a-url"},
		{Name: "app", EntryUrl: "ftp://example.com"},
		{Name: "app", EntryUrl: "/relative/path"},
	}
	for _, req := range cases {
		err := validateContainerAppSave(&req)
		require.ErrorIs(t, err, repo.ErrInvalidArgument, "req %+v should be rejected", req)
	}
}

func TestContainerAppManifest(t *testing.T) {
	raw, err := containerAppManifest("  desc  ", true)
	require.NoError(t, err)
	require.JSONEq(t, `{"description":"desc","openInPlatform":true}`, string(raw))

	// 空 description 归一化为空串
	raw, err = containerAppManifest("", false)
	require.NoError(t, err)
	require.JSONEq(t, `{"description":"","openInPlatform":false}`, string(raw))
}

func TestContainerAppData(t *testing.T) {
	item := repo.ProjectApp{
		ID:           11,
		Name:         "grafana",
		IconAssetID:  7,
		EntryURL:     "https://grafana.example.com",
		ManifestJSON: []byte(`{"description":"dashboards","openInPlatform":true}`),
		Status:       "active",
	}
	view := containerAppData(item)
	require.Equal(t, int64(11), view["appId"])
	require.Equal(t, "grafana", view["name"])
	require.Equal(t, "dashboards", view["description"])
	require.Equal(t, int64(7), view["iconAssetId"])
	require.Equal(t, "https://grafana.example.com", view["entryUrl"])
	require.Equal(t, "https://grafana.example.com", view["url"])
	require.Equal(t, true, view["openInPlatform"])
	require.Equal(t, true, view["enabled"])
	require.Equal(t, "active", view["status"])
}

func TestContainerAppData_CapitalActiveIsEnabled(t *testing.T) {
	item := repo.ProjectApp{Status: "Active"}
	require.True(t, containerAppData(item)["enabled"].(bool))

	item = repo.ProjectApp{Status: "stopped"}
	require.False(t, containerAppData(item)["enabled"].(bool))
}

func TestContainerAppData_InvalidManifestIgnored(t *testing.T) {
	item := repo.ProjectApp{
		ID:           1,
		Name:         "x",
		ManifestJSON: []byte(`{invalid-json`),
		Status:       "active",
	}
	// 解析失败不报错，字段回退零值
	view := containerAppData(item)
	require.Equal(t, "", view["description"])
	require.Equal(t, false, view["openInPlatform"])
}

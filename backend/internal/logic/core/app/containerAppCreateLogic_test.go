// TASK-028 ContainerAppCreateLogic 单元测试：
//
//	目标：覆盖外部容器应用创建（入参校验 + manifest 生成 + 落库）。
//	正常路径：合法 name/entryUrl 创建成功，app_key 以 container_ 前缀生成，
//	          site_type=externalContainer、status=active，manifest 含 description/openInPlatform。
//	异常情况：name 或 entryUrl 为空返回 400；entryUrl 非法（非 URL/非 http(s)）返回 400。
package app

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestContainerAppCreate_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppCreateLogic(ctx, svcCtx)

	resp, err := l.ContainerAppCreate(&types.ContainerAppSaveReq{
		Name:           "grafana",
		Description:    "dashboards",
		EntryUrl:       "https://grafana.example.com",
		OpenInPlatform: true,
	})
	require.NoError(t, err)
	view := respData(t, resp)
	appID, ok := view["appId"].(int64)
	require.True(t, ok)
	require.Positive(t, appID)
	require.Equal(t, "grafana", view["name"])
	require.Equal(t, "https://grafana.example.com", view["entryUrl"])
	require.Equal(t, true, view["openInPlatform"])
	require.Equal(t, true, view["enabled"])

	var item repo.ProjectApp
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&item, appID).Error)
	require.Equal(t, int64(0), item.ProjectID)
	require.Equal(t, "externalContainer", item.SiteType)
	require.Equal(t, "active", item.Status)
	require.Contains(t, item.AppKey, "container_")
	require.JSONEq(t, `{"description":"dashboards","openInPlatform":true}`, string(item.ManifestJSON))
}

func TestContainerAppCreate_TrimsWhitespace(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppCreateLogic(ctx, svcCtx)

	resp, err := l.ContainerAppCreate(&types.ContainerAppSaveReq{
		Name:     "  padded  ",
		EntryUrl: "  http://a.example.com  ",
	})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, "padded", view["name"])
	require.Equal(t, "http://a.example.com", view["entryUrl"])
}

func TestContainerAppCreate_MissingNameRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppCreateLogic(ctx, svcCtx)

	_, err := l.ContainerAppCreate(&types.ContainerAppSaveReq{EntryUrl: "https://x.example.com"})
	requireHTTPError(t, err, 400)
}

func TestContainerAppCreate_MissingEntryURLRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppCreateLogic(ctx, svcCtx)

	_, err := l.ContainerAppCreate(&types.ContainerAppSaveReq{Name: "grafana"})
	requireHTTPError(t, err, 400)
}

func TestContainerAppCreate_InvalidEntryURLRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppCreateLogic(ctx, svcCtx)

	// 非 URL
	_, err := l.ContainerAppCreate(&types.ContainerAppSaveReq{Name: "a", EntryUrl: "not-a-url"})
	requireHTTPError(t, err, 400)

	// 非 http(s) 协议
	_, err = l.ContainerAppCreate(&types.ContainerAppSaveReq{Name: "b", EntryUrl: "ftp://x.example.com"})
	requireHTTPError(t, err, 400)
}

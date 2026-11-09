// TASK-028 ContainerAppUpdateLogic 单元测试：
//
//	目标：覆盖外部容器应用更新（AppId 校验 + 入参校验 + 覆盖 name/entryUrl/icon/manifest）。
//	正常路径：合法请求更新成功，name/entryUrl/iconAssetId/manifest 均被覆盖，
//	          不影响 status。
//	异常情况：AppId<=0 或 name/entryUrl 为空或 entryUrl 非法返回 400；app 不存在返回 404。
package app

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestContainerAppUpdate_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppUpdateLogic(ctx, svcCtx)
	item := insertContainerApp(t, svcCtx, "grafana", "https://grafana.example.com", "active")

	resp, err := l.ContainerAppUpdate(&types.ContainerAppSaveReq{
		AppId:          item.ID,
		Name:           "grafana-new",
		Description:    "updated",
		EntryUrl:       "https://grafana2.example.com",
		IconAssetId:    88,
		OpenInPlatform: false,
	})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, item.ID, view["appId"])
	require.Equal(t, "grafana-new", view["name"])
	require.Equal(t, "https://grafana2.example.com", view["entryUrl"])
	require.Equal(t, int64(88), view["iconAssetId"])
	require.Equal(t, false, view["openInPlatform"])
	require.Equal(t, "active", view["status"])

	var updated repo.ProjectApp
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&updated, item.ID).Error)
	require.Equal(t, "grafana-new", updated.Name)
	require.Equal(t, int64(88), updated.IconAssetID)
	require.JSONEq(t, `{"description":"updated","openInPlatform":false}`, string(updated.ManifestJSON))
}

func TestContainerAppUpdate_ZeroAppIDRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppUpdateLogic(ctx, svcCtx)

	_, err := l.ContainerAppUpdate(&types.ContainerAppSaveReq{Name: "x", EntryUrl: "https://x.example.com"})
	requireHTTPError(t, err, 400)
}

func TestContainerAppUpdate_InvalidNameRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppUpdateLogic(ctx, svcCtx)
	item := insertContainerApp(t, svcCtx, "grafana", "https://grafana.example.com", "active")

	_, err := l.ContainerAppUpdate(&types.ContainerAppSaveReq{AppId: item.ID, EntryUrl: "https://x.example.com"})
	requireHTTPError(t, err, 400)
}

func TestContainerAppUpdate_InvalidEntryURLRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppUpdateLogic(ctx, svcCtx)
	item := insertContainerApp(t, svcCtx, "grafana", "https://grafana.example.com", "active")

	_, err := l.ContainerAppUpdate(&types.ContainerAppSaveReq{AppId: item.ID, Name: "x", EntryUrl: "ftp://x.example.com"})
	requireHTTPError(t, err, 400)
}

func TestContainerAppUpdate_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewContainerAppUpdateLogic(ctx, svcCtx)

	_, err := l.ContainerAppUpdate(&types.ContainerAppSaveReq{
		AppId: 999999, Name: "x", EntryUrl: "https://x.example.com",
	})
	requireHTTPError(t, err, 404)
}

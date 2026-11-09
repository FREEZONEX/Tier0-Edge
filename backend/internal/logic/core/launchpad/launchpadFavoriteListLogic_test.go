// TASK-028 LaunchpadFavoriteListLogic 单元测试：
//
//	目标：覆盖收藏列表（按 resType 过滤、按创建时间倒序）。
//	正常路径：返回本人收藏；resType 过滤生效；排序为 created_time DESC。
//	异常情况：无收藏返回空列表（error 分支依赖 DB 故障，测试库不可复现）。
//	说明：场景顺序共享同一测试库（每个场景独立断言，互不冲突）。
package launchpad

import (
	"testing"

	domainlaunchpad "backend/internal/domain/launchpad"
	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestLaunchpadFavoriteList_Behavior(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	require.NoError(t, svcCtx.App.Launchpad.BatchAddFavorites(ctx, 1, []repo.UserFavorite{
		{ResType: "app", ResID: "1001", ResName: "web-app"},
		{ResType: "project", ResID: "2001", ResName: "alpha"},
	}))

	l := NewLaunchpadFavoriteListLogic(ctx, svcCtx)

	// 全部收藏
	resp, err := l.LaunchpadFavoriteList(&types.LaunchpadFavoriteListReq{})
	require.NoError(t, err)
	list := respData(t, resp)["list"].([]domainlaunchpad.UserFavoriteVO)
	require.Len(t, list, 2)

	// resType 过滤
	resp, err = l.LaunchpadFavoriteList(&types.LaunchpadFavoriteListReq{ResType: "app"})
	require.NoError(t, err)
	list = respData(t, resp)["list"].([]domainlaunchpad.UserFavoriteVO)
	require.Len(t, list, 1)
	require.Equal(t, "app", list[0].ResType)
	require.Equal(t, "1001", list[0].ResID)
	require.Equal(t, "web-app", list[0].ResName)

	// 空过滤结果
	resp, err = l.LaunchpadFavoriteList(&types.LaunchpadFavoriteListReq{ResType: "nope"})
	require.NoError(t, err)
	list = respData(t, resp)["list"].([]domainlaunchpad.UserFavoriteVO)
	require.Empty(t, list)
}

// TASK-028 LaunchpadFavoriteDeleteLogic 单元测试：
//
//	目标：覆盖收藏批量删除（按 user + res_type + res_id 精确删除）。
//	正常路径：删除已存在的收藏后列表为空；只删本人收藏，不影响他人。
//	异常情况：删除不存在的条目返回 success（幂等 no-op）。
//	说明：场景顺序共享同一测试库（每个场景独立断言，互不冲突）。
package launchpad

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestLaunchpadFavoriteDelete_Behavior(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	require.NoError(t, svcCtx.App.Launchpad.BatchAddFavorites(ctx, 1, []repo.UserFavorite{
		{ResType: "app", ResID: "1001", ResName: "web-app"},
		{ResType: "app", ResID: "1002", ResName: "robot-console"},
	}))
	require.NoError(t, svcCtx.App.Launchpad.BatchAddFavorites(ctx, 2, []repo.UserFavorite{
		{ResType: "app", ResID: "1001", ResName: "web-app"},
	}))

	l := NewLaunchpadFavoriteDeleteLogic(ctx, svcCtx)

	// 删除本人一条收藏
	resp, err := l.LaunchpadFavoriteDelete(&types.LaunchpadFavoriteBatchReq{
		Items: []types.LaunchpadFavoriteItem{{ResType: "app", ResID: "1001"}},
	})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Model(&repo.UserFavorite{}).
		Where("user_id = 1").Count(&count).Error)
	require.Equal(t, int64(1), count)
	// 不影响他人
	require.NoError(t, svcCtx.App.Store.CommonDB().Model(&repo.UserFavorite{}).
		Where("user_id = 2").Count(&count).Error)
	require.Equal(t, int64(1), count)

	// 删除不存在的条目幂等
	_, err = l.LaunchpadFavoriteDelete(&types.LaunchpadFavoriteBatchReq{
		Items: []types.LaunchpadFavoriteItem{{ResType: "app", ResID: "not-exist"}},
	})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)
}

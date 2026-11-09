// TASK-028 LaunchpadFavoriteCreateLogic 单元测试：
//
//	目标：覆盖收藏批量新增（写入 sys_user_favorite、幂等去重）。
//	正常路径：多条目成功写入，按 (user_id, res_type, res_id) 唯一索引幂等。
//	异常情况：空条目/无 user（ctx 无 Subject）返回 success 且不写库（领域层安全 no-op）。
//	说明：场景顺序共享同一测试库（每个场景独立断言，互不冲突）。
package launchpad

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestLaunchpadFavoriteCreate_Behavior(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewLaunchpadFavoriteCreateLogic(ctx, svcCtx)

	// 批量写入
	resp, err := l.LaunchpadFavoriteCreate(&types.LaunchpadFavoriteBatchReq{
		Items: []types.LaunchpadFavoriteItem{
			{ResType: "app", ResID: "1001", ResName: "web-app"},
			{ResType: "app", ResID: "1002", ResName: "robot-console"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	// 重复写入幂等
	_, err = l.LaunchpadFavoriteCreate(&types.LaunchpadFavoriteBatchReq{
		Items: []types.LaunchpadFavoriteItem{{ResType: "app", ResID: "1001", ResName: "web-app"}},
	})
	require.NoError(t, err)

	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Model(&repo.UserFavorite{}).
		Where("user_id = 1").Count(&count).Error)
	require.Equal(t, int64(2), count)

	// 空条目 no-op
	_, err = l.LaunchpadFavoriteCreate(&types.LaunchpadFavoriteBatchReq{Items: nil})
	require.NoError(t, err)
	require.NoError(t, svcCtx.App.Store.CommonDB().Model(&repo.UserFavorite{}).
		Where("user_id = 1").Count(&count).Error)
	require.Equal(t, int64(2), count)
}

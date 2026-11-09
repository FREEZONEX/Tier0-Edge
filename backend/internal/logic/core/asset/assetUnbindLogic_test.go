// TASK-028 AssetUnbindLogic 单元测试：
//
//	目标：覆盖 asset 解绑（按绑定 id 软删除 asset_binding 记录）。
//	正常路径：已存在绑定解绑成功，响应 deleted=true，记录 deleted_time 非 0。
//	异常情况：绑定不存在返回 404。
package asset

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestAssetUnbind_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetUnbindLogic(ctx, svcCtx)
	fileID := insertAsset(t, svcCtx, "unbind.bin")
	bindingID := mustBind(t, svcCtx, fileID, "projectApp", 3)

	resp, err := l.AssetUnbind(&types.IdReq{Id: bindingID})
	require.NoError(t, err)
	require.Equal(t, true, respData(t, resp)["deleted"])

	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("asset_binding").
		Where("id = ? AND deleted_time = 0", bindingID).Count(&count).Error)
	require.Zero(t, count, "binding should be soft deleted")
}

func TestAssetUnbind_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetUnbindLogic(ctx, svcCtx)

	_, err := l.AssetUnbind(&types.IdReq{Id: 999999})
	requireHTTPError(t, err, 404)
}

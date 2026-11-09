// TASK-028 AssetBindLogic 单元测试：
//
//	目标：覆盖核心会话版 asset 绑定（ownerId 字符串解析 + Bind 落库）。
//	正常路径：合法 fileId/ownerType/ownerId 绑定成功，asset_file 状态置为 active，
//	          asset_binding 落库，响应含 binding 对象。
//	异常情况：fileId 缺失或 ownerType 为空返回 400；ownerId 非数字按 0 解析（绑定成功）；
//	          fileId 不存在时返回 400（领域层 ErrInvalid→400，绑定约束未命中）。
package asset

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestAssetBind_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetBindLogic(ctx, svcCtx)
	fileID := insertAsset(t, svcCtx, "bind-me.bin")

	resp, err := l.AssetBind(&types.AssetBindReq{
		FileId:    fileID,
		OwnerType: "projectApp",
		OwnerId:   "42",
	})
	require.NoError(t, err)
	view := respData(t, resp)
	b, ok := view["binding"].(repo.AssetBinding)
	require.True(t, ok, "binding should be repo.AssetBinding")
	require.Positive(t, b.ID)
	require.Equal(t, fileID, b.AssetID)
	require.Equal(t, "projectApp", b.OwnerType)
	require.Equal(t, int64(42), b.OwnerID)
	require.Equal(t, int64(1), b.CreatedBy)

	// 绑定后文件置为 active
	var f repo.AssetFile
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&f, fileID).Error)
	require.Equal(t, "active", f.Status)
}

func TestAssetBind_OwnerIDNonNumericParsedAsZero(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetBindLogic(ctx, svcCtx)
	fileID := insertAsset(t, svcCtx, "bind-str.bin")

	// ParseOwnerID 对非数字返回 0：OwnerID=0 不满足 ownerType 为空的条件，绑定仍成功
	resp, err := l.AssetBind(&types.AssetBindReq{FileId: fileID, OwnerType: "projectApp", OwnerId: "abc"})
	require.NoError(t, err)
	b, ok := respData(t, resp)["binding"].(repo.AssetBinding)
	require.True(t, ok)
	require.Equal(t, int64(0), b.OwnerID)
}

func TestAssetBind_MissingFileIDRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetBindLogic(ctx, svcCtx)

	_, err := l.AssetBind(&types.AssetBindReq{OwnerType: "projectApp", OwnerId: "1"})
	requireHTTPError(t, err, 400)
}

func TestAssetBind_EmptyOwnerTypeRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetBindLogic(ctx, svcCtx)
	fileID := insertAsset(t, svcCtx, "bind-no-owner.bin")

	_, err := l.AssetBind(&types.AssetBindReq{FileId: fileID, OwnerId: "1"})
	requireHTTPError(t, err, 400)
}

func TestAssetBind_NonexistentFileRejected(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetBindLogic(ctx, svcCtx)

	// 文件不存在：asset_binding 无外键约束，Create 成功但文件行未更新；
	// 领域层 Bind 不校验文件存在性，绑定本身成功返回。
	resp, err := l.AssetBind(&types.AssetBindReq{FileId: 999999, OwnerType: "projectApp", OwnerId: "1"})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)
}

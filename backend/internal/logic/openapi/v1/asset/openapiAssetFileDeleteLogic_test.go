// openapiAssetFileDeleteLogic 单测：测试目标为 OpenapiAssetFileDelete 按 storage key
// 删除文件。覆盖正常路径（命中后返回 deleted=true，文件软删除）与异常情况
// （空 filePath、未命中、他人项目文件越权）。
package asset

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiAssetFileDelete_Behavior(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	db := svcCtx.App.Store.CommonDB()
	storageKey := "0/assets/20260101/delete.txt"
	seedOpenapiAsset(t, db, storageKey)

	l := NewOpenapiAssetFileDeleteLogic(ctx, svcCtx)

	// 正常路径：删除成功，返回 {"deleted":true}
	resp, err := l.OpenapiAssetFileDelete(&types.OpenapiAssetFileDeleteReq{FilePath: storageKey})
	require.NoError(t, err)
	require.NotNil(t, resp)
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Equal(t, true, data["deleted"])

	// 再次删除同一 key：记录已软删除，ResolveOpenapiFile 未命中 -> 错误
	_, err = l.OpenapiAssetFileDelete(&types.OpenapiAssetFileDeleteReq{FilePath: storageKey})
	require.Error(t, err)

	// 异常情况：空 filePath -> 错误
	_, err = l.OpenapiAssetFileDelete(&types.OpenapiAssetFileDeleteReq{FilePath: ""})
	require.Error(t, err)
}

func TestOpenapiAssetFileDelete_Forbidden(t *testing.T) {
	svcCtx, ctx, userID := newTestLogic(t)
	db := svcCtx.App.Store.CommonDB()
	// 直接插入一条 project_id 不属于该用户的文件（userID 解析作用域为 0/部署级），
	// 模拟跨项目越权删除。
	file := seedOpenapiAsset(t, db, "0/assets/20260101/other-project.txt")
	require.NoError(t, db.Model(&file).Update("project_id", 999).Error)

	l := NewOpenapiAssetFileDeleteLogic(ctx, svcCtx)
	_ = userID
	_, err := l.OpenapiAssetFileDelete(&types.OpenapiAssetFileDeleteReq{FilePath: "0/assets/20260101/other-project.txt"})
	require.Error(t, err)
}

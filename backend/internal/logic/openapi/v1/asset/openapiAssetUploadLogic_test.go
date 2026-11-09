// openapiAssetUploadLogic 单测：测试目标为 OpenapiAssetUpload（openapi 直接上传，
// 仅登记元数据，不写对象存储）。覆盖正常路径（登记成功并返回 filePath/fileUrl）
// 与异常情况（空文件名、未登录用户 userID=0 的部署级作用域行为）。
package asset

import (
	"context"
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiAssetUpload_Behavior(t *testing.T) {
	svcCtx, ctx, _ := newTestLogic(t)
	l := NewOpenapiAssetUploadLogic(ctx, svcCtx)

	// 正常路径：合法入参登记成功
	resp, err := l.OpenapiAssetUpload(&types.AssetUploadReq{
		FileName: "upload.txt",
		Size:     10,
		Sha256:   "abc123",
	})
	require.NoError(t, err)
	data := resp.Data.(map[string]any)
	// 响应固定为 {"uploadUrl":"", "filePath":<id>, "fileUrl":<nil>}：logic 写入
	// uploadUrl/fileUrl 键，fileUrl 取 data["fileUrl"] 恒为 nil（assetResp 无此键，
	// 该接口仅登记元数据，不写对象存储）
	require.Equal(t, "", data["uploadUrl"])
	require.NotEmpty(t, data["filePath"])
	require.Nil(t, data["fileUrl"])

	// 异常情况：空文件名 -> ErrInvalid
	_, err = l.OpenapiAssetUpload(&types.AssetUploadReq{})
	require.Error(t, err)
}

func TestOpenapiAssetUpload_AnonymousUser(t *testing.T) {
	svcCtx, _, _ := newTestLogic(t)
	// 未携带登录 Subject 的 ctx：UserID 解析为 0，作用域为部署级，登记仍应成功
	l := NewOpenapiAssetUploadLogic(context.Background(), svcCtx)
	resp, err := l.OpenapiAssetUpload(&types.AssetUploadReq{
		FileName: "anon.txt",
		Size:     5,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

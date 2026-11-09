// TASK-028 AssetMultipartCompleteLogic 单元测试：
//
//	目标：覆盖分片上传 complete（temp 定位 + 权限复检 + 合并 + 项目归属绑定）。
//	正常路径：本地驱动用例覆盖权限/定位/参数校验分支；完整分片会话成功路径
//	          （含项目归属绑定）由内存仿真 S3 驱动（newS3Service）覆盖。
//	异常情况：temp 记录不存在或 fileKey 不匹配返回 404；非管理员返回 404；
//	          local 驱动不支持分片返回 501。
package asset

import (
	"net/http"
	"strings"
	"testing"

	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestAssetMultipartComplete_RejectsUnknownTemp(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetMultipartCompleteLogic(ctx, svcCtx)

	_, err := l.AssetMultipartComplete(&types.AssetMultipartCompleteReq{
		FileKey: "nope", UploadId: "nope",
		Parts: []types.AssetMultipartCompletePart{{PartNumber: 1, Etag: "e1"}},
	})
	requireHTTPError(t, err, 404)
}

func TestAssetMultipartComplete_RejectsNonManagerUser(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	projectID := mustCreateProject(t, svcCtx, ctx, "mp-cpl-user")
	insertTempAsset(t, svcCtx, projectID, "assets/big.zip", "cpl-upload-1", "flat_big_zip")

	userCtx := svctest.CtxWithUser(2)
	l2 := NewAssetMultipartCompleteLogic(userCtx, svcCtx)
	_, err := l2.AssetMultipartComplete(&types.AssetMultipartCompleteReq{
		FileKey: "flat_big_zip", UploadId: "cpl-upload-1",
		Parts: []types.AssetMultipartCompletePart{{PartNumber: 1, Etag: "e1"}},
	})
	requireHTTPError(t, err, 404)
}

func TestAssetMultipartComplete_LocalDriverUnsupported(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetMultipartCompleteLogic(ctx, svcCtx)
	projectID := mustCreateProject(t, svcCtx, ctx, "mp-cpl-local")
	insertTempAsset(t, svcCtx, projectID, "assets/big.zip", "cpl-upload-2", "flat_big_zip2")

	_, err := l.AssetMultipartComplete(&types.AssetMultipartCompleteReq{
		FileKey: "flat_big_zip2", UploadId: "cpl-upload-2",
		Parts: []types.AssetMultipartCompletePart{{PartNumber: 1, Etag: "e1"}},
	})
	requireHTTPError(t, err, 501)
}

func TestAssetMultipartComplete_SuccessOnS3Sim(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	projectID := mustCreateProject(t, svcCtx, ctx, "mp-cpl-s3")

	// 替换 Asset 服务为 S3 仿真驱动，跑完整分片链路
	svcCtx.App.Asset = newS3Service(t, svcCtx)

	initLogic := NewAssetMultipartInitLogic(ctx, svcCtx)
	initResp, err := initLogic.AssetMultipartInit(&types.AssetMultipartInitReq{
		ProjectId: projectID,
		FileName:  "big.zip",
		Size:      10 * 1024 * 1024,
		PartSize:  5 * 1024 * 1024,
	})
	require.NoError(t, err)
	initView := respData(t, initResp)
	fileKey, _ := initView["fileKey"].(string)
	uploadID, _ := initView["uploadId"].(string)
	require.NotEmpty(t, fileKey)
	require.NotEmpty(t, uploadID)

	urlsLogic := NewAssetMultipartPartUrlsLogic(ctx, svcCtx)
	urlsResp, err := urlsLogic.AssetMultipartPartUrls(&types.AssetMultipartPartUrlsReq{
		FileKey: fileKey, UploadId: uploadID, PartNumbers: []int32{1},
	})
	require.NoError(t, err)
	partUrls, ok := respData(t, urlsResp)["partUrls"].([]map[string]any)
	require.True(t, ok, "partUrls should be []map[string]any")
	require.Len(t, partUrls, 1)
	require.NotEmpty(t, partUrls[0]["url"])

	// 完成合并：仿真驱动按 uploadId 合并并生成对象。
	// 注意：PartUrls 只签发预签名地址，测试中不会真实 PUT 分片；因此 ListParts
	// 汇总大小为 0，complete 按"未上传任何分片"拒绝。这里在 complete 前模拟客户端
	// 已通过预签名地址上传了 1 片（向仿真端点 PUT 一次真实分片）。
	partURL, _ := partUrls[0]["url"].(string)
	req, err := http.NewRequest(http.MethodPut, partURL, strings.NewReader(strings.Repeat("x", 1024)))
	require.NoError(t, err)
	putResp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = putResp.Body.Close()
	require.Equal(t, http.StatusOK, putResp.StatusCode)

	cplLogic := NewAssetMultipartCompleteLogic(ctx, svcCtx)
	cplResp, err := cplLogic.AssetMultipartComplete(&types.AssetMultipartCompleteReq{
		FileKey: fileKey, UploadId: uploadID,
		Parts: []types.AssetMultipartCompletePart{{PartNumber: 1, Etag: `"e1"`}},
	})
	require.NoError(t, err)
	view := respData(t, cplResp)
	fileID, ok := view["fileId"].(int64)
	require.True(t, ok)
	require.Positive(t, fileID)
	// filePath 是项目作用域对象 key（projectID/日期/随机名），不是原始文件名
	require.Contains(t, view["filePath"].(string), "/assets/")
	require.Contains(t, view["filePath"].(string), ".zip")

	// 记录置为 active 并绑定到所属项目
	var f repo.AssetFile
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&f, fileID).Error)
	require.Equal(t, "active", f.Status)
	require.Equal(t, projectID, f.ProjectID)
	var binding repo.AssetBinding
	require.NoError(t, svcCtx.App.Store.CommonDB().
		Where("asset_id = ? AND owner_type = ? AND owner_id = ? AND deleted_time = 0",
			fileID, "projectApp", projectID).First(&binding).Error)
	require.Positive(t, binding.ID)
}

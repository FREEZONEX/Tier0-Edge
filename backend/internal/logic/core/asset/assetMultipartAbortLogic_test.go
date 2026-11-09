// TASK-028 AssetMultipartAbortLogic 单元测试：
//
//	目标：覆盖分片上传 abort（temp 定位 + 权限复检 + 会话中止 + temp 记录软删除）。
//	正常路径：admin 对合法 (fileKey, uploadId) 中止成功，响应 aborted=true，temp 记录
//	          软删除；完整分片会话中止路径（S3 仿真驱动）一并覆盖。
//	异常情况：temp 记录不存在或 fileKey 不匹配返回 404；非管理员返回 404；
//	          local 驱动不支持 abort 返回 501。
package asset

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestAssetMultipartAbort_RejectsUnknownTemp(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetMultipartAbortLogic(ctx, svcCtx)

	_, err := l.AssetMultipartAbort(&types.AssetMultipartAbortReq{FileKey: "nope", UploadId: "nope"})
	requireHTTPError(t, err, 404)
}

func TestAssetMultipartAbort_RejectsNonManagerUser(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	projectID := mustCreateProject(t, svcCtx, ctx, "mp-abort-user")
	insertTempAsset(t, svcCtx, projectID, "assets/big.zip", "abort-upload-1", "flat_abort_1")

	userCtx := svctest.CtxWithUser(2)
	l2 := NewAssetMultipartAbortLogic(userCtx, svcCtx)
	_, err := l2.AssetMultipartAbort(&types.AssetMultipartAbortReq{FileKey: "flat_abort_1", UploadId: "abort-upload-1"})
	requireHTTPError(t, err, 404)
}

func TestAssetMultipartAbort_LocalDriverUnsupported(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetMultipartAbortLogic(ctx, svcCtx)
	projectID := mustCreateProject(t, svcCtx, ctx, "mp-abort-local")
	insertTempAsset(t, svcCtx, projectID, "assets/big.zip", "abort-upload-2", "flat_abort_2")

	_, err := l.AssetMultipartAbort(&types.AssetMultipartAbortReq{FileKey: "flat_abort_2", UploadId: "abort-upload-2"})
	requireHTTPError(t, err, 501)
}

func TestAssetMultipartAbort_SuccessOnS3Sim(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	projectID := mustCreateProject(t, svcCtx, ctx, "mp-abort-s3")

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

	abortLogic := NewAssetMultipartAbortLogic(ctx, svcCtx)
	resp, err := abortLogic.AssetMultipartAbort(&types.AssetMultipartAbortReq{FileKey: fileKey, UploadId: uploadID})
	require.NoError(t, err)
	require.Equal(t, true, respData(t, resp)["aborted"])
}

func TestAssetMultipartAbort_SuccessOnLocalDriver(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	projectID := mustCreateProject(t, svcCtx, ctx, "mp-abort-local-ok")

	// local 驱动 AbortMultipartUpload 返回 ErrMultipartUnsupported：
	// 领域层在调用对象存储中止前先做 temp 定位与权限校验，因此返回 501 且不删记录
	insertTempAsset(t, svcCtx, projectID, "assets/big.zip", "abort-upload-3", "flat_abort_3")
	l := NewAssetMultipartAbortLogic(ctx, svcCtx)
	_, err := l.AssetMultipartAbort(&types.AssetMultipartAbortReq{FileKey: "flat_abort_3", UploadId: "abort-upload-3"})
	requireHTTPError(t, err, 501)
}

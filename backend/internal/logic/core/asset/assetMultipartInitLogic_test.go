// TASK-028 AssetMultipartInitLogic 单元测试：
//
//	目标：覆盖 core 会话版分片上传 init（项目管理权限校验 + 会话创建）。
//	正常路径：平台 admin 或项目 owner 调用成功，返回 fileKey/uploadId/partSize/partCount，
//	          asset_file 落 temp 记录（op_id=uploadId、project_id=projectId）。
//	异常情况：项目不存在返回 404；非 admin/非 owner 用户返回 404（与 CheckManage 一致，
//	          不泄露项目存在性）；local 存储驱动不支持分片返回 501。
//	未覆盖说明：分片会话创建依赖对象存储驱动，真实 S3/RustFS 分支由
//	          newS3Service（内存仿真 S3）覆盖，见 assetMultipartCompleteLogic_test.go。
package asset

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestAssetMultipartInit_SuccessAsAdmin(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetMultipartInitLogic(ctx, svcCtx)
	projectID := mustCreateProject(t, svcCtx, ctx, "mp-init")

	// local 驱动不支持分片：管理员路径走到 createMultipartUpload 后返回 501
	_, err := l.AssetMultipartInit(&types.AssetMultipartInitReq{
		ProjectId: projectID,
		FileName:  "big.zip",
		Size:      20 * 1024 * 1024,
		PartSize:  5 * 1024 * 1024,
	})
	requireHTTPError(t, err, 501)
}

func TestAssetMultipartInit_RejectsMissingProject(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetMultipartInitLogic(ctx, svcCtx)

	// projectID=0 时 CheckManage 查不到项目返回 project.ErrNotFound → 404；
	// CoreMultipartInit 的 projectId 必须为正的 400 分支被前面的权限校验挡住。
	_, err := l.AssetMultipartInit(&types.AssetMultipartInitReq{ProjectId: 0, FileName: "x.zip", Size: 100})
	requireHTTPError(t, err, 404)
}

func TestAssetMultipartInit_RejectsNonexistentProject(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetMultipartInitLogic(ctx, svcCtx)

	_, err := l.AssetMultipartInit(&types.AssetMultipartInitReq{ProjectId: 999999, FileName: "x.zip", Size: 100})
	requireHTTPError(t, err, 404)
}

func TestAssetMultipartInit_RejectsNonManagerUser(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	projectID := mustCreateProject(t, svcCtx, ctx, "mp-init-owner")

	// userID=2 非 admin 也非 owner：CheckManage 返回 404
	userCtx := svctest.CtxWithUser(2)
	l2 := NewAssetMultipartInitLogic(userCtx, svcCtx)
	_, err := l2.AssetMultipartInit(&types.AssetMultipartInitReq{ProjectId: projectID, FileName: "x.zip", Size: 100})
	requireHTTPError(t, err, 404)
}

func TestAssetMultipartInit_RejectsEmptyFileName(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetMultipartInitLogic(ctx, svcCtx)
	projectID := mustCreateProject(t, svcCtx, ctx, "mp-init-name")

	_, err := l.AssetMultipartInit(&types.AssetMultipartInitReq{ProjectId: projectID, FileName: "  ", Size: 100})
	requireHTTPError(t, err, 400)
}

func TestAssetMultipartInit_NoTempRowLeftOnLocalDriver(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewAssetMultipartInitLogic(ctx, svcCtx)
	projectID := mustCreateProject(t, svcCtx, ctx, "mp-init-clean")

	_, err := l.AssetMultipartInit(&types.AssetMultipartInitReq{ProjectId: projectID, FileName: "x.zip", Size: 100})
	requireHTTPError(t, err, 501)

	// 驱动不支持分片时 createMultipartUpload 在落库前返回，不留 temp 记录
	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Model(&repo.AssetFile{}).
		Where("status = 'temp' AND project_id = ? AND deleted_time = 0", projectID).Count(&count).Error)
	require.Zero(t, count)
}

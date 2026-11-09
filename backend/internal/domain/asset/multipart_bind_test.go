package asset

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	domainproject "backend/internal/domain/project"
	"backend/internal/repo"
)

// 本文件包含依赖真实 DB 的集成测试（TEST_DATABASE_URL 未配置时跳过，与仓库既有约定一致），
// 覆盖 Codex 评审修复的行为契约：
//   - ResolveMultipartTemp 按 (fileKey, uploadId) 解析 temp 记录并携带 ProjectID，是
//     core 分片 follow-up（part-urls/complete/abort）复检项目权限的数据来源；
//   - core complete 后按 (ownerType=projectApp, ownerId=projectId) 绑定（镜像单发上传
//     UploadAttachment→Bind 的落库语义），绑定后导入归属校验 checkImportAssetOwned 可通过。

// TestResolveMultipartTemp_ByOpID 校验 temp 解析器：uploadId 全局唯一命中记录并返回
// ProjectID；fileKey 不匹配或 uploadId 不存在一律 ErrNotFound（不泄露对象信息）。
func TestResolveMultipartTemp_ByOpID(t *testing.T) {
	ctx := context.Background()
	db := repo.GetCommonConn(ctx)
	if db == nil {
		t.Skip("common DB not initialized")
	}
	const projectID = int64(900011)
	fileKey := fmt.Sprintf("resolve-temp-%d", time.Now().UnixNano())
	uploadID := "op-" + fileKey
	assetRepo := repo.NewAssetRepo(ctx)
	created, err := assetRepo.CreateAssetFile(ctx, repo.AssetFile{
		FileKey:       fileKey,
		OriginalName:  "multipart-test.zip",
		ContentType:   "application/zip",
		SizeBytes:     1024,
		StorageDriver: "local",
		StorageKey:    fileKey,
		Status:        "temp",
		ProjectID:     projectID,
		OpID:          uploadID,
	})
	if err != nil {
		t.Fatalf("insert temp asset_file: %v", err)
	}
	defer func() {
		_ = db.Exec("DELETE FROM asset_binding WHERE asset_id = ?", created.ID).Error
		_ = db.Exec("DELETE FROM asset_file WHERE id = ?", created.ID).Error
	}()

	svc := &Service{repo: repo.NewAssetRepo(ctx)}
	item, err := svc.ResolveMultipartTemp(ctx, fileKey, uploadID)
	if err != nil {
		t.Fatalf("ResolveMultipartTemp err = %v", err)
	}
	if item.ProjectID != projectID {
		t.Errorf("ProjectID = %d, want %d", item.ProjectID, projectID)
	}
	if _, err := svc.ResolveMultipartTemp(ctx, "wrong-file-key", uploadID); !errors.Is(err, ErrNotFound) {
		t.Errorf("fileKey 不匹配 err = %v, want ErrNotFound", err)
	}
	if _, err := svc.ResolveMultipartTemp(ctx, fileKey, "op-no-such"); !errors.Is(err, ErrNotFound) {
		t.Errorf("uploadId 不存在 err = %v, want ErrNotFound", err)
	}
}

// TestCoreCompleteBind_ImportOwnership 校验 P1 绑定契约：core complete 后以
// domainproject.ProjectImportAssetOwnerType（projectApp）+ 记录 ProjectID 调用
// BindAsset（即 logic 层 Bind 的落库），绑定被创建且文件置为 active，随后导入归属
// 校验 GetAssetFileByOwner 可通过；他 project 的 ownerId 或其它 ownerType 一律 ErrNotFound。
func TestCoreCompleteBind_ImportOwnership(t *testing.T) {
	ctx := context.Background()
	db := repo.GetCommonConn(ctx)
	if db == nil {
		t.Skip("common DB not initialized")
	}
	const (
		projectID      = int64(900012)
		otherProjectID = int64(900013)
	)
	assetRepo := repo.NewAssetRepo(ctx)
	created, err := assetRepo.CreateAssetFile(ctx, repo.AssetFile{
		FileKey:       fmt.Sprintf("bind-core-%d", time.Now().UnixNano()),
		OriginalName:  "import-package.zip",
		ContentType:   "application/zip",
		StorageDriver: "local",
		Status:        "temp",
		ProjectID:     projectID,
	})
	if err != nil {
		t.Fatalf("insert asset_file: %v", err)
	}
	defer func() {
		_ = db.Exec("DELETE FROM asset_binding WHERE asset_id = ?", created.ID).Error
		_ = db.Exec("DELETE FROM asset_file WHERE id = ?", created.ID).Error
	}()

	// 镜像 core complete 后的绑定：ownerType=projectApp、ownerId=记录 ProjectID
	if _, err := assetRepo.BindAsset(ctx, repo.AssetBinding{
		AssetID:   created.ID,
		OwnerType: domainproject.ProjectImportAssetOwnerType,
		OwnerID:   projectID,
	}); err != nil {
		t.Fatalf("BindAsset: %v", err)
	}
	got, err := assetRepo.GetAssetFileByOwner(ctx, created.ID, domainproject.ProjectImportAssetOwnerType, projectID)
	if err != nil {
		t.Fatalf("绑定后导入归属校验应通过: %v", err)
	}
	if got.Status != "active" {
		t.Errorf("绑定后文件 status = %q, want active", got.Status)
	}
	if _, err := assetRepo.GetAssetFileByOwner(ctx, created.ID, domainproject.ProjectImportAssetOwnerType, otherProjectID); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("他 project 归属校验应 ErrNotFound, got %v", err)
	}
	if _, err := assetRepo.GetAssetFileByOwner(ctx, created.ID, "projectAppReplacement", projectID); !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("非 projectApp ownerType 归属校验应 ErrNotFound, got %v", err)
	}
}

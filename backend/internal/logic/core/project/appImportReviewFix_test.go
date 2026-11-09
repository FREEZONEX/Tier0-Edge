package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	domainproject "backend/internal/domain/project"
	"backend/internal/repo"
)

// 本文件包含依赖真实 DB 的集成测试（TEST_DATABASE_URL 未配置时跳过，与仓库既有约定一致），
// 覆盖 Codex 评审修复的行为契约：
//   - Replace 归属校验接受两种 ownerType 且 ownerId 必须是当前 projectID；
//   - 覆盖/更新导入标记 deploying 时先 CAS 后写 manifest，CAS 失败不改写胜者 manifest。

// TestCheckImportAssetOwned_OwnerTypes 校验 Replace 归属校验：
// ownerType 为 projectApp 或 projectAppReplacement 且 ownerId=当前 projectID 均通过；
// Import 路径仍只接受 projectApp；他 Project 的 ownerId 一律 403 拒绝。
func TestCheckImportAssetOwned_OwnerTypes(t *testing.T) {
	ctx := context.Background()
	db := repo.GetCommonConn(ctx)
	if db == nil {
		t.Skip("common DB not initialized")
	}
	const (
		projectID      = int64(900001)
		otherProjectID = int64(900002)
	)
	cleanup := func() {
		_ = db.Exec("DELETE FROM asset_binding WHERE asset_id IN (SELECT id FROM asset_file WHERE original_name LIKE 'import-asset-owner-%')").Error
		_ = db.Exec("DELETE FROM asset_file WHERE original_name LIKE 'import-asset-owner-%'").Error
	}
	cleanup()
	defer cleanup()

	insertFile := func(ownerType string, ownerID int64) int64 {
		fileKey := fmt.Sprintf("import-asset-owner-%s-%d-%d", ownerType, ownerID, time.Now().UnixNano())
		var id int64
		if err := db.Raw("INSERT INTO asset_file (file_key, original_name) VALUES (?, ?) RETURNING id", fileKey, "import-asset-owner").Scan(&id).Error; err != nil {
			t.Fatalf("insert asset_file: %v", err)
		}
		if err := db.Exec("INSERT INTO asset_binding (asset_id, owner_type, owner_id) VALUES (?, ?, ?)", id, ownerType, ownerID).Error; err != nil {
			t.Fatalf("insert asset_binding: %v", err)
		}
		return id
	}
	fileProjectApp := insertFile("projectApp", projectID)
	fileReplace := insertFile("projectAppReplacement", projectID)
	fileOtherProject := insertFile("projectApp", otherProjectID)
	fileOtherProjectReplace := insertFile("projectAppReplacement", otherProjectID)

	// 两种 ownerType 均通过 Replace 校验（ownerId=projectID）
	if err := checkImportAssetOwned(ctx, fileProjectApp, projectID, projectImportAssetOwnerType, projectReplaceAssetOwnerType); err != nil {
		t.Errorf("projectApp 绑定应通过 Replace 校验: %v", err)
	}
	if err := checkImportAssetOwned(ctx, fileReplace, projectID, projectImportAssetOwnerType, projectReplaceAssetOwnerType); err != nil {
		t.Errorf("projectAppReplacement 绑定应通过 Replace 校验: %v", err)
	}
	// Import 路径只接受 projectApp：projectAppReplacement 绑定被拒绝
	if err := checkImportAssetOwned(ctx, fileReplace, projectID, projectImportAssetOwnerType); !errors.Is(err, domainproject.ErrImportAssetDenied) {
		t.Errorf("projectAppReplacement 绑定应被 Import 校验拒绝, got %v", err)
	}
	// 他 Project 的 ownerId 一律拒绝（含两种 ownerType）
	if err := checkImportAssetOwned(ctx, fileOtherProject, projectID, projectImportAssetOwnerType, projectReplaceAssetOwnerType); !errors.Is(err, domainproject.ErrImportAssetDenied) {
		t.Errorf("他 Project 的 projectApp 绑定应拒绝, got %v", err)
	}
	if err := checkImportAssetOwned(ctx, fileOtherProjectReplace, projectID, projectImportAssetOwnerType, projectReplaceAssetOwnerType); !errors.Is(err, domainproject.ErrImportAssetDenied) {
		t.Errorf("他 Project 的 projectAppReplacement 绑定应拒绝, got %v", err)
	}
}

// TestMarkAppReplaceDeploying_CASFailureKeepsManifest 校验覆盖/更新导入标记 deploying 的顺序：
// 先 CAS 后写 manifest；CAS 失败（并发胜者已在部署）时不得改写 manifest——
// 胜者的 deploying 标记与其他字段必须原样保留（旧实现先写 manifest 再 CAS，失败会把胜者标记清掉）。
func TestMarkAppReplaceDeploying_CASFailureKeepsManifest(t *testing.T) {
	ctx := context.Background()
	db := repo.GetCommonConn(ctx)
	if db == nil {
		t.Skip("common DB not initialized")
	}
	const (
		projectID = int64(900003)
		appID     = int64(900004)
		userID    = int64(7)
	)
	cleanup := func() {
		_ = db.Exec("DELETE FROM project_app WHERE id = ?", appID).Error
		_ = db.Exec("DELETE FROM project WHERE id = ?", projectID).Error
	}
	cleanup()
	defer cleanup()
	if err := db.Exec("INSERT INTO project (id, project_key, name) VALUES (?, ?, ?)", projectID,
		fmt.Sprintf("review-fix-%d", time.Now().UnixNano()), "review-fix").Error; err != nil {
		t.Fatalf("insert project: %v", err)
	}
	insertApp := func(status, manifest string) {
		if err := db.Exec("INSERT INTO project_app (id, project_id, app_key, name, manifest_json, status) VALUES (?, ?, ?, ?, ?::jsonb, ?)",
			appID, projectID, "app_review_fix", "review-fix-app", manifest, status).Error; err != nil {
			t.Fatalf("insert project_app: %v", err)
		}
	}
	projectRepo := repo.NewProjectRepo(ctx)

	// 场景 1：CAS 成功 → status 置为 deploying，manifest 写入 deploying=true
	insertApp("active", `{"host_port": 8123}`)
	if err := markAppReplaceDeploying(ctx, projectRepo, projectID, appID, "active", userID); err != nil {
		t.Fatalf("markAppReplaceDeploying 成功路径: %v", err)
	}
	app, err := projectRepo.GetProjectApp(ctx, projectID, appID)
	if err != nil {
		t.Fatalf("GetProjectApp: %v", err)
	}
	if app.Status != "deploying" {
		t.Errorf("status = %q, want deploying", app.Status)
	}
	if deploying := manifestBool(t, app.ManifestJSON, "deploying"); !deploying {
		t.Errorf("manifest 应标记 deploying=true, got %s", app.ManifestJSON)
	}
	_ = db.Exec("DELETE FROM project_app WHERE id = ?", appID)

	// 场景 2：CAS 失败（并发胜者已在部署）→ 不改写 manifest，胜者标记与其余字段原样保留
	insertApp("deploying", `{"host_port": 8124, "deploying": true}`)
	if err := markAppReplaceDeploying(ctx, projectRepo, projectID, appID, "active", userID); !errors.Is(err, domainproject.ErrAppReplaceInProgress) {
		t.Fatalf("CAS 失败 err = %v, want ErrAppReplaceInProgress", err)
	}
	app, err = projectRepo.GetProjectApp(ctx, projectID, appID)
	if err != nil {
		t.Fatalf("GetProjectApp: %v", err)
	}
	if deploying := manifestBool(t, app.ManifestJSON, "deploying"); !deploying {
		t.Errorf("CAS 失败不得改写胜者 manifest 的 deploying 标记, got %s", app.ManifestJSON)
	}
	var hostPort int64
	if err := db.Raw("SELECT (manifest_json->>'host_port')::bigint FROM project_app WHERE id = ?", appID).Scan(&hostPort).Error; err != nil {
		t.Fatalf("query host_port: %v", err)
	}
	if hostPort != 8124 {
		t.Errorf("CAS 失败不得改写胜者 manifest 其余字段, host_port = %d, want 8124", hostPort)
	}
}

// manifestBool 解析 manifest JSON 并读取指定 bool 键（测试辅助）。
func manifestBool(t *testing.T, raw json.RawMessage, key string) bool {
	t.Helper()
	manifest := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &manifest); err != nil {
			t.Fatalf("unmarshal manifest %s: %v", raw, err)
		}
	}
	value, ok := manifest[key].(bool)
	return ok && value
}

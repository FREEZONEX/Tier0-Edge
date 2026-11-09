// 本文件覆盖覆盖导入（Replace）改名冲突的行为契约（2026-09-24 SIB 部署卡死回归）：
//   - mergeArtifactAppSuccess 目标名被同项目其他存活应用占用时跳过改名，
//     但 deploying=false / status=active / container_name 等其余回写必须完成，
//     不能让一场成功的部署卡死在 deploying；
//   - 目标名空闲时正常改名。
package project

import (
	"testing"

	domainproject "backend/internal/domain/project"
	"backend/internal/repo"
	"backend/internal/svc/svctest"

	"github.com/stretchr/testify/require"
)

func TestMergeArtifactAppSuccess_NameConflictKeepsWriteback(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	appRepo := repo.NewProjectRepo(ctx)
	projectID := createTestProject(t, svcCtx, ctx, "replace-rename")
	targetID := createTestManualApp(t, svcCtx, ctx, projectID, "replace-target")
	createTestManualApp(t, svcCtx, ctx, projectID, "taken-name")

	// 模拟覆盖导入标记部署中
	require.NoError(t, appRepo.UpdateProjectAppStatus(ctx, projectID, targetID, "deploying", 1))

	// 目标名被占用：整体回写必须成功，仅跳过改名
	err := mergeArtifactAppSuccess(ctx, projectID, targetID, "taken-name", "replace-app-uuid", 30110, "port",
		"https://entry.example/", nil, 1, "app-replace-target-abc123")
	require.NoError(t, err, "name conflict must not fail the success writeback")

	got, err := appRepo.GetProjectApp(ctx, projectID, targetID)
	require.NoError(t, err)
	require.Equal(t, "replace-target", got.Name, "conflicted rename should keep old name")
	require.Equal(t, "active", got.Status)
	var manifest map[string]any
	require.NoError(t, jsonUnmarshal(got.ManifestJSON, &manifest))
	require.Equal(t, false, manifest["deploying"])
	require.Equal(t, "app-replace-target-abc123", manifest["container_name"])
	require.Equal(t, "replace-app-uuid", manifest["app_id"])

	// 目标名空闲：正常改名
	require.NoError(t, appRepo.UpdateProjectAppStatus(ctx, projectID, targetID, "deploying", 1))
	require.NoError(t, mergeArtifactAppSuccess(ctx, projectID, targetID, "free-name", "replace-app-uuid", 30110, "port",
		"", nil, 1, ""))
	got, err = appRepo.GetProjectApp(ctx, projectID, targetID)
	require.NoError(t, err)
	require.Equal(t, "free-name", got.Name)
	require.Equal(t, "active", got.Status)
}

// TestCheckAppNameDuplicate_ReplaceFailFast 覆盖导入入口的重名预检：
// 包内 meta.name 与项目内其他存活应用重名时返回 409（ErrAppNameDuplicate），避免整场部署白跑。
func TestCheckAppNameDuplicate_ReplaceFailFast(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	logic := NewProjectAppImportLogic(ctx, svcCtx)
	projectID := createTestProject(t, svcCtx, ctx, "replace-fail-fast")
	createTestManualApp(t, svcCtx, ctx, projectID, "existing-name")

	require.NoError(t, logic.checkAppNameDuplicate(projectID, ""))
	require.NoError(t, logic.checkAppNameDuplicate(projectID, "free-name"))
	err := logic.checkAppNameDuplicate(projectID, "existing-name")
	require.ErrorIs(t, err, domainproject.ErrAppNameDuplicate,
		"409 映射由 handler 边界的 logicx 助手完成，logic 层返回哨兵错误")
}

// 仅大小写变体的改名不触发重名跳过：GetProjectAppByName 按 LOWER(name) 匹配会命中自己，
// 唯一索引区分大小写，改回自己名字的大小写变体应正常生效（Codex P2）。
func TestMergeArtifactAppSuccess_CaseOnlyRenameApplies(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	appRepo := repo.NewProjectRepo(ctx)
	projectID := createTestProject(t, svcCtx, ctx, "replace-case-rename")
	appID := createTestManualApp(t, svcCtx, ctx, projectID, "emba-mes")

	require.NoError(t, mergeArtifactAppSuccess(ctx, projectID, appID, "EMBA-MES", "case-uuid", 30110, "port",
		"", nil, 1, "app-case-rename-x"))
	got, err := appRepo.GetProjectApp(ctx, projectID, appID)
	require.NoError(t, err)
	require.Equal(t, "EMBA-MES", got.Name)
	require.Equal(t, "active", got.Status)
}

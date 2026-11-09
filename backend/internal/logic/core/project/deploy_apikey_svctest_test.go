// ensureAppApiKey 换签链路验证（svctest：真实 PG 测试库，生产迁移+种子原样执行）。
// 覆盖迁移策略的核心约束：存量 service 型 key 在重新部署时换签为 app 型新 key，
// 但旧 key 不吊销、旧 sys_api_key 注册行原样保留——正在运行的实例不受影响。
package project

import (
	"strings"
	"testing"

	domaincommon "backend/internal/domain/common"
	"backend/internal/repo"
	"backend/internal/svc/svctest"

	"github.com/stretchr/testify/require"
)

func TestEnsureAppApiKey_RotatesLegacyServiceKeyOnRedeploy(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)
	db := svcCtx.App.Store.CommonDB()

	appID := "0bddfabf-1779-4b2f-9a1f-000000000001"
	legacyKey := domaincommon.FormatWorkspaceAPIKey("sk-svc-", domaincommon.PlatformWorkspaceID, "legacy-plaintext-tail")

	// 存量状态：app_api_keys 持旧 service 型 key，sys_api_key 有对应注册行（历史名）
	require.NoError(t, db.Exec(
		"INSERT INTO app_api_keys (app_id, name, api_key, project_id) VALUES (?, 'default', ?, 0)",
		appID, legacyKey,
	).Error)
	require.NoError(t, repo.NewAPIKeyRepo(ctx).RegisterAPIKey(
		ctx, legacyKey, "app-"+appID+"-default", "", 1, "service", "full", "internal", nil,
	))

	newKey, err := ensureAppApiKey(ctx, appID, 0)
	require.NoError(t, err)

	// 换签结果：新 key 为 app 型新形态，app_api_keys 已指向新 key
	require.NotEqual(t, legacyKey, newKey)
	require.True(t, strings.HasPrefix(newKey, "sk-app-"), "new key should use sk-app- prefix, got %q", newKey)

	var stored repo.AppApiKey
	require.NoError(t, db.Where("app_id = ?", appID).First(&stored).Error)
	require.Equal(t, newKey, stored.ApiKey)

	// 旧 key 不吊销：旧明文仍可通过 sys_api_key 鉴权校验（注册行 app-<appID>-default 原样保留）
	legacyDetail, err := repo.NewAPIKeyRepo(ctx).ValidateAPIKeyDetail(ctx, legacyKey)
	require.NoError(t, err, "legacy key must stay valid for running instances")
	require.Equal(t, "service", legacyDetail.KeyType)

	// 新 key 注册行：name=app-<完整appID>、owner_type=app、usage=internal
	newDetail, err := repo.NewAPIKeyRepo(ctx).ValidateAPIKeyDetail(ctx, newKey)
	require.NoError(t, err)
	require.Equal(t, "app", newDetail.KeyType)

	var newUsageType string
	require.NoError(t, db.Table("sys_api_key").
		Select("usage_type").
		Where("name = ? AND owner_type = 'app' AND deleted_time = 0", "app-"+appID).
		Scan(&newUsageType).Error)
	require.Equal(t, "internal", newUsageType)

	var newRow struct {
		AppID string
	}
	require.NoError(t, db.Table("sys_api_key").
		Select("app_id").
		Where("name = ? AND owner_type = 'app' AND deleted_time = 0", "app-"+appID).
		Scan(&newRow).Error)
	require.Equal(t, appID, newRow.AppID, "app_id should be persisted on the new registry row")

	// 再次部署：新形态纯复用，不再换签
	reusedKey, err := ensureAppApiKey(ctx, appID, 0)
	require.NoError(t, err)
	require.Equal(t, newKey, reusedKey)
}

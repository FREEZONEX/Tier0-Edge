// openapi asset logic 包共享测试助手：
// - newTestLogic：装配 svctest 测试上下文（独立 PG 库 + miniredis）与带登录 Subject 的 ctx；
// - seedOpenapiAsset：直接落库一条可被 ResolveOpenapiFile 命中的 asset_file 记录，
//   避免依赖预签名 POST / 分片等外部存储能力（svctest 使用 local 驱动，均不支持）。
package asset

import (
	"context"
	"testing"

	"backend/internal/logic/logicx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/svc/svctest"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// adminUserID 返回 Seed 内置管理员 tier0 的 user_id（新库中通常为 1）。
func adminUserID(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var userID int64
	require.NoError(t, db.Table("sys_user_info").Where("user_name = ?", "tier0").Pluck("user_id", &userID).Error)
	require.Positive(t, userID)
	return userID
}

// newTestLogic 装配完整 svc.ServiceContext（每次调用独立测试库）。
func newTestLogic(t *testing.T) (*svc.ServiceContext, context.Context, int64) {
	t.Helper()
	svcCtx := svctest.NewTestServiceContext(t)
	db := svcCtx.App.Store.CommonDB()
	userID := adminUserID(t, db)
	return svcCtx, svctest.CtxWithUser(userID), userID
}

// seedOpenapiAsset 直接落库一条 active asset_file 记录（storage_key 由调用方给定）。
// 与 openapi.go 上传路径一致：project_id 取 userID 的解析作用域（无 project 时为 0），
// 可见性默认为 private。
func seedOpenapiAsset(t *testing.T, db *gorm.DB, storageKey string) repo.AssetFile {
	t.Helper()
	file := repo.AssetFile{
		FileKey:       "seed_" + storageKey,
		OriginalName:  "seed.txt",
		ContentType:   "text/plain",
		SizeBytes:     12,
		Sha256:        "",
		StorageDriver: "local",
		StorageKey:    storageKey,
		Visibility:    "private",
		Status:        "active",
		ProjectID:     0,
	}
	file.CreatedBy = 1
	created := testkit.Insert(t, db, &file)
	// ListActiveAssetFilesByStorageKey 以 deleted_time = 0 过滤，默认值满足。
	return *created
}

// ctxWithUserID 构造携带指定 userID 的 ctx（用于验证非 owner 用户的越权行为）。
func ctxWithUserID(userID int64) context.Context {
	return svctest.CtxWithUser(userID)
}

// ensureLogicError 断言 err 是 logicx.Error 翻译后的 HTTP 错误。
func ensureLogicError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	require.NotNil(t, logicx.Error(err))
}

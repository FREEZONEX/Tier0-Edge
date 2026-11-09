package project

import (
	"context"
	"fmt"
	"os"
	"testing"

	"backend/internal/config"
	"backend/internal/repo"
)

// TestMain 与 backend/internal/logic/core/project 保持一致：
// 配置 TEST_DATABASE_URL 时打开测试数据库并迁移 schema，未配置时 DB 相关测试跳过。
// 多个包测试可能共用同一测试库：schema 已迁移时跳过迁移，避免重复迁移失败。
func TestMain(m *testing.M) {
	if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
		store, err := repo.Open(context.Background(), config.DatabaseConf{UnsDbUrl: dsn})
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to open test database: %v\n", err)
			os.Exit(1)
		}
		defer store.Close()
		if !store.CommonDB().Migrator().HasTable("project_app") {
			if err := store.Migrate(context.Background()); err != nil {
				fmt.Fprintf(os.Stderr, "failed to migrate test database: %v\n", err)
				os.Exit(1)
			}
		}
	}
	os.Exit(m.Run())
}

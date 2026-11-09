// Package testkit 提供企业版后端的 PG-only 单元测试基建（TASK-028 阶段 0 交付）。
//
// 约定（与 doc 仓库 04-单元测试方案 v1.1 一致）：
//   - 数据库唯一后端为 PostgreSQL：默认连接
//     postgres://test:test@localhost:5432/tier0_test?sslmode=disable，
//     可用环境变量 TEST_DATABASE_URL 覆盖（本机端口不一致或 CI 时）。
//   - 每个测试创建独立数据库（t_<测试名>_<随机后缀>），结束后自动 DROP，完全隔离。
//     不做 schema 级隔离的原因：sink 迁移 DDL 固定使用 public schema
//     （internal/repo/migrations/sink_schema.sql），schema 级隔离会跨测试共享该 schema。
//   - 建表直接复用生产迁移（repo.Store.Migrate：schema.sql + 版本化迁移），并执行
//     repo.Store.Seed 灌入内置 Admin/Operator 角色与默认资源，与生产启动路径一致。
//   - 包级连接 fallback 通过 repo.SetStoreForTest 注入当前测试库；包内测试默认串行执行，
//     测试不得使用 t.Parallel()（全局连接为包级单例）。
package testkit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"backend/internal/config"
	"backend/internal/infra/cache"
	"backend/internal/repo"

	"github.com/alicebob/miniredis/v2"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DefaultDSN 为默认本地 PG 连接串（未设置 TEST_DATABASE_URL 时使用）。
const DefaultDSN = "postgres://test:test@localhost:5432/tier0_test?sslmode=disable"

// DSN 返回测试数据库管理连接串：优先 TEST_DATABASE_URL，否则 DefaultDSN。
func DSN() string {
	if v := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL")); v != "" {
		return v
	}
	return DefaultDSN
}

// SetLegacySourceState seeds historical persisted flags for lifecycle regression
// tests. Source activation is no longer a product operation. This fixture also
// advances the metadata revision, preserving tests of old snapshot boundaries.
func SetLegacySourceState(ctx context.Context, service interface {
	Source(context.Context, int64) (map[string]any, error)
}, id, revision int64, state string) (map[string]any, error) {
	if state != "enabled" && state != "disabled" {
		return nil, repo.ErrInvalidArgument
	}
	_, err := repo.NewSourceStreamRepo(ctx).ChangeSource(ctx, id, revision, func(_ *gorm.DB, item *repo.Source) error {
		item.DesiredState = state
		return nil
	})
	if err != nil {
		return nil, err
	}
	return service.Source(ctx, id)
}

// NewTestDB 创建独立测试数据库并完成迁移与种子数据，返回 *repo.Store 与该库 DSN。
// 同时把包级连接 fallback 注入为当前测试库（repo.SetStoreForTest）。
// t.Cleanup 中自动关闭连接并 DROP DATABASE（WITH (FORCE)）。
func NewTestDB(t *testing.T) (*repo.Store, string) {
	t.Helper()
	adminDSN := DSN()
	admin, err := gorm.Open(postgres.Open(adminDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("testkit: connect admin db: %v", err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatalf("testkit: admin sql db: %v", err)
	}

	name := newDBName(t.Name())
	if err := admin.Exec(fmt.Sprintf("CREATE DATABASE %s", quoteIdent(name))).Error; err != nil {
		t.Fatalf("testkit: create database %s: %v", name, err)
	}
	testDSN := withDBName(adminDSN, name)

	ctx := context.Background()
	store, err := repo.Open(ctx, config.DatabaseConf{UnsDbUrl: testDSN})
	if err != nil {
		dropTestDB(admin, name)
		t.Fatalf("testkit: open store: %v", err)
	}
	if err := store.Migrate(ctx); err != nil {
		store.Close()
		dropTestDB(admin, name)
		t.Fatalf("testkit: migrate: %v", err)
	}
	if err := store.Seed(ctx, config.SecurityConf{InitialAdminPassword: "tier0"}, config.GatewayConf{}); err != nil {
		store.Close()
		dropTestDB(admin, name)
		t.Fatalf("testkit: seed: %v", err)
	}
	repo.SetStoreForTest(store.CommonDB(), store.UnsDB())

	t.Cleanup(func() {
		store.Close()
		dropTestDB(admin, name)
		_ = adminSQL.Close()
	})
	return store, testDSN
}

// NewTestRedis 返回 miniredis 实例（t.Cleanup 自动关闭）。
func NewTestRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	return miniredis.RunT(t)
}

// NewTestCache 返回挂在 miniredis 上的 cache.Client（t.Cleanup 自动关闭）。
func NewTestCache(t *testing.T) *cache.Client {
	t.Helper()
	mr := NewTestRedis(t)
	client := cache.Open(config.RedisConf{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// Insert 向测试库写入一条记录（gorm Create；PG 自增主键经 RETURNING 回填到 model）。
func Insert[T any](t *testing.T, db *gorm.DB, model *T) *T {
	t.Helper()
	if err := db.Create(model).Error; err != nil {
		t.Fatalf("testkit: insert %T: %v", model, err)
	}
	return model
}

// newDBName 生成测试数据库名：t_<测试名净化>_<6位随机hex>，总长不超过 63。
func newDBName(testName string) string {
	var b strings.Builder
	b.WriteString("t_")
	for _, r := range testName {
		if len(b.String()) >= 40 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	b.WriteByte('_')
	randBytes := make([]byte, 3)
	if _, err := rand.Read(randBytes); err != nil {
		panic(fmt.Sprintf("testkit: rand: %v", err))
	}
	b.WriteString(hex.EncodeToString(randBytes))
	return b.String()
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// withDBName 把 DSN 中的数据库名替换为 name，保留其余参数。
func withDBName(dsn, name string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		// 非 URL 形态的 DSN（key=value 风格）直接拼接不支持，保持原样由上层报错
		return dsn
	}
	u.Path = "/" + name
	return u.String()
}

func dropTestDB(admin *gorm.DB, name string) {
	_ = admin.Exec(fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE)", quoteIdent(name))).Error
}

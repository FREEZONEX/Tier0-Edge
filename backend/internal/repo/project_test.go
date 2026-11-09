package repo

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// sqlCaptureLogger 记录 gorm DryRun 阶段生成的 SQL，用于校验 repo 方法实际发出的查询。
type sqlCaptureLogger struct {
	sqls []string
}

func (l *sqlCaptureLogger) LogMode(logger.LogLevel) logger.Interface { return l }
func (l *sqlCaptureLogger) Info(context.Context, string, ...any)     {}
func (l *sqlCaptureLogger) Warn(context.Context, string, ...any)     {}
func (l *sqlCaptureLogger) Error(context.Context, string, ...any)    {}
func (l *sqlCaptureLogger) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	if strings.TrimSpace(sql) != "" {
		l.sqls = append(l.sqls, sql)
	}
}

func newDryRunDBWithCapture(t *testing.T, capture *sqlCaptureLogger) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN:                  "host=localhost user=test password=test dbname=test port=5432 sslmode=disable",
		PreferSimpleProtocol: true,
	}), &gorm.Config{
		DryRun:                 true,
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 capture,
	})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db
}

// TestCasDeployStatusError 覆盖 CAS 决策纯函数：0 行命中视为并发冲突（应用不存在或已在部署中）。
func TestCasDeployStatusError(t *testing.T) {
	if err := casDeployStatusError(0); !errors.Is(err, ErrAppDeployInProgress) {
		t.Fatalf("casDeployStatusError(0) = %v, want ErrAppDeployInProgress", err)
	}
	if err := casDeployStatusError(1); err != nil {
		t.Fatalf("casDeployStatusError(1) = %v, want nil", err)
	}
}

// TestUpdateProjectAppStatusDeployingCAS_SQL 校验 CAS 标记 deploying 的更新条件：
// 仅当 status<>'deploying' 且未删除时命中，防止并发导入重建同一应用。
func TestUpdateProjectAppStatusDeployingCAS_SQL(t *testing.T) {
	capture := &sqlCaptureLogger{}
	db := newDryRunDBWithCapture(t, capture)
	r := &ProjectRepo{db: db}
	// DryRun 不真正执行、RowsAffected=0，方法应返回并发冲突错误
	err := r.UpdateProjectAppStatusDeployingCAS(context.Background(), 7, 11, 3)
	if !errors.Is(err, ErrAppDeployInProgress) {
		t.Fatalf("dry-run CAS error = %v, want ErrAppDeployInProgress", err)
	}
	if len(capture.sqls) == 0 {
		t.Fatal("expected captured SQL")
	}
	sql := strings.ToLower(capture.sqls[len(capture.sqls)-1])
	for _, want := range []string{"status <> 'deploying'", "deleted_time = 0", "'deploying'"} {
		if !strings.Contains(sql, want) {
			t.Errorf("CAS SQL missing %q, got: %s", want, capture.sqls[len(capture.sqls)-1])
		}
	}
}

// TestGetAssetFileByOwner_SQL 校验按 owner 归属查询 asset 的 SQL：
// 必须 join asset_binding 并以 owner_type/owner_id 过滤，且两侧都要求存活记录。
func TestGetAssetFileByOwner_SQL(t *testing.T) {
	capture := &sqlCaptureLogger{}
	db := newDryRunDBWithCapture(t, capture)
	r := NewAssetRepo(db)
	_, err := r.GetAssetFileByOwner(context.Background(), 5, "projectApp", 9)
	if !errors.Is(err, ErrNotFound) {
		// DryRun 不返回行；若实现返回非 ErrNotFound 说明查询构建异常
		t.Logf("dry-run GetAssetFileByOwner err = %v（DryRun 下通常为 ErrNotFound）", err)
	}
	if len(capture.sqls) == 0 {
		t.Fatal("expected captured SQL")
	}
	sql := strings.ToLower(capture.sqls[len(capture.sqls)-1])
	for _, want := range []string{"asset_binding", "owner_type", "owner_id", "deleted_time = 0"} {
		if !strings.Contains(sql, want) {
			t.Errorf("GetAssetFileByOwner SQL missing %q, got: %s", want, capture.sqls[len(capture.sqls)-1])
		}
	}
}

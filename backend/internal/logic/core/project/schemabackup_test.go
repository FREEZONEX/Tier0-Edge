package project

import (
	"strings"
	"testing"

	"backend/internal/config"
)

func TestFirstNonEmptyLine(t *testing.T) {
	if got := firstNonEmptyLine("\n  \nabc123\ndef456\n"); got != "abc123" {
		t.Fatalf("got %q, want %q", got, "abc123")
	}
	if got := firstNonEmptyLine("\n\n"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestSchemaDumpDSN_AdminFallback(t *testing.T) {
	conf := config.Config{}
	conf.Database.UnsDbUrl = "postgres://postgres:secret@postgresql:5432/uns"
	dsn, err := schemaDumpDSN(conf, "project_7")
	if err != nil {
		t.Fatalf("schemaDumpDSN failed: %v", err)
	}
	// 未配置 RoleSecret 时沿用 admin 凭据，仅替换 database 路径
	if !strings.HasPrefix(dsn, "postgres://postgres:secret@postgresql:5432/project_7") {
		t.Fatalf("unexpected dsn: %s", dsn)
	}
}

func TestSchemaDumpDSN_DerivedRole(t *testing.T) {
	conf := config.Config{}
	conf.Database.UnsDbUrl = "postgres://postgres:secret@postgresql:5432/uns"
	conf.TenantPG.RoleSecret = "test-role-secret"
	dsn, err := schemaDumpDSN(conf, "project_7")
	if err != nil {
		t.Fatalf("schemaDumpDSN failed: %v", err)
	}
	// 配置 RoleSecret 时使用应用受限 role（schema 内对象的 owner），备份恢复后属主一致
	if !strings.HasPrefix(dsn, "postgres://rw_project_7:") {
		t.Fatalf("expected derived role user in dsn, got: %s", dsn)
	}
	if !strings.Contains(dsn, "@postgresql:5432/project_7") {
		t.Fatalf("expected project database in dsn, got: %s", dsn)
	}
}

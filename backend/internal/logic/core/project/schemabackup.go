package project

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"backend/internal/config"
	"backend/internal/service/tenantpg"
)

// 本文件实现覆盖导入（Import to replace）的应用 schema 备份与恢复：
// 准备阶段在 drizzle push 前用 PG 容器内的 pg_dump 把在用 schema 备份到临时文件；
// 备份后的任何失败先 drop schema cascade 再用 psql 导入备份恢复，部署成功后删除备份文件。
// 复用 deploy/bin/backup.sh 的既有模式：docker exec 到 compose 的 PG 服务容器执行 pg_dump/psql。

// backupAppSchema 用 pg_dump 把应用 schema 备份到临时文件，返回备份文件路径。
func backupAppSchema(conf config.Config, projectDB, schemaName string) (string, error) {
	containerID, err := findLocalPGContainer(conf.Database.UnsDbUrl)
	if err != nil {
		return "", err
	}
	dsn, err := schemaDumpDSN(conf, projectDB)
	if err != nil {
		return "", err
	}
	tmpFile, err := os.CreateTemp("", "app-schema-backup-*.sql")
	if err != nil {
		return "", fmt.Errorf("create schema backup temp file: %w", err)
	}
	backupPath := tmpFile.Name()
	var stderr bytes.Buffer
	cmd := exec.Command("docker", "exec", containerID, "pg_dump", dsn,
		"--schema="+schemaName, "--no-owner", "--no-privileges")
	cmd.Stdout = tmpFile
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	closeErr := tmpFile.Close()
	if runErr != nil {
		_ = os.Remove(backupPath)
		return "", fmt.Errorf("pg_dump schema %s failed: %w, output: %s", schemaName, runErr, strings.TrimSpace(stderr.String()))
	}
	if closeErr != nil {
		_ = os.Remove(backupPath)
		return "", fmt.Errorf("write schema backup file: %w", closeErr)
	}
	return backupPath, nil
}

// restoreAppSchema 从备份文件恢复应用 schema：先 drop schema cascade 并重建（含授权），再用 psql 导入备份。
func restoreAppSchema(ctx context.Context, conf config.Config, lifecycle *tenantpg.Lifecycle, projectDB, schemaName, backupPath string) error {
	containerID, err := findLocalPGContainer(conf.Database.UnsDbUrl)
	if err != nil {
		return err
	}
	dsn, err := schemaDumpDSN(conf, projectDB)
	if err != nil {
		return err
	}
	if err := lifecycle.DropSchema(ctx, projectDB, schemaName); err != nil {
		return fmt.Errorf("drop app schema %s before restore: %w", schemaName, err)
	}
	if err := lifecycle.CreateSchema(ctx, projectDB, schemaName); err != nil {
		return fmt.Errorf("recreate app schema %s before restore: %w", schemaName, err)
	}
	dumpFile, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("open schema backup file: %w", err)
	}
	defer dumpFile.Close()
	var stderr bytes.Buffer
	cmd := exec.Command("docker", "exec", "-i", containerID, "psql", dsn, "-v", "ON_ERROR_STOP=1", "-q")
	// pg_dump 输出自带 CREATE SCHEMA 语句，与上面重建的 schema 冲突（schema already exists），导入前过滤掉
	cmd.Stdin = filterCreateSchemaStmt(dumpFile)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("psql restore schema %s failed: %w, output: %s", schemaName, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// createSchemaStmtPattern 匹配 pg_dump 输出中的 CREATE SCHEMA 语句行
var createSchemaStmtPattern = regexp.MustCompile(`(?i)^\s*CREATE\s+SCHEMA\b`)

// filterCreateSchemaStmt 流式过滤 pg_dump 输出中的 CREATE SCHEMA 语句：
// restoreAppSchema 在导入前已重建 schema 并授权，dump 中自带的 CREATE SCHEMA 会导致冲突报错。
func filterCreateSchemaStmt(r io.Reader) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		var writeErr error
		for scanner.Scan() {
			line := scanner.Text()
			if createSchemaStmtPattern.MatchString(line) {
				continue
			}
			if _, err := pw.Write([]byte(line + "\n")); err != nil {
				writeErr = err
				break
			}
		}
		if writeErr == nil {
			writeErr = scanner.Err()
		}
		_ = pw.CloseWithError(writeErr)
	}()
	return pr
}

// findLocalPGContainer 按 UnsDbUrl 的 host（compose service 名）定位运行中的本地 PG 容器。
// 外部 PG（非本机 compose 服务）找不到容器，此时无法通过 docker exec 备份/恢复 schema。
func findLocalPGContainer(unsDbURL string) (string, error) {
	u, err := tenantpg.ParsePostgresDSN(unsDbURL)
	if err != nil {
		return "", fmt.Errorf("parse uns db url: %w", err)
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("uns db url host is empty")
	}
	// 优先按 compose service label 精确匹配；其次按容器名模糊匹配（兼容非 compose label 的部署）
	for _, filter := range []string{"label=com.docker.compose.service=" + host, "name=" + host} {
		output, err := exec.Command("docker", "ps", "-q", "--filter", filter).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("docker ps failed: %v, output: %s", err, string(output))
		}
		if id := firstNonEmptyLine(string(output)); id != "" {
			return id, nil
		}
	}
	return "", fmt.Errorf("no running local PG container found for host %q, schema backup unavailable", host)
}

// schemaDumpDSN 构造 pg_dump/psql 连接 project database 的 DSN。
// 配置 RoleSecret 时使用应用受限 role（schema 内对象的 owner，drizzle push 即以其执行，备份恢复后属主一致）；
// 否则沿用 admin 凭据。DSN host 为 compose service 名，在 PG 容器内同样可解析（指向容器自身）。
func schemaDumpDSN(conf config.Config, projectDB string) (string, error) {
	if conf.TenantPG.RoleSecret != "" {
		user, pass, err := tenantpg.DeriveCredentials(conf.TenantPG.RoleSecret, projectDB)
		if err != nil {
			return "", fmt.Errorf("derive credentials: %w", err)
		}
		return tenantpg.BuildProjectConnDSNWithCreds(conf.Database.UnsDbUrl, projectDB, user, pass)
	}
	return tenantpg.BuildProjectConnDSN(conf.Database.UnsDbUrl, projectDB)
}

// firstNonEmptyLine 返回输出中第一个非空行（去除空白）
func firstNonEmptyLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// countAppSchemaTables 统计项目库中指定 schema 的业务表数量（不含占位表）。
// 用于全新部署前检测孤儿 schema：删除应用时 DropSchema 失败会遗留带表的 schema，
// 确定性 schema 名（包内 appId 派生）重导同一包时会静默挂上，旧数据会泄漏进全新安装。
func countAppSchemaTables(conf config.Config, projectDB, schemaName string) (int, error) {
	containerID, err := findLocalPGContainer(conf.Database.UnsDbUrl)
	if err != nil {
		return 0, err
	}
	dsn, err := schemaDumpDSN(conf, projectDB)
	if err != nil {
		return 0, err
	}
	query := fmt.Sprintf(
		"SELECT count(*) FROM information_schema.tables WHERE table_schema='%s' AND table_name <> '__schema_marker'",
		strings.ReplaceAll(schemaName, "'", "''"),
	)
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("docker", "exec", containerID, "psql", dsn, "-t", "-Ac", query)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("count schema tables failed: %w, output: %s", err, strings.TrimSpace(stderr.String()))
	}
	count, err := strconv.Atoi(strings.TrimSpace(stdout.String()))
	if err != nil {
		return 0, fmt.Errorf("parse schema table count %q: %w", stdout.String(), err)
	}
	return count, nil
}

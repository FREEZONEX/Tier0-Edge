package project

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"backend/internal/repo"
	"backend/internal/service/tenantpg"

	"github.com/zeromicro/go-zero/core/logx"
)

// reservedSystemSchemas PostgreSQL 系统/保留 schema 名，禁止作为应用 schema 使用。
var reservedSystemSchemas = map[string]bool{
	"public":             true,
	"information_schema": true,
	"pg_catalog":         true,
	"pg_toast":           true,
	"pg_temp_1":          true,
	"pg_toast_temp_1":    true,
	"pg_statistic":       true,
}

// normalizeSchemaName 把来源 App ID（meta.json 的 appId，通常是 UUID）规范化为合法 PG 标识符。
// 规则（与 tenantpg.ValidateIdentifier 的 [a-z0-9_] ≤63 对齐）：
//  1. 统一小写
//  2. 连字符（UUID 分隔符）直接删除；其余非法字符（空格、点、斜杠等）替换为下划线
//  3. 前缀 app_
//  4. 截断到 63 并去除尾部下划线；首字符必须为字母或下划线
//  5. 通过 ValidateIdentifier 才返回，否则返回空串（调用方回退本地自增 ID）
//
// 例：appId="123e4567-e89b-12d3-a456-426614174000" → "app_123e4567e89b12d3a456426614174000"
func normalizeSchemaName(appID string) string {
	raw := strings.ToLower(strings.TrimSpace(appID))
	if raw == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(raw) + 4)
	b.WriteString("app_")
	for _, r := range raw {
		switch {
		case r == '-':
			// UUID 连字符直接删除
			continue
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_':
			b.WriteRune(r)
		case unicode.IsSpace(r), r == '.', r == '/', r == '\\', r == ':':
			// 常见非法字符替换为下划线
			b.WriteRune('_')
		default:
			// 其余非法字符（含大写，已小写化）替换为下划线
			b.WriteRune('_')
		}
	}
	name := b.String()
	// 去掉尾部下划线（UUID 规范化不会产生，但替换路径可能）
	name = strings.TrimRight(name, "_")
	if len(name) > 63 {
		name = strings.TrimRight(name[:63], "_")
	}
	if !isReservedSystemSchema(name) {
		if err := tenantpg.ValidateIdentifier(name); err == nil {
			return name
		}
	}
	return ""
}

// isReservedSystemSchema 判断是否为系统/保留 schema 名。
func isReservedSystemSchema(name string) bool {
	if reservedSystemSchemas[name] {
		return true
	}
	if strings.HasPrefix(name, "pg_") || strings.HasPrefix(name, "template") {
		return true
	}
	return false
}

// resolvePreallocatedSchemaName 校验用户「高级」区提交的 schema 名并返回预占值：
// 非空时必须通过 PG 标识符校验且非系统/保留名，否则报错（阻止创建记录）；
// 为空返回空串，部署时回退 app_{本地自增ID}（兼容老包/未填高级区）。
func resolvePreallocatedSchemaName(ctx context.Context, schemaName string, projectID int64) string {
	name := strings.TrimSpace(schemaName)
	if name == "" {
		return ""
	}
	if isReservedSystemSchema(name) {
		logx.WithContext(ctx).Errorf("[resolvePreallocatedSchemaName] 拒绝系统保留 schema 名: projectID=%d schema=%s", projectID, name)
		return ""
	}
	if err := tenantpg.ValidateIdentifier(name); err != nil {
		logx.WithContext(ctx).Errorf("[resolvePreallocatedSchemaName] 非法 schema 名: projectID=%d schema=%s err=%v", projectID, name, err)
		return ""
	}
	return name
}

// ErrSchemaNameTaken 用户指定的 schema 名已被同 Project 其他应用占用（(project_id, schema_name) 唯一索引冲突）。
var ErrSchemaNameTaken = errors.New("project.schemaNameTaken")

// resolveImportSchemaName 导入预占 schema 名的决策顺序：
//  1. 显式提交的 schemaName（「高级」区，合法性已由 buildImportOptions 校验）；
//  2. 包内 meta.json appId 的规范化名（app_<uuid 去连字符>，与前端预填规则一致）——
//     避免前端预览未返回/用户未提交时静默回退到 app_<自增ID>；
//  3. 两者均为空（老包无 appId）时返回空串，部署阶段回退 app_<自增ID>。
func resolveImportSchemaName(ctx context.Context, schemaName, metaAppID string, projectID int64) string {
	if name := resolvePreallocatedSchemaName(ctx, schemaName, projectID); name != "" {
		return name
	}
	if fallback := normalizeSchemaName(metaAppID); fallback != "" {
		logx.WithContext(ctx).Infof("[resolveImportSchemaName] schemaName 为空，按包内 appId 命名 schema: projectID=%d schema=%s", projectID, fallback)
		return fallback
	}
	return ""
}

// validateSchemaOwnedByApp 校验待删除的 schema 确实属于当前 app：
// 若 (project_id, schema_name) 未命中当前 app（schema 被其他活跃 app 预占或记录被异常修改），返回错误，
// 调用方跳过 DROP SCHEMA，避免误删其他 App 的数据。
func validateSchemaOwnedByApp(ctx context.Context, projectID, appID int64, schemaName string) error {
	if strings.TrimSpace(schemaName) == "" {
		return nil
	}
	apps, err := repo.NewProjectRepo(ctx).ListProjectApps(ctx, projectID)
	if err != nil {
		return fmt.Errorf("query apps for schema ownership: %w", err)
	}
	for _, a := range apps {
		if a.ID == appID {
			continue
		}
		if a.SchemaName == schemaName {
			return fmt.Errorf("schema %q is owned by another app (appID=%d)", schemaName, a.ID)
		}
	}
	return nil
}

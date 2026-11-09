// 导出需要查看能力，使用真实角色/权限映射检查独立导出权限。
package audit

import (
	"backend/internal/contextx"
	"backend/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAuditExportLogic(t *testing.T) {
	svc, ctx := newAuditEnv(t)
	mustInsertAuditLog(t, svc, 1, "UNS", "Update", "1", "Device", 200, 1)
	_, err := NewAuditLogExportLogic(ctx, svc).AuditLogExport(&types.AuditLogPageReq{})
	requireHTTPError(t, err, 403)
	subject, _ := contextx.SubjectFrom(ctx)
	subject.ResourceKeys = map[string]struct{}{"system.audit.log": {}, "system.audit.export": {}}
	ctx = contextx.WithSubject(ctx, subject)
	resp, err := NewAuditLogExportLogic(ctx, svc).AuditLogExport(&types.AuditLogPageReq{})
	require.NoError(t, err)
	require.Equal(t, 1, resp.Data.(map[string]any)["total"])
	var count int64
	require.NoError(t, svc.App.Store.CommonDB().Table("sys_resource_action").Where("action_value = ? AND methods = ?", "/api/core/audit-logs/export", "POST").Count(&count).Error)
	require.EqualValues(t, 1, count)
	subject.ResourceKeys = map[string]struct{}{"system.audit.log": {}}
	allow, _, err := svc.App.Permission.Allow(ctx, subject, "api", "POST", "/api/core/audit-logs/export")
	require.NoError(t, err)
	require.False(t, allow)
}

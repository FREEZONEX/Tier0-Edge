// 审计仓储回归：完整中文详情、元数据、幂等写入和组合条件，使用独立 PostgreSQL 库。
package repo_test

import (
	"backend/internal/repo"
	"backend/internal/testkit"
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAuditStorageAndFilters(t *testing.T) {
	testkit.NewTestDB(t)
	ctx := context.Background()
	r := repo.NewAuditRepo(ctx)
	raw, _ := json.Marshal(map[string]any{"after": map[string]any{"path": "厂区/设备_1%", "description": strings.Repeat("完整中文", 500)}})
	now := time.Now().UTC().Truncate(time.Millisecond)
	item := repo.AuditLog{UserID: 1, OperatorName: "测试管理员", ScopeType: 2, ScopeID: 42, ScopeName: "工厂", ResType: "UNS", ResID: "123", ResName: strings.Repeat("中", 300), ResourcePath: "厂区/设备_1%", BusinessType: "Update", Code: 201, DetailJSON: string(raw), RequestID: "req-audit", IP: "127.0.0.1", UserAgent: "test-agent", CreatedTime: now, EventID: "event-audit"}
	require.NoError(t, r.InsertAuditLog(ctx, item))
	require.NoError(t, r.InsertAuditLog(ctx, item))
	rows, total, err := r.ListAuditLogs(ctx, repo.AuditLogFilter{ScopeType: 2, ScopeID: 42, ScopeName: "工", OperatorID: 1, ResourceKeyword: "设备_1%", RequestID: "req-audit", IP: "127.0.0.1", Result: "success", StartTime: now.Add(-time.Second).UnixMilli(), EndTime: now.UnixMilli()})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	require.Equal(t, string(raw), rows[0].DetailJSON)
	require.True(t, rows[0].DetailValid)
	require.Len(t, []rune(rows[0].ResName), 256)
	require.Equal(t, 2, rows[0].ScopeType)
	require.Equal(t, "test-agent", rows[0].UserAgent)
	require.Equal(t, "success", rows[0].Result)
	for _, f := range []repo.AuditLogFilter{{Result: "accepted"}, {Result: "failed"}, {ScopeType: 1}, {ScopeType: -1}, {OperatorID: 2}, {ResourceKeyword: "设备X1"}, {RequestID: "wrong"}, {IP: "127.0.0.2"}, {ScopeName: "wrong"}, {ResID: "wrong"}, {ResType: "Auth"}, {BusinessTypeCode: repo.AuditBizDelete}, {StartTime: now.Add(time.Second).UnixMilli()}} {
		_, total, err = r.ListAuditLogs(ctx, f)
		require.NoError(t, err)
		require.Zero(t, total)
	}
	item.EventID = "legacy"
	item.ScopeType = 0
	item.Code = 500
	item.DetailJSON = `{"before":`
	require.NoError(t, r.InsertAuditLog(ctx, item))
	rows, total, err = r.ListAuditLogs(ctx, repo.AuditLogFilter{ScopeType: -1, Result: "failed"})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.False(t, rows[0].DetailValid)
	item.EventID = "accepted"
	item.Code = 202
	require.NoError(t, r.InsertAuditLog(ctx, item))
	rows, _, err = r.ListAuditLogs(ctx, repo.AuditLogFilter{Result: "accepted"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestAuditBusinessResultAndLegacyFilters(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	r := repo.NewAuditRepo(ctx)
	require.NoError(t, r.InsertAuditLog(ctx, repo.AuditLog{UserID: 1, Code: 200, Result: "accepted", EventID: "async", ResType: "UNS"}))
	require.NoError(t, r.InsertAuditLog(ctx, repo.AuditLog{UserID: 1, Code: 200, Result: "failed", EventID: "failed-job", ResType: "UNS"}))
	require.NoError(t, store.CommonDB().Exec(`INSERT INTO sys_user_oper_log(user_id,res_type,business_type,code) VALUES (1,'UNS',4,202),(1,'UNS',4,200),(1,'UNS',4,500)`).Error)
	for result, want := range map[string]int{"accepted": 2, "success": 1, "failed": 2} {
		rows, count, err := r.ListAuditLogs(ctx, repo.AuditLogFilter{Result: result})
		require.NoError(t, err)
		require.EqualValues(t, want, count)
		for _, row := range rows {
			require.Equal(t, result, row.Result)
		}
		exported, err := r.ExportAuditLogs(ctx, repo.AuditLogFilter{Result: result})
		require.NoError(t, err)
		require.Len(t, exported, want)
		for _, row := range exported {
			require.Equal(t, result, row.Result)
		}
	}
	rows, _, err := r.ListAuditLogs(ctx, repo.AuditLogFilter{Result: "accepted", Code: func() *int { v := 200; return &v }()})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 200, rows[0].Code)
}

func TestAuditLegacyMigration(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	db := store.CommonDB()
	// 每个测试独立库：恢复旧表结构，保留一条旧版截断的详情。
	require.NoError(t, db.Exec(`ALTER TABLE sys_user_oper_log
		DROP COLUMN scope_type, DROP COLUMN scope_id, DROP COLUMN scope_name,
		DROP COLUMN request_id, DROP COLUMN ip, DROP COLUMN user_agent,
		DROP COLUMN result_msg, DROP COLUMN resource_path, DROP COLUMN event_id, DROP COLUMN result,
		ALTER COLUMN detail TYPE VARCHAR(500)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO sys_user_oper_log(user_id,business_type,detail) VALUES(1,2,'{"before":')`).Error)
	migration, err := os.ReadFile("migrations/versions/common/V2.2.5_003__extend_audit_log.sql")
	require.NoError(t, err)
	resultMigration, err := os.ReadFile("migrations/versions/common/V2.2.5_004__audit_result_and_outbox_cleanup.sql")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		require.NoError(t, db.Exec(string(migration)).Error)
		require.NoError(t, db.Exec(string(resultMigration)).Error)
	}
	rows, total, err := repo.NewAuditRepo(context.Background()).ListAuditLogs(context.Background(), repo.AuditLogFilter{ScopeType: -1})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Equal(t, `{"before":`, rows[0].DetailJSON)
	require.False(t, rows[0].DetailValid)
	require.Empty(t, rows[0].RequestID)
	require.Equal(t, "success", rows[0].Result)
	long := strings.Repeat("历史升级后完整详情", 200)
	require.NoError(t, db.Exec("UPDATE sys_user_oper_log SET detail = ?", long).Error)
}

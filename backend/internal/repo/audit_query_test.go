// 导出和字典使用同一真实数据集合，检查上限与分页排序。
package repo_test

import (
	"backend/internal/repo"
	"backend/internal/testkit"
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAuditExportAndOptions(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	r := repo.NewAuditRepo(ctx)
	require.NoError(t, store.CommonDB().Exec(`INSERT INTO sys_user_oper_log(user_id,res_type,business_type,code,operator_name) SELECT 1,'UNS',2,200,'literal_%' FROM generate_series(1,10001)`).Error)
	rows, err := r.ExportAuditLogs(ctx, repo.AuditLogFilter{OperatorKeyword: "_%"})
	require.NoError(t, err)
	require.Len(t, rows, repo.AuditExportLimit+1)
	require.Greater(t, rows[0].ID, rows[1].ID)
	options, err := r.AuditFilterOptions(ctx, repo.AuditLogFilter{})
	require.NoError(t, err)
	require.Equal(t, []repo.AuditOption{{ResType: "UNS", BusinessTypeCode: 2, BusinessType: "Update"}}, options)
	rows, err = r.ExportAuditLogs(ctx, repo.AuditLogFilter{Result: "failed"})
	require.NoError(t, err)
	require.Empty(t, rows)
}

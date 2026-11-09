// 查询参数边界与 CSV 注入回归；导出超限不得静默截断。
package audit

import (
	"backend/internal/testkit"
	"context"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestAuditQueryValidation(t *testing.T) {
	for _, q := range []PageQuery{{StartTime: "invalid"}, {StartTime: "2000", EndTime: "1000"}, {Result: "unknown"}, {ScopeType: 3}, {IP: "invalid"}, {BusinessType: "Unknown"}, {OperatorID: -1}} {
		_, err := buildFilter(q)
		require.ErrorIs(t, err, ErrInvalidFilter)
	}
	_, err := buildFilter(PageQuery{StartTime: "2026-09-06T01:00:00+08:00", EndTime: "2026-09-06T01:00:00Z", BusinessType: "Update"})
	require.NoError(t, err)
	for _, value := range []string{"=1+1", " \t@SUM(A1)", "+12", "-1", "\tname", "\rname"} {
		require.True(t, strings.HasPrefix(csvCell(value), "'"))
	}
	require.Equal(t, "普通文本", csvCell("普通文本"))
}

func TestAuditExportLimit(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	s := New(context.Background())
	require.NoError(t, store.CommonDB().Exec(`INSERT INTO sys_user_oper_log(user_id,res_type,business_type,code,res_name) SELECT 1,'UNS',2,200,'=SUM(1,2)' FROM generate_series(1,10001)`).Error)
	_, err := s.Export(context.Background(), PageQuery{})
	require.ErrorIs(t, err, ErrExportLimit)
	require.NoError(t, store.CommonDB().Exec(`DELETE FROM sys_user_oper_log WHERE id > 1`).Error)
	result, err := s.Export(context.Background(), PageQuery{})
	require.NoError(t, err)
	require.Contains(t, result["content"], "'=SUM(1,2)")
	require.Equal(t, 1, result["total"])
}

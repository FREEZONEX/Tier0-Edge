package edgesql

// 使用独立真实 PostgreSQL 检查实际语法/对象错误、只读限制与结果大小边界。
import (
	"context"
	"testing"

	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestSQLSafeErrorsPostgres(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	db, err := store.UnsDB().DB()
	require.NoError(t, err)
	for _, tc := range []struct{ sql, code string }{
		{"select broken syntax ???", SQLInvalidCode},
		{"select missing_column from pg_catalog.pg_class limit 1", SQLObjectNotFoundCode},
		{"create table forbidden_sql_test(id integer)", SQLInvalidCode},
		{"select repeat('x',1048577) as payload", "SOURCE_RESOURCE_LIMIT"},
	} {
		_, _, _, err := ReadNamed(context.Background(), db, tc.sql, 1, "postgres")
		require.Error(t, err)
		require.Equal(t, tc.code, DiagnosticCode(SafeError(context.Background(), err)))
	}
	items, _, truncated, err := ReadNamed(context.Background(), db, "select generate_series(1,3) as n", 1, "postgres")
	require.NoError(t, err)
	require.True(t, truncated)
	require.Len(t, items, 1)
}

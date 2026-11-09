package repo_test

// 用真实 PostgreSQL 和正式迁移核对 Go/SQL 字段路径合同，避免查询与唯一约束使用不同解释。
import (
	"encoding/json"
	"strings"
	"testing"

	"backend/internal/fieldpath"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func TestDatabaseFieldPathsMatchPublicParser(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	db := store.UnsDB()
	for _, path := range []string{
		"value", "设备.温度", `a\.b.c\\d`, "items.0.name", "a?b.c'd", "a.00",
		strings.Repeat("x", 512), "a.b.c.d.e.f.g.h.i.j",
	} {
		expected, err := fieldpath.Parse(path)
		require.NoError(t, err)
		var encoded string
		require.NoError(t, db.Raw("SELECT to_json(public.parse_field_path(?))::text", path).Scan(&encoded).Error, "%q", path)
		var actual []string
		require.NoError(t, json.Unmarshal([]byte(encoded), &actual))
		require.Equal(t, expected, actual, "%q", path)
	}
	for _, path := range []string{
		"", ".a", "a.", "a..b", `a\`, `a\q`, "a.$b", "a.__proto__", "a.prototype", "a.constructor",
		"a.\nb", "a.\u007fb", strings.Repeat("x", 513), strings.Repeat("温", 171), "a.b.c.d.e.f.g.h.i.j.k",
	} {
		_, err := fieldpath.Parse(path)
		require.ErrorIs(t, err, fieldpath.ErrInvalid, "%q", path)
		var encoded string
		require.Error(t, db.Raw("SELECT to_json(public.parse_field_path(?))::text", path).Scan(&encoded).Error, "%q", path)
	}
	for _, value := range []string{"", "温度", "a'b", "?", "a?'b?", `a\b`, "');DROP TABLE public.uns_meta;--"} {
		var actual string
		require.NoError(t, db.Raw("SELECT "+fieldpath.PGTextLiteral(value)).Scan(&actual).Error)
		require.Equal(t, value, actual)
	}
	var count int64
	require.NoError(t, db.Table("public.uns_meta").Count(&count).Error, "literal text must never execute as SQL")
}

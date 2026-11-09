package repo_test

// 独立本地PG：所有发现SQL在只读连接上成功，缺失投影只影响对应能力。
import (
	"context"
	"testing"

	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func TestCapabilitySchemasReadOnlyAndIndependent(t *testing.T) {
	store, _ := testkit.NewTestDB(t)
	ctx := context.Background()
	db, err := store.UnsDB().DB()
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	require.NoError(t, store.UnsDB().Exec("SET default_transaction_read_only=on").Error)
	actual := store.CapabilitySchemas(ctx)
	require.Equal(t, map[string]bool{"namespace.v2": true, "source": true, "stream": true}, actual)
	require.NoError(t, store.UnsDB().Exec("SET default_transaction_read_only=off").Error)
	require.NoError(t, store.UnsDB().Exec("DROP VIEW public.source_projection").Error)
	require.Equal(t, map[string]bool{"namespace.v2": true, "source": false, "stream": true}, store.CapabilitySchemas(ctx))
}

package repo_test

import (
	"context"
	"testing"

	"backend/internal/config"
	"backend/internal/testkit"
	"github.com/stretchr/testify/require"
)

func TestSeedPreservesLegacyLoginGrants(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		t.Run(map[bool]string{true: "legacy_login", false: "export_only"}[legacy], func(t *testing.T) {
			store, _ := testkit.NewTestDB(t)
			db := store.CommonDB()
			// 模拟旧权限形态及自定义角色，不修改系统角色的默认授权。
			require.NoError(t, db.Exec(`INSERT INTO sys_role_resource(role_id, resource_id) VALUES (9001, 191)`).Error)
			if legacy {
				require.NoError(t, db.Exec(`DELETE FROM sys_resource_info WHERE id = 192`).Error)
				require.NoError(t, db.Exec(`UPDATE sys_resource_info SET resource_key = 'system.login.log' WHERE id = 191`).Error)
			}
			for range 2 {
				require.NoError(t, store.Seed(context.Background(), config.SecurityConf{}, config.GatewayConf{}))
			}
			var count int64
			require.NoError(t, db.Table("sys_role_resource").Where("role_id = 9001 AND resource_id = 192 AND deleted_time = 0").Count(&count).Error)
			if legacy {
				require.EqualValues(t, 1, count)
			} else {
				require.Zero(t, count)
			}
		})
	}
}

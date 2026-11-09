// TASK-028 cliauth session_store 单元测试：
//   目标：覆盖 CLI 授权会话的内存存储（包级 sync.Map）读写与过期逻辑，纯函数不依赖 DB。
//   正常路径：setupCode 归一化（trim+upper）；绑定后状态为 completed 且携带 apiKey；
//             未绑定返回 pending。
//   异常情况：会话过期返回 expired 并从存储中删除；空 setupCode 不写入存储。
package cliauth

import (
	"testing"
	"time"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestNormalizeCliAuthSetupCode(t *testing.T) {
	require.Equal(t, "ABC123", normalizeCliAuthSetupCode("  abc123  "))
	require.Equal(t, "", normalizeCliAuthSetupCode("  "))
}

func TestStoreAndLoadCompleted(t *testing.T) {
	key := "UNIT-COMPLETED"
	cliAuthSessions.Delete(key)
	defer cliAuthSessions.Delete(key)

	storeCliAuthCompleted(key, types.CliAuthBindResp{
		ApiKey:        "t0_unit_key",
		WorkspaceName: "default",
	})
	resp := loadCliAuthStatus(key)
	require.Equal(t, "completed", resp.Status)
	require.Equal(t, "t0_unit_key", resp.ApiKey)
	require.Equal(t, "default", resp.WorkspaceName)
	require.NotEmpty(t, resp.ExpiresAt)
}

func TestLoadStatus_PendingWhenNotStored(t *testing.T) {
	resp := loadCliAuthStatus("UNIT-NOT-STORED")
	require.Equal(t, "pending", resp.Status)
	require.Equal(t, "default", resp.WorkspaceName)
	require.Empty(t, resp.ApiKey)
}

func TestLoadStatus_ExpiredSessionRemoved(t *testing.T) {
	key := "UNIT-EXPIRED"
	cliAuthSessions.Delete(key)
	defer cliAuthSessions.Delete(key)

	cliAuthSessions.Store(key, cliAuthSession{
		status:        "completed",
		apiKey:        "stale",
		workspaceName: "default",
		expiresAt:     time.Now().UTC().Add(-time.Minute).UnixMilli(),
	})
	resp := loadCliAuthStatus(key)
	require.Equal(t, "expired", resp.Status)
	require.Empty(t, resp.ApiKey)
	// 过期会话应被删除，再次查询回到 pending
	_, exists := cliAuthSessions.Load(key)
	require.False(t, exists)
	require.Equal(t, "pending", loadCliAuthStatus(key).Status)
}

func TestStoreCliAuthCompleted_EmptyCodeIgnored(t *testing.T) {
	cliAuthSessions.Delete("")
	storeCliAuthCompleted("   ", types.CliAuthBindResp{ApiKey: "x"})
	_, exists := cliAuthSessions.Load("")
	require.False(t, exists)
}

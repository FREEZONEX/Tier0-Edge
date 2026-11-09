// 字典接口基于现有审计数据生成，非法时间返回 400。
package audit

import (
	"backend/internal/repo"
	"backend/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAuditOptionsLogic(t *testing.T) {
	svc, ctx := newAuditEnv(t)
	mustInsertAuditLog(t, svc, 1, "UNS", "Update", "1", "Device", 200, 1)
	logic := NewAuditLogOptionsLogic(ctx, svc)
	resp, err := logic.AuditLogOptions(&types.AuditLogPageReq{})
	require.NoError(t, err)
	require.Len(t, resp.Data.([]repo.AuditOption), 1)
	_, err = logic.AuditLogOptions(&types.AuditLogPageReq{StartTime: "invalid"})
	requireHTTPError(t, err, 400)
}

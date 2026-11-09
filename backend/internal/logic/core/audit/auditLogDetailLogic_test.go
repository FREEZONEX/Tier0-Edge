// TASK-028 AuditLogDetailLogic 单元测试：
//
//	目标：覆盖审计日志详情查询。
//	正常路径：详情返回单条日志且字段归一化（businessType 由 code 反解、operatorId 由 user_id 生成）。
//	异常情况：详情不存在返回 404。
package audit

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditLogDetailLogic_Detail_Success(t *testing.T) {
	svcCtx, ctx := newAuditEnv(t)
	id := mustInsertAuditLog(t, svcCtx, 1, "UserManagement", "Create", "10", "alice", 200, 1)

	l := NewAuditLogDetailLogic(ctx, svcCtx)
	resp, err := l.AuditLogDetail(&types.IdReq{Id: id})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	// 详情直接包 repo.AuditLog 结构体（非 map）
	item, ok := resp.Data.(repo.AuditLog)
	require.True(t, ok, "detail data should be repo.AuditLog")
	assert.Equal(t, "alice", item.ResName)
	assert.Equal(t, "Create", item.BusinessType)
	assert.Equal(t, "1", item.OperatorID)
}

func TestAuditLogDetailLogic_Detail_NotFound(t *testing.T) {
	svcCtx, ctx := newAuditEnv(t)
	l := NewAuditLogDetailLogic(ctx, svcCtx)

	_, err := l.AuditLogDetail(&types.IdReq{Id: 999999})
	requireHTTPError(t, err, 404)
}

// 查询 DTO 透传与错误响应保持一致。
package audit

import (
	domainaudit "backend/internal/domain/audit"
	"backend/internal/types"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAuditQueryMapping(t *testing.T) {
	q := toPageQuery(&types.AuditLogPageReq{ScopeName: "factory", OperatorId: 7, ResId: "device", ResourceKeyword: "name", RequestId: "req", Ip: "127.0.0.1", Result: "failed", Code: 403})
	require.EqualValues(t, 7, q.OperatorID)
	require.Equal(t, "factory", q.ScopeName)
	require.Equal(t, "device", q.ResID)
	require.Equal(t, "name", q.ResourceKeyword)
	require.Equal(t, "req", q.RequestID)
	require.Equal(t, "127.0.0.1", q.IP)
	require.Equal(t, "failed", q.Result)
	require.Equal(t, 403, *q.Code)
	requireHTTPError(t, queryError(domainaudit.ErrInvalidFilter), 400)
	requireHTTPError(t, queryError(domainaudit.ErrExportLimit), 400)
}

// TASK-028 audit 模块测试共享助手：
//
//	目标：提供 Envelope 解包、HTTP 错误断言与审计日志直插工具。
//	正常路径：mustInsertAuditLog 直插 sys_user_oper_log 行（含 detail JSON）。
//	异常情况：无（纯工具函数）。
package audit

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	respx "backend/internal/httpx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

// newAuditEnv 每次调用创建独立测试库 + miniredis 的完整 ServiceContext 与登录 ctx。
func newAuditEnv(t *testing.T) (*svc.ServiceContext, context.Context) {
	t.Helper()
	return svctest.NewTestServiceContext(t), svctest.CtxWithUser(1)
}

// respData 解包成功响应的 Data 为 map[string]any。
func respData(t *testing.T, resp *types.Envelope) map[string]any {
	t.Helper()
	require.NotNil(t, resp)
	require.Equal(t, 200, resp.Code)
	m, ok := resp.Data.(map[string]any)
	require.True(t, ok, "envelope data should be map[string]any")
	return m
}

// requireHTTPError 断言 err 为 logicx.Error 包装出的 HTTP 错误且状态码匹配。
func requireHTTPError(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	var httpErr *respx.HTTPError
	require.ErrorAs(t, err, &httpErr, "logicx.Error should wrap respx.HTTPError")
	require.Equal(t, status, httpErr.Status)
}

// mustInsertAuditLog 直插一条审计日志，返回其 ID。
func mustInsertAuditLog(t *testing.T, svcCtx *svc.ServiceContext, userID int64, resType, bizType, resID, resName string, code int64, showInRecent int64) int64 {
	t.Helper()
	detail, _ := json.Marshal(map[string]any{"path": "/api/core/test", "method": "POST"})
	row := repo.AuditLog{
		UserID:           userID,
		ResType:          resType,
		ResID:            resID,
		ResName:          resName,
		BusinessTypeCode: repo.AuditBusinessTypeCode(bizType),
		DetailJSON:       string(detail),
		Code:             code,
		IsShowInRecent:   showInRecent,
		CreatedTime:      time.Now().UTC(),
	}
	require.NoError(t, svcCtx.App.Store.CommonDB().Create(&row).Error)
	return row.ID
}

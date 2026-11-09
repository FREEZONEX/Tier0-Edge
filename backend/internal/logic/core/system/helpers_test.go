// TASK-028 system 模块测试共享助手：
//
//	目标：提供 Envelope 解包与 HTTP 错误断言工具，避免重复代码。
//	异常情况：无（纯工具函数）。
package system

import (
	"testing"

	respx "backend/internal/httpx"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

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

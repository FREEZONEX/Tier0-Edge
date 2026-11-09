// TASK-028 auth 模块测试共享助手：
//   目标：为 9 个 logic 文件提供统一的 ServiceContext 装配与 Envelope/HTTP 错误断言。
//   正常路径：创建完整测试环境（独立 PG 库 + miniredis）与带登录 Subject 的 ctx，
//             以及 Envelope 解包、HTTP 错误断言。
//   异常情况：无（纯工具函数，不涉及业务异常分支）。
package auth

import (
	"context"
	"testing"

	respx "backend/internal/httpx"
	"backend/internal/svc"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

// newSvcEnv 每次调用创建独立测试库 + miniredis 的完整 ServiceContext 与登录 ctx。
// ctx 携带 Seed 内置的 tier0 管理员（UserID=1）Subject。
func newSvcEnv(t *testing.T) (*svc.ServiceContext, context.Context) {
	t.Helper()
	return svctest.NewTestServiceContext(t), svctest.CtxWithUser(1)
}

// requireOK 断言响应为成功 Envelope（Code 200）。
func requireOK(t *testing.T, resp *types.Envelope) {
	t.Helper()
	require.NotNil(t, resp)
	require.Equal(t, 200, resp.Code)
}

// requireHTTPError 断言 err 为 HTTP 错误且状态码匹配。
func requireHTTPError(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	var httpErr *respx.HTTPError
	require.ErrorAs(t, err, &httpErr, "expected respx.HTTPError")
	require.Equal(t, status, httpErr.Status)
}

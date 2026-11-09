// TASK-028 apikey 模块测试共享助手：
//   目标：为 4 个 logic 文件提供统一的场景搭建与断言工具。
//   正常路径：构造完整 ServiceContext + 登录 ctx（seed 内置 tier0 管理员）、
//             领域层创建 API Key（与 logic 同链路）、Envelope 解包与 HTTP 错误断言。
//   异常情况：无（纯工具函数，不涉及业务异常分支）。
package apikey

import (
	"context"
	"testing"

	"backend/internal/httpx"
	"backend/internal/svc"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

// newSvcEnv 每次调用创建独立测试库 + miniredis 的完整 ServiceContext 与登录 ctx。
func newSvcEnv(t *testing.T) (*svc.ServiceContext, context.Context) {
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
	var httpErr *httpx.HTTPError
	require.ErrorAs(t, err, &httpErr, "logicx.Error should wrap httpx.HTTPError")
	require.Equal(t, status, httpErr.Status)
}

// mustCreateAPIKey 经领域层创建 API Key，返回 key ID（明文 raw 只用于校验前缀）。
func mustCreateAPIKey(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, name string) (int64, string) {
	t.Helper()
	view, err := svcCtx.App.APIKey.Create(ctx, name, 1, "read_only", "external", "personal", nil)
	require.NoError(t, err)
	id, ok := view["id"].(int64)
	require.True(t, ok, "api key id should be int64")
	require.Positive(t, id)
	raw, _ := view["apiKey"].(string)
	return id, raw
}

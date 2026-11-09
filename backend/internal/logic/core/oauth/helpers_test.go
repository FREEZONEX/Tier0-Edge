// TASK-028 oauth 模块测试共享助手：
//
//	目标：为 7 个 OAuth logic 文件提供统一的环境搭建与断言工具，避免重复代码。
//	正常路径：Envelope 解包、HTTP 错误断言、通过领域层创建 OAuth client。
//	异常情况：无（纯工具函数）。
package oauth

import (
	"context"
	"strings"
	"testing"

	domainoauth "backend/internal/domain/oauth"
	respx "backend/internal/httpx"
	"backend/internal/repo"
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
	var httpErr *respx.HTTPError
	require.ErrorAs(t, err, &httpErr, "logicx.Error should wrap respx.HTTPError")
	require.Equal(t, status, httpErr.Status)
}

// mustCreateOAuthClient 经领域层创建 OAuth client，返回其数据库 id 与 client_id。
// redirectURIs 建议使用完整 URL（Authorize 为精确匹配）。
func mustCreateOAuthClient(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, name string, redirectURIs ...string) (int64, string) {
	t.Helper()
	view, err := svcCtx.App.OAuth.CreateClient(ctx, domainoauth.ClientCommand{
		ClientName:     name,
		RedirectURIs:   redirectURIs,
		AllowedOrigins: []string{"http://localhost:3000"},
		UserID:         1,
	})
	require.NoError(t, err)
	client, ok := view["client"].(repo.OAuthClient)
	require.True(t, ok, "create client view should carry repo.OAuthClient")
	require.NotEmpty(t, client.ClientID)
	require.Positive(t, client.ID)
	return client.ID, client.ClientID
}

// repeatRune 构造长度为 n 的重复字符字符串，用于超长字段断言。
func repeatRune(r rune, n int) string {
	return strings.Repeat(string(r), n)
}

// newSvcEnvNoUser 创建完整 ServiceContext，但 ctx 不携带登录 Subject。
func newSvcEnvNoUser(t *testing.T) *svc.ServiceContext {
	t.Helper()
	return svctest.NewTestServiceContext(t)
}

// contextWithUser 返回携带指定 userID Subject 的 ctx（用户不存在时用于 404 用例）。
func contextWithUser(t *testing.T, userID int64) context.Context {
	t.Helper()
	return svctest.CtxWithUser(userID)
}

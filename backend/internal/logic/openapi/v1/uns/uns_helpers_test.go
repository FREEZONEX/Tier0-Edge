// uns_helpers_test.go — uns OpenAPI 包测试共用助手。
//
// 测试目标：为各 OpenAPI UNS 用例提供登录 ctx 构造与 HTTP 错误断言助手。
// 正常路径：logicx.Error 把领域错误映射为 respx.HTTPError。
// 异常情况：断言错误状态码。
package uns

import (
	"context"
	"testing"

	"backend/internal/contextx"
	respx "backend/internal/httpx"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

// withUserSubject 构造携带登录用户 Subject 的 context。
func withUserSubject(userID int64) context.Context {
	return contextx.WithSubject(context.Background(), contextx.Subject{
		UserID:   userID,
		UserName: "tier0",
		AuthType: "session",
	})
}

// requireHTTPStatus 断言 err 为 respx.HTTPError 且状态码为 status。
func requireHTTPStatus(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	var httpErr *respx.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, status, httpErr.Status)
}

// createOpenapiUnsFileNode 走 OpenapiUnsNodeCreateLogic 创建 State 下的文件节点，
// 返回响应 data（map[string]any）。
func createOpenapiUnsFileNode(t *testing.T, svcCtx *svc.ServiceContext, name string) map[string]any {
	t.Helper()
	ctx := withUserSubject(1)
	resp, err := NewOpenapiUnsNodeCreateLogic(ctx, svcCtx).OpenapiUnsNodeCreate(&types.OpenapiUnsNodeSaveReq{
		Name:      name,
		NodeType:  "file",
		TopicType: "State",
	})
	require.NoError(t, err)
	data, ok := resp.Data.(map[string]any)
	require.True(t, ok)
	require.Positive(t, data["id"].(int64))
	return data
}

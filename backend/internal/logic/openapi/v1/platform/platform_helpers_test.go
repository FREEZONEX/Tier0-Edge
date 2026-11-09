// platform_helpers_test.go — platform 包测试共用助手。
//
// 测试目标：为 OpenAPI Platform 用例提供 HTTP 错误断言助手。
// 正常路径：logicx.Error 把领域错误映射为 respx.HTTPError。
// 异常情况：断言错误状态码。
package platform

import (
	"testing"

	respx "backend/internal/httpx"

	"github.com/stretchr/testify/require"
)

func requireHTTPStatus(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	var httpErr *respx.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Equal(t, status, httpErr.Status)
}

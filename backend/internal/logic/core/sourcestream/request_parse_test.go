package sourcestream

// 验证真实 HTTP 参数解析边界，避免领域层单测漏掉 go-zero 的 JSON/path 混合标签行为。
import (
	"net/http/httptest"
	"strings"
	"testing"

	"backend/internal/httpx"
	"backend/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSourceCreateHTTPRequestParsing(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/core/uns/sources", strings.NewReader(`{"expectedRevision":0,"document":{"name":"test","protocol":"modbus-tcp","endpoint":"tcp://test:1502","runtimeLocation":"local","modbus":{"unitId":7,"pollIntervalMs":1000,"ranges":[]}}}`))
	r.Header.Set("Content-Type", "application/json")
	var req types.SourceSaveReq
	require.NoError(t, httpx.ParseSourceStreamRequest(r, &req))
	require.Zero(t, req.SourceId)
	require.NotNil(t, req.Document)
}

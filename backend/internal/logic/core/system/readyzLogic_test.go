// TASK-028 ReadyzLogic 单元测试：
//
//	目标：覆盖就绪检查入口。
//	正常路径：DB/Redis 可用时返回 200 及 db/redis 就绪状态。
//	异常情况：store 为 nil 时经 App.Ready 报错并返回 503。
package system

import (
	"context"
	"testing"

	respx "backend/internal/httpx"
	"backend/internal/svc/svctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadyzLogic_Readyz_OK(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	l := NewReadyzLogic(context.Background(), svcCtx)
	resp, err := l.Readyz()
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, 200, resp.Code)

	data, ok := resp.Data.(map[string]string)
	require.True(t, ok, "readiness data should be map[string]string")
	assert.Equal(t, "ok", data["db"])
	assert.Equal(t, "ok", data["redis"])
}

func TestReadyzLogic_Readyz_StoreUnavailable(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	svcCtx.App.Store = nil
	l := NewReadyzLogic(context.Background(), svcCtx)

	_, err := l.Readyz()
	require.Error(t, err)
	var httpErr *respx.HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, 503, httpErr.Status)
}

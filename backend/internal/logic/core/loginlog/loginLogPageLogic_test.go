// 登录日志分页 logic 单元测试：
//
//	目标：覆盖 LoginLogPage 的 Envelope 封装与请求参数透传。
//	正常路径：分页返回 pageNo/pageSize/total/data；userName/ipAddr/code 过滤生效。
//	异常情况：无。
package loginlog

import (
	"context"
	"testing"

	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLoginLogEnv 每次调用创建独立测试库 + miniredis 的完整 ServiceContext 与登录 ctx。
func newLoginLogEnv(t *testing.T) (*svc.ServiceContext, context.Context) {
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

func TestLoginLogPageLogic_Page_Empty(t *testing.T) {
	svcCtx, ctx := newLoginLogEnv(t)
	l := NewLoginLogPageLogic(ctx, svcCtx)

	resp, err := l.LoginLogPage(&types.LoginLogPageReq{PageNo: 1, PageSize: 10})
	require.NoError(t, err)

	view := respData(t, resp)
	assert.Equal(t, 1, view["pageNo"])
	assert.Equal(t, 10, view["pageSize"])
	assert.Equal(t, int64(0), view["total"])
	assert.Empty(t, view["data"])
}

func TestLoginLogPageLogic_Page_WithItems(t *testing.T) {
	svcCtx, ctx := newLoginLogEnv(t)
	loginLogRepo := repo.NewUserLoginLogRepo(ctx)
	require.NoError(t, loginLogRepo.InsertUserLoginLog(ctx, repo.UserLoginLog{UserID: 1, UserName: "tier0", IpAddr: "10.0.0.8", Code: 200}))
	require.NoError(t, loginLogRepo.InsertUserLoginLog(ctx, repo.UserLoginLog{UserName: "ghost", IpAddr: "192.168.1.2", Code: 401, Msg: "invalid username or password"}))

	l := NewLoginLogPageLogic(ctx, svcCtx)
	resp, err := l.LoginLogPage(&types.LoginLogPageReq{PageNo: 1, PageSize: 10})
	require.NoError(t, err)

	view := respData(t, resp)
	assert.Equal(t, int64(2), view["total"])
	items, ok := view["data"].([]repo.UserLoginLog)
	require.True(t, ok, "data should be []repo.UserLoginLog")
	require.Len(t, items, 2)
	assert.Equal(t, "ghost", items[0].UserName)
}

func TestLoginLogPageLogic_Page_FilterByCode(t *testing.T) {
	svcCtx, ctx := newLoginLogEnv(t)
	loginLogRepo := repo.NewUserLoginLogRepo(ctx)
	require.NoError(t, loginLogRepo.InsertUserLoginLog(ctx, repo.UserLoginLog{UserID: 1, UserName: "tier0", Code: 200}))
	require.NoError(t, loginLogRepo.InsertUserLoginLog(ctx, repo.UserLoginLog{UserName: "ghost", Code: 401}))

	l := NewLoginLogPageLogic(ctx, svcCtx)
	resp, err := l.LoginLogPage(&types.LoginLogPageReq{PageNo: 1, PageSize: 10, Code: 200, UserName: "tier"})
	require.NoError(t, err)

	view := respData(t, resp)
	assert.Equal(t, int64(1), view["total"])
}

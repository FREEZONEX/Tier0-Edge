// TASK-028 CliAuthStatusLogic 单元测试：
//   目标：覆盖 CLI 设备码轮询状态查询。
//   正常路径：已绑定 setupCode 返回 completed（含 apiKey）；未绑定返回 pending。
//   异常情况：setupCode 为空返回 400。
package cliauth

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestCliAuthStatus_MissingSetupCode(t *testing.T) {
	svcCtx, ctx := newCliAuthEnv(t)
	l := NewCliAuthStatusLogic(ctx, svcCtx)

	_, err := l.CliAuthStatus(&types.CliAuthStatusReq{})
	requireHTTPError(t, err, 400)
}

func TestCliAuthStatus_PendingBeforeBind(t *testing.T) {
	svcCtx, ctx := newCliAuthEnv(t)
	l := NewCliAuthStatusLogic(ctx, svcCtx)

	resp, err := l.CliAuthStatus(&types.CliAuthStatusReq{SetupCode: "status-pending-01"})
	require.NoError(t, err)
	data, ok := resp.Data.(types.CliAuthStatusResp)
	require.True(t, ok, "data should be CliAuthStatusResp")
	require.Equal(t, "pending", data.Status)
	require.Equal(t, "default", data.WorkspaceName)
}

func TestCliAuthStatus_CompletedAfterBind(t *testing.T) {
	svcCtx, ctx := newCliAuthEnv(t)
	code := "status-done-01"
	defer cliAuthSessions.Delete(code)

	// 先经 Bind 链路完成绑定
	bindResp, err := NewCliAuthBindLogic(ctx, svcCtx).CliAuthBind(&types.CliAuthBindReq{SetupCode: code})
	require.NoError(t, err)
	bindData, ok := bindResp.Data.(types.CliAuthBindResp)
	require.True(t, ok, "data should be CliAuthBindResp")
	apiKey := bindData.ApiKey
	require.NotEmpty(t, apiKey)

	l := NewCliAuthStatusLogic(ctx, svcCtx)
	resp, err := l.CliAuthStatus(&types.CliAuthStatusReq{SetupCode: code})
	require.NoError(t, err)
	data, ok := resp.Data.(types.CliAuthStatusResp)
	require.True(t, ok)
	require.Equal(t, "completed", data.Status)
	require.Equal(t, apiKey, data.ApiKey)
}

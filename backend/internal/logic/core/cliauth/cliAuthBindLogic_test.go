// TASK-028 CliAuthBindLogic 单元测试：
//   目标：覆盖 CLI 设备码绑定：校验 setupCode 必填、默认命名/默认权限、自定义覆盖，
//         以及绑定完成后会话状态写入。
//   正常路径：绑定成功返回 apiKey/name/permission，API Key 落库，会话状态为 completed。
//   异常情况：setupCode 为空返回 400。
package cliauth

import (
	"context"
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestCliAuthBind_MissingSetupCode(t *testing.T) {
	svcCtx, ctx := newCliAuthEnv(t)
	l := NewCliAuthBindLogic(ctx, svcCtx)

	_, err := l.CliAuthBind(&types.CliAuthBindReq{})
	requireHTTPError(t, err, 400)
}

func TestCliAuthBind_Defaults(t *testing.T) {
	svcCtx, ctx := newCliAuthEnv(t)
	defer cliAuthSessions.Delete("CLI-DEFAULT-01")
	l := NewCliAuthBindLogic(ctx, svcCtx)

	resp, err := l.CliAuthBind(&types.CliAuthBindReq{SetupCode: "CLI-DEFAULT-01"})
	require.NoError(t, err)
	requireOK(t, resp)
	data, ok := resp.Data.(types.CliAuthBindResp)
	require.True(t, ok, "data should be CliAuthBindResp")
	require.NotEmpty(t, data.ApiKey)
	// 默认命名：cli-<setupCode 小写>
	require.Equal(t, "cli-cli-default-01", data.Name)
	// 默认权限：full_access
	require.Equal(t, "full_access", data.Permission)
	require.Equal(t, "default", data.WorkspaceName)

	// 绑定完成后 CLI 轮询应可见 completed
	status := loadCliAuthStatus("cli-default-01")
	require.Equal(t, "completed", status.Status)
	require.Equal(t, data.ApiKey, status.ApiKey)
}

func TestCliAuthBind_CustomNameAndPermission(t *testing.T) {
	svcCtx, ctx := newCliAuthEnv(t)
	l := NewCliAuthBindLogic(ctx, svcCtx)

	resp, err := l.CliAuthBind(&types.CliAuthBindReq{
		SetupCode:  "custom-77",
		Name:       "my-cli-token",
		Permission: "read_only",
	})
	require.NoError(t, err)
	data, ok := resp.Data.(types.CliAuthBindResp)
	require.True(t, ok, "data should be CliAuthBindResp")
	require.Equal(t, "my-cli-token", data.Name)
	require.Equal(t, "read_only", data.Permission)
	require.NotEmpty(t, data.ApiKey)
}

func TestCliAuthBind_NoUserIDCreatesWithZeroOwner(t *testing.T) {
	// 登录态由 HTTP middleware 保证，logic 层不校验 UserID；
	// 无 Subject 的 ctx 下 UserID=0，key 仍会创建（owner_id=0），属实际生产行为。
	svcCtx, _ := newCliAuthEnv(t)
	l := NewCliAuthBindLogic(context.Background(), svcCtx)

	resp, err := l.CliAuthBind(&types.CliAuthBindReq{SetupCode: "nouser-1"})
	require.NoError(t, err)
	requireOK(t, resp)
	data, ok := resp.Data.(types.CliAuthBindResp)
	require.True(t, ok, "data should be CliAuthBindResp")
	require.NotEmpty(t, data.ApiKey)
}

func TestMapString(t *testing.T) {
	require.Equal(t, "", mapString(nil, "k"))
	require.Equal(t, "", mapString(map[string]any{"k": 42}, "k"))
	require.Equal(t, "v", mapString(map[string]any{"k": "v"}, "k"))
	require.Equal(t, "", mapString(map[string]any{"k": "v"}, "missing"))
}

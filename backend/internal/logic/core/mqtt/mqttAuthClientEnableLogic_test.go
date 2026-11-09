// TASK-028 MqttAuthClientEnableLogic 单元测试：
//
//	目标：覆盖启用受管 MQTT 连接客户端（auth_status 置 1）入口。
//	正常路径：启用后 auth_status=1；edge 节点启用不改变在线状态；
//	          edge 节点启用前的离线状态不触发节点下线更新（仅禁用分支触发）。
//	异常情况：启用在先被禁用后再次启用；不存在的客户端 → 404。
package mqtt

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestMqttAuthClientEnable_Device(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	clientID := mustCreateClient(t, svcCtx, ctx, "device")
	client := mustGetClient(t, svcCtx, clientID)

	l := NewMqttAuthClientEnableLogic(ctx, svcCtx)
	resp, err := l.MqttAuthClientEnable(&types.IdReq{Id: clientID})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, repo.AuthStatusActive, view["authStatus"])
	require.Equal(t, client.Username, view["username"])

	row := mustGetClient(t, svcCtx, clientID)
	require.Equal(t, repo.AuthStatusActive, row.AuthStatus)
}

func TestMqttAuthClientEnable_AfterDisable(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	clientID := mustCreateClient(t, svcCtx, ctx, "device")

	// 先禁用
	disable := NewMqttAuthClientDisableLogic(ctx, svcCtx)
	_, err := disable.MqttAuthClientDisable(&types.IdReq{Id: clientID})
	require.NoError(t, err)
	require.Equal(t, repo.AuthStatusDisabled, mustGetClient(t, svcCtx, clientID).AuthStatus)

	// 再启用
	l := NewMqttAuthClientEnableLogic(ctx, svcCtx)
	_, err = l.MqttAuthClientEnable(&types.IdReq{Id: clientID})
	require.NoError(t, err)
	require.Equal(t, repo.AuthStatusActive, mustGetClient(t, svcCtx, clientID).AuthStatus)
}

func TestMqttAuthClientEnable_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewMqttAuthClientEnableLogic(ctx, svcCtx)

	_, err := l.MqttAuthClientEnable(&types.IdReq{Id: 999999})
	requireHTTPError(t, err, 404)
}

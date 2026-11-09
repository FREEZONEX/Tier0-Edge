// TASK-028 MqttAuthClientDisableLogic 单元测试：
//
//	目标：覆盖禁用受管 MQTT 连接客户端（auth_status 置 2、connect_status 置 2）入口。
//	正常路径：禁用后 auth_status=2 且 connect_status=2；
//	          Edge 凭据禁用后不能继续通过身份校验。
//	异常情况：不存在的客户端 → 404。
package mqtt

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestMqttAuthClientDisable_Device(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	clientID := mustCreateClient(t, svcCtx, ctx, "device")

	l := NewMqttAuthClientDisableLogic(ctx, svcCtx)
	resp, err := l.MqttAuthClientDisable(&types.IdReq{Id: clientID})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, repo.AuthStatusDisabled, view["authStatus"])

	row := mustGetClient(t, svcCtx, clientID)
	require.Equal(t, repo.AuthStatusDisabled, row.AuthStatus)
	require.Equal(t, repo.ConnectStatusOffline, row.ConnectStatus)
}

func TestMqttAuthClientDisable_EdgeCredentialRevoked(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	edgeID := mustCreateEdgeClient(t, svcCtx, ctx)
	l := NewMqttAuthClientDisableLogic(ctx, svcCtx)
	_, err := l.MqttAuthClientDisable(&types.IdReq{Id: edgeID})
	require.NoError(t, err)
	row := mustGetClient(t, svcCtx, edgeID)
	require.Equal(t, repo.AuthStatusDisabled, row.AuthStatus)
}

func TestMqttAuthClientDisable_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewMqttAuthClientDisableLogic(ctx, svcCtx)

	_, err := l.MqttAuthClientDisable(&types.IdReq{Id: 999999})
	requireHTTPError(t, err, 404)
}

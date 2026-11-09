// TASK-028 MqttAuthClientResetPasswordLogic 单元测试：
//
//	目标：覆盖重置受管 MQTT 连接客户端密码入口。
//	正常路径：重置后返回新密码（非空、与旧密码不同），connect_status 置为 offline；
//	          updated_by 记录操作人。
//	异常情况：不存在的客户端 → 404。
package mqtt

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestMqttAuthClientResetPassword_Device(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	clientID := mustCreateClient(t, svcCtx, ctx, "device")
	oldPassword := mustGetClient(t, svcCtx, clientID).Password

	l := NewMqttAuthClientResetPasswordLogic(ctx, svcCtx)
	resp, err := l.MqttAuthClientResetPassword(&types.IdReq{Id: clientID})
	require.NoError(t, err)
	view := respData(t, resp)
	require.NotEmpty(t, view["password"])
	require.NotEqual(t, oldPassword, view["password"])

	row := mustGetClient(t, svcCtx, clientID)
	require.Equal(t, view["password"], row.Password)
	require.Equal(t, repo.ConnectStatusOffline, row.ConnectStatus)
	require.Equal(t, int64(1), row.UpdatedBy)
}

func TestMqttAuthClientResetPassword_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewMqttAuthClientResetPasswordLogic(ctx, svcCtx)

	_, err := l.MqttAuthClientResetPassword(&types.IdReq{Id: 999999})
	requireHTTPError(t, err, 404)
}

// TASK-028 MqttAuthClientCreateLogic 单元测试：
//
//	目标：覆盖创建受管 MQTT 连接客户端入口。
//	正常路径：创建 device 客户端成功，返回凭据并落库（用户名=clientId=id，
//	          auth_status=1、connect_status=2）；type 传任意可创建类型都归一为 device。
//	异常情况：空名称 → 400；名称/描述超长 → 400；未登录（无 Subject）→ 400。
package mqtt

import (
	"context"
	"testing"

	"backend/internal/domain/common"
	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestMqttAuthClientCreate_Device(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewMqttAuthClientCreateLogic(ctx, svcCtx)

	resp, err := l.MqttAuthClientCreate(&types.MqttAuthClientCreateReq{
		Name:                        "my-device",
		ClientType:                  "device",
		Description:                 "camera gateway",
		ClientIdRandomSuffixEnabled: true,
	})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, "my-device", view["name"])
	require.NotEmpty(t, view["clientId"])
	require.NotEmpty(t, view["username"])
	require.NotEmpty(t, view["password"])
	require.Equal(t, "device", view["type"])

	row := mustGetClient(t, svcCtx, view["id"].(int64))
	require.Equal(t, repo.AuthStatusActive, row.AuthStatus)
	require.Equal(t, repo.ConnectStatusOffline, row.ConnectStatus)
	require.Equal(t, repo.ConnectClientTypeDevice, row.Type)
	require.Equal(t, "my-device", row.Name)
	require.Equal(t, "camera gateway", row.Description)
	require.Equal(t, int64(1), row.CreatedBy)
	require.True(t, row.ClientIDRandomSuffixEnabled)
}

func TestMqttAuthClientCreate_SystemTypeAllowed(t *testing.T) {
	// 生产行为：创建入口允许 system 类型（creatableClientTypeValue 保留 system）。
	svcCtx, ctx := newSvcEnv(t)
	l := NewMqttAuthClientCreateLogic(ctx, svcCtx)

	resp, err := l.MqttAuthClientCreate(&types.MqttAuthClientCreateReq{
		Name:       "sys-client",
		ClientType: "system",
	})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, "system", view["type"])
	require.Equal(t, repo.ConnectClientTypeSystem, mustGetClient(t, svcCtx, view["id"].(int64)).Type)
}

func TestMqttAuthClientCreate_InvalidName(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewMqttAuthClientCreateLogic(ctx, svcCtx)

	_, err := l.MqttAuthClientCreate(&types.MqttAuthClientCreateReq{Name: "   "})
	requireHTTPError(t, err, 400)

	_, err = l.MqttAuthClientCreate(&types.MqttAuthClientCreateReq{
		Name: repeatRune('a', common.ConnectionNameMaxLen+1),
	})
	requireHTTPError(t, err, 400)
}

func TestMqttAuthClientCreate_InvalidDescription(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewMqttAuthClientCreateLogic(ctx, svcCtx)

	_, err := l.MqttAuthClientCreate(&types.MqttAuthClientCreateReq{
		Name:        "ok-name",
		Description: repeatRune('x', common.DescMaxLen+1),
	})
	requireHTTPError(t, err, 400)
}

func TestMqttAuthClientCreate_NoSubject(t *testing.T) {
	// 生产行为：无登录 Subject 时 UserID=0，客户端仍可创建（CreatedBy=0），
	// 不校验操作人。
	svcCtx := newSvcEnvNoUser(t)
	l := NewMqttAuthClientCreateLogic(context.Background(), svcCtx)

	resp, err := l.MqttAuthClientCreate(&types.MqttAuthClientCreateReq{Name: "no-user"})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, int64(0), mustGetClient(t, svcCtx, view["id"].(int64)).CreatedBy)
}

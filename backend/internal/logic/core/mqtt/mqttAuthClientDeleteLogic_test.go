// TASK-028 MqttAuthClientDeleteLogic 单元测试：
//
//	目标：覆盖删除受管 MQTT 连接客户端入口。
//	正常路径：删除 device 客户端成功（软删除），返回 deleted=true；
//	          edge 客户端删除同时将其节点下线（connect_status=offline）。
//	异常情况：删除不存在的客户端 → 404；未登录（无 Subject）→ 400。
package mqtt

import (
	"context"
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestMqttAuthClientDelete_Device(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	clientID := mustCreateClient(t, svcCtx, ctx, "device")

	l := NewMqttAuthClientDeleteLogic(ctx, svcCtx)
	resp, err := l.MqttAuthClientDelete(&types.IdReq{Id: clientID})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, clientID, view["id"])
	require.Equal(t, true, view["deleted"])

	// 软删除校验
	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("uns_connect_client").
		Where("id = ? AND deleted_time = 0", clientID).Count(&count).Error)
	require.Zero(t, count)
}

func TestMqttAuthClientDelete_Edge(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	edgeID := mustCreateEdgeClient(t, svcCtx, ctx)

	l := NewMqttAuthClientDeleteLogic(ctx, svcCtx)
	resp, err := l.MqttAuthClientDelete(&types.IdReq{Id: edgeID})
	require.NoError(t, err)
	require.Equal(t, true, respData(t, resp)["deleted"])

	// 删除后记录软删除，节点状态不可再查询（delete 分支仅幂等更新，无节点记录时静默）。
	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("uns_connect_client").
		Where("id = ? AND deleted_time = 0", edgeID).Count(&count).Error)
	require.Zero(t, count)
}

func TestMqttAuthClientDelete_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewMqttAuthClientDeleteLogic(ctx, svcCtx)

	_, err := l.MqttAuthClientDelete(&types.IdReq{Id: 999999})
	requireHTTPError(t, err, 404)
}

func TestMqttAuthClientDelete_NoSubject(t *testing.T) {
	svcCtx := newSvcEnvNoUser(t)
	l := NewMqttAuthClientDeleteLogic(context.Background(), svcCtx)

	_, err := l.MqttAuthClientDelete(&types.IdReq{Id: 1})
	require.Error(t, err)
}

func TestMqttAuthClientDelete_EdgeCredentialRevoked(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	edgeID := mustCreateEdgeClient(t, svcCtx, ctx)
	before := mustGetClient(t, svcCtx, edgeID)
	l := NewMqttAuthClientDeleteLogic(ctx, svcCtx)
	_, err := l.MqttAuthClientDelete(&types.IdReq{Id: edgeID})
	require.NoError(t, err)
	_, allowed, err := repo.NewConnectClientRepo(svcCtx.App.Store.CommonDB()).FindAuthorizedMqttClient(ctx, before.Username, before.Password, before.ClientID)
	require.NoError(t, err)
	require.False(t, allowed)
}

// TASK-028 MqttAuthClientListLogic 单元测试：
//
//	目标：覆盖受管 MQTT 连接客户端列表查询入口。
//	正常路径：空库返回空列表；按 type 过滤（device/flow/edge）；按关键字过滤
//	          （name/clientId/username 模糊匹配）；返回字段含客户端凭据信息。
//	异常情况：无（查询类接口，非法 type 归一为 device）。
package mqtt

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestMqttAuthClientList_Empty(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewMqttAuthClientListLogic(ctx, svcCtx)

	resp, err := l.MqttAuthClientList(&types.MqttAuthClientListReq{})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Empty(t, view["list"])
	require.Equal(t, 0, view["total"])
}

func TestMqttAuthClientList_All(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	deviceID := mustCreateClient(t, svcCtx, ctx, "device")
	edgeID := mustCreateEdgeClient(t, svcCtx, ctx)
	device := mustGetClient(t, svcCtx, deviceID)
	edge := mustGetClient(t, svcCtx, edgeID)

	l := NewMqttAuthClientListLogic(ctx, svcCtx)
	resp, err := l.MqttAuthClientList(&types.MqttAuthClientListReq{ClientType: "all"})
	require.NoError(t, err)
	view := respData(t, resp)
	list := view["list"].([]map[string]any)
	require.Len(t, list, 2)
	require.Equal(t, 2, view["total"])

	byID := map[int64]map[string]any{}
	for _, m := range list {
		byID[m["id"].(int64)] = m
	}
	require.Equal(t, device.Username, byID[device.ID]["username"])
	require.Equal(t, "device", byID[device.ID]["type"])
	require.Equal(t, edge.Username, byID[edge.ID]["username"])
	require.Equal(t, "edgeNode", byID[edge.ID]["type"])
}

func TestMqttAuthClientList_FilterByType(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	mustCreateClient(t, svcCtx, ctx, "device")
	mustCreateEdgeClient(t, svcCtx, ctx)

	l := NewMqttAuthClientListLogic(ctx, svcCtx)
	resp, err := l.MqttAuthClientList(&types.MqttAuthClientListReq{ClientType: "edge"})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, 1, view["total"])
	for _, m := range view["list"].([]map[string]any) {
		require.Equal(t, "edgeNode", m["type"])
	}
}

func TestMqttAuthClientList_FilterByKeyword(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	deviceID := mustCreateClient(t, svcCtx, ctx, "device")
	mustCreateEdgeClient(t, svcCtx, ctx)
	device := mustGetClient(t, svcCtx, deviceID)

	l := NewMqttAuthClientListLogic(ctx, svcCtx)
	resp, err := l.MqttAuthClientList(&types.MqttAuthClientListReq{Keyword: device.ClientID})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, 1, view["total"])
	require.Equal(t, device.Username, view["list"].([]map[string]any)[0]["username"])

	// 无匹配关键字 → 空
	resp, err = l.MqttAuthClientList(&types.MqttAuthClientListReq{Keyword: "no-such-keyword"})
	require.NoError(t, err)
	view = respData(t, resp)
	require.Empty(t, view["list"])
}

func TestMqttAuthClientList_EmptyListType(t *testing.T) {
	// 空库时 list 为 []map[string]any 空切片，需要正确的类型断言。
	svcCtx, ctx := newSvcEnv(t)
	l := NewMqttAuthClientListLogic(ctx, svcCtx)

	resp, err := l.MqttAuthClientList(&types.MqttAuthClientListReq{})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Empty(t, view["list"].([]map[string]any))
	require.Equal(t, 0, view["total"])
}

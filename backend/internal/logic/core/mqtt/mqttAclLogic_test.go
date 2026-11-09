// TASK-028 MqttAclLogic 单元测试：
//
//	目标：覆盖 EMQX 发布/订阅 ACL 鉴权入口。
//	正常路径：受管 connect client 用户名+clientId 匹配放行；
//	          edge 节点对 cloudsync/fleet 专属 topic 放行、无关 topic 拒绝；
//	          $SYS/# 一律拒绝。
//	异常情况：open 账号路径依赖真实 APIKey 与缓存会话（部分已通过
//	          mqttAuthLogic_test.go 的会话写入覆盖）。
package mqtt

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestMqttAcl_ManagedClientAllowed(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	clientID := mustCreateClient(t, svcCtx, ctx, "device")
	client := mustGetClient(t, svcCtx, clientID)

	l := NewMqttAclLogic(ctx, svcCtx)
	view := respData(t, mustMqttAcl(t, l, types.MqttAclReq{
		Username: client.Username,
		Clientid: client.ClientID,
		Topic:    "enterprise/Metric/Robot_0",
		Action:   "publish",
	}))
	require.Equal(t, true, view["allow"])
	require.Equal(t, "allow", view["result"])
	require.Equal(t, "publish", view["action"])
}

func TestMqttAcl_UnknownClientDenied(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewMqttAclLogic(ctx, svcCtx)

	view := respData(t, mustMqttAcl(t, l, types.MqttAclReq{
		Username: "nobody",
		Clientid: "nobody-1",
		Topic:    "enterprise/Metric/Robot_0",
		Action:   "publish",
	}))
	require.Equal(t, false, view["allow"])
	require.Equal(t, "deny", view["result"])
}

func TestMqttAcl_EdgeNodeTopics(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	edgeID := mustCreateEdgeClient(t, svcCtx, ctx)
	edge := mustGetClient(t, svcCtx, edgeID)
	t.Logf("edge: id=%d clientID=%s username=%s type=%d", edge.ID, edge.ClientID, edge.Username, edge.Type)

	l := NewMqttAclLogic(ctx, svcCtx)
	cases := []struct {
		topic  string
		action string
		allow  bool
	}{
		{"cloudsync/" + edge.ClientID + "/meta", "publish", false},
		{"cloudsync/" + edge.ClientID + "/data", "publish", false},
		{"cloudsync/" + edge.ClientID + "/user-sync/request", "subscribe", false},
		{"cloudsync/" + edge.ClientID + "/user-sync/request", "publish", false},
		{"cloudsync/" + edge.ClientID + "/user-sync/result", "publish", false},
		{"cloudsync/" + edge.ClientID + "/user-snapshot/response/node-1", "publish", false},
		{"fleet/v1/" + edge.ClientID + "/hello", "publish", true},
		{"fleet/v1/" + edge.ClientID + "/command", "subscribe", true},
		{"fleet/v1/" + edge.ClientID + "/command", "publish", false},
		{"fleet/v1/" + edge.ClientID + "/unknown", "publish", false},
		{"enterprise/Metric/Robot_0", "publish", false},
	}
	for _, tc := range cases {
		view := respData(t, mustMqttAcl(t, l, types.MqttAclReq{
			Username: edge.Username,
			Clientid: edge.ClientID,
			Topic:    tc.topic,
			Action:   tc.action,
		}))
		require.Equal(t, tc.allow, view["allow"], "topic=%s action=%s", tc.topic, tc.action)
	}
}

func TestMqttAcl_SystemTopicDenied(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	clientID := mustCreateClient(t, svcCtx, ctx, "device")
	client := mustGetClient(t, svcCtx, clientID)

	l := NewMqttAclLogic(ctx, svcCtx)
	view := respData(t, mustMqttAcl(t, l, types.MqttAclReq{
		Username: client.Username,
		Clientid: client.ClientID,
		Topic:    "$SYS/brokers",
		Action:   "subscribe",
	}))
	require.Equal(t, false, view["allow"])
}

func TestMqttAcl_OpenSessionReadAllowed(t *testing.T) {
	// 先经 MqttAuth 写入 open APIKey 会话（含 uns.read），再验证 ACL 放行订阅。
	svcCtx, ctx := newSvcEnv(t)
	raw := mustCreateAPIKey(t, svcCtx, ctx, "uns.read")

	auth := NewMqttAuthLogic(ctx, svcCtx)
	view := respData(t, mustMqttAuth(t, auth, types.MqttAuthReq{
		Username: "1&open",
		Password: raw,
		Clientid: "1&dev-random-2",
	}))
	require.Equal(t, true, view["allow"])

	// 诊断：验证会话已写入 cache
	body, err := svcCtx.App.Cache.GetString(ctx, "cache:uns:emqx:open:1:1&dev-random-2")
	t.Logf("session body=%q err=%v", body, err)

	l := NewMqttAclLogic(ctx, svcCtx)
	view = respData(t, mustMqttAcl(t, l, types.MqttAclReq{
		Username: "1&open",
		Clientid: "1&dev-random-2",
		Topic:    "enterprise/Metric/Robot_0",
		Action:   "subscribe",
	}))
	require.Equal(t, true, view["allow"])
}

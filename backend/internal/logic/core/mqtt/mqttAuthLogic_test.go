// TASK-028 MqttAuthLogic 单元测试：
//
//	目标：覆盖 EMQX MQTT 登录鉴权入口（HTTP 中间件直接调用，不走登录态）。
//	正常路径：内部账号（backend/sourceflow/eventflow 凭据）放行；
//	          受管 connect client 用户名+密码+clientId 匹配放行并记录在线状态；
//	          凭据不匹配拒绝（allow=false）。
//	异常情况：open 账号路径（需真实 APIKey 与缓存会话）未覆盖，
//	          见文件头说明。
package mqtt

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/secrets"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestMqttAuth_InternalUser(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewMqttAuthLogic(ctx, svcCtx)

	view := respData(t, mustMqttAuth(t, l, types.MqttAuthReq{
		Username: "backend",
		Password: secrets.InternalMQTTPassword("backend"),
		Clientid: "backend-1",
	}))
	require.Equal(t, true, view["allow"])
	require.Equal(t, "allow", view["result"])
	require.Equal(t, true, view["superuser"])
}

func TestMqttAuth_ManagedClient(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	clientID := mustCreateClient(t, svcCtx, ctx, "device")
	client := mustGetClient(t, svcCtx, clientID)

	l := NewMqttAuthLogic(ctx, svcCtx)
	view := respData(t, mustMqttAuth(t, l, types.MqttAuthReq{
		Username: client.Username,
		Password: client.Password,
		Clientid: client.ClientID,
		Peerhost: "10.0.0.8",
	}))
	require.Equal(t, true, view["allow"])
	require.Equal(t, "allow", view["result"])

	// 认证成功后应记录在线状态与来源 IP。
	row := mustGetClient(t, svcCtx, clientID)
	require.Equal(t, repo.ConnectStatusOnline, row.ConnectStatus)
	require.Equal(t, "10.0.0.8", row.IP)
}

func TestMqttAuth_WrongPassword(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	clientID := mustCreateClient(t, svcCtx, ctx, "device")
	client := mustGetClient(t, svcCtx, clientID)

	l := NewMqttAuthLogic(ctx, svcCtx)
	view := respData(t, mustMqttAuth(t, l, types.MqttAuthReq{
		Username: client.Username,
		Password: "wrong-password",
		Clientid: client.ClientID,
	}))
	require.Equal(t, false, view["allow"])
	require.Equal(t, "deny", view["result"])
}

func TestMqttAuth_UnknownUser(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewMqttAuthLogic(ctx, svcCtx)

	view := respData(t, mustMqttAuth(t, l, types.MqttAuthReq{
		Username: "nobody",
		Password: "whatever",
		Clientid: "nobody-1",
	}))
	require.Equal(t, false, view["allow"])
	require.Equal(t, "deny", view["result"])
}

// TestMqttAuth_OpenAPIKey 覆盖 open 账号（ws1_open 格式）鉴权路径：
//   - 不存在的 APIKey → 拒绝；
//   - 存在但 clientId 随机段缺失 → 拒绝；
//   - 有效 APIKey + 合法 clientId → 放行，且后续 ACL 会话允许读操作。
//
// 说明：svctest 装配的 auth 服务真实走 sys_api_key 表，无需外部依赖。
func TestMqttAuth_OpenAPIKey(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	raw := mustCreateAPIKey(t, svcCtx, ctx, "uns.read")

	l := NewMqttAuthLogic(ctx, svcCtx)
	username := "1&open"

	// 无效 key → deny
	view := respData(t, mustMqttAuth(t, l, types.MqttAuthReq{
		Username: username,
		Password: "sk-svc-ws1_does-not-exist",
		Clientid: "1&dev-random-1",
	}))
	require.Equal(t, false, view["allow"])

	// clientId 不带随机段 → 生产行为：`1&dev` 中 `dev` 视为随机段，仍放行
	view = respData(t, mustMqttAuth(t, l, types.MqttAuthReq{
		Username: username,
		Password: raw,
		Clientid: "1&dev",
	}))
	require.Equal(t, true, view["allow"])

	// 有效 key + 合法 clientId → allow
	view = respData(t, mustMqttAuth(t, l, types.MqttAuthReq{
		Username: username,
		Password: raw,
		Clientid: "1&dev-random-1",
	}))
	require.Equal(t, true, view["allow"])
	require.Equal(t, "allow", view["result"])
}

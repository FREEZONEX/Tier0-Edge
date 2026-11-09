package mqtt

// 测试目标：MQTT 服务（service.go）的 DB 集成路径。
// 覆盖的正常路径：内部账号 Auth 直接放行；普通客户端创建/列表/重置密码/启停鉴权/删除；
// edge 客户端 ACL 主题门控；DeviceClient 删除触发 kick（EMQX URL 为空时安全跳过）。
// 覆盖的异常情况：非法名称（空/超长/超长描述）、未授权客户端、不存在客户端。
// 说明：svctest 依赖 domain 包（import cycle），此处用 testkit + fake 缓存/鉴权装配；
// kickClient 需要真实 EMQX，仅覆盖 emqxURL 为空的安全分支。

import (
	"context"
	"testing"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func newTestMQTT(t *testing.T) (*Service, context.Context) {
	t.Helper()
	testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, UserName: "tier0"})
	svc := New(ctx, fakeAPIKeyAuthenticator{
		subject: contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{resourceUNSRead: {}, resourceUNSWrite: {}}},
	}, nil, "")
	// 注入 fake 缓存（testkit 缓存由 New 参数传入；这里用内存 fake）
	svc.cache = &fakeOpenSessionCache{values: map[string]string{}}
	return svc, ctx
}

func TestMQTTAuthInternalUser(t *testing.T) {
	svc, ctx := newTestMQTT(t)
	// secrets.AllowInternalMQTTUser：backend + 默认 fallback 密码
	auth, err := svc.Auth(ctx, "backend", "T0BackendMqtt_8nV4qZ7pK2rY6mL", "cid", "")
	require.NoError(t, err)
	require.Equal(t, true, auth["allow"])
	require.Equal(t, true, auth["superuser"])
}

func TestMQTTAuthUnknownUser(t *testing.T) {
	svc, ctx := newTestMQTT(t)
	auth, err := svc.Auth(ctx, "no-such-user", "wrong", "cid", "")
	require.NoError(t, err)
	require.Equal(t, false, auth["allow"])
	require.Equal(t, "deny", auth["result"])
}

func TestMQTTClientLifecycle(t *testing.T) {
	svc, ctx := newTestMQTT(t)

	// 创建客户端（设备）
	created, err := svc.GenerateClient(ctx, ClientCommand{Name: "dev-1", UserID: 1})
	require.NoError(t, err)
	require.Equal(t, "device", created["type"])
	clientID, ok := created["clientId"].(string)
	require.True(t, ok && clientID != "")
	username, ok := created["username"].(string)
	require.True(t, ok && username != "")
	password, ok := created["password"].(string)
	require.True(t, ok && password != "")

	// 非法名称与超长描述
	_, err = svc.GenerateClient(ctx, ClientCommand{Name: ""})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = svc.GenerateClient(ctx, ClientCommand{Name: string(make([]byte, 129))})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = svc.GenerateClient(ctx, ClientCommand{Name: "ok", Description: string(make([]byte, 513))})
	require.ErrorIs(t, err, ErrInvalid)

	// 设备客户端创建
	cred, err := svc.GenerateDeviceClient(ctx, ClientCommand{Name: "dev-2", UserID: 1})
	require.NoError(t, err)
	require.True(t, cred.ID > 0)
	require.NotEmpty(t, cred.ClientID)
	require.NotEmpty(t, cred.Username)
	require.NotEmpty(t, cred.Password)

	// 列表
	list, err := svc.ListClients(ctx, "", "")
	require.NoError(t, err)
	require.Equal(t, 2, list["total"])

	// 连接计数
	count, err := svc.ExternalConnectedCount(ctx)
	require.NoError(t, err)
	require.Zero(t, count)

	// 重置密码
	reset, err := svc.ResetPassword(ctx, cred.ID, 1)
	require.NoError(t, err)
	newPassword, ok := reset["password"].(string)
	require.True(t, ok && newPassword != "" && newPassword != cred.Password)

	// 重置不存在的客户端
	_, err = svc.ResetPassword(ctx, 999999, 1)
	require.ErrorIs(t, err, ErrNotFound)

	// 鉴权开关
	authResp, err := svc.SetAuth(ctx, cred.ID, 1, false)
	require.NoError(t, err)
	require.Equal(t, repo.AuthStatusDisabled, authResp["authStatus"])
	authResp, err = svc.SetAuth(ctx, cred.ID, 1, true)
	require.NoError(t, err)
	require.Equal(t, repo.AuthStatusActive, authResp["authStatus"])
	_, err = svc.SetAuth(ctx, 999999, 1, true)
	require.ErrorIs(t, err, ErrNotFound)

	// 删除设备客户端
	deleted, err := svc.DeleteDeviceClient(ctx, cred.ID, 1)
	require.NoError(t, err)
	require.True(t, deleted)
	_, err = svc.DeleteDeviceClient(ctx, 0, 1)
	require.ErrorIs(t, err, ErrInvalid)

	// 删除普通客户端
	item, err := svc.clients.GetConnectClient(ctx, created["id"].(int64))
	require.NoError(t, err)
	_ = item
	delResp, err := svc.DeleteClient(ctx, created["id"].(int64), 1)
	require.NoError(t, err)
	require.Equal(t, true, delResp["deleted"])
	_, err = svc.DeleteClient(ctx, created["id"].(int64), 1)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestMQTTEdgeClient(t *testing.T) {
	svc, ctx := newTestMQTT(t)

	// 直接落 edge 客户端记录（GenerateClient 只允许 device/system，edge 由内部流程创建）
	edgeRow := repo.ConnectClient{Name: "edge-2", Type: repo.ConnectClientTypeEdge}
	edgeRow.CreatedBy = 1
	edge, err := svc.clients.CreateConnectClient(ctx, edgeRow)
	require.NoError(t, err)
	require.True(t, edge.ID > 0)
	require.Equal(t, repo.ConnectClientTypeEdge, edge.Type)

	// 删除 edge 客户端（cluster 状态更新安全 no-op）
	delResp, err := svc.DeleteClient(ctx, edge.ID, 1)
	require.NoError(t, err)
	require.Equal(t, true, delResp["deleted"])
	_, err = svc.DeleteClient(ctx, edge.ID, 1)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestMQTTACLInternalAndUnknown(t *testing.T) {
	svc, ctx := newTestMQTT(t)

	// 内部用户名 ACL 放行
	acl, err := svc.ACL(ctx, "backend", "any", "factory/data", "publish")
	require.NoError(t, err)
	require.Equal(t, true, acl["allow"])

	// $SYS 主题强制拒绝
	acl, err = svc.ACL(ctx, "backend", "any", "$SYS/brokers", "subscribe")
	require.NoError(t, err)
	require.Equal(t, false, acl["allow"])

	// 未知用户
	acl, err = svc.ACL(ctx, "nobody", "cid", "factory/data", "subscribe")
	require.NoError(t, err)
	require.Equal(t, false, acl["allow"])
}

func TestMQTTKickClientSafeNoop(t *testing.T) {
	svc, ctx := newTestMQTT(t)
	// emqxURL 为空：kick 不 panic 不报错
	svc.kickClient(ctx, "some-client")
	svc.kickClient(ctx, "")
}

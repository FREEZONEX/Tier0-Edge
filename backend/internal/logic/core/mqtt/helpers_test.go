// TASK-028 mqtt 模块测试共享助手：
//
//	目标：为 8 个 MQTT logic 文件提供统一的环境搭建与断言工具，避免重复代码。
//	正常路径：Envelope 解包、HTTP 错误断言、通过领域层创建 connect client。
//	异常情况：无（纯工具函数）。
package mqtt

import (
	"context"
	"strings"
	"testing"

	domainmqtt "backend/internal/domain/mqtt"
	respx "backend/internal/httpx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

// newSvcEnv 每次调用创建独立测试库 + miniredis 的完整 ServiceContext 与登录 ctx。
func newSvcEnv(t *testing.T) (*svc.ServiceContext, context.Context) {
	t.Helper()
	return svctest.NewTestServiceContext(t), svctest.CtxWithUser(1)
}

// newSvcEnvNoUser 创建完整 ServiceContext，但 ctx 不携带登录 Subject。
func newSvcEnvNoUser(t *testing.T) *svc.ServiceContext {
	t.Helper()
	return svctest.NewTestServiceContext(t)
}

// repeatRune 构造长度为 n 的重复字符字符串，用于超长字段断言。
func repeatRune(r rune, n int) string {
	return strings.Repeat(string(r), n)
}

// respData 解包成功响应的 Data 为 map[string]any。
func respData(t *testing.T, resp *types.Envelope) map[string]any {
	t.Helper()
	require.NotNil(t, resp)
	require.Equal(t, 200, resp.Code)
	m, ok := resp.Data.(map[string]any)
	require.True(t, ok, "envelope data should be map[string]any")
	return m
}

// requireHTTPError 断言 err 为 logicx.Error 包装出的 HTTP 错误且状态码匹配。
func requireHTTPError(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	var httpErr *respx.HTTPError
	require.ErrorAs(t, err, &httpErr, "logicx.Error should wrap respx.HTTPError")
	require.Equal(t, status, httpErr.Status)
}

// mustCreateClient 经领域层创建 connect client，返回其 ID。
func mustCreateClient(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, clientType string) int64 {
	t.Helper()
	view, err := svcCtx.App.MQTT.GenerateClient(ctx, domainmqtt.ClientCommand{
		Name:        "test-client",
		Type:        clientType,
		Description: "created by unit test",
		UserID:      1,
	})
	require.NoError(t, err)
	id, ok := view["id"].(int64)
	require.True(t, ok, "client view id should be int64")
	require.Positive(t, id)
	return id
}

// mustCreateEdgeClient 直接经 repo 创建 edge 类型 connect client（生产 CreateClient
// 入口会把 edge 归一为 device，edge client 只能由 repo/节点注册链路创建），返回其 ID。
func mustCreateEdgeClient(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context) int64 {
	t.Helper()
	item, err := repo.NewConnectClientRepo(ctx).CreateConnectClient(ctx, repo.ConnectClient{
		Name:        "edge-client",
		Type:        repo.ConnectClientTypeEdge,
		Description: "edge node created by unit test",
		SoftTime:    repo.SoftTime{NoDelTime: repo.NoDelTime{CreatedBy: 1}},
	})
	require.NoError(t, err)
	require.Equal(t, repo.ConnectClientTypeEdge, item.Type)
	return item.ID
}

// mustGetClient 读取一条未被软删除的 connect client 记录。
func mustGetClient(t *testing.T, svcCtx *svc.ServiceContext, id int64) repo.ConnectClient {
	t.Helper()
	var client repo.ConnectClient
	require.NoError(t, svcCtx.App.Store.CommonDB().
		Where("id = ? AND deleted_time = 0", id).First(&client).Error)
	return client
}

// mustMqttAuth 调用 MqttAuth 并断言无业务错误，返回响应 Data。
func mustMqttAuth(t *testing.T, l *MqttAuthLogic, req types.MqttAuthReq) *types.Envelope {
	t.Helper()
	resp, err := l.MqttAuth(&req)
	require.NoError(t, err)
	return resp
}

// mustMqttAcl 调用 MqttAcl 并断言无业务错误，返回响应 Data。
func mustMqttAcl(t *testing.T, l *MqttAclLogic, req types.MqttAclReq) *types.Envelope {
	t.Helper()
	resp, err := l.MqttAcl(&req)
	require.NoError(t, err)
	return resp
}

// mustCreateAPIKey 经领域层创建带指定资源（逗号分隔）的 service 类型 APIKey，
// 返回明文 key（workspace=1 编码，可被 ParseWorkspaceIDFromAPIKey 解析）。
func mustCreateAPIKey(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, resourceKeys string) string {
	t.Helper()
	view, err := svcCtx.App.APIKey.Create(ctx, "mqtt-test-key", 1, "read_only", "external", "service", splitCSV(resourceKeys))
	require.NoError(t, err)
	raw, ok := view["apiKey"].(string)
	require.True(t, ok, "api key view should contain raw key")
	require.NotEmpty(t, raw)
	return raw
}

func splitCSV(values string) []string {
	var out []string
	for _, v := range strings.Split(values, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

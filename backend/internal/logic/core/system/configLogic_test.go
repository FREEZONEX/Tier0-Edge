// TASK-028 ConfigLogic 单元测试：
//
//	目标：覆盖系统启动配置聚合（产品版本、UNS 树懒加载开关、集群运行态、语言、容器映射）。
//	正常路径：svcCtx 完整装配时 Config 返回完整配置项，UNS 服务可用时返回活跃节点数。
//	异常情况：UNS 不可用时 lazyTree 回退 true；非法语言环境变量归一化为空。
package system

import (
	"context"
	"os"
	"testing"

	"backend/internal/config"
	respx "backend/internal/httpx"
	"backend/internal/repo"
	"backend/internal/runtimeenv"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigLogic_Config_Full(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	svcCtx.Config.ProductVersion = "v1.2.3"
	svcCtx.Config.Cluster = config.ClusterConf{NodeKey: "edge-1", NodeName: "edge-node"}
	svcCtx.Config.DataQuality.Enabled = true
	ctx := context.Background()

	l := NewConfigLogic(ctx, svcCtx)
	resp, err := l.Config()
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	data, ok := resp.Data.(map[string]any)
	require.True(t, ok, "envelope data should be map[string]any")
	assert.Equal(t, runtimeenv.ProductName(), data["appTitle"])
	assert.Equal(t, true, data["authEnable"])
	assert.Equal(t, "/tier0-login", data["loginPath"])
	assert.Equal(t, "v1.2.3", data["productVersion"])
	assert.Equal(t, "v1.2.3", data["version"])
	assert.Equal(t, "en-US", data["lang"])
	assert.Equal(t, "auto", data["unsTreeLoadMode"])
	assert.Equal(t, int64(2000), data["unsTreeAutoThreshold"])
	assert.Equal(t, true, data["enableAutoCategorization"])
	assert.Equal(t, false, data["aiChatConfigured"])
	assert.Equal(t, true, data["dataQualityEnabled"])
	assert.Equal(t, "_quality", data["qualityName"])
	assert.Equal(t, "_timestamp", data["timestampName"])
	assert.Equal(t, "1883", data["mqttTcpPort"])
	assert.Equal(t, "8083", data["mqttWebsocketPort"])
	assert.Equal(t, "8084", data["mqttWebsocketTslPort"])
	assert.Equal(t, "", data["mqttWebsocketUrl"])

	cluster, ok := data["cluster"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, cluster["enabled"])
	assert.Equal(t, "edge-1", cluster["nodeKey"])
	assert.Equal(t, "edge-node", cluster["nodeName"])

	containerMap, ok := data["containerMap"].(map[string]any)
	require.True(t, ok)
	for _, key := range []string{"sourceflow", "eventflow", "oauth", "cluster", "marimo"} {
		entry, ok := containerMap[key].(map[string]any)
		assert.True(t, ok, "containerMap[%s] should exist", key)
		assert.Equal(t, true, entry["envMap"].(map[string]any)["service_is_show"])
	}
}

func TestConfigLogic_AIChatConfigured(t *testing.T) {
	t.Setenv("AI_GATEWAY_WORKSPACE_ID", "")
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := context.Background()
	_, err := repo.NewAIProviderConfigRepo(ctx).Create(ctx, repo.AIProviderConfig{
		WorkspaceID: 1, Name: "Chat only", Provider: "openai-compatible",
		BaseURL: "https://api.openai.com/v1", Model: "gpt-test", APIKeyCiphertext: "test-ciphertext",
		IsDefault: true, Enabled: true,
	})
	require.NoError(t, err)
	resp, err := NewConfigLogic(ctx, svcCtx).Config()
	require.NoError(t, err)
	assert.Equal(t, true, resp.Data.(map[string]any)["aiChatConfigured"])

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	assert.Nil(t, NewConfigLogic(cancelled, svcCtx).aiChatConfigured())

	// 工作空间 1 已配置也不能让其他空间误判为就绪。
	t.Setenv("AI_GATEWAY_WORKSPACE_ID", "2")
	assert.Equal(t, false, NewConfigLogic(ctx, svcCtx).aiChatConfigured())
	_, err = repo.NewAIProviderConfigRepo(ctx).Create(ctx, repo.AIProviderConfig{
		WorkspaceID: 2, Name: "Agent workspace", Provider: "openai-compatible",
		BaseURL: "https://api.openai.com/v1", Model: "gpt-test", APIKeyCiphertext: "test-ciphertext",
		IsDefault: true, Enabled: true,
	})
	require.NoError(t, err)
	for _, value := range []string{"2", " 2 "} {
		t.Setenv("AI_GATEWAY_WORKSPACE_ID", value)
		assert.Equal(t, true, NewConfigLogic(ctx, svcCtx).aiChatConfigured())
	}
	for _, value := range []string{"0", "-1", "invalid", "9223372036854775808"} {
		t.Setenv("AI_GATEWAY_WORKSPACE_ID", value)
		assert.Nil(t, NewConfigLogic(ctx, svcCtx).aiChatConfigured(), "workspace ID %q", value)
	}
}

func TestConfigLogic_Config_NilSvcCtxFallbacks(t *testing.T) {
	l := &ConfigLogic{ctx: context.Background()}
	resp, err := l.Config()
	require.NoError(t, err)
	data := resp.Data.(map[string]any)

	cluster := data["cluster"].(map[string]any)
	assert.Equal(t, false, cluster["enabled"])
	assert.Equal(t, "edge", cluster["role"])
	assert.Equal(t, "dev", data["productVersion"])
	assert.Equal(t, true, data["lazyTree"])
	assert.Contains(t, data, "aiChatConfigured")
	assert.Nil(t, data["aiChatConfigured"])
	assert.Equal(t, false, data["dataQualityEnabled"])
}

func TestConfigLogic_ConfiguredLanguageAndPort(t *testing.T) {
	t.Setenv("LANGUAGE", "zh_CN")
	t.Setenv("OS_MQTT_TCP_PORT", "2883")
	t.Setenv("OS_MQTT_WEBSOCKET_PORT", "9083")
	t.Setenv("MQTT_WEBSOCKET_PUBLIC_URL", "wss://edge.example.com/mqtt")

	svcCtx := svctest.NewTestServiceContext(t)
	l := NewConfigLogic(context.Background(), svcCtx)
	resp, err := l.Config()
	require.NoError(t, err)
	data := resp.Data.(map[string]any)

	assert.Equal(t, "zh-CN", data["lang"])
	assert.Equal(t, "2883", data["mqttTcpPort"])
	assert.Equal(t, "9083", data["mqttWebsocketPort"])
	assert.Equal(t, "wss://edge.example.com/mqtt", data["mqttWebsocketUrl"])
	// 未设置的 TLS 端口回退默认值
	assert.Equal(t, "8084", data["mqttWebsocketTslPort"])
}

func TestConfigLogic_ProductVersionEnvFallback(t *testing.T) {
	t.Setenv("PRODUCT_VERSION", "env-9.9")
	svcCtx := svctest.NewTestServiceContext(t)
	svcCtx.Config.ProductVersion = ""

	l := NewConfigLogic(context.Background(), svcCtx)
	resp, err := l.Config()
	require.NoError(t, err)
	assert.Equal(t, "env-9.9", resp.Data.(map[string]any)["productVersion"])
}

func TestConfigLogic_UnsTreeLazyResolution(t *testing.T) {
	// auto 模式：节点数超过阈值 => 懒加载
	assert.True(t, resolveUnsTreeLazy(unsTreeLoadModeAuto, unsTreeAutoThreshold+1))
	// auto 模式：节点数未超阈值 => 全量加载
	assert.False(t, resolveUnsTreeLazy(unsTreeLoadModeAuto, unsTreeAutoThreshold))
	// lazy / full 显式模式
	assert.True(t, resolveUnsTreeLazy(unsTreeLoadModeLazy, 0))
	assert.False(t, resolveUnsTreeLazy(unsTreeLoadModeFull, 999999))
	// 未知模式回退懒加载
	assert.True(t, resolveUnsTreeLazy("unknown-mode", 0))
	// 大小写与空白容忍
	assert.False(t, resolveUnsTreeLazy(" Full ", 0))
}

func TestConfigLogic_UnsUnavailableFallsBackLazy(t *testing.T) {
	l := &ConfigLogic{ctx: context.Background()}
	lazy, count := l.unsTreeLoadingConfig()
	assert.True(t, lazy)
	assert.Zero(t, count)
}

func TestConfigLogic_ClusterRuntimeConfig(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	svcCtx.Config.Cluster = config.ClusterConf{NodeKey: "host-key", NodeName: "host-name"}
	l := &ConfigLogic{ctx: context.Background(), svcCtx: svcCtx}
	cfg := l.clusterRuntimeConfig()
	assert.Equal(t, true, cfg["enabled"])
	assert.Equal(t, "host-key", cfg["nodeKey"])
	assert.Equal(t, "host-name", cfg["nodeName"])

	// license 未装配时 isHost=false、role=edge
	assert.Equal(t, false, cfg["isHost"])
	assert.Equal(t, "edge", cfg["role"])
}

func TestConfigLogic_NormalizeLanguage(t *testing.T) {
	assert.Equal(t, "zh-CN", normalizeLanguage("zh"))
	assert.Equal(t, "zh-CN", normalizeLanguage("zh_CN"))
	assert.Equal(t, "zh-CN", normalizeLanguage(" ZH-cn "))
	assert.Equal(t, "en-US", normalizeLanguage("en"))
	assert.Equal(t, "en-US", normalizeLanguage("en_us"))
	assert.Equal(t, "en-US", normalizeLanguage("EN-US"))
	assert.Equal(t, "", normalizeLanguage("fr"))
	assert.Equal(t, "", normalizeLanguage(""))
}

func TestConfigLogic_ConfiguredLanguageEnvOrder(t *testing.T) {
	t.Setenv("LANGUAGE", "")
	t.Setenv("OS_LANG", "zh-CN")
	assert.Equal(t, "zh-CN", configuredLanguage())

	t.Setenv("LANGUAGE", "en-US")
	assert.Equal(t, "en-US", configuredLanguage())
	_ = os.Unsetenv("OS_LANG")
}

func TestConfigLogic_EnvelopeContract(t *testing.T) {
	resp := respx.Envelope(map[string]any{"k": "v"})
	require.NotNil(t, resp)
	require.IsType(t, &types.Envelope{}, resp)
}

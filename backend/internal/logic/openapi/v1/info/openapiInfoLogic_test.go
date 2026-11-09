// openapiInfoLogic_test.go — OpenapiInfoLogic 测试。
//
// 测试目标：OpenAPI 服务信息（产品名/版本/能力列表/MQTT broker 地址）入口。
// 正常路径：svcCtx 携带 ProductVersion 与 ExternalMqttBrokerHost 时返回对应值，
//   未配置时版本回退 "dev"、broker 回退 "emqx"。
// 异常情况：纯函数 openapiProductVersion / openapiMqttBrokerHost 的 nil svcCtx
//   与环境变量回退路径。
package info

import (
	"os"
	"testing"

	"backend/internal/config"
	"backend/internal/svc"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiInfo_Configured(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	svcCtx.Config.ProductVersion = "v1.2.3"
	svcCtx.Config.Cluster.ExternalMqttBrokerHost = "mqtt.example.com:1883"

	l := NewOpenapiInfoLogic(svctest.CtxWithUser(1), svcCtx)
	resp, err := l.OpenapiInfo()
	require.NoError(t, err)
	data, ok := resp.Data.(types.InfoResp)
	require.True(t, ok)
	require.Equal(t, "edge-enterprise", data.Name)
	require.Equal(t, "v1.2.3", data.Version)
	require.Equal(t, "mqtt.example.com:1883", data.MqttBroker)
	require.Contains(t, data.Capabilities, "uns.read")
	require.Contains(t, data.Capabilities, "mqtt")
}

func TestOpenapiInfo_Defaults(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	svcCtx.Config.ProductVersion = ""
	svcCtx.Config.Cluster.ExternalMqttBrokerHost = ""

	t.Setenv("PRODUCT_VERSION", "")
	l := NewOpenapiInfoLogic(svctest.CtxWithUser(1), svcCtx)
	resp, err := l.OpenapiInfo()
	require.NoError(t, err)
	data := resp.Data.(types.InfoResp)
	require.Equal(t, "dev", data.Version)
	require.Equal(t, "emqx", data.MqttBroker)
}

func TestOpenapiProductVersion_EnvFallback(t *testing.T) {
	t.Setenv("PRODUCT_VERSION", "v9.9.9")
	require.Equal(t, "v9.9.9", openapiProductVersion(&svc.ServiceContext{Config: config.Config{}}))
	require.Equal(t, "v9.9.9", openapiProductVersion(nil))

	t.Setenv("PRODUCT_VERSION", "  ")
	require.Equal(t, "dev", openapiProductVersion(nil))
}

func TestOpenapiMqttBrokerHost_NilSvcCtx(t *testing.T) {
	require.Equal(t, "emqx", openapiMqttBrokerHost(nil))

	svcCtx := &svc.ServiceContext{Config: config.Config{}}
	svcCtx.Config.Cluster.ExternalMqttBrokerHost = "  "
	require.Equal(t, "emqx", openapiMqttBrokerHost(svcCtx))

	svcCtx.Config.Cluster.ExternalMqttBrokerHost = " broker "
	require.Equal(t, "broker", openapiMqttBrokerHost(svcCtx))
}

// 防止误用未设置环境变量的情况污染其他用例。
func TestOpenapiInfo_EnvProductVersionOverride(t *testing.T) {
	old, had := os.LookupEnv("PRODUCT_VERSION")
	defer func() {
		if had {
			_ = os.Setenv("PRODUCT_VERSION", old)
		} else {
			_ = os.Unsetenv("PRODUCT_VERSION")
		}
	}()
	_ = os.Unsetenv("PRODUCT_VERSION")

	svcCtx := svctest.NewTestServiceContext(t)
	svcCtx.Config.ProductVersion = "  "
	resp, err := NewOpenapiInfoLogic(svctest.CtxWithUser(1), svcCtx).OpenapiInfo()
	require.NoError(t, err)
	require.Equal(t, "dev", resp.Data.(types.InfoResp).Version)
}

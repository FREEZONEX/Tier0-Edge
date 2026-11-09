package sourcestream

// 配置校验不连接外部系统，拒绝无效协议、采集参数和明文密码。
import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestValidateSourceWithoutRuntime(t *testing.T) {
	s := &Service{}
	require.NoError(t, s.ValidateSource(map[string]any{"name": "MQTT", "protocol": "mqtt", "endpoint": "mqtt://broker:1883", "runtimeLocation": "local", "mqtt": map[string]any{"clientId": "test"}}))
	for _, input := range []any{nil, []int{1}, map[string]any{"name": "bad"}, map[string]any{"name": "MQTT", "protocol": "mqtt", "endpoint": "mqtt://broker:1883", "password": "secret"}} {
		require.Error(t, s.ValidateSource(input))
	}
}

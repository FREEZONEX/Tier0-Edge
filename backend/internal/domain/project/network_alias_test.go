// 测试目标：AppInternalURL 根据已部署别名和容器端口生成内网地址。
// 正常路径：JSON 解码端口、整数端口和合法边界；异常路径：缺失或错误类型的字段、越界端口。
package project

import "testing"

func TestAppInternalURL(t *testing.T) {
	cases := []struct {
		name     string
		manifest map[string]any
		want     string
	}{
		{"json port", map[string]any{"network_alias": "order-api", "container_port": float64(3000)}, "http://order-api:3000"},
		{"integer port", map[string]any{"network_alias": "order-api", "container_port": 8080}, "http://order-api:8080"},
		{"minimum port", map[string]any{"network_alias": "a", "container_port": 1}, "http://a:1"},
		{"maximum port", map[string]any{"network_alias": "a", "container_port": 65535}, "http://a:65535"},
		{"nil manifest", nil, ""},
		{"empty manifest", map[string]any{}, ""},
		{"missing alias", map[string]any{"container_port": 3000}, ""},
		{"empty alias", map[string]any{"network_alias": "", "container_port": 3000}, ""},
		{"invalid alias type", map[string]any{"network_alias": 123, "container_port": 3000}, ""},
		{"missing port", map[string]any{"network_alias": "order-api"}, ""},
		{"host port only", map[string]any{"network_alias": "order-api", "host_port": 30100}, ""},
		{"string port", map[string]any{"network_alias": "order-api", "container_port": "3000"}, ""},
		{"null port", map[string]any{"network_alias": "order-api", "container_port": nil}, ""},
		{"zero port", map[string]any{"network_alias": "order-api", "container_port": 0}, ""},
		{"negative port", map[string]any{"network_alias": "order-api", "container_port": -1}, ""},
		{"port overflow", map[string]any{"network_alias": "order-api", "container_port": float64(65536)}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AppInternalURL(tc.manifest); got != tc.want {
				t.Fatalf("AppInternalURL(%v) = %q, want %q", tc.manifest, got, tc.want)
			}
		})
	}
}

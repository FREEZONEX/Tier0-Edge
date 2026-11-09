package project

import "fmt"

// AppInternalURL 仅展示成功部署记录中的容器端口，避免误用宿主机/网关端口。
func AppInternalURL(manifest map[string]any) string {
	alias, _ := manifest["network_alias"].(string)
	var port int
	switch value := manifest["container_port"].(type) {
	case float64:
		port = int(value)
	case int:
		port = value
	}
	if alias == "" || port <= 0 || port > 65535 {
		return ""
	}
	return fmt.Sprintf("http://%s:%d", alias, port)
}

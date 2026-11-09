package appgateway

import (
	"context"
	"fmt"
	"net"

	"backend/internal/config"
	"backend/internal/repo"
)

// IsPortAvailable 尝试本地监听端口，成功则关闭并返回 true，用于判断端口是否被占用
func IsPortAvailable(port int) bool {
	if port <= 0 {
		return false
	}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// AllocatePort 在配置范围内分配一个既不在 DB 路由表中、本地又可监听的端口
func AllocatePort(ctx context.Context, conf config.Config, exclude map[int]bool) (int, error) {
	return allocatePortInRange(ctx, conf.AppGateway.PortRangeStart, conf.AppGateway.PortRangeEnd, exclude)
}

// AllocateAppHostPort 在应用宿主机端口范围内分配一个既不在 DB 路由表中、本地又可监听的端口
func AllocateAppHostPort(ctx context.Context, conf config.Config, exclude map[int]bool) (int, error) {
	return allocatePortInRange(ctx, conf.AppGateway.AppHostPortRangeStart, conf.AppGateway.AppHostPortRangeEnd, exclude)
}

func allocatePortInRange(ctx context.Context, start, end int, exclude map[int]bool) (int, error) {
	routeRepo := repo.NewAppGatewayRouteRepo(ctx)
	routes, err := routeRepo.ListAll(ctx)
	if err != nil {
		return 0, err
	}

	allocated := make(map[int]bool, len(routes)+len(exclude))
	for _, r := range routes {
		if r.Port > 0 {
			allocated[r.Port] = true
		}
	}
	for p := range exclude {
		allocated[p] = true
	}

	for port := start; port <= end; port++ {
		if allocated[port] {
			continue
		}
		if !IsPortAvailable(port) {
			continue
		}
		return port, nil
	}

	return 0, fmt.Errorf("no available port in range %d-%d", start, end)
}

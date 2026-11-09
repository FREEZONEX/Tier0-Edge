package appgateway

import (
	"backend/internal/repo"
	"context"
	"net"
	"strings"
	"sync"

	"github.com/zeromicro/go-zero/core/logx"
)

// Router 应用路由表
type Router struct {
	portRoutes   map[int]*repo.AppGatewayRoute
	domainRoutes map[string]*repo.AppGatewayRoute
	// subdomainRoutes 以单级子域名(shortID)为 key 的路由索引，支持通配父域
	// （如 *.apps.example.com）下的泛解析请求匹配。
	subdomainRoutes map[string]*repo.AppGatewayRoute
	// subdomainSuffix 通配父域（不含 * 前缀），如 "apps.example.com"；为空时禁用通配匹配。
	subdomainSuffix string
	rw              sync.RWMutex
}

// NewRouter 创建路由表（仅精确域名匹配，无通配父域）
func NewRouter() *Router {
	return newRouter("")
}

// NewRouterWithSuffix 创建支持通配父域匹配的路由表。
// subdomainSuffix 为通配父域（不含 * 前缀），如 "apps.example.com"；
// 为空时与 NewRouter 等价，仅支持精确域名匹配。
func NewRouterWithSuffix(subdomainSuffix string) *Router {
	return newRouter(subdomainSuffix)
}

func newRouter(subdomainSuffix string) *Router {
	return &Router{
		portRoutes:      make(map[int]*repo.AppGatewayRoute),
		domainRoutes:    make(map[string]*repo.AppGatewayRoute),
		subdomainRoutes: make(map[string]*repo.AppGatewayRoute),
		subdomainSuffix: subdomainSuffix,
	}
}

// subdomainKey 从完整域名提取单级子域名 key；形如 <sub>.<suffix> 且 <sub> 不含 "." 才返回。
// 不匹配返回 ""。
func (r *Router) subdomainKey(domain string) string {
	if r.subdomainSuffix == "" || domain == "" {
		return ""
	}
	suffix := "." + r.subdomainSuffix
	if !strings.HasSuffix(domain, suffix) {
		return ""
	}
	sub := strings.TrimSuffix(domain, suffix)
	if sub == "" || strings.Contains(sub, ".") {
		return ""
	}
	return sub
}

// LoadFromDB 从数据库加载路由表
func (r *Router) LoadFromDB(ctx context.Context) error {
	routeRepo := repo.NewAppGatewayRouteRepo(ctx)
	routes, err := routeRepo.ListAll(ctx)
	if err != nil {
		return err
	}

	r.rw.Lock()
	defer r.rw.Unlock()

	r.portRoutes = make(map[int]*repo.AppGatewayRoute)
	r.domainRoutes = make(map[string]*repo.AppGatewayRoute)
	r.subdomainRoutes = make(map[string]*repo.AppGatewayRoute)

	for _, route := range routes {
		if route.AccessMode == "port" && route.Port > 0 {
			r.portRoutes[route.Port] = route
		} else if route.AccessMode == "subdomain" && route.Domain != "" {
			r.domainRoutes[route.Domain] = route
			if key := r.subdomainKey(route.Domain); key != "" {
				r.subdomainRoutes[key] = route
			}
		}
	}

	logx.Infof("[AppGateway] 加载路由表: %d 条记录", len(routes))
	return nil
}

// FindByPort 根据端口查找路由
func (r *Router) FindByPort(port int) *repo.AppGatewayRoute {
	r.rw.RLock()
	defer r.rw.RUnlock()
	return r.portRoutes[port]
}

// PortRoutes 返回所有端口路由的副本
func (r *Router) PortRoutes() map[int]*repo.AppGatewayRoute {
	r.rw.RLock()
	defer r.rw.RUnlock()
	out := make(map[int]*repo.AppGatewayRoute, len(r.portRoutes))
	for k, v := range r.portRoutes {
		out[k] = v
	}
	return out
}

// AllRoutes returns every active route once, preserving its persisted mode.
func (r *Router) AllRoutes() []*repo.AppGatewayRoute {
	r.rw.RLock()
	defer r.rw.RUnlock()
	out := make([]*repo.AppGatewayRoute, 0, len(r.portRoutes)+len(r.domainRoutes))
	for _, route := range r.portRoutes {
		out = append(out, route)
	}
	for _, route := range r.domainRoutes {
		out = append(out, route)
	}
	return out
}

// FindByDomain 根据域名查找路由（兼容 host:port 输入）。
// 匹配顺序：精确域名 → 通配父域单级子域名（如 <shortID>.apps.example.com）。
func (r *Router) FindByDomain(domain string) *repo.AppGatewayRoute {
	host := domain
	if strings.Contains(host, ":") {
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
	}

	r.rw.RLock()
	defer r.rw.RUnlock()

	if route := r.domainRoutes[host]; route != nil {
		return route
	}
	if key := r.subdomainKey(host); key != "" {
		if route := r.subdomainRoutes[key]; route != nil {
			return route
		}
	}
	return nil
}

// AddRoute 添加路由
func (r *Router) AddRoute(route *repo.AppGatewayRoute) {
	r.rw.Lock()
	defer r.rw.Unlock()

	if route.AccessMode == "port" && route.Port > 0 {
		r.portRoutes[route.Port] = route
	} else if route.AccessMode == "subdomain" && route.Domain != "" {
		r.domainRoutes[route.Domain] = route
		if key := r.subdomainKey(route.Domain); key != "" {
			r.subdomainRoutes[key] = route
		}
	}
}

// RemoveRoute 移除路由（清理端口、精确域名、通配子域名三个索引）
func (r *Router) RemoveRoute(appID string) {
	r.rw.Lock()
	defer r.rw.Unlock()

	for port, route := range r.portRoutes {
		if route.AppID == appID {
			delete(r.portRoutes, port)
			break
		}
	}

	for domain, route := range r.domainRoutes {
		if route.AppID == appID {
			delete(r.domainRoutes, domain)
		}
	}

	for sub, route := range r.subdomainRoutes {
		if route.AppID == appID {
			delete(r.subdomainRoutes, sub)
		}
	}
}

package appgateway

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"backend/internal/config"
	"backend/internal/repo"
	"backend/share/utils"

	"github.com/zeromicro/go-zero/core/logx"
)

// Gateway 应用网关
type Gateway struct {
	conf       config.AppGatewayConfig
	appConf    config.Config
	router     *Router
	auth       *Auth
	proxy      *Proxy
	tlsConfig  *tls.Config
	listeners  map[int]net.Listener
	listenerMu sync.Mutex
}

const maxRouteListenRetries = 3

var (
	instance *Gateway
	once     sync.Once
)

// applyDefaults 补充 YAML 中缺失的默认值，支持环境变量覆盖
func applyDefaults(c config.AppGatewayConfig) config.AppGatewayConfig {
	if c.PortRangeStart == 0 {
		logx.Infof("[AppGateway] [WARN] PortRangeStart 未配置，使用默认值 30000")
		c.PortRangeStart = 30000
	}
	if c.PortRangeEnd == 0 {
		logx.Infof("[AppGateway] [WARN] PortRangeEnd 未配置，使用默认值 30099")
		c.PortRangeEnd = 30099
	}
	if c.DefaultAccessMode == "" {
		c.DefaultAccessMode = "port"
	}
	c.Scheme = config.NormalizeAppGatewayScheme(c.Scheme)
	if c.CertPath == "" {
		c.CertPath = "/app/certs/cert.pem"
	}
	if c.KeyPath == "" {
		c.KeyPath = "/app/certs/key.pem"
	}
	if v := os.Getenv("APP_GATEWAY_ENABLED"); v != "" {
		c.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("APP_GATEWAY_DOMAIN_SUFFIX"); v != "" {
		c.SubdomainSuffix = v
	}
	if v := os.Getenv("APP_GATEWAY_ACCESS_MODE"); v != "" {
		c.DefaultAccessMode = strings.ToLower(strings.TrimSpace(v))
	}
	if v := os.Getenv("APP_GATEWAY_LOGIN_URL"); v != "" {
		c.LoginURL = v
	}
	if v := os.Getenv("APP_GATEWAY_SKIP_AUTH"); v == "true" || v == "1" {
		c.SkipAuth = true
	}
	if v := os.Getenv("APP_GATEWAY_EXTERNAL_HOST"); v != "" {
		c.ExternalHost = v
	}
	if v := os.Getenv("APP_GATEWAY_EXTERNAL_PORT"); v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			c.ExternalPort = port
		}
	}
	if v := os.Getenv("APP_GATEWAY_SCHEME"); v != "" {
		c.Scheme = config.NormalizeAppGatewayScheme(v)
	}
	if v := os.Getenv("APP_GATEWAY_LISTENER_SCHEME"); v != "" {
		c.ListenerScheme = v
	}
	c.ListenerScheme = config.NormalizeAppGatewayListenerScheme(c.ListenerScheme, c.Scheme)
	return c
}

// NewGateway 创建应用网关单例
func NewGateway(c config.Config) *Gateway {
	once.Do(func() {
		conf := applyDefaults(c.AppGateway)
		c.AppGateway = conf
		instance = &Gateway{
			conf:      conf,
			appConf:   c,
			listeners: make(map[int]net.Listener),
		}
		instance.router = NewRouterWithSuffix(conf.SubdomainSuffix)
		instance.auth = NewAuth(c)
		instance.proxy = NewProxy(instance.router, conf)
	})
	return instance
}

// ReloadRoutes reloads app gateway routes in the current backend process.
func ReloadRoutes(ctx context.Context) error {
	if instance == nil || instance.router == nil {
		return fmt.Errorf("app gateway is not initialized")
	}
	if err := instance.router.LoadFromDB(ctx); err != nil {
		return err
	}
	if err := instance.ensurePortTLSConfig(); err != nil {
		return err
	}
	if err := instance.reconcileGeneratedAppURLs(ctx); err != nil {
		return err
	}
	instance.restartPortListeners(ctx)
	return nil
}

// InvalidateRoleCache 清空网关项目角色缓存。
// 项目成员/成员角色/应用角色绑定变更后调用，避免被撤销的角色在缓存 TTL 内继续生效。
// 网关未初始化时安全 no-op。
func InvalidateRoleCache() {
	if instance == nil || instance.auth == nil || instance.auth.resolver == nil {
		return
	}
	instance.auth.resolver.invalidateAll()
}

// Start 启动应用网关
func (g *Gateway) Start() error {
	if !g.conf.Enabled {
		logx.Info("[AppGateway] 已禁用")
		return nil
	}

	// 从 DB 加载路由表
	if err := g.router.LoadFromDB(context.Background()); err != nil {
		logx.Errorf("[AppGateway] 加载路由表失败: %v", err)
	}
	if err := g.ensurePortTLSConfig(); err != nil {
		return err
	}
	if err := g.reconcileGeneratedAppURLs(context.Background()); err != nil {
		return fmt.Errorf("reconcile generated app URLs: %w", err)
	}

	// 按实际路由懒监听端口，端口被占用时自动迁移
	if err := g.startPortListeners(); err != nil {
		return err
	}

	logx.Info("[AppGateway] 网关启动完成")
	return nil
}

func (g *Gateway) ensurePortTLSConfig() error {
	if g.conf.ListenerScheme != "https" || len(g.router.PortRoutes()) == 0 || g.tlsConfig != nil {
		return nil
	}
	tlsConf, err := LoadTLSConfig(g.conf.CertPath, g.conf.KeyPath)
	if err != nil {
		return fmt.Errorf("load AppGateway port TLS certificate: %w", err)
	}
	g.tlsConfig = tlsConf
	return nil
}

func (g *Gateway) reconcileGeneratedAppURLs(ctx context.Context) error {
	projectRepo := repo.NewProjectRepo(ctx)
	for _, route := range g.router.AllRoutes() {
		var entryURL string
		switch route.AccessMode {
		case "port":
			if route.Port <= 0 || strings.TrimSpace(g.conf.ExternalHost) == "" {
				continue
			}
			entryURL = fmt.Sprintf("%s://%s:%d/", g.conf.Scheme, g.conf.ExternalHost, route.Port)
		case "subdomain":
			if strings.TrimSpace(route.Domain) == "" {
				continue
			}
			authority := config.AppGatewayPublicAuthority(g.conf, route.Domain)
			entryURL = fmt.Sprintf("%s://%s/", g.conf.Scheme, authority)
		default:
			continue
		}
		if err := projectRepo.UpdateGeneratedAppEntryURLByRuntimeID(ctx, route.AppID, entryURL); err != nil {
			return err
		}
	}
	return nil
}

// startPortListeners 按路由懒监听端口；若端口被占用则自动重新分配并更新 DB
func (g *Gateway) startPortListeners() error {
	for _, route := range g.router.PortRoutes() {
		if err := g.ensureListenerForRoute(context.Background(), route); err != nil {
			logx.Errorf("[AppGateway] 为应用 %s 启动监听失败: %v", route.AppID, err)
			// 单个应用监听失败不应阻塞整个网关启动
		}
	}
	return nil
}

// ensureListenerForRoute 确保某条端口路由已被监听；端口被占用时自动迁移并重试
func (g *Gateway) ensureListenerForRoute(ctx context.Context, route *repo.AppGatewayRoute) error {
	g.listenerMu.Lock()
	defer g.listenerMu.Unlock()

	if _, ok := g.listeners[route.Port]; ok {
		return nil
	}

	var ln net.Listener
	var err error
	for i := 0; i < maxRouteListenRetries; i++ {
		ln, err = net.Listen("tcp", fmt.Sprintf(":%d", route.Port))
		if err == nil {
			break
		}

		logx.Infof("[AppGateway] 端口 %d 被占用，尝试为应用 %s 重新分配端口 (第 %d/%d 次)", route.Port, route.AppID, i+1, maxRouteListenRetries)
		newPort, allocErr := AllocatePort(ctx, g.appConf, nil)
		if allocErr != nil {
			return fmt.Errorf("端口 %d 被占用且无法重新分配: %w", route.Port, allocErr)
		}

		if updateErr := g.updateRoutePort(ctx, route.AppID, newPort); updateErr != nil {
			return fmt.Errorf("更新路由端口失败: %w", updateErr)
		}

		route.Port = newPort
	}

	if err != nil {
		return fmt.Errorf("应用 %s 经过 %d 次重试后仍无法监听: %w", route.AppID, maxRouteListenRetries, err)
	}

	g.serveListener(ln, route.Port)
	g.listeners[route.Port] = ln
	return nil
}

// updateRoutePort 将应用路由的端口更新到 DB 与内存路由表
func (g *Gateway) updateRoutePort(ctx context.Context, appID string, newPort int) error {
	routeRepo := repo.NewAppGatewayRouteRepo(ctx)
	existing, findErr := routeRepo.FindByAppID(ctx, appID)
	if findErr != nil {
		return fmt.Errorf("查询路由失败: %w", findErr)
	}

	oldPort := existing.Port
	existing.Port = newPort
	if updateErr := routeRepo.Update(ctx, existing); updateErr != nil {
		return fmt.Errorf("写入新端口失败: %w", updateErr)
	}

	g.router.RemoveRoute(appID)
	g.router.AddRoute(existing)

	logx.Infof("[AppGateway] 应用 %s 端口从 %d 迁移到 %d", appID, oldPort, newPort)
	return nil
}

// serveListener 在指定端口启动 HTTP/HTTPS 服务
func (g *Gateway) serveListener(ln net.Listener, port int) {
	handler := http.HandlerFunc(g.handleRequest)
	if g.conf.ListenerScheme == "https" {
		tlsListener := tls.NewListener(ln, g.tlsConfig)
		utils.Go(context.Background(), func() {
			logx.Infof("[AppGateway] 监听端口 %d (HTTPS)", port)
			if err := http.Serve(tlsListener, handler); err != nil && err != http.ErrServerClosed {
				logx.Errorf("[AppGateway] 端口 %d 服务停止: %v", port, err)
			}
		})
		return
	}

	utils.Go(context.Background(), func() {
		logx.Infof("[AppGateway] 监听端口 %d (HTTP)", port)
		if err := http.Serve(ln, handler); err != nil && err != http.ErrServerClosed {
			logx.Errorf("[AppGateway] 端口 %d 服务停止: %v", port, err)
		}
	})
}

// stopListener 关闭某个端口的监听
func (g *Gateway) stopListener(port int) {
	g.listenerMu.Lock()
	defer g.listenerMu.Unlock()

	if ln, ok := g.listeners[port]; ok {
		_ = ln.Close()
		delete(g.listeners, port)
	}
}

// restartPortListeners 热加载后对齐 listener 与路由表
func (g *Gateway) restartPortListeners(ctx context.Context) {
	desired := g.router.PortRoutes()

	g.listenerMu.Lock()
	currentPorts := make(map[int]bool, len(g.listeners))
	for p := range g.listeners {
		currentPorts[p] = true
	}
	g.listenerMu.Unlock()

	for port := range currentPorts {
		if _, ok := desired[port]; !ok {
			g.stopListener(port)
		}
	}

	for _, route := range desired {
		if err := g.ensureListenerForRoute(ctx, route); err != nil {
			logx.Errorf("[AppGateway] 热加载后监听应用 %s 失败: %v", route.AppID, err)
		}
	}
}

// MatchesSubdomainHost 判断 host（剥端口后）是否匹配 AppGateway 配置的通配父域，
// 用于主入口按 Host 分流。子域名部分必须为单级（不含 "."）。
// 网关未初始化或未配置通配父域时返回 false（安全 no-op）。
func MatchesSubdomainHost(host string) bool {
	if instance == nil {
		return false
	}
	return instance.matchesSubdomainHost(host)
}

// MatchesDomainHost reports whether the main listener should dispatch this
// Host to AppGateway. Persisted exact domains remain routable even when the
// configured suffix or the default access mode changes.
func MatchesDomainHost(host string) bool {
	if instance == nil {
		return false
	}
	return instance.matchesDomainHost(host)
}

func (g *Gateway) matchesDomainHost(host string) bool {
	if g.matchesSubdomainHost(host) {
		return true
	}
	return g.router != nil && g.router.FindByDomain(host) != nil
}

// matchesSubdomainHost 判断 host 是否匹配通配父域后缀
func (g *Gateway) matchesSubdomainHost(host string) bool {
	h := host
	if strings.Contains(h, ":") {
		if hh, _, err := net.SplitHostPort(h); err == nil {
			h = hh
		}
	}
	suffix := g.conf.SubdomainSuffix
	if suffix == "" {
		return false
	}
	if !strings.HasSuffix(h, "."+suffix) {
		return false
	}
	sub := strings.TrimSuffix(h, "."+suffix)
	if sub == "" || strings.Contains(sub, ".") {
		return false
	}
	return true
}

// HandleHTTP 主入口按 Host 分流进入时，将请求交给 AppGateway 处理。
// 网关未初始化时回 404（安全 no-op）。
func HandleHTTP(w http.ResponseWriter, r *http.Request) {
	if instance == nil {
		http.Error(w, "App gateway not initialized", http.StatusNotFound)
		return
	}
	instance.handleRequest(w, r)
}

// handleRequest 统一请求处理
func (g *Gateway) handleRequest(w http.ResponseWriter, r *http.Request) {
	// 1. 获取路由（先解析路由，鉴权阶段需要项目/应用上下文做角色门控）
	route := g.resolveRoute(r)
	if route == nil {
		http.Error(w, "App not found", http.StatusNotFound)
		return
	}

	// 2. 鉴权 + 项目角色注入
	if !g.auth.Authenticate(w, r, route) {
		return
	}

	// 3. 代理请求
	g.proxy.ServeHTTP(w, r, route)
}

// resolveRoute 根据请求解析路由
func (g *Gateway) resolveRoute(r *http.Request) *repo.AppGatewayRoute {
	// 尝试从端口查路由
	_, portStr, err := net.SplitHostPort(r.Host)
	if err == nil && portStr != "" {
		port, _ := strconv.Atoi(portStr)
		if port >= g.conf.PortRangeStart && port <= g.conf.PortRangeEnd {
			return g.router.FindByPort(port)
		}
	}

	// 尝试从域名查路由
	host := r.Host
	if strings.Contains(host, ":") {
		host, _, _ = net.SplitHostPort(host)
	}
	return g.router.FindByDomain(host)
}

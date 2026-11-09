package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backend/internal/appgateway"
	"backend/internal/config"
	"backend/internal/contextx"
	"backend/internal/domain/auth"
	"backend/internal/domain/fleettunnel"
	"backend/internal/httpx"
	"backend/internal/permission"
	"backend/internal/repo"
	"backend/internal/secrets"
)

type HTTPGateway struct {
	nativeApps          NativeAppResolver
	config              config.GatewayConf
	auth                *auth.Service
	permission          *permission.Evaluator
	routes              *repo.GatewayRepo
	fleetTunnel         *fleettunnel.Service
	s3Proxy             *S3Proxy
	aiProxy             *AIProxy
	legacyFlowsDisabled bool
}

func (g *HTTPGateway) BindFleetTunnel(tunnel *fleettunnel.Service) {
	if g != nil {
		g.fleetTunnel = tunnel
	}
}

type runtimeConfig struct {
	LaunchpadPort       string `json:"launchpadPort"`
	LaunchpadStandalone bool   `json:"launchpadStandalone"`
	SourceStreamEnabled bool   `json:"sourceStreamEnabled"`
}

const internalProxyTokenHeader = "X-Tier0-Internal-Token"

// DisableLegacyFlows blocks both generated API routes and old proxy routes.
// Hiding a menu alone would still allow Node-RED requests from saved URLs.
func (g *HTTPGateway) DisableLegacyFlows() { g.legacyFlowsDisabled = true }

func legacyFlowPath(path string) bool {
	path = strings.ToLower(strings.TrimRight(path, "/"))
	for _, prefix := range []string{"/marimo", "/api/core/notebooks", "/api/core/notebook-folders", "/api/core/notebook-import", "/flow", "/sourceflow", "/eventflow", "/nodered", "/api/core/flows", "/openapi/v1/flow", "/openapi/v1/flows"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return path == "/openapi/v1/uns/unsbindflow"
}

func NewHTTPGateway(ctx context.Context, c config.GatewayConf, authSvc *auth.Service, perm *permission.Evaluator, s3Proxy *S3Proxy, aiProxy *AIProxy) *HTTPGateway {
	return &HTTPGateway{
		config:     c,
		auth:       authSvc,
		permission: perm,
		routes:     repo.NewGatewayRepo(ctx),
		s3Proxy:    s3Proxy,
		aiProxy:    aiProxy,
	}
}

func (g *HTTPGateway) Handle(w http.ResponseWriter, r *http.Request) bool {
	return g.handle(w, r, false)
}

func (g *HTTPGateway) HandleLaunchpad(w http.ResponseWriter, r *http.Request) bool {
	return g.handle(w, r, true)
}

func (g *HTTPGateway) handle(w http.ResponseWriter, r *http.Request, launchpadStandalone bool) bool {
	if g.handleNativeApp(w, r) {
		return true
	}
	if r.URL.Path == "/mcp-authorize" {
		// 用户授权页不允许嵌入 iframe，避免点击劫持；不影响其它平台页面。
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
	}
	if legacyFlowPath(r.URL.Path) || retiredComponentPath(r.URL.Path) {
		http.Error(w, "This component has been retired", http.StatusGone)
		return true
	}
	if r.URL.Path == "/runtime-env.js" {
		g.serveRuntimeEnv(w, r, launchpadStandalone)
		return true
	}
	if r.Method == http.MethodGet && (r.URL.Path == "/api/health" || r.URL.Path == "/api/health/") {
		g.serveHealth(w)
		return true
	}
	if !launchpadStandalone && g.fleetTunnel != nil && g.fleetTunnel.TryHandleConnect(w, r) {
		return true
	}

	if g.aiProxy != nil && g.aiProxy.TryProxy(w, r) {
		return true
	}
	// /mqtt 浏览器 WebSocket 入口：先于 SPA fallback 等通用路由拦截，代理到 EMQX。
	if g.tryMQTTWebSocket(w, r) {
		return true
	}
	if isInternalPath(r.URL.Path) {
		return false
	}
	if strings.Contains(r.URL.Path, "..") {
		http.Error(w, "path traversal denied", http.StatusBadRequest)
		return true
	}
	// S3 object-storage prefixes are proxied before the configured gateway
	// routes: presigned URLs rewritten to the platform entrance must reach the
	// object store with the signing host/path intact for SigV4 verification.
	if g.s3Proxy != nil && g.s3Proxy.TryProxy(w, r) {
		return true
	}
	if g.tryGatewayRoute(w, r) {
		return true
	}
	return false
}

func (g *HTTPGateway) serveRuntimeEnv(w http.ResponseWriter, r *http.Request, launchpadStandalone bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	payload, err := json.Marshal(runtimeConfig{
		LaunchpadPort:       strings.TrimSpace(g.config.LaunchpadPort),
		LaunchpadStandalone: launchpadStandalone,
		SourceStreamEnabled: g.legacyFlowsDisabled,
	})
	if err != nil {
		http.Error(w, "runtime config unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte("window.__TIER0_RUNTIME_CONFIG__ = "))
	_, _ = w.Write(payload)
	_, _ = w.Write([]byte(";\n"))
}

func (g *HTTPGateway) tryGatewayRoute(w http.ResponseWriter, r *http.Request) bool {
	routes, err := g.routes.ListGatewayRoutes(r.Context())
	if err != nil {
		http.Error(w, "gateway snapshot unavailable", http.StatusServiceUnavailable)
		return true
	}
	for _, route := range routes {
		if !route.Enabled {
			continue
		}
		if !routeMatches(route, r.Method, r.URL.Path) {
			continue
		}
		switch route.TargetType {
		case "reverseProxy":
			if !g.authorizeRoute(w, r, route) {
				return true
			}
			g.proxy(w, r, route)
			return true
		case "static":
			if !g.authorizeRoute(w, r, route) {
				return true
			}
			g.serveStatic(w, r)
			return true
		default:
			http.Error(w, "unsupported gateway target type", http.StatusBadGateway)
			return true
		}
	}
	return false
}

// nodeRedAdminAPIPrefixes 是 Node-RED Admin API 的固定首段路径（Node-RED 官方定义，4.x 含 plugins/locales/resources）。
// /flow/{source|event}/** 网关路由只放开 http in 节点接口，这些 Admin 路径一律拦截。
// 注意：不含 node（Node-RED 无独立 /node 路由，避免误伤用户 http in 如 /node/health）；
// debug 是 Admin 路由（debug 节点 emit/clear），保留。
var nodeRedAdminAPIPrefixes = []string{
	"flows", "flow", "nodes", "settings", "comms", "auth", "credentials",
	"context", "debug", "palette", "editor", "info", "theme",
	"diagnostics", "icons", "library", "history", "preview", "projects",
	"runtime", "version", "plugins", "locales", "resources",
	// built-in node 提供的 admin 触发端点（触发流执行，非 http in 接口）
	"inject",
}

// isFlowAPIProxyRoute 判断路由是否为 App 访问 Node-RED 原生接口的 /flow/{source|event}/** 路由。
func isFlowAPIProxyRoute(route repo.GatewayRoute) bool {
	return strings.HasPrefix(route.PathPattern, "/flow/source/") ||
		strings.HasPrefix(route.PathPattern, "/flow/event/")
}

// isNodeRedAdminPath 判断 /flow/{source|event}/<path> 的 <path> 首段是否为 Node-RED Admin API。
// 大小写不敏感（Node-RED 的 Express router 默认大小写不敏感，/Flows 也会命中 /flows）。
// 空 suffix（/flow/source、/flow/source/ 或 /flow/event/ 等）视为 Admin（会代理到 Node-RED 根路径 editor 页面）→ true。
// 例：/flow/source/flows -> "flows" -> true；/flow/source/Flows -> "flows" -> true；
//
//	/flow/source/api/custom -> "api" -> false。
func isNodeRedAdminPath(path string) bool {
	rest := path
	for _, prefix := range []string{"/flow/source", "/flow/event"} {
		if strings.HasPrefix(rest, prefix) {
			rest = strings.TrimPrefix(rest, prefix)
			// 裸前缀或空 suffix（/flow/source、/flow/source/、/flow/source// 等）→ editor 根页面，拒绝
			if rest == "" || rest == "/" || strings.Trim(rest, "/") == "" {
				return true
			}
			break
		}
	}
	// 非 /flow/{source|event} 前缀不算 Admin 路径
	if rest == path {
		return false
	}
	first := strings.ToLower(strings.Trim(rest, "/"))
	if i := strings.IndexByte(first, '/'); i >= 0 {
		first = first[:i]
	}
	for _, p := range nodeRedAdminAPIPrefixes {
		if first == p {
			return true
		}
	}
	return false
}

func (g *HTTPGateway) authorizeRoute(w http.ResponseWriter, r *http.Request, route repo.GatewayRoute) bool {
	switch route.AuthPolicy {
	case "public":
		// /flow/source/** 公开 http in 接口（无需 API key）；
		// Node-RED Admin API 仍拦截（防止公开改画布/部署）。
		if isFlowAPIProxyRoute(route) && isNodeRedAdminPath(r.URL.Path) {
			http.Error(w, "forbidden: node-red admin api not allowed", http.StatusForbidden)
			return false
		}
		return true
	case "login":
		subject, err := g.auth.AuthenticateRequest(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return false
		}
		allowed, required, err := g.permission.Allow(r.Context(), subject, "gateway", r.Method, r.URL.Path)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return false
		}
		if allowed {
			return true
		}
		if required != "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return false
		}
		if route.ResourceKey != "" {
			if !contextx.HasResource(subject, route.ResourceKey) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return false
			}
			return true
		}
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	case "apikey":
		// API key 鉴权：用于 App 直接访问 Node-RED 自身接口（/flow/{source|event}/**）。
		// 复用 OpenAPI 的 API key 解析（兼容 SDK 双头：Authorization: Bearer + X-API-Key）。
		subject, err := g.auth.AuthenticateAPIKey(r.Context(), auth.APIKeyFromRequest(r))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return false
		}
		if subject.APIKeyUsageType == repo.APIKeyUsageAppData {
			http.Error(w, "App data keys require OpenAPI v2", http.StatusForbidden)
			return false
		}
		// 收窄策略：/flow/{source|event}/** 只放开 http in 节点接口，
		// 拦截 Node-RED Admin API 固定路径（/flows /flow /nodes /settings /comms 等）。
		if isFlowAPIProxyRoute(route) && isNodeRedAdminPath(r.URL.Path) {
			http.Error(w, "forbidden: node-red admin api not allowed", http.StatusForbidden)
			return false
		}
		allowed, required, err := g.permission.Allow(r.Context(), subject, "gateway", r.Method, r.URL.Path)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return false
		}
		if allowed {
			return true
		}
		if required != "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return false
		}
		if route.ResourceKey != "" {
			if !contextx.HasResource(subject, route.ResourceKey) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return false
			}
			return true
		}
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	default:
		http.Error(w, "unsupported auth policy", http.StatusForbidden)
		return false
	}
}

func (g *HTTPGateway) proxy(w http.ResponseWriter, r *http.Request, route repo.GatewayRoute) {
	target, err := url.Parse(route.TargetUrl)
	if err != nil || target.Scheme == "" || target.Host == "" {
		http.Error(w, "invalid upstream", http.StatusBadGateway)
		return
	}
	if isDeniedTarget(target) {
		http.Error(w, "upstream denied by ssrf policy", http.StatusForbidden)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.URL.Path = rewriteProxyPath(r.URL.Path, route)
		req.URL.RawPath = ""
		req.Host = target.Host
		req.Header.Set("X-Forwarded-Host", httpx.ForwardedHost(r, g.config.TrustProxyHeaders))
		req.Header.Set("X-Forwarded-Proto", httpx.ForwardedProto(r, g.config.TrustProxyHeaders))
		injectInternalProxyAuth(req, route)
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		rewriteProxyLocation(resp, route, target)
		relaxEmbeddedProxyHeaders(resp, route)
		return nil
	}
	proxy.ServeHTTP(w, r)
}

func rewriteProxyLocation(resp *http.Response, route repo.GatewayRoute, target *url.URL) {
	location := strings.TrimSpace(resp.Header.Get("Location"))
	if location == "" {
		return
	}
	u, err := url.Parse(location)
	if err != nil {
		return
	}
	if u.IsAbs() {
		if !strings.EqualFold(u.Host, target.Host) {
			return
		}
		u.Scheme = ""
		u.Host = ""
	}
	prefix := strings.TrimSuffix(strings.TrimSuffix(route.PathPattern, "/**"), "/")
	if prefix == "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, prefix+"/") || u.Path == prefix {
		resp.Header.Set("Location", u.String())
		return
	}
	u.Path = prefix + u.Path
	resp.Header.Set("Location", u.String())
}

func relaxEmbeddedProxyHeaders(resp *http.Response, route repo.GatewayRoute) {
	if strings.HasPrefix(route.PathPattern, "/marimo/") ||
		strings.HasPrefix(route.PathPattern, "/emqx/") {
		resp.Header.Del("Content-Security-Policy")
		resp.Header.Del("X-Frame-Options")
	}
}

func injectInternalProxyAuth(req *http.Request, route repo.GatewayRoute) {
	switch {
	case strings.HasPrefix(route.PathPattern, "/nodered/"),
		strings.HasPrefix(route.PathPattern, "/eventflow/"),
		strings.HasPrefix(route.PathPattern, "/flow/source/"),
		strings.HasPrefix(route.PathPattern, "/flow/event/"):
		if token := secrets.InternalToken("NODERED_INTERNAL_TOKEN"); token != "" {
			req.Header.Set(internalProxyTokenHeader, token)
		}
	case strings.HasPrefix(route.PathPattern, "/marimo/"):
		if token := secrets.InternalToken("MARIMO_INTERNAL_TOKEN"); token != "" {
			req.Header.Set(internalProxyTokenHeader, token)
			req.SetBasicAuth("tier0", token)
			req.Header.Set("Marimo-Server-Token", token)
		}
	}
}

// 独立打包的子应用（同源 iframe 承载），目录位于 WebDir/<name>，需要各自的 SPA index 回退
var subAppPrefixes = []string{"anchor"}

// SPA 的 index.html 不能被浏览器启发式缓存（否则发版后用户拿到旧 HTML → 旧 JS）；
// Vite 产物 assets/ 下的文件名带内容 hash，可长期 immutable 缓存。
func serveSPAFile(w http.ResponseWriter, r *http.Request, filePath string) {
	if strings.HasSuffix(filePath, "index.html") {
		w.Header().Set("Cache-Control", "no-cache")
	} else if strings.Contains(filepath.ToSlash(filePath), "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeFile(w, r, filePath)
}

func (g *HTTPGateway) serveSubApp(w http.ResponseWriter, r *http.Request) bool {
	path := strings.TrimPrefix(r.URL.Path, "/")
	name := ""
	for _, prefix := range subAppPrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			name = prefix
			break
		}
	}
	if name == "" {
		return false
	}
	// 开发代理（显式设置 ANCHOR_DEV_PROXY_URL 才生效）：/anchor/* 同源转发到本机 Vite dev server，
	// HMR 的 WebSocket upgrade 由 ReverseProxy 原生透传
	if name == "anchor" && strings.TrimSpace(g.config.AnchorDevProxyUrl) != "" {
		g.proxyDevUpstream(w, r, g.config.AnchorDevProxyUrl)
		return true
	}
	webDir := g.config.WebDir
	if webDir == "" {
		webDir = "./web"
	}
	indexPath := filepath.Join(webDir, name, "index.html")
	if _, err := os.Stat(indexPath); err != nil {
		// 子应用未随镜像打包时退回默认行为（本地前端 dev 代理或主应用 index）
		return false
	}
	filePath := filepath.Join(webDir, filepath.FromSlash(path))
	if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
		serveSPAFile(w, r, filePath)
		return true
	}
	serveSPAFile(w, r, indexPath)
	return true
}

func (g *HTTPGateway) serveStatic(w http.ResponseWriter, r *http.Request) {
	if g.serveSubApp(w, r) {
		return
	}
	if g.config.LocalFrontendDev && strings.TrimSpace(g.config.FrontendDevUrl) != "" {
		g.proxyFrontendDev(w, r)
		return
	}
	webDir := g.config.WebDir
	if webDir == "" {
		webDir = "./web"
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	filePath := filepath.Join(webDir, filepath.FromSlash(path))
	if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
		serveSPAFile(w, r, filePath)
		return
	}
	indexPath := filepath.Join(webDir, "index.html")
	if _, err := os.Stat(indexPath); err == nil {
		serveSPAFile(w, r, indexPath)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<!doctype html><html><body><div id=\"root\">frontend build not found</div></body></html>"))
}

func (g *HTTPGateway) serveHealth(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code": 200,
		"msg":  "success",
		"data": map[string]any{
			"status": "ok",
			"name":   "backend",
		},
	})
}

func (g *HTTPGateway) proxyFrontendDev(w http.ResponseWriter, r *http.Request) {
	g.proxyDevUpstream(w, r, g.config.FrontendDevUrl)
}

// 开发态反向代理到本机前端 dev server（主应用/子应用共用）；
// WebSocket upgrade（Vite HMR）由 httputil.ReverseProxy 原生透传。
func (g *HTTPGateway) proxyDevUpstream(w http.ResponseWriter, r *http.Request, upstream string) {
	target, err := url.Parse(strings.TrimSpace(upstream))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		http.Error(w, "invalid frontend dev upstream", http.StatusBadGateway)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = r.Host
		req.Header.Set("X-Forwarded-Host", httpx.ForwardedHost(r, g.config.TrustProxyHeaders))
		req.Header.Set("X-Forwarded-Proto", httpx.ForwardedProto(r, g.config.TrustProxyHeaders))
		// Vite dev server rejects oversized Cookie headers with 431; static/HMR assets do not need them.
		req.Header.Del("Cookie")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
}

func isInternalPath(path string) bool {
	// 原生 MCP/OAuth 交给 Backend 的协议鉴权，不能被 GET SPA fallback 当作 HTML。
	if path == "/mcp" || strings.HasPrefix(path, "/mcp/oauth/") ||
		path == "/.well-known/oauth-protected-resource" || path == "/.well-known/oauth-protected-resource/mcp" ||
		path == "/.well-known/oauth-authorization-server" || path == "/.well-known/openid-configuration" {
		return true
	}
	return strings.HasPrefix(path, "/api/core/") ||
		strings.HasPrefix(path, "/api/fleet/") ||
		strings.HasPrefix(path, "/api/node/v1/fleet/") ||
		strings.HasPrefix(path, "/openapi/v1/") ||
		strings.HasPrefix(path, "/openapi/v2/") ||
		path == "/healthz" ||
		path == "/readyz" ||
		path == "/metrics"
}

func routeMatches(route repo.GatewayRoute, method, path string) bool {
	if len(route.Methods) > 0 {
		ok := false
		for _, m := range route.Methods {
			if strings.EqualFold(m, method) || m == "*" {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if route.PathPattern == path {
		return true
	}
	if strings.HasSuffix(route.PathPattern, "/**") {
		prefix := strings.TrimSuffix(route.PathPattern, "/**")
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
	return false
}

func rewriteProxyPath(path string, route repo.GatewayRoute) string {
	if route.RewritePath != "" {
		return route.RewritePath
	}
	targetPath := strings.TrimSpace(route.TargetPath)
	if targetPath == "" {
		targetPath = "/"
	}
	if !strings.HasPrefix(targetPath, "/") {
		targetPath = "/" + targetPath
	}
	if targetPath != "/" {
		targetPath = strings.TrimRight(targetPath, "/")
	}
	if !route.StripPrefix {
		if targetPath == "" {
			return path
		}
		return joinProxyPath(targetPath, path)
	}
	prefix := strings.TrimSuffix(route.PathPattern, "/**")
	rest := strings.TrimPrefix(path, prefix)
	if rest == "" {
		rest = "/"
	}
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	return joinProxyPath(targetPath, rest)
}

func joinProxyPath(base, rest string) string {
	if base == "" || base == "/" {
		return rest
	}
	if rest == "" || rest == "/" {
		return base + "/"
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(rest, "/")
}

func splitPathSegments(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func isDeniedTarget(target *url.URL) bool {
	host := strings.Split(target.Host, ":")[0]
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate()
}

// matchesLaunchpadDomain 判断请求 Host（剥端口后）是否等于 Launchpad 独立站点域名。
// launchpadDomain 为空（未配置）时返回 false。
func matchesLaunchpadDomain(host, launchpadDomain string) bool {
	lp := strings.TrimSpace(launchpadDomain)
	if lp == "" {
		return false
	}
	h := host
	if strings.Contains(h, ":") {
		if hh, _, err := net.SplitHostPort(h); err == nil {
			h = hh
		}
	}
	return strings.EqualFold(h, lp)
}

// HandleDispatch 主入口统一分流（HTTP 与 HTTPS listener 共用）：
//  1. Launchpad 域名（config.Gateway.LaunchpadDomain）→ HandleLaunchpad（独立站点模式）
//  2. 持久化 App 域名或通配域名（*.<AppGateway.SubdomainSuffix>）→ AppGateway
//  3. 其他 → 主站 HTTPGateway.Handle；未处理则交给 next（go-zero 路由）
func (g *HTTPGateway) HandleDispatch(w http.ResponseWriter, r *http.Request, next http.Handler) {
	if matchesLaunchpadDomain(r.Host, g.config.LaunchpadDomain) {
		if g.HandleLaunchpad(w, r) {
			return
		}
		// HandleLaunchpad 未处理（非 Launchpad 路径）→ 交给 go-zero
		next.ServeHTTP(w, r)
		return
	}

	// 持久化精确域名必须独立于当前默认 access mode/suffix 保持可访问；
	// 当前通配 suffix 下的未知域名也交给 AppGateway 返回明确的 App 404。
	if appgateway.MatchesDomainHost(r.Host) {
		appgateway.HandleHTTP(w, r)
		return
	}

	if g.Handle(w, r) {
		return
	}
	next.ServeHTTP(w, r)
}

func WithHTTPGateway(gateway *HTTPGateway) func(*http.Server) {
	return func(svr *http.Server) {
		if svr == nil || svr.Handler == nil || gateway == nil {
			return
		}
		next := svr.Handler
		svr.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gateway.HandleDispatch(w, r, next)
		})
	}
}

func WithLaunchpadHTTPGateway(gateway *HTTPGateway) func(*http.Server) {
	return func(svr *http.Server) {
		if svr == nil || svr.Handler == nil || gateway == nil {
			return
		}
		next := svr.Handler
		svr.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if gateway.HandleLaunchpad(w, r) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func WaitReady(ctx context.Context, app interface{ Ready(context.Context) error }, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := app.Ready(ctx); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timeout waiting gateway dependencies")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

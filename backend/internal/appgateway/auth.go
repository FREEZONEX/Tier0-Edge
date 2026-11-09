package appgateway

import (
	"backend/internal/config"
	"backend/internal/httpx"
	"backend/internal/repo"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// Auth 应用网关鉴权模块
type Auth struct {
	conf     config.Config
	client   *http.Client
	resolver *roleResolver
}

// NewAuth 创建鉴权模块
func NewAuth(c config.Config) *Auth {
	return &Auth{
		conf: c,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		resolver: newRoleResolver(),
	}
}

// Authenticate 鉴权，成功返回 true 并注入 user header 与项目角色头
func (a *Auth) Authenticate(w http.ResponseWriter, r *http.Request, route *repo.AppGatewayRoute) bool {
	if a.conf.AppGateway.SkipAuth {
		return true
	}

	// 防伪造：剥离客户端带入的网关注入头（对齐 cloud B4）
	StripInjectedHeaders(r)

	path := r.URL.Path
	isLoginOrAPI := path == "/login" || strings.HasPrefix(path, "/api/")

	token, fromURL := a.extractToken(r)
	if token == "" {
		if !isLoginOrAPI {
			a.redirectToLogin(w, r)
			return false
		}
		return true
	}

	// 验证 token
	userInfo, err := a.validateToken(token)
	if err != nil {
		logx.Errorf("[AppGateway] Token 验证失败: %v", err)
		a.redirectToLogin(w, r)
		return false
	}

	// 项目角色门控 + 角色头注入（对齐 cloud runtime-roles 语义）
	if !a.gateByProjectRoles(w, r, route, userInfo) {
		return false
	}

	// 如果 token 来自 URL 参数，设置 context 以便 response 阶段设置 cookie
	if fromURL {
		ctx := context.WithValue(r.Context(), "token_from_url", token)
		*r = *r.WithContext(ctx)
	}

	// 注入 user header
	userJSON, err := json.Marshal(userInfo)
	if err != nil {
		logx.Errorf("[AppGateway] 序列化 user header 失败: %v", err)
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return false
	}
	r.Header.Set("user", string(userJSON))

	return true
}

// gateByProjectRoles 按项目绑定角色做访问门控并注入角色头。
// 有效角色 = 成员角色 ∩ 应用绑定角色；多角色全量写入 X-Tier0-Business-Roles（CSV），
// 默认激活角色（role_key 字母序第一个）写入 X-Tier0-Active-Role 与 user JSON 的 role 字段。
func (a *Auth) gateByProjectRoles(w http.ResponseWriter, r *http.Request, route *repo.AppGatewayRoute, userInfo map[string]string) bool {
	if route == nil || a.resolver == nil {
		logx.Errorf("[AppGateway] 项目角色解析上下文缺失: routeNil=%t resolverNil=%t", route == nil, a.resolver == nil)
		http.Error(w, `{"error":"service unavailable: roles context missing"}`, http.StatusServiceUnavailable)
		return false
	}

	userID, err := strconv.ParseInt(userInfo["userID"], 10, 64)
	if err != nil || userID <= 0 {
		logx.Errorf("[AppGateway] user context 缺失有效 userID: %q", userInfo["userID"])
		http.Error(w, `{"error":"unauthorized: user context missing"}`, http.StatusUnauthorized)
		return false
	}

	roles, err := a.resolver.resolve(r.Context(), route, userID)
	if err != nil {
		switch {
		case errors.Is(err, errNotProjectMember):
			// 非项目成员：不披露应用存在性，appId 置 0 → 前端 App not found
			logx.Infof("[AppGateway] 非项目成员，重定向异常页: appID=%s userID=%d", route.AppID, userID)
			if !a.redirectToLaunchpadApp(w, r, route, "not-found", false) {
				http.Error(w, `{"error":"forbidden: not a project member or app role holder"}`, http.StatusForbidden)
			}
		case errors.Is(err, errNoValidAppRole):
			// 成员但缺 App 适用角色：携带真实 appId → 前端 App access required
			logx.Infof("[AppGateway] 成员缺适用角色，重定向异常页: appID=%s userID=%d", route.AppID, userID)
			if !a.redirectToLaunchpadApp(w, r, route, "access-required", true) {
				http.Error(w, `{"error":"forbidden: not a project member or app role holder"}`, http.StatusForbidden)
			}
		case errors.Is(err, errAppNotFound):
			logx.Infof("[AppGateway] 路由对应的应用记录不存在，重定向异常页: appID=%s", route.AppID)
			if !a.redirectToLaunchpadApp(w, r, route, "not-found", false) {
				http.Error(w, `{"error":"app not found"}`, http.StatusNotFound)
			}
		default:
			logx.Errorf("[AppGateway] 项目角色解析失败: appID=%s userID=%d err=%v", route.AppID, userID, err)
			http.Error(w, `{"error":"service unavailable: roles query failed"}`, http.StatusServiceUnavailable)
		}
		return false
	}

	// 开放 App（应用未绑定角色）：放行，注入运行时头让下游 APP 识别为合法空角色集合
	SetRuntimeRoleHeaders(r, roles)
	userInfo["role"] = r.Header.Get(headerXTier0ActiveRole)
	return true
}

func SetRuntimeRoleHeaders(r *http.Request, roles []string) {
	r.Header.Set(headerXTier0Runtime, runtimeValueDeployed)
	r.Header.Set(headerXTier0BusinessRoles, strings.Join(roles, ","))
	r.Header.Del(headerXTier0ActiveRole)
	if len(roles) > 0 {
		r.Header.Set(headerXTier0ActiveRole, roles[0])
	}
}

// extractToken 从请求中提取 token
func (a *Auth) extractToken(r *http.Request) (string, bool) {
	// 1. 从 URL 参数获取 _token。Launchpad 每次打开 app 都会注入当前 token，
	// 应优先于可能过期的跨端口 cookie。
	if token := r.URL.Query().Get("_token"); token != "" {
		return token, true
	}

	// 2. 从 Cookie 获取 edge_session（前端主 session）。无 _token 的直接访问
	// 应优先使用当前平台登录态，避免旧 app_auth_token 抢占新登录用户。
	if cookie, err := r.Cookie("edge_session"); err == nil && cookie.Value != "" {
		return cookie.Value, false
	}

	// 3. 从 app_auth_token 获取应用端口会话。应用首跳会清理 URL 上的 _token，
	// 后续无平台 session 的静态资源请求继续使用这份 app 网关 token。
	if cookie, err := r.Cookie("app_auth_token"); err == nil && cookie.Value != "" {
		return cookie.Value, false
	}

	return "", false
}

// validateToken 调用本进程后端 API 验证 token，返回 App 期望的用户字段
func (a *Auth) validateToken(token string) (map[string]string, error) {
	apiPort := a.conf.Port
	if apiPort == 0 {
		apiPort = 8080
	}
	authURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d/api/core/oauth/userinfo", apiPort))

	req, err := http.NewRequest("GET", authURL.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth failed: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析 userinfo 响应并转换为 App 期望的格式
	var userinfoResp struct {
		Sub               json.Number `json:"sub"`
		PreferredUsername string      `json:"preferred_username"`
		UserName          string      `json:"user_name"`
		Email             string      `json:"email"`
	}

	if err := json.Unmarshal(body, &userinfoResp); err != nil {
		logx.Errorf("[AppGateway] 解析 userinfo 响应失败: %v, body: %s", err, string(body))
		return nil, fmt.Errorf("failed to parse userinfo response")
	}

	userName := userinfoResp.PreferredUsername
	if userName == "" {
		userName = userinfoResp.UserName
	}

	return map[string]string{
		"userID":   userinfoResp.Sub.String(),
		"userName": userName,
		"email":    userinfoResp.Email,
		"role":     "",
	}, nil
}

// redirectToLogin 重定向到应用自身的登录页
func (a *Auth) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	loginURL := a.conf.AppGateway.LoginURL
	if loginURL == "" {
		scheme := config.NormalizeAppGatewayScheme(a.conf.AppGateway.Scheme)
		loginURL = fmt.Sprintf("%s://%s/login", scheme, r.Host)
	}
	http.Redirect(w, r, loginURL, http.StatusFound)
}

// redirectToLaunchpadApp 将角色门控失败的主文档请求 302 到平台 Launchpad 的应用详情页
// /launchpad/:projectId/:appId，由前端 app-detail 页按 403/404 渲染
// App access required / App not found 异常页，避免直接暴露网关原始错误 JSON
// （产品要求：页面不得展示 forbidden 等后端原始错误）。
//
// 跳转地址按当前请求 Host 动态推导（见 launchpadAuthority），请求未携带 Host 时
// 回落既有配置（{scheme}://{ExternalHost}，AppGatewayPublicAuthority，非标准端口时
// 携带 ExternalPort），与 APP_GATEWAY_LOGIN_URL / 主入口 URL 同源推导。
//
// 仅主文档导航（Sec-Fetch-Dest: document 或 Accept 含 text/html）才重定向；
// XHR/fetch（/api/、/graphql、/v1/* 等）保持 JSON 403/404，保留网关错误语义。
// discloseAppID=false（非成员 / App 不存在）时 appId 置 0，不披露应用存在性。
// 请求与配置均解析不出入口时返回 false，调用方回退原 403/404 JSON。
func (a *Auth) redirectToLaunchpadApp(w http.ResponseWriter, r *http.Request, route *repo.AppGatewayRoute, kind string, discloseAppID bool) bool {
	// 仅主文档导航重定向；数据/API 请求保持 JSON 403/404
	if !isDocumentNavigation(r) {
		return false
	}
	authority := a.launchpadAuthority(r)
	if authority == "" {
		logx.Infof("[AppGateway] [WARN] 外部入口未配置（ExternalHost 为空）且请求未携带 Host，跳过异常页重定向: appID=%s", route.AppID)
		return false
	}
	base := fmt.Sprintf("%s://%s", config.NormalizeAppGatewayScheme(a.conf.AppGateway.Scheme), authority)

	// 网关路由只持有部署产物 UUID，前端异常页需要 project_app 数字主键；
	// 应用已不存在（ErrNotFound）或非成员（不披露存在性）时用 0 占位，
	// 前端 App() 仍返回 404 → App not found。
	appID := int64(0)
	if discloseAppID {
		app, err := a.resolver.newStore(r.Context()).GetProjectAppByDeployAppID(r.Context(), route.ProjectID, route.AppID)
		switch {
		case err == nil:
			appID = app.ID
		case errors.Is(err, repo.ErrNotFound):
			// 应用已删除/不存在：跳 App not found 页（appId 保持 0）
		default:
			logx.Errorf("[AppGateway] 重定向异常页时查询应用失败: appID=%s err=%v", route.AppID, err)
			return false
		}
	}

	target := fmt.Sprintf("%s/launchpad/%d/%d", base, route.ProjectID, appID)
	logx.Infof("[AppGateway] 重定向到 Launchpad 异常页: kind=%s target=%s", kind, target)
	http.Redirect(w, r, target, http.StatusFound)
	return true
}

// launchpadAuthority 解析 Launchpad 异常页所在的平台入口 authority（host[:port]）。
// 优先按当前请求 Host 动态推导，服务器换 IP / 多网卡后不再依赖安装期烘入的
// ExternalHost；请求未携带 Host 时回落配置 ExternalHost。
//   - subdomain 形态（host 为 <label>.<SubdomainSuffix>，label 为 shortID 或旧格式
//     app-<shortID>）：平台入口取去掉最左标签的父域，请求显式端口原样保留；
//   - 其余（port 模式 / IP 直访）：取请求 host 的主机名部分，端口沿用
//     AppGatewayPublicAuthority 的 ExternalPort 归一化。
func (a *Auth) launchpadAuthority(r *http.Request) string {
	requestHost := strings.TrimSpace(httpx.ForwardedHost(r, a.conf.Gateway.TrustProxyHeaders))
	hostname := httpx.HostnameOnly(requestHost)
	if hostname == "" {
		return config.AppGatewayPublicAuthority(a.conf.AppGateway, a.conf.AppGateway.ExternalHost)
	}
	if parent, ok := parentDomainOfSubdomainHost(hostname, a.conf.AppGateway.SubdomainSuffix); ok {
		_, port, err := net.SplitHostPort(requestHost)
		if err != nil || port == "" {
			return parent
		}
		return net.JoinHostPort(parent, port)
	}
	return config.AppGatewayPublicAuthority(a.conf.AppGateway, hostname)
}

// parentDomainOfSubdomainHost 判断 host 是否为单标签子域 <label>.<suffix>；
// 是则返回去最左标签后的父域。
func parentDomainOfSubdomainHost(host, suffix string) (string, bool) {
	suffix = strings.ToLower(strings.TrimSpace(suffix))
	if suffix == "" {
		return "", false
	}
	lower := strings.ToLower(host)
	rest, ok := strings.CutSuffix(lower, "."+suffix)
	if !ok || rest == "" || strings.Contains(rest, ".") {
		return "", false
	}
	dot := strings.Index(host, ".")
	if dot <= 0 || dot+1 >= len(host) {
		return "", false
	}
	return host[dot+1:], true
}

// isDocumentNavigation 判定请求是否为浏览器主文档导航。
// 优先用 Sec-Fetch-Dest（现代浏览器），回退到 Accept 头含 text/html。
// XHR/fetch（Sec-Fetch-Dest: empty）与非 HTML accept 不重定向，保留 JSON 错误语义。
func isDocumentNavigation(r *http.Request) bool {
	if r == nil {
		return false
	}
	if dest := r.Header.Get("Sec-Fetch-Dest"); dest != "" {
		return dest == "document"
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html")
}

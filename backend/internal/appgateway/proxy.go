package appgateway

import (
	"backend/internal/config"
	"backend/internal/repo"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
)

// Proxy 反向代理
type Proxy struct {
	router *Router
	scheme string
}

// NewProxy 创建反向代理
func NewProxy(router *Router, conf ...config.AppGatewayConfig) *Proxy {
	scheme := "https"
	if len(conf) > 0 && conf[0].Scheme == "http" {
		scheme = "http"
	}
	return &Proxy{
		router: router,
		scheme: scheme,
	}
}

// ServeHTTP 代理请求到应用容器
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request, route *repo.AppGatewayRoute) {
	targetURL := fmt.Sprintf("http://%s:%d", route.ContainerHost, route.ContainerPort)
	target, err := url.Parse(targetURL)
	if err != nil {
		http.Error(w, "Invalid target", http.StatusInternalServerError)
		return
	}

	proxy := httputil.NewSingleHostReverseProxy(target)

	// 修改请求
	proxy.Director = func(req *http.Request) {
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		req.URL.Path = r.URL.Path
		req.URL.RawQuery = r.URL.RawQuery
		req.Header = r.Header.Clone()
		req.Header.Set("X-Forwarded-Host", r.Host)
		req.Header.Set("X-Forwarded-Proto", p.scheme)
		req.Header.Set("X-Forwarded-Scheme", p.scheme)
	}

	// 修改响应
	originalHost := r.Host
	publicScheme := p.scheme
	proxy.ModifyResponse = func(resp *http.Response) error {
		return modifyResponse(resp, originalHost, route, publicScheme)
	}

	proxy.ServeHTTP(w, r)
}

// modifyResponse 修改响应（Location 重写、Cookie 设置）
func modifyResponse(resp *http.Response, originalHost string, route *repo.AppGatewayRoute, publicScheme ...string) error {
	scheme := "https"
	if len(publicScheme) > 0 && publicScheme[0] == "http" {
		scheme = "http"
	}
	// 1. 重写 Location header
	if loc := resp.Header.Get("Location"); loc != "" {
		loc = rewriteLocation(loc, originalHost, route, scheme)
		resp.Header.Set("Location", loc)
	}

	// 2. 设置 app_auth_token cookie（如果从 URL 获取了 _token）
	if resp.Request != nil {
		if token := resp.Request.Context().Value("token_from_url"); token != nil {
			if tokenStr, ok := token.(string); ok && tokenStr != "" {
				cookie := fmt.Sprintf("app_auth_token=%s; Path=/; Max-Age=300", tokenStr)
				if scheme == "https" {
					cookie += "; Secure; SameSite=None"
				} else {
					cookie += "; SameSite=Lax"
				}
				resp.Header.Add("Set-Cookie", cookie)
			}
		}
	}
	if scheme == "http" {
		normalizeHTTPSetCookie(resp.Header)
	}

	return nil
}

// rewriteLocation 重写 Location header
func rewriteLocation(loc, originalHost string, route *repo.AppGatewayRoute, publicScheme ...string) string {
	scheme := "https"
	if len(publicScheme) > 0 && publicScheme[0] == "http" {
		scheme = "http"
	}
	internalHost := fmt.Sprintf("%s:%d", route.ContainerHost, route.ContainerPort)

	// http://172.18.0.1:30001/path -> public gateway URL
	if strings.Contains(loc, internalHost) {
		loc = strings.Replace(loc, "http://"+internalHost, scheme+"://"+originalHost, 1)
		loc = strings.Replace(loc, "https://"+internalHost, scheme+"://"+originalHost, 1)
	}
	if scheme == "http" {
		loc = strings.Replace(loc, "https://"+originalHost, "http://"+originalHost, 1)
	}

	// 处理应用使用容器内部 IP 的情况（如 172.18.x.x:port）
	containerIPPattern := regexp.MustCompile(`https?://172\.18\.\d+\.\d+:\d+`)
	if containerIPPattern.MatchString(loc) {
		loc = containerIPPattern.ReplaceAllString(loc, scheme+"://"+originalHost)
	}

	// 清除 _token 参数
	re := regexp.MustCompile(`([?&])_token=[^&]*`)
	loc = re.ReplaceAllString(loc, "$1")
	loc = strings.Replace(loc, "?&", "?", 1)
	loc = strings.TrimRight(loc, "?&")
	if strings.HasSuffix(loc, "?") {
		loc = strings.TrimSuffix(loc, "?")
	}

	return loc
}

func normalizeHTTPSetCookie(header http.Header) {
	values := header.Values("Set-Cookie")
	if len(values) == 0 {
		return
	}
	header.Del("Set-Cookie")
	for _, value := range values {
		value = strings.ReplaceAll(value, "; Secure", "")
		value = strings.ReplaceAll(value, "; secure", "")
		value = strings.ReplaceAll(value, "SameSite=None", "SameSite=Lax")
		header.Add("Set-Cookie", value)
	}
}

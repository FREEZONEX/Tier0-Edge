package gateway

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// S3Proxy reverse-proxies object-storage bucket prefixes (e.g. /private/,
// /public/) on the platform entrance to the in-cluster S3 endpoint.
//
// The host-rewrite signing model: the backend issues SigV4 presigned URLs
// against the internal endpoint (e.g. http://rustfs:9000), then rewrites only
// scheme://host to the public entrance before returning them to clients. The
// gateway proxies those requests back to the internal endpoint with
// req.Host rewritten to target.Host and the path untouched, so the Host and
// path the object store verifies are exactly the ones that were signed.
type S3Proxy struct {
	target   *url.URL
	prefixes []string
	proxy    *httputil.ReverseProxy
}

// NewS3Proxy builds an S3 gateway proxy. It returns nil when the storage
// driver is not s3, the endpoint is empty/unparseable, or no bucket name is
// given; callers treat nil as "no S3 proxying" and skip the gateway hook.
func NewS3Proxy(driver, endpoint string, buckets ...string) *S3Proxy {
	if strings.ToLower(strings.TrimSpace(driver)) != "s3" {
		return nil
	}
	raw := strings.TrimSpace(endpoint)
	if raw == "" {
		return nil
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	target, err := url.Parse(raw)
	if err != nil || target.Host == "" {
		return nil
	}
	prefixes := make([]string, 0, len(buckets))
	for _, bucket := range buckets {
		bucket = strings.Trim(strings.TrimSpace(bucket), "/")
		if bucket == "" {
			continue
		}
		prefixes = append(prefixes, "/"+bucket+"/")
	}
	if len(prefixes) == 0 {
		return nil
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		// Align with the saas reverse proxy: the upstream must see the signing
		// host, otherwise SigV4 verification fails. Path and query are left
		// exactly as the client sent them.
		req.Host = target.Host
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, err.Error(), http.StatusBadGateway)
	}
	// 客户端经 public endpoint 跨域直传时，对象存储自身（RustFS/MinIO）默认不
	// 带 CORS 头：这里在代理响应上统一补齐，放行跨域读取并暴露 ETag（前端合并
	// 分片需要），避免依赖对象存储侧的 CORS 配置。
	proxy.ModifyResponse = func(resp *http.Response) error {
		setS3CorsHeaders(resp.Header)
		return nil
	}
	return &S3Proxy{target: target, prefixes: prefixes, proxy: proxy}
}

const (
	s3CorsAllowMethods  = "GET, PUT, POST, HEAD, DELETE, OPTIONS"
	s3CorsExposeHeaders = "ETag, Content-Length, Content-Type"
	s3CorsMaxAge        = "600"
)

// setS3CorsHeaders 为对象存储代理响应补齐跨域头。预签名 URL 本身就是访问凭证，
// 允许任意源（*）不会扩大权限面；直传请求不携带 cookie，无需按源回显。
func setS3CorsHeaders(h http.Header) {
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Expose-Headers", s3CorsExposeHeaders)
}

// matchesPrefix reports whether the path falls under a proxied bucket prefix.
func (p *S3Proxy) matchesPrefix(path string) bool {
	for _, prefix := range p.prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// serveS3Preflight answers CORS preflight (OPTIONS) for proxied bucket paths.
// 浏览器对跨域 PUT 必发 OPTIONS 预检；对象存储不响应预检，必须由网关代答，
// 否则预检失败、直传 PUT 根本发不出去。
func serveS3Preflight(w http.ResponseWriter, r *http.Request) {
	setS3CorsHeaders(w.Header())
	w.Header().Set("Access-Control-Allow-Methods", s3CorsAllowMethods)
	// 回显预检请求声明的头部（content-type、x-amz-* 等），避免维护硬编码清单。
	if requested := r.Header.Get("Access-Control-Request-Headers"); requested != "" {
		w.Header().Set("Access-Control-Allow-Headers", requested)
	}
	w.Header().Set("Access-Control-Max-Age", s3CorsMaxAge)
	w.WriteHeader(http.StatusNoContent)
}

// TryProxy proxies the request to the object store when its method is an S3
// object operation (GET/PUT/POST/HEAD/DELETE) and its path starts with one of
// the configured bucket prefixes. OPTIONS 预检不转发，由网关直接代答 CORS。
// It reports whether the request was handled.
// POST 必须放行：预签名 POST（POST policy）直传的 multipart 表单以 POST 提交，
// 否则经 public endpoint 改写后的 postUrl 会落到普通后端路由而上传失败。
func (p *S3Proxy) TryProxy(w http.ResponseWriter, r *http.Request) bool {
	if p == nil {
		return false
	}
	if !p.matchesPrefix(r.URL.Path) {
		return false
	}
	// 跨域直传的 OPTIONS 预检由网关代答，不转发给对象存储。
	if r.Method == http.MethodOptions {
		serveS3Preflight(w, r)
		return true
	}
	switch r.Method {
	case http.MethodGet, http.MethodPut, http.MethodPost, http.MethodHead, http.MethodDelete:
	default:
		return false
	}
	p.proxy.ServeHTTP(w, r)
	return true
}

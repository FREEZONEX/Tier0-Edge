package middleware

import (
	"net/http"

	"backend/internal/httpx"
)

// RequestAuthorityMiddleware 每请求解析对外 scheme+host 存入 ctx，供读路径按当前
// 请求 Host 动态重建生成型入口地址（服务器换 IP / 双网卡后不再依赖安装期配置值）。
type RequestAuthorityMiddleware struct {
	trustProxyHeaders bool
}

func NewRequestAuthorityMiddleware(trustProxyHeaders bool) *RequestAuthorityMiddleware {
	return &RequestAuthorityMiddleware{trustProxyHeaders: trustProxyHeaders}
}

func (m *RequestAuthorityMiddleware) Handle(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m != nil && r != nil && r.Host != "" {
			scheme, host := httpx.Authority(r, m.trustProxyHeaders)
			r = r.WithContext(httpx.WithAuthority(r.Context(), scheme, host))
			if m.trustProxyHeaders {
				r = r.WithContext(httpx.WithProxyPrefix(r.Context(), r.Header.Get("X-Forwarded-Prefix")))
			}
		}
		next(w, r)
	}
}

package mcpoauth

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"backend/internal/contextx"
	"backend/internal/httpx"
	"backend/internal/mcpaccess"
	"backend/internal/permission"
	"backend/internal/types"
	zerox "github.com/zeromicro/go-zero/rest/httpx"
)

func secureHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}
func write(w http.ResponseWriter, value any, err error, internal bool, status int) {
	secureHeaders(w)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		f := failure(err)
		w.WriteHeader(f.Status)
		if internal {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": f.Status, "msg": f.Code})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": f.Code})
		}
		return
	}
	w.WriteHeader(status)
	if internal {
		value = map[string]any{"code": 200, "msg": "ok", "data": value}
	}
	_ = json.NewEncoder(w).Encode(value)
}
func parse(r *http.Request, value any) error {
	var err error
	if r.Method == http.MethodGet {
		err = zerox.ParseForm(r, value)
	} else {
		// go-zero Parse 会同时校验 JSON 和 form；标准 token 表单不能被空 JSON 覆盖。
		kind, _, parseErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if parseErr != nil || r.URL.RawQuery != "" {
			return invalid("invalid_request")
		}
		switch kind {
		case "application/json":
			err = zerox.ParseJsonBody(r, value)
		case "application/x-www-form-urlencoded":
			err = zerox.ParseForm(r, value)
			for _, values := range r.Form {
				if len(values) != 1 {
					return invalid("invalid_request")
				}
			}
		default:
			return invalid("invalid_request")
		}
	}
	if err != nil {
		return invalid("invalid_request")
	}
	return nil
}
func (s *Service) sameOrigin(r *http.Request) bool {
	// 管理页可经实例的内部入口访问，显式 MCP 公网地址仍绑定同一授权资源。
	if scheme, host, ok := httpx.AuthorityFrom(r.Context()); ok && r.Header.Get("Origin") == scheme+"://"+host {
		return true
	}
	_, issuer, err := endpoints(r.Context(), s.configuration)
	if err != nil {
		return false
	}
	u, _ := url.Parse(issuer)
	return r.Header.Get("Origin") == u.Scheme+"://"+u.Host
}

// Handle 是标准协议的薄 HTTP 适配；地址/DTO/路由均由 .api 生成。
func (s *Service) Handle(operation string, w http.ResponseWriter, r *http.Request) {
	secureHeaders(w)
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	internal := strings.HasPrefix(operation, "McpOAuthRequest") || operation == "McpOAuthDecision" || operation == "McpOAuthGrants" || operation == "McpOAuthGrantRevoke"
	if r.URL.Query().Has("token") || r.URL.Query().Has("access_token") || r.URL.Query().Has("apiKey") {
		write(w, nil, invalid("invalid_request"), internal, 200)
		return
	}
	if _, _, err := endpoints(r.Context(), s.configuration); err != nil {
		write(w, nil, err, internal, 200)
		return
	}
	if internal && r.Method == http.MethodPost && !s.sameOrigin(r) {
		write(w, nil, &Failure{Code: "invalid_origin", Status: 403}, true, 200)
		return
	}
	var value any
	var err error
	status := 200
	switch operation {
	case "McpOAuthResourceMetadata", "McpOAuthPathResourceMetadata":
		value, err = s.ResourceMetadata(r.Context())
	case "McpOAuthServerMetadata", "McpOAuthServerMetadataAlias":
		value, err = s.ServerMetadata(r.Context())
	case "McpOAuthRegister":
		var input types.McpOAuthRegisterReq
		err = parse(r, &input)
		if err == nil {
			value, err = s.Register(r.Context(), input)
			status = 201
		}
	case "McpOAuthAuthorize":
		for _, values := range r.URL.Query() {
			if len(values) != 1 {
				write(w, nil, invalid("invalid_request"), false, 200)
				return
			}
		}
		var input types.McpOAuthAuthorizeReq
		err = parse(r, &input)
		if err == nil {
			pending, browser, beginErr := s.Begin(r.Context(), input)
			err = beginErr
			if err == nil {
				base, _ := url.Parse(pending.Issuer)
				cookiePath := strings.TrimRight(base.Path, "/") + "/"
				http.SetCookie(w, &http.Cookie{Name: cookieName(pending.RequestID), Value: browser, HttpOnly: true, Secure: base.Scheme == "https", SameSite: http.SameSiteLaxMode, Path: cookiePath, MaxAge: int(requestTTL.Seconds())})
				http.Redirect(w, r, cookiePath+"mcp-authorize?requestId="+url.QueryEscape(pending.RequestID), http.StatusSeeOther)
				return
			}
		}
	case "McpOAuthToken":
		var input types.McpOAuthTokenReq
		err = parse(r, &input)
		if err == nil {
			value, err = s.Exchange(r.Context(), input)
		}
	case "McpOAuthRevoke":
		var input types.McpOAuthRevokeReq
		err = parse(r, &input)
		if err == nil {
			value, err = s.Revoke(r.Context(), input)
		}
	case "McpOAuthRequest":
		var input types.McpOAuthRequestReq
		err = parse(r, &input)
		if err == nil {
			value, err = s.Request(r.Context(), input.RequestId, browserSecret(r, input.RequestId))
		}
	case "McpOAuthDecision":
		var input types.McpOAuthDecisionReq
		err = parse(r, &input)
		if err == nil {
			value, err = s.Decide(r.Context(), input, browserSecret(r, input.RequestId))
			if err == nil {
				_, issuer, _ := endpoints(r.Context(), s.configuration)
				u, _ := url.Parse(issuer)
				http.SetCookie(w, &http.Cookie{Name: cookieName(input.RequestId), Value: "", HttpOnly: true, Secure: u.Scheme == "https", SameSite: http.SameSiteLaxMode, Path: strings.TrimRight(u.Path, "/") + "/", MaxAge: -1})
			}
		}
	case "McpOAuthGrants":
		value, err = s.Grants(r.Context())
	case "McpOAuthGrantRevoke":
		var input types.McpOAuthGrantRevokeReq
		err = parse(r, &input)
		if err == nil {
			value, err = s.RevokeGrant(r.Context(), input.GrantId)
		}
	default:
		err = &Failure{Code: "not_found", Status: 404}
	}
	write(w, value, err, internal, status)
}

func (s *Service) challenge(w http.ResponseWriter, r *http.Request, status int, errorCode string) {
	_, issuer, err := endpoints(r.Context(), s.configuration)
	if err != nil {
		write(w, nil, err, false, 200)
		return
	}
	// 初次发现交给资源元数据声明候选范围，避免将所有新连接锁定为只读。
	value := fmt.Sprintf(`Bearer resource_metadata=%q`, issuer+"/.well-known/oauth-protected-resource")
	if status == http.StatusForbidden && errorCode == "insufficient_scope" {
		value += `, scope="uns.read"`
	}
	if errorCode != "" {
		value += fmt.Sprintf(`, error=%q`, errorCode)
	}
	w.Header().Set("WWW-Authenticate", value)
	secureHeaders(w)
	http.Error(w, "MCP authentication required", status)
}

// Protect 优先识别 MCP 专用 access token，其余复用现有 API Key 鉴权和权限匹配。
// 平台 session / 原 App OAuth token 不能在这里兑换为 MCP 身份。
func (s *Service) Protect(evaluator *permission.Evaluator, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.configuration.Disabled {
			write(w, nil, unavailable(), false, 200)
			return
		}
		if r.URL.Query().Has("token") || r.URL.Query().Has("apiKey") || r.URL.Query().Has("apikey") || r.URL.Query().Has("access_token") {
			s.challenge(w, r, 401, "invalid_token")
			return
		}
		raw := r.Header.Get("X-API-Key")
		bearer := r.Header.Get("Authorization")
		if raw == "" && strings.HasPrefix(bearer, "Bearer ") {
			raw = strings.TrimPrefix(bearer, "Bearer ")
		}
		if raw == "" {
			s.challenge(w, r, 401, "")
			return
		}
		var subject contextx.Subject
		var err error
		if strings.HasPrefix(raw, "mcat_") {
			subject, err = s.Authenticate(r, raw)
		} else {
			subject, err = s.auth.AuthenticateAPIKey(r.Context(), raw)
			if err == nil {
				subject = mcpaccess.Constrain(subject, mcpaccess.Available(subject))
			}
		}
		if err != nil {
			s.challenge(w, r, 401, "invalid_token")
			return
		}
		allowed, _, err := evaluator.Allow(r.Context(), subject, "openapi", r.Method, "/mcp")
		if err != nil {
			write(w, nil, unavailable(), false, 200)
			return
		}
		if !allowed {
			s.challenge(w, r, 403, "insufficient_scope")
			return
		}
		ctx := contextx.WithSubject(r.Context(), subject)
		if s.audit != nil {
			s.audit.BindSubject(ctx, subject)
		}
		next(w, r.WithContext(ctx))
	}
}

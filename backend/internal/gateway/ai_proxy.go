package gateway

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"

	"backend/internal/domain/aigateway"
)

const (
	aiProxyPrefix         = "/api/internal/ai/"
	aiGatewayManagedModel = "gateway-managed"
)

var errAIModelNotConfigured = errors.New("AI model is not configured")

type AIProxy struct {
	service *aigateway.Service
}

func NewAIProxy(service *aigateway.Service) *AIProxy {
	return &AIProxy{service: service}
}

func (p *AIProxy) TryProxy(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, aiProxyPrefix) {
		return false
	}
	if p == nil || p.service == nil {
		writeAIProxyError(w, http.StatusServiceUnavailable, "AI gateway unavailable")
		return true
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		writeAIProxyError(w, http.StatusMethodNotAllowed, "method not allowed")
		return true
	}
	serviceName, authenticated := p.service.AuthenticateToken(bearerToken(r.Header.Get("Authorization")))
	if !authenticated {
		writeAIProxyError(w, http.StatusUnauthorized, "invalid internal service token")
		return true
	}
	if claimed := strings.TrimSpace(r.Header.Get("X-Tier0-Service")); claimed != "" && !strings.EqualFold(claimed, serviceName) {
		writeAIProxyError(w, http.StatusUnauthorized, "internal service identity mismatch")
		return true
	}
	configID, workspaceID, upstreamPath, ok := parseAIProxyPath(r.URL.Path)
	if !ok || strings.Contains(upstreamPath, "..") {
		writeAIProxyError(w, http.StatusBadRequest, "invalid AI gateway path")
		return true
	}
	var resolved aigateway.ResolvedConfig
	var err error
	if isAIEmbeddingsPath(upstreamPath) {
		// 向量请求可能与 chat 走不同上游（AI 设置里的「单独配置」模式），
		// 且 model 替换取 embeddingModel 而不是对话模型。
		resolved, err = p.service.ResolveEmbedding(r.Context(), configID, workspaceID)
	} else {
		resolved, err = p.service.Resolve(r.Context(), configID, workspaceID)
	}
	if err != nil {
		writeAIResolveError(w, err)
		return true
	}
	p.proxy(w, r, resolved, upstreamPath)
	return true
}

func (p *AIProxy) proxy(w http.ResponseWriter, r *http.Request, resolved aigateway.ResolvedConfig, upstreamPath string) {
	if err := overrideAIRequestModel(r, upstreamPath, resolved.Model); err != nil {
		if errors.Is(err, errAIModelNotConfigured) {
			message := "AI chat model is not configured; configure a model in AI Settings"
			if isAIEmbeddingsPath(upstreamPath) {
				message = "AI embedding model is not configured; configure an embedding model in AI Settings"
			}
			writeAIProxyError(w, http.StatusServiceUnavailable, message)
			return
		}
		writeAIProxyError(w, http.StatusBadRequest, "invalid AI request body")
		return
	}
	target := *resolved.BaseURL
	proxy := &httputil.ReverseProxy{
		Transport: p.service.HTTPTransport(),
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = joinAIPath(target.Path, upstreamPath)
			req.URL.RawPath = ""
			req.Host = target.Host
			stripAIAuthHeaders(req.Header)
			injectProviderAuth(req.Header, resolved.Provider, resolved.APIKey)
		},
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("Set-Cookie")
			resp.Header.Set("Cache-Control", "no-store")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			if errors.Is(err, aigateway.ErrUpstreamRestricted) {
				writeAIProxyError(w, http.StatusForbidden, "AI provider access is restricted by system policy")
				return
			}
			if errors.Is(err, aigateway.ErrUpstreamNotAllowed) {
				writeAIProxyError(w, http.StatusForbidden, "AI provider is not trusted")
				return
			}
			writeAIProxyError(w, http.StatusBadGateway, "AI provider unavailable")
		},
		FlushInterval: -1,
	}
	w.Header().Set("X-Tier0-AI-Config-ID", strconv.FormatInt(resolved.ID, 10))
	proxy.ServeHTTP(w, r)
}

const maxAIJSONRequestBytes = 32 << 20

// overrideAIRequestModel makes the model stored in Enterprise AI Settings the
// source of truth. Notebook and UNS Agent may send a client-side model alias to
// satisfy their SDK/UI, but that alias is never forwarded to the provider.
func overrideAIRequestModel(r *http.Request, upstreamPath, model string) error {
	model = strings.TrimSpace(model)
	if r.Method != http.MethodPost || r.Body == nil || !isAIModelOverridePath(upstreamPath) {
		return nil
	}
	// 内部占位名只用于让客户端把选模交给网关，不能作为已配置的上游模型。
	if strings.EqualFold(model, aiGatewayManagedModel) {
		return errAIModelNotConfigured
	}
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if contentType != "" && !strings.HasPrefix(contentType, "application/json") {
		return errors.New("AI request body must be JSON")
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxAIJSONRequestBytes+1))
	_ = r.Body.Close()
	if err != nil || len(body) > maxAIJSONRequestBytes {
		return errors.New("AI request body is unreadable or too large")
	}
	if len(bytes.TrimSpace(body)) == 0 {
		setAIRequestBody(r, body)
		return nil
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		return errors.New("AI request body must be a JSON object")
	}
	if model == "" {
		var requestedModel string
		if rawModel, ok := payload["model"]; ok {
			if err := json.Unmarshal(rawModel, &requestedModel); err != nil {
				return errors.New("AI request model must be a string")
			}
		}
		// 保留客户端显式指定真实模型的兼容行为，但缺配时必须在网关内拦截占位名。
		if strings.EqualFold(strings.TrimSpace(requestedModel), aiGatewayManagedModel) {
			return errAIModelNotConfigured
		}
		setAIRequestBody(r, body)
		return nil
	}
	encodedModel, err := json.Marshal(model)
	if err != nil {
		return err
	}
	payload["model"] = encodedModel
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	setAIRequestBody(r, rewritten)
	return nil
}

func setAIRequestBody(r *http.Request, body []byte) {
	r.Body = io.NopCloser(bytes.NewReader(body))
	// 重试也必须使用替换后的请求体，不能经 GetBody 重新带出原始占位名。
	r.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Length", strconv.Itoa(len(body)))
	r.TransferEncoding = nil
}

func isAIModelOverridePath(path string) bool {
	path = "/" + strings.Trim(strings.ToLower(path), "/")
	return strings.HasSuffix(path, "/chat/completions") ||
		strings.HasSuffix(path, "/responses") ||
		strings.HasSuffix(path, "/messages") ||
		strings.HasSuffix(path, "/embeddings")
}

func isAIEmbeddingsPath(path string) bool {
	path = "/" + strings.Trim(strings.ToLower(path), "/")
	return strings.HasSuffix(path, "/embeddings")
}

func parseAIProxyPath(path string) (configID, workspaceID int64, upstreamPath string, ok bool) {
	segments := strings.Split(strings.TrimPrefix(path, aiProxyPrefix), "/")
	if len(segments) < 3 {
		return 0, 0, "", false
	}
	id, err := strconv.ParseInt(segments[1], 10, 64)
	if err != nil || id <= 0 {
		return 0, 0, "", false
	}
	upstreamPath = "/" + strings.Join(segments[2:], "/")
	switch segments[0] {
	case "configs":
		return id, 0, upstreamPath, true
	case "workspaces":
		return 0, id, upstreamPath, true
	default:
		return 0, 0, "", false
	}
}

func bearerToken(value string) string {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func stripAIAuthHeaders(header http.Header) {
	for _, name := range []string{
		"Authorization", "Api-Key", "X-Api-Key", "Proxy-Authorization",
		"X-Tier0-Service", "X-Tier0-Internal-Token", "Cookie",
	} {
		header.Del(name)
	}
}

func injectProviderAuth(header http.Header, provider, apiKey string) {
	switch provider {
	case aigateway.ProviderAzureOpenAI:
		header.Set("Api-Key", apiKey)
	case aigateway.ProviderAnthropic:
		header.Set("X-Api-Key", apiKey)
	default:
		header.Set("Authorization", "Bearer "+apiKey)
	}
}

func joinAIPath(base, suffix string) string {
	base = strings.TrimRight(base, "/")
	if base == "" {
		return "/" + strings.TrimLeft(suffix, "/")
	}
	suffix = "/" + strings.TrimLeft(suffix, "/")
	baseSegment := base[strings.LastIndex(base, "/")+1:]
	if isAPIVersionSegment(baseSegment) && (suffix == "/"+baseSegment || strings.HasPrefix(suffix, "/"+baseSegment+"/")) {
		suffix = strings.TrimPrefix(suffix, "/"+baseSegment)
	}
	return base + suffix
}

func isAPIVersionSegment(segment string) bool {
	if len(segment) < 2 || segment[0] != 'v' {
		return false
	}
	for _, ch := range segment[1:] {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func writeAIResolveError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, aigateway.ErrNotFound):
		writeAIProxyError(w, http.StatusNotFound, "AI configuration not found")
	case errors.Is(err, aigateway.ErrDisabled):
		writeAIProxyError(w, http.StatusServiceUnavailable, "AI configuration disabled")
	case errors.Is(err, aigateway.ErrCredentialKeyMissing), errors.Is(err, aigateway.ErrCredentialInvalid):
		writeAIProxyError(w, http.StatusServiceUnavailable, "AI credentials unavailable")
	case errors.Is(err, aigateway.ErrUpstreamRestricted):
		writeAIProxyError(w, http.StatusForbidden, "AI provider access is restricted by system policy")
	case errors.Is(err, aigateway.ErrUpstreamNotAllowed):
		writeAIProxyError(w, http.StatusForbidden, "AI provider is not trusted")
	default:
		writeAIProxyError(w, http.StatusInternalServerError, "AI gateway unavailable")
	}
}

func writeAIProxyError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code": status,
		"msg":  message,
	})
}

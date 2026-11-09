package aigateway

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"backend/internal/config"
	"backend/internal/repo"
)

const (
	ProviderOpenAICompatible = "openai_compatible"
	ProviderAzureOpenAI      = "azure_openai"
	ProviderAnthropic        = "anthropic"

	ServiceNotebook = "notebook"
	ServiceUNSAgent = "uns-agent"

	EmbeddingModeInherit = "inherit"
	EmbeddingModeCustom  = "custom"

	// ExpectedEmbeddingDimensions 是 uns-agent 数据库向量列的固定宽度；
	// 向量模型输出维度必须与它一致，否则向量写入直接失败。
	ExpectedEmbeddingDimensions = 1536

	trustedHostsSettingKey = "ai_gateway_trusted_hosts"
	maxTrustedHosts        = 100
	maxTrustedHostLength   = 253
	happyEyeballsDelay     = 250 * time.Millisecond
)

var (
	ErrInvalid              = errors.New("AIGateway.invalid")
	ErrNotFound             = errors.New("AIGateway.notFound")
	ErrDisabled             = errors.New("AIGateway.disabled")
	ErrCredentialKeyMissing = errors.New("AIGateway.credentialKeyMissing")
	ErrCredentialInvalid    = errors.New("AIGateway.credentialInvalid")
	ErrUpstreamNotAllowed   = errors.New("AIGateway.upstreamNotAllowed")
	ErrUpstreamRestricted   = errors.New("AIGateway.upstreamRestricted")
	ErrAlreadyConfigured    = errors.New("AIGateway.alreadyConfigured")
	ErrConnectionFailed     = errors.New("AIGateway.connectionFailed")
	ErrTrustedHostsInvalid  = errors.New("AIGateway.trustedHostsInvalid")

	defaultTrustedHosts = []string{
		"api.openai.com",
		"api.anthropic.com",
		"*.openai.azure.com",
	}
)

type Service struct {
	repo           *repo.AIProviderConfigRepo
	systemSettings *repo.SystemSettingRepo
	cipher         *credentialCipher
	notebookToken  string
	unsAgentToken  string
	operatorHosts  []string
	privateHosts   []string
	transport      http.RoundTripper
}

type SaveCommand struct {
	ID          int64
	WorkspaceID int64
	Name        string
	Provider    string
	BaseURL     string
	Model       string
	APIKey      string
	IsDefault   *bool
	Enabled     *bool
	UserID      int64

	EmbeddingMode    string
	EmbeddingBaseURL string
	EmbeddingAPIKey  string
	EmbeddingModel   string
}

type ResolvedConfig struct {
	ID          int64
	WorkspaceID int64
	Provider    string
	BaseURL     *url.URL
	APIKey      string
	Model       string
}

func New(ctx context.Context, conf config.AIGatewayConf) (*Service, error) {
	cipher, err := newCredentialCipher(conf.CredentialKey)
	if err != nil {
		return nil, err
	}
	operatorHosts := splitAllowedHosts(conf.AllowedHosts)
	// Older deployments wrote the former built-in defaults into this variable.
	// Treat only that exact legacy set as "no operator ceiling"; a customized
	// value remains a deliberate deployment-level restriction.
	if isLegacyDefaultHostSet(operatorHosts) {
		operatorHosts = nil
	}
	privateHosts := splitAllowedHosts(conf.AllowedPrivateHosts)
	return &Service{
		repo:           repo.NewAIProviderConfigRepo(ctx),
		systemSettings: repo.NewSystemSettingRepo(ctx),
		cipher:         cipher,
		notebookToken:  strings.TrimSpace(conf.NotebookToken),
		unsAgentToken:  strings.TrimSpace(conf.UNSAgentToken),
		operatorHosts:  operatorHosts,
		privateHosts:   privateHosts,
		transport:      newAIGatewayTransport(privateHosts),
	}, nil
}

func (s *Service) TrustedHosts(ctx context.Context) (map[string]any, error) {
	hosts, customized, err := s.loadTrustedHosts(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{"hosts": hosts, "customized": customized}, nil
}

func (s *Service) SaveTrustedHosts(ctx context.Context, hosts []string) (map[string]any, error) {
	normalized, err := normalizeTrustedHosts(hosts)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	if err := s.systemSettings.PutSetting(ctx, trustedHostsSettingKey, string(payload)); err != nil {
		return nil, err
	}
	return map[string]any{"hosts": normalized, "customized": true}, nil
}

func (s *Service) List(ctx context.Context, workspaceID int64) (map[string]any, error) {
	workspaceID = normalizeWorkspaceID(workspaceID)
	items, err := s.repo.List(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	views := make([]map[string]any, 0, len(items))
	for _, item := range items {
		views = append(views, configView(item))
	}
	return map[string]any{"list": views, "total": len(views)}, nil
}

func (s *Service) Create(ctx context.Context, cmd SaveCommand) (map[string]any, error) {
	existing, err := s.repo.List(ctx, normalizeWorkspaceID(cmd.WorkspaceID))
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, ErrAlreadyConfigured
	}
	item, apiKey, embeddingAPIKey, _, err := s.normalizeSave(ctx, cmd, true)
	if err != nil {
		return nil, err
	}
	ciphertext, err := s.cipher.encrypt(apiKey)
	if err != nil {
		return nil, err
	}
	item.APIKeyCiphertext = ciphertext
	item.KeySuffix = secretSuffix(apiKey)
	if embeddingAPIKey != "" {
		embeddingCiphertext, err := s.cipher.encrypt(embeddingAPIKey)
		if err != nil {
			return nil, err
		}
		item.EmbeddingAPIKeyCiphertext = embeddingCiphertext
		item.EmbeddingKeySuffix = secretSuffix(embeddingAPIKey)
	}
	created, err := s.repo.Create(ctx, item)
	if err != nil {
		if errors.Is(err, repo.ErrDuplicate) {
			return nil, ErrAlreadyConfigured
		}
		return nil, err
	}
	return configView(created), nil
}

func (s *Service) Update(ctx context.Context, cmd SaveCommand) (map[string]any, error) {
	if cmd.ID <= 0 {
		return nil, ErrInvalid
	}
	item, apiKey, embeddingAPIKey, embeddingSet, err := s.normalizeSave(ctx, cmd, false)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{
		"name":       item.Name,
		"provider":   item.Provider,
		"base_url":   item.BaseURL,
		"model":      item.Model,
		"is_default": item.Enabled,
		"enabled":    item.Enabled,
		"updated_by": item.UpdatedBy,
	}
	if apiKey != "" {
		ciphertext, err := s.cipher.encrypt(apiKey)
		if err != nil {
			return nil, err
		}
		updates["api_key_ciphertext"] = ciphertext
		updates["key_suffix"] = secretSuffix(apiKey)
	}
	// 请求完全不带 embedding 字段（旧客户端）时不触碰已存的 embedding 列。
	if embeddingSet {
		updates["embedding_mode"] = item.EmbeddingMode
		updates["embedding_base_url"] = item.EmbeddingBaseURL
		updates["embedding_model"] = item.EmbeddingModel
		if embeddingAPIKey != "" {
			embeddingCiphertext, err := s.cipher.encrypt(embeddingAPIKey)
			if err != nil {
				return nil, err
			}
			updates["embedding_api_key_ciphertext"] = embeddingCiphertext
			updates["embedding_key_suffix"] = secretSuffix(embeddingAPIKey)
		} else if item.EmbeddingMode == EmbeddingModeCustom {
			// 切到单独配置但没带新 Key 时，必须已有加密保存的 embedding Key。
			stored, err := s.repo.Get(ctx, cmd.ID)
			if err != nil {
				return nil, normalizeNotFound(err)
			}
			if stored.EmbeddingAPIKeyCiphertext == "" {
				return nil, ErrInvalid
			}
		}
	}
	makeDefault := item.Enabled
	updated, err := s.repo.Update(ctx, cmd.ID, item.WorkspaceID, updates, makeDefault)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return configView(updated), nil
}

func (s *Service) Delete(ctx context.Context, id, workspaceID, userID int64) error {
	if id <= 0 {
		return ErrInvalid
	}
	return normalizeNotFound(s.repo.Delete(ctx, id, normalizeWorkspaceID(workspaceID), userID))
}

func (s *Service) TestConnection(ctx context.Context, cmd SaveCommand) error {
	workspaceID := normalizeWorkspaceID(cmd.WorkspaceID)
	provider := strings.ToLower(strings.TrimSpace(cmd.Provider))
	baseURL := strings.TrimSpace(cmd.BaseURL)
	apiKey := strings.TrimSpace(cmd.APIKey)
	if apiKey == "" {
		items, err := s.repo.List(ctx, workspaceID)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return ErrInvalid
		}
		stored := items[0]
		apiKey, err = s.cipher.decrypt(stored.APIKeyCiphertext)
		if err != nil {
			return err
		}
		if baseURL == "" {
			baseURL = stored.BaseURL
		}
		if provider == "" {
			provider = stored.Provider
		}
	}
	if provider == "" {
		provider = ProviderOpenAICompatible
	}
	if !supportedProvider(provider) || apiKey == "" {
		return ErrInvalid
	}
	target, err := s.validateBaseURL(ctx, baseURL)
	if err != nil {
		return err
	}
	target.Path = strings.TrimRight(target.Path, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return ErrConnectionFailed
	}
	switch provider {
	case ProviderAzureOpenAI:
		req.Header.Set("Api-Key", apiKey)
	case ProviderAnthropic:
		req.Header.Set("X-Api-Key", apiKey)
	default:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := s.httpClient(15 * time.Second).Do(req)
	if err != nil {
		if errors.Is(err, ErrUpstreamNotAllowed) {
			return ErrUpstreamNotAllowed
		}
		if errors.Is(err, ErrUpstreamRestricted) {
			return ErrUpstreamRestricted
		}
		return ErrConnectionFailed
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return ErrConnectionFailed
	}
	return nil
}

const (
	EmbeddingTestStatusOK            = "ok"
	EmbeddingTestStatusUnsupported   = "unsupported"
	EmbeddingTestStatusUnauthorized  = "unauthorized"
	EmbeddingTestStatusModelNotFound = "model_not_found"
	EmbeddingTestStatusFailed        = "failed"
)

type EmbeddingTestResult struct {
	Status             string `json:"status"`
	HTTPStatus         int    `json:"httpStatus"`
	Model              string `json:"model"`
	Dimensions         int    `json:"dimensions"`
	ExpectedDimensions int    `json:"expectedDimensions"`
	DimensionsMatch    bool   `json:"dimensionsMatch"`
	LatencyMs          int64  `json:"latencyMs"`
}

// TestEmbedding 真实调用一次 /embeddings 并实测返回向量的维度。
// 连通性/鉴权/端点缺失以 Status 分类返回而不是笼统报错，
// 维度以实测数组长度为准，用于在保存前拦住维度不符的模型。
func (s *Service) TestEmbedding(ctx context.Context, cmd SaveCommand) (EmbeddingTestResult, error) {
	workspaceID := normalizeWorkspaceID(cmd.WorkspaceID)
	items, err := s.repo.List(ctx, workspaceID)
	if err != nil {
		return EmbeddingTestResult{}, err
	}
	var stored repo.AIProviderConfig
	hasStored := len(items) > 0
	if hasStored {
		stored = items[0]
	}

	mode := strings.ToLower(strings.TrimSpace(cmd.EmbeddingMode))
	if mode == "" {
		mode = stored.EmbeddingMode
	}
	if mode == "" {
		mode = EmbeddingModeInherit
	}
	if mode != EmbeddingModeInherit && mode != EmbeddingModeCustom {
		return EmbeddingTestResult{}, ErrInvalid
	}
	model := strings.TrimSpace(cmd.EmbeddingModel)
	if model == "" {
		model = stored.EmbeddingModel
	}
	if model == "" {
		return EmbeddingTestResult{}, ErrInvalid
	}
	provider := strings.ToLower(strings.TrimSpace(cmd.Provider))
	if provider == "" {
		provider = stored.Provider
	}
	if provider == "" {
		provider = ProviderOpenAICompatible
	}
	if !supportedProvider(provider) {
		return EmbeddingTestResult{}, ErrInvalid
	}

	var baseURL, apiKey string
	if mode == EmbeddingModeCustom {
		baseURL = strings.TrimSpace(cmd.EmbeddingBaseURL)
		if baseURL == "" {
			baseURL = stored.EmbeddingBaseURL
		}
		apiKey = strings.TrimSpace(cmd.EmbeddingAPIKey)
		if apiKey == "" && stored.EmbeddingAPIKeyCiphertext != "" {
			apiKey, err = s.cipher.decrypt(stored.EmbeddingAPIKeyCiphertext)
			if err != nil {
				return EmbeddingTestResult{}, err
			}
		}
	} else {
		baseURL = strings.TrimSpace(cmd.BaseURL)
		if baseURL == "" {
			baseURL = stored.BaseURL
		}
		apiKey = strings.TrimSpace(cmd.APIKey)
		if apiKey == "" && stored.APIKeyCiphertext != "" {
			apiKey, err = s.cipher.decrypt(stored.APIKeyCiphertext)
			if err != nil {
				return EmbeddingTestResult{}, err
			}
		}
	}
	if apiKey == "" {
		return EmbeddingTestResult{}, ErrInvalid
	}
	target, err := s.validateBaseURL(ctx, baseURL)
	if err != nil {
		return EmbeddingTestResult{}, err
	}
	target.Path = strings.TrimRight(target.Path, "/") + "/embeddings"

	payload, err := json.Marshal(map[string]any{"model": model, "input": "connection test"})
	if err != nil {
		return EmbeddingTestResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(payload))
	if err != nil {
		return EmbeddingTestResult{}, ErrConnectionFailed
	}
	req.Header.Set("Content-Type", "application/json")
	switch provider {
	case ProviderAzureOpenAI:
		req.Header.Set("Api-Key", apiKey)
	case ProviderAnthropic:
		req.Header.Set("X-Api-Key", apiKey)
	default:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	result := EmbeddingTestResult{Model: model, ExpectedDimensions: ExpectedEmbeddingDimensions}
	start := time.Now()
	resp, err := s.httpClient(20 * time.Second).Do(req)
	if err != nil {
		if errors.Is(err, ErrUpstreamNotAllowed) {
			return result, ErrUpstreamNotAllowed
		}
		if errors.Is(err, ErrUpstreamRestricted) {
			return result, ErrUpstreamRestricted
		}
		result.Status = EmbeddingTestStatusFailed
		return result, nil
	}
	defer resp.Body.Close()
	result.LatencyMs = time.Since(start).Milliseconds()
	result.HTTPStatus = resp.StatusCode

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		result.Status = EmbeddingTestStatusUnauthorized
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed:
		// 404 既可能是端点不存在，也可能是模型名不存在（OpenAI 对未知模型同样回 404）。
		// 错误体里出现 "model" 字样时按模型不存在处理，避免把拼错模型名的用户
		// 误导去「切换单独配置」。
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		if bytes.Contains(bytes.ToLower(body), []byte("model")) {
			result.Status = EmbeddingTestStatusModelNotFound
		} else {
			result.Status = EmbeddingTestStatusUnsupported
		}
	case resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices:
		var parsed struct {
			Data []struct {
				Embedding []float64 `json:"embedding"`
			} `json:"data"`
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if readErr != nil || json.Unmarshal(body, &parsed) != nil ||
			len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
			result.Status = EmbeddingTestStatusFailed
		} else {
			result.Status = EmbeddingTestStatusOK
			result.Dimensions = len(parsed.Data[0].Embedding)
			result.DimensionsMatch = result.Dimensions == ExpectedEmbeddingDimensions
		}
	default:
		result.Status = EmbeddingTestStatusFailed
	}
	return result, nil
}

func (s *Service) AuthenticateToken(token string) (string, bool) {
	token = strings.TrimSpace(token)
	if constantTimeTokenEqual(token, s.notebookToken) {
		return ServiceNotebook, true
	}
	if constantTimeTokenEqual(token, s.unsAgentToken) {
		return ServiceUNSAgent, true
	}
	return "", false
}

// ChatConfigured 仅检查默认聊天配置是否齐全，不请求供应商，也不依赖向量模型。
func (s *Service) ChatConfigured(ctx context.Context, workspaceID int64) (bool, error) {
	item, err := s.resolveItem(ctx, 0, workspaceID)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrDisabled) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	model := strings.TrimSpace(item.Model)
	return strings.TrimSpace(item.BaseURL) != "" && strings.TrimSpace(item.APIKeyCiphertext) != "" &&
		model != "" && !strings.EqualFold(model, "gateway-managed"), nil
}

func (s *Service) Resolve(ctx context.Context, configID, workspaceID int64) (ResolvedConfig, error) {
	item, err := s.resolveItem(ctx, configID, workspaceID)
	if err != nil {
		return ResolvedConfig{}, err
	}
	baseURL, err := s.validateBaseURL(ctx, item.BaseURL)
	if err != nil {
		return ResolvedConfig{}, err
	}
	apiKey, err := s.cipher.decrypt(item.APIKeyCiphertext)
	if err != nil {
		return ResolvedConfig{}, err
	}
	return ResolvedConfig{
		ID: item.ID, WorkspaceID: item.WorkspaceID, Provider: item.Provider,
		BaseURL: baseURL, APIKey: apiKey, Model: item.Model,
	}, nil
}

// ResolveEmbedding 解析向量（embedding）请求应使用的上游连接：
// inherit 模式复用 chat 的 Base URL 与 API Key，custom 模式使用独立配置；
// 两种模式下 Model 均为 embeddingModel；为空时仅保留客户端真实模型的透传，
// 内部占位名由网关在转发前拦截并返回缺少向量模型配置的错误。
func (s *Service) ResolveEmbedding(ctx context.Context, configID, workspaceID int64) (ResolvedConfig, error) {
	item, err := s.resolveItem(ctx, configID, workspaceID)
	if err != nil {
		return ResolvedConfig{}, err
	}
	resolved := ResolvedConfig{
		ID: item.ID, WorkspaceID: item.WorkspaceID, Provider: item.Provider,
		Model: item.EmbeddingModel,
	}
	if strings.EqualFold(item.EmbeddingMode, EmbeddingModeCustom) {
		resolved.BaseURL, err = s.validateBaseURL(ctx, item.EmbeddingBaseURL)
		if err != nil {
			return ResolvedConfig{}, err
		}
		resolved.APIKey, err = s.cipher.decrypt(item.EmbeddingAPIKeyCiphertext)
		if err != nil {
			return ResolvedConfig{}, err
		}
		return resolved, nil
	}
	resolved.BaseURL, err = s.validateBaseURL(ctx, item.BaseURL)
	if err != nil {
		return ResolvedConfig{}, err
	}
	resolved.APIKey, err = s.cipher.decrypt(item.APIKeyCiphertext)
	if err != nil {
		return ResolvedConfig{}, err
	}
	return resolved, nil
}

func (s *Service) resolveItem(ctx context.Context, configID, workspaceID int64) (repo.AIProviderConfig, error) {
	var item repo.AIProviderConfig
	var err error
	if configID > 0 {
		item, err = s.repo.Get(ctx, configID)
	} else {
		item, err = s.repo.GetDefault(ctx, normalizeWorkspaceID(workspaceID))
	}
	if err != nil {
		return repo.AIProviderConfig{}, normalizeNotFound(err)
	}
	if !item.Enabled {
		return repo.AIProviderConfig{}, ErrDisabled
	}
	return item, nil
}

func (s *Service) normalizeSave(ctx context.Context, cmd SaveCommand, creating bool) (repo.AIProviderConfig, string, string, bool, error) {
	name := strings.TrimSpace(cmd.Name)
	if name == "" {
		name = "Enterprise AI"
	}
	provider := strings.ToLower(strings.TrimSpace(cmd.Provider))
	if provider == "" {
		provider = ProviderOpenAICompatible
	}
	if name == "" || len(name) > 100 || len(strings.TrimSpace(cmd.Model)) > 128 || !supportedProvider(provider) {
		return repo.AIProviderConfig{}, "", "", false, ErrInvalid
	}
	baseURL, err := s.validateBaseURL(ctx, cmd.BaseURL)
	if err != nil {
		return repo.AIProviderConfig{}, "", "", false, err
	}
	apiKey := strings.TrimSpace(cmd.APIKey)
	if creating && apiKey == "" {
		return repo.AIProviderConfig{}, "", "", false, ErrInvalid
	}
	embeddingMode := strings.ToLower(strings.TrimSpace(cmd.EmbeddingMode))
	embeddingModel := strings.TrimSpace(cmd.EmbeddingModel)
	embeddingBaseURL := strings.TrimSpace(cmd.EmbeddingBaseURL)
	embeddingAPIKey := strings.TrimSpace(cmd.EmbeddingAPIKey)
	// 请求带任一 embedding 字段即视为新客户端，走完整校验；完全不带则保持
	// 旧客户端兼容——create 落默认值，update 不触碰已存的 embedding 列。
	// 模型名允许为空（产品拍板不做必填）：客户端真实模型仍可透传，但网关
	// 不向上游发送未解析的内部占位名；前端提示对 UNS Agent 向量能力的影响。
	embeddingSet := embeddingMode != "" || embeddingModel != "" || embeddingBaseURL != "" || embeddingAPIKey != ""
	if embeddingSet {
		if embeddingMode == "" {
			embeddingMode = EmbeddingModeInherit
		}
		if embeddingMode != EmbeddingModeInherit && embeddingMode != EmbeddingModeCustom {
			return repo.AIProviderConfig{}, "", "", false, ErrInvalid
		}
		if len(embeddingModel) > 128 {
			return repo.AIProviderConfig{}, "", "", false, ErrInvalid
		}
		// 单独配置之外也保留已填的 embedding Base URL，避免切换模式时丢输入。
		if embeddingBaseURL != "" {
			parsed, err := s.validateBaseURL(ctx, embeddingBaseURL)
			if err != nil {
				return repo.AIProviderConfig{}, "", "", false, err
			}
			embeddingBaseURL = strings.TrimRight(parsed.String(), "/")
		} else if embeddingMode == EmbeddingModeCustom {
			return repo.AIProviderConfig{}, "", "", false, ErrInvalid
		}
		if creating && embeddingMode == EmbeddingModeCustom && embeddingAPIKey == "" {
			return repo.AIProviderConfig{}, "", "", false, ErrInvalid
		}
	} else {
		embeddingMode = EmbeddingModeInherit
	}
	enabled := true
	if cmd.Enabled != nil {
		enabled = *cmd.Enabled
	}
	isDefault := enabled
	return repo.AIProviderConfig{
		ID: cmd.ID, WorkspaceID: normalizeWorkspaceID(cmd.WorkspaceID), Name: name,
		Provider: provider, BaseURL: strings.TrimRight(baseURL.String(), "/"), Model: strings.TrimSpace(cmd.Model),
		EmbeddingMode: embeddingMode, EmbeddingBaseURL: embeddingBaseURL, EmbeddingModel: embeddingModel,
		IsDefault: isDefault, Enabled: enabled, CreatedBy: cmd.UserID, UpdatedBy: cmd.UserID,
	}, apiKey, embeddingAPIKey, embeddingSet, nil
}

func (s *Service) validateBaseURL(ctx context.Context, raw string) (*url.URL, error) {
	trustedHosts, _, err := s.loadTrustedHosts(ctx)
	if err != nil {
		return nil, err
	}
	return validateBaseURLWithHosts(raw, trustedHosts, s.operatorHosts, s.privateHosts)
}

func validateBaseURLWithHosts(raw string, trustedHosts, operatorHosts, privateHosts []string) (*url.URL, error) {
	target, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || target.Hostname() == "" || target.User != nil || target.RawQuery != "" || target.Fragment != "" {
		return nil, ErrInvalid
	}
	if target.Scheme != "https" && target.Scheme != "http" {
		return nil, ErrInvalid
	}
	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	if target.Scheme == "https" {
		if ip := net.ParseIP(host); ip != nil {
			if ip.IsPrivate() && outboundIPAllowed(ip, host, privateHosts) {
				return target, nil
			}
			if ip.IsPrivate() {
				return nil, ErrUpstreamRestricted
			}
			return nil, ErrInvalid
		}
		if !hostAllowed(host, trustedHosts) {
			return nil, ErrUpstreamNotAllowed
		}
		if len(operatorHosts) > 0 && !hostAllowed(host, operatorHosts) {
			return nil, ErrUpstreamRestricted
		}
		return target, nil
	}
	// Provider credentials normally require TLS. Plain HTTP is limited to an
	// explicitly allowlisted RFC1918/ULA IP literal for private model gateways;
	// hostnames and wildcard allowlist entries are intentionally insufficient.
	if ip := net.ParseIP(host); ip == nil || !ip.IsPrivate() {
		return nil, ErrInvalid
	}
	if !privateHTTPHostAllowed(host, privateHosts) {
		return nil, ErrUpstreamRestricted
	}
	return target, nil
}

// HTTPTransport returns the egress-controlled transport shared by connection
// tests and the streaming reverse proxy.
func (s *Service) HTTPTransport() http.RoundTripper {
	if s.transport != nil {
		return s.transport
	}
	return newAIGatewayTransport(s.privateHosts)
}

func (s *Service) httpClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: s.HTTPTransport(),
		// Provider credentials must never follow a redirect to another origin.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func newAIGatewayTransport(privateHosts []string) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// Proxy environment variables move DNS resolution outside this process and
	// defeat the address check below. AI provider egress therefore connects
	// directly and validates every resolved destination before dialing it.
	transport.Proxy = nil
	dialer := &aiGatewayDialer{
		privateHosts: append([]string(nil), privateHosts...),
		dialer:       net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second},
	}
	transport.DialContext = dialer.DialContext
	return transport
}

type aiGatewayDialer struct {
	privateHosts []string
	dialer       net.Dialer
}

func (d *aiGatewayDialer) DialContext(ctx context.Context, network, endpoint string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}

	privateAddressDenied := false
	allowedAddresses := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		if !outboundIPAllowed(net.IP(address.AsSlice()), host, d.privateHosts) {
			privateAddressDenied = privateAddressDenied || address.IsPrivate()
			continue
		}
		allowedAddresses = append(allowedAddresses, address)
	}
	if len(allowedAddresses) > 0 {
		return d.raceDialContext(ctx, network, port, interleaveIPFamilies(allowedAddresses))
	}
	if privateAddressDenied {
		return nil, ErrUpstreamRestricted
	}
	return nil, ErrInvalid
}

type aiGatewayDialResult struct {
	conn net.Conn
	err  error
}

func (d *aiGatewayDialer) raceDialContext(ctx context.Context, network, port string, addresses []netip.Addr) (net.Conn, error) {
	dialCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan aiGatewayDialResult, len(addresses))
	for index, address := range addresses {
		index, address := index, address
		go func() {
			if index > 0 {
				timer := time.NewTimer(time.Duration(index) * happyEyeballsDelay)
				defer timer.Stop()
				select {
				case <-dialCtx.Done():
					results <- aiGatewayDialResult{err: dialCtx.Err()}
					return
				case <-timer.C:
				}
			}
			conn, err := d.dialer.DialContext(dialCtx, network, net.JoinHostPort(address.String(), port))
			select {
			case results <- aiGatewayDialResult{conn: conn, err: err}:
			case <-dialCtx.Done():
				if conn != nil {
					_ = conn.Close()
				}
			}
		}()
	}

	var lastErr error
	for received := 0; received < len(addresses); received++ {
		result := <-results
		if result.err == nil && result.conn != nil {
			cancel()
			remaining := len(addresses) - received - 1
			go func() {
				for range remaining {
					lateResult := <-results
					if lateResult.conn != nil {
						_ = lateResult.conn.Close()
					}
				}
			}()
			return result.conn, nil
		}
		if result.err != nil && !errors.Is(result.err, context.Canceled) {
			lastErr = result.err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrInvalid
}

func interleaveIPFamilies(addresses []netip.Addr) []netip.Addr {
	if len(addresses) < 2 {
		return addresses
	}
	ipv4 := make([]netip.Addr, 0, len(addresses))
	ipv6 := make([]netip.Addr, 0, len(addresses))
	for _, address := range addresses {
		if address.Is4() || address.Is4In6() {
			ipv4 = append(ipv4, address)
		} else {
			ipv6 = append(ipv6, address)
		}
	}
	first, second := ipv6, ipv4
	if addresses[0].Is4() || addresses[0].Is4In6() {
		first, second = ipv4, ipv6
	}
	ordered := make([]netip.Addr, 0, len(addresses))
	for len(first) > 0 || len(second) > 0 {
		if len(first) > 0 {
			ordered = append(ordered, first[0])
			first = first[1:]
		}
		if len(second) > 0 {
			ordered = append(ordered, second[0])
			second = second[1:]
		}
	}
	return ordered
}

var nonPublicOutboundPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
	netip.MustParsePrefix("5f00::/16"),
	netip.MustParsePrefix("fec0::/10"),
}

func outboundIPAllowed(ip net.IP, host string, allowedHosts []string) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if address.IsPrivate() {
		return hostAllowed(host, allowedHosts)
	}
	if !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range nonPublicOutboundPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func configView(item repo.AIProviderConfig) map[string]any {
	embeddingMode := item.EmbeddingMode
	if embeddingMode == "" {
		embeddingMode = EmbeddingModeInherit
	}
	return map[string]any{
		"id": item.ID, "workspaceId": item.WorkspaceID, "name": item.Name,
		"provider": item.Provider, "baseUrl": item.BaseURL, "model": item.Model,
		"keySuffix": item.KeySuffix, "apiKeySet": item.APIKeyCiphertext != "",
		"embeddingMode": embeddingMode, "embeddingBaseUrl": item.EmbeddingBaseURL,
		"embeddingModel": item.EmbeddingModel, "embeddingKeySuffix": item.EmbeddingKeySuffix,
		"embeddingApiKeySet": item.EmbeddingAPIKeyCiphertext != "",
		"isDefault":          item.IsDefault, "enabled": item.Enabled,
		"createdTime": item.CreatedTime, "updatedTime": item.UpdatedTime,
	}
}

func (s *Service) loadTrustedHosts(ctx context.Context) ([]string, bool, error) {
	if s.systemSettings == nil {
		return append([]string(nil), defaultTrustedHosts...), false, nil
	}
	value, err := s.systemSettings.GetSetting(ctx, trustedHostsSettingKey)
	if errors.Is(err, repo.ErrNotFound) {
		hosts, err := s.initialTrustedHosts(ctx)
		return hosts, false, err
	}
	if err != nil {
		return nil, false, err
	}
	var hosts []string
	if err := json.Unmarshal([]byte(value), &hosts); err != nil {
		return nil, true, err
	}
	normalized, err := normalizeTrustedHosts(hosts)
	if err != nil {
		return nil, true, err
	}
	return normalized, true, nil
}

func (s *Service) initialTrustedHosts(ctx context.Context) ([]string, error) {
	if s.repo == nil {
		return append([]string(nil), defaultTrustedHosts...), nil
	}
	configs, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	return initialTrustedHostsFromConfigs(configs), nil
}

func initialTrustedHostsFromConfigs(configs []repo.AIProviderConfig) []string {
	hosts := append([]string(nil), defaultTrustedHosts...)
	seen := make(map[string]struct{}, maxTrustedHosts)
	for _, host := range hosts {
		seen[host] = struct{}{}
	}
	for _, item := range configs {
		for _, rawURL := range []string{item.BaseURL, item.EmbeddingBaseURL} {
			target, parseErr := url.Parse(strings.TrimSpace(rawURL))
			if parseErr != nil {
				continue
			}
			host, ok := normalizeTrustedHost(target.Hostname())
			if !ok {
				continue
			}
			if _, exists := seen[host]; exists {
				continue
			}
			if len(hosts) >= maxTrustedHosts {
				return hosts
			}
			seen[host] = struct{}{}
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func normalizeTrustedHosts(hosts []string) ([]string, error) {
	if len(hosts) > maxTrustedHosts {
		return nil, ErrTrustedHostsInvalid
	}
	seen := make(map[string]struct{}, len(hosts))
	normalized := make([]string, 0, len(hosts))
	for _, raw := range hosts {
		host, ok := normalizeTrustedHost(raw)
		if !ok {
			return nil, ErrTrustedHostsInvalid
		}
		if _, exists := seen[host]; exists {
			continue
		}
		seen[host] = struct{}{}
		normalized = append(normalized, host)
	}
	return normalized, nil
}

func normalizeTrustedHost(raw string) (string, bool) {
	host := strings.ToLower(strings.TrimSpace(raw))
	host = strings.TrimSuffix(host, ".")
	if len(host) == 0 || len(host) > maxTrustedHostLength || net.ParseIP(host) != nil {
		return "", false
	}
	if strings.HasPrefix(host, "*.") {
		host = strings.TrimPrefix(host, "*.")
		if host == "" {
			return "", false
		}
	}
	wildcard := strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "*.")
	if !strings.Contains(host, ".") {
		return "", false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, char := range label {
			if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' {
				return "", false
			}
		}
	}
	if wildcard {
		return "*." + host, true
	}
	return host, true
}

func isLegacyDefaultHostSet(hosts []string) bool {
	if len(hosts) != len(defaultTrustedHosts) {
		return false
	}
	wanted := make(map[string]struct{}, len(defaultTrustedHosts))
	for _, host := range defaultTrustedHosts {
		wanted[host] = struct{}{}
	}
	for _, host := range hosts {
		if _, ok := wanted[host]; !ok {
			return false
		}
		delete(wanted, host)
	}
	return len(wanted) == 0
}

func splitAllowedHosts(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.ToLower(strings.TrimSpace(part)); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func hostAllowed(host string, patterns []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	for _, pattern := range patterns {
		if host == pattern {
			return true
		}
		if strings.HasPrefix(pattern, "*.") {
			suffix := strings.TrimPrefix(pattern, "*")
			if strings.HasSuffix(host, suffix) && host != strings.TrimPrefix(suffix, ".") {
				return true
			}
		}
	}
	return false
}

func privateHTTPHostAllowed(host string, patterns []string) bool {
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil || !ip.IsPrivate() {
		return false
	}
	for _, pattern := range patterns {
		allowedIP := net.ParseIP(strings.Trim(strings.TrimSpace(pattern), "[]"))
		if allowedIP != nil && ip.Equal(allowedIP) {
			return true
		}
	}
	return false
}

func supportedProvider(provider string) bool {
	switch provider {
	case ProviderOpenAICompatible, ProviderAzureOpenAI, ProviderAnthropic:
		return true
	default:
		return false
	}
}

func secretSuffix(secret string) string {
	if len(secret) <= 4 {
		return secret
	}
	return secret[len(secret)-4:]
}

func normalizeWorkspaceID(workspaceID int64) int64 {
	if workspaceID <= 0 {
		return 1
	}
	return workspaceID
}

func normalizeNotFound(err error) error {
	if errors.Is(err, repo.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

func constantTimeTokenEqual(got, expected string) bool {
	return expected != "" && len(got) == len(expected) && subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

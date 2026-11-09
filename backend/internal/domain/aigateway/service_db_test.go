package aigateway

// 测试目标：AI 网关服务（service.go）的 DB 集成路径。
// 覆盖的正常路径：Create/Update/Delete/List/Resolve/ResolveEmbedding/TestConnection 校验门
// 与 DB 落库；AuthenticateToken 令牌匹配；secretSuffix/normalizeWorkspaceID/
// splitAllowedHosts/hostAllowed/privateHTTPHostAllowed/outboundIPAllowed/supportedProvider 纯函数。
// 覆盖的异常情况：重复配置（ErrAlreadyConfigured）、非法 ID/名称、上游未授权、
// 配置不存在（ErrNotFound）、禁用配置（ErrDisabled）。
// 说明：svctest 依赖 domain 包（import cycle），此处用 testkit 装配；
// TestConnection/TestEmbedding 的真实 HTTP 调用不覆盖（需外部服务），仅覆盖参数校验门。

import (
	"context"
	"errors"
	"testing"

	"backend/internal/config"
	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func newTestAIGateway(t *testing.T) (*Service, context.Context) {
	t.Helper()
	testkit.NewTestDB(t)
	ctx := contextx.WithSubject(context.Background(), contextx.Subject{UserID: 1, UserName: "tier0"})
	svc, err := New(ctx, config.AIGatewayConf{
		CredentialKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", // 32 字节 base64
		AllowedHosts:  "api.openai.com,*.example.com",
	})
	require.NoError(t, err)
	return svc, ctx
}

// 聊天配置状态只检查默认聊天配置，覆盖缺配置、缺向量、禁用、不完整、跨 workspace 与检查失败。
func TestChatConfigured(t *testing.T) {
	svc, ctx := newTestAIGateway(t)
	configured, err := svc.ChatConfigured(ctx, 0)
	require.NoError(t, err)
	require.False(t, configured)

	item, err := svc.repo.Create(ctx, repo.AIProviderConfig{
		WorkspaceID: 1, Name: "Chat only", Provider: ProviderOpenAICompatible,
		BaseURL: "https://api.openai.com/v1", Model: "gpt-test", APIKeyCiphertext: "test-ciphertext",
		IsDefault: true, Enabled: true,
	})
	require.NoError(t, err)
	configured, err = svc.ChatConfigured(ctx, 0)
	require.NoError(t, err)
	require.True(t, configured, "聊天配置完整时不要求向量模型")
	configured, err = svc.ChatConfigured(ctx, 2)
	require.NoError(t, err)
	require.False(t, configured, "不借用其他 workspace 的配置")

	for _, tc := range []struct {
		field   string
		value   any
		restore any
	}{
		{"enabled", false, true},
		{"is_default", false, true},
		{"model", "", "gpt-test"},
		{"model", " gateway-managed ", "gpt-test"},
		{"api_key_ciphertext", " ", "test-ciphertext"},
		{"base_url", "", "https://api.openai.com/v1"},
	} {
		_, err = svc.repo.Update(ctx, item.ID, 1, map[string]any{tc.field: tc.value}, false)
		require.NoError(t, err)
		configured, err = svc.ChatConfigured(ctx, 0)
		require.NoError(t, err)
		require.False(t, configured, tc.field)
		_, err = svc.repo.Update(ctx, item.ID, 1, map[string]any{tc.field: tc.restore}, false)
		require.NoError(t, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = svc.ChatConfigured(cancelled, 0)
	require.Error(t, err, "检查失败必须返回错误，不能被误判为缺配置")
	require.NoError(t, svc.repo.Delete(ctx, item.ID, 1, 1))
	configured, err = svc.ChatConfigured(ctx, 0)
	require.NoError(t, err)
	require.False(t, configured)
}

func TestAIGatewayCRUD(t *testing.T) {
	svc, ctx := newTestAIGateway(t)

	// 创建
	cmd := SaveCommand{
		WorkspaceID: 1,
		Name:        "My AI",
		Provider:    ProviderOpenAICompatible,
		BaseURL:     "https://api.openai.com/v1",
		Model:       "gpt-4o",
		APIKey:      "sk-test-1234",
		UserID:      1,
	}
	view, err := svc.Create(ctx, cmd)
	require.NoError(t, err)
	id, ok := view["id"].(int64)
	require.True(t, ok && id > 0)
	require.Equal(t, "1234", view["keySuffix"])
	require.Equal(t, true, view["apiKeySet"])
	require.Equal(t, EmbeddingModeInherit, view["embeddingMode"])

	// 重复创建（已存在配置）
	_, err = svc.Create(ctx, cmd)
	require.ErrorIs(t, err, ErrAlreadyConfigured)

	// 非法 provider（需新实例，避免命中"已存在配置"的提前返回）
	badSvc, _ := newTestAIGateway(t)
	bad := cmd
	bad.Provider = "gemini"
	_, err = badSvc.Create(ctx, bad)
	require.ErrorIs(t, err, ErrInvalid)

	// 非法 URL
	bad = cmd
	bad.BaseURL = "ftp://x"
	_, err = badSvc.Create(ctx, bad)
	require.ErrorIs(t, err, ErrInvalid)

	// 私网 host 未经运维显式放行
	bad = cmd
	bad.BaseURL = "https://10.20.30.40/v1"
	_, err = badSvc.Create(ctx, bad)
	require.ErrorIs(t, err, ErrUpstreamRestricted)

	// 创建时缺 APIKey
	bad = cmd
	bad.APIKey = ""
	_, err = badSvc.Create(ctx, bad)
	require.ErrorIs(t, err, ErrInvalid)

	// 列表
	list, err := svc.List(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 1, list["total"])

	// 更新
	update := SaveCommand{
		ID:          id,
		WorkspaceID: 1,
		Name:        "My AI 2",
		Provider:    ProviderOpenAICompatible,
		BaseURL:     "https://api.openai.com/v1",
		Model:       "gpt-4o-mini",
		APIKey:      "sk-test-9999",
		UserID:      1,
	}
	updated, err := svc.Update(ctx, update)
	require.NoError(t, err)
	require.Equal(t, "My AI 2", updated["name"])
	require.Equal(t, "9999", updated["keySuffix"])

	// 更新不存在的配置
	update.ID = 999999
	_, err = svc.Update(ctx, update)
	require.ErrorIs(t, err, ErrNotFound)

	// 非法 ID
	update.ID = 0
	_, err = svc.Update(ctx, update)
	require.ErrorIs(t, err, ErrInvalid)

	// 删除
	require.NoError(t, svc.Delete(ctx, id, 1, 1))
	require.ErrorIs(t, svc.Delete(ctx, id, 1, 1), ErrNotFound)
	require.ErrorIs(t, svc.Delete(ctx, 0, 1, 1), ErrInvalid)
}

func TestAIGatewayTrustedHosts(t *testing.T) {
	svc, ctx := newTestAIGateway(t)

	view, err := svc.TrustedHosts(ctx)
	require.NoError(t, err)
	require.Equal(t, false, view["customized"])
	require.Equal(t, defaultTrustedHosts, view["hosts"])

	_, err = svc.repo.Create(ctx, repo.AIProviderConfig{
		WorkspaceID:      1,
		Name:             "Existing AI",
		Provider:         ProviderOpenAICompatible,
		BaseURL:          "https://legacy.example.com/v1",
		EmbeddingMode:    EmbeddingModeCustom,
		EmbeddingBaseURL: "https://embeddings.example.net/v1",
	})
	require.NoError(t, err)
	view, err = svc.TrustedHosts(ctx)
	require.NoError(t, err)
	require.Equal(t, false, view["customized"])
	require.Equal(t, append(append([]string(nil), defaultTrustedHosts...), "legacy.example.com", "embeddings.example.net"), view["hosts"])

	view, err = svc.SaveTrustedHosts(ctx, []string{" API.EXAMPLE.COM. ", "*.models.example.com", "api.example.com"})
	require.NoError(t, err)
	require.Equal(t, true, view["customized"])
	require.Equal(t, []string{"api.example.com", "*.models.example.com"}, view["hosts"])

	view, err = svc.TrustedHosts(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"api.example.com", "*.models.example.com"}, view["hosts"])

	_, err = svc.SaveTrustedHosts(ctx, []string{"https://api.example.com"})
	require.ErrorIs(t, err, ErrTrustedHostsInvalid)
}

func TestAIGatewayResolve(t *testing.T) {
	svc, ctx := newTestAIGateway(t)
	cmd := SaveCommand{
		WorkspaceID: 1,
		Name:        "AI",
		Provider:    ProviderOpenAICompatible,
		BaseURL:     "https://api.openai.com/v1",
		Model:       "gpt-4o",
		APIKey:      "sk-secret-key",
		UserID:      1,
	}
	view, err := svc.Create(ctx, cmd)
	require.NoError(t, err)
	id := view["id"].(int64)

	// 按 ID 解析
	resolved, err := svc.Resolve(ctx, id, 1)
	require.NoError(t, err)
	require.Equal(t, "sk-secret-key", resolved.APIKey)
	require.Equal(t, "gpt-4o", resolved.Model)
	require.Equal(t, "api.openai.com", resolved.BaseURL.Hostname())

	// 按默认解析（workspace 维度）
	resolved, err = svc.Resolve(ctx, 0, 1)
	require.NoError(t, err)
	require.Equal(t, id, resolved.ID)

	// 不存在
	_, err = svc.Resolve(ctx, 999999, 1)
	require.ErrorIs(t, err, ErrNotFound)

	// 禁用后解析 → ErrDisabled
	disabled := cmd
	disabled.ID = id
	disabled.Enabled = boolPtr(false)
	disabled.APIKey = ""
	_, err = svc.Update(ctx, disabled)
	require.NoError(t, err)
	_, err = svc.Resolve(ctx, id, 1)
	require.ErrorIs(t, err, ErrDisabled)
}

func TestAIGatewayResolveEmbedding(t *testing.T) {
	svc, ctx := newTestAIGateway(t)
	cmd := SaveCommand{
		WorkspaceID:      1,
		Name:             "AI",
		Provider:         ProviderOpenAICompatible,
		BaseURL:          "https://api.openai.com/v1",
		Model:            "gpt-4o",
		APIKey:           "sk-chat-key",
		EmbeddingMode:    EmbeddingModeCustom,
		EmbeddingBaseURL: "https://api.openai.com/v1/embeddings",
		EmbeddingModel:   "text-embedding-3-small",
		EmbeddingAPIKey:  "sk-embed-key",
		UserID:           1,
	}
	view, err := svc.Create(ctx, cmd)
	require.NoError(t, err)
	id := view["id"].(int64)

	// custom 模式使用独立配置
	resolved, err := svc.ResolveEmbedding(ctx, id, 1)
	require.NoError(t, err)
	require.Equal(t, "sk-embed-key", resolved.APIKey)
	require.Equal(t, "api.openai.com", resolved.BaseURL.Hostname())
	require.Equal(t, "text-embedding-3-small", resolved.Model)

	// 切换到 inherit
	update := cmd
	update.ID = id
	update.EmbeddingMode = EmbeddingModeInherit
	update.APIKey = ""
	update.EmbeddingAPIKey = ""
	_, err = svc.Update(ctx, update)
	require.NoError(t, err)
	resolved, err = svc.ResolveEmbedding(ctx, id, 1)
	require.NoError(t, err)
	require.Equal(t, "sk-chat-key", resolved.APIKey)
	require.Equal(t, "api.openai.com", resolved.BaseURL.Hostname())
}

func TestAIGatewayAuthenticateToken(t *testing.T) {
	svc := &Service{
		notebookToken: "nb-token",
		unsAgentToken: "ua-token",
	}
	service, ok := svc.AuthenticateToken("nb-token")
	require.True(t, ok)
	require.Equal(t, ServiceNotebook, service)
	service, ok = svc.AuthenticateToken("ua-token")
	require.True(t, ok)
	require.Equal(t, ServiceUNSAgent, service)
	_, ok = svc.AuthenticateToken("wrong")
	require.False(t, ok)
	_, ok = svc.AuthenticateToken("")
	require.False(t, ok)
	// token 先 TrimSpace 再比较，带空白仍匹配
	service, ok = svc.AuthenticateToken("  nb-token  ")
	require.True(t, ok)
	require.Equal(t, ServiceNotebook, service)
}

func TestAIGatewayHelpers(t *testing.T) {
	if secretSuffix("abcd") != "abcd" || secretSuffix("abcdefgh") != "efgh" {
		t.Fatal("secretSuffix mismatch")
	}
	if normalizeWorkspaceID(0) != 1 || normalizeWorkspaceID(-1) != 1 || normalizeWorkspaceID(7) != 7 {
		t.Fatal("normalizeWorkspaceID mismatch")
	}
	hosts := splitAllowedHosts(" a.com , b.com ,, ")
	require.Equal(t, []string{"a.com", "b.com"}, hosts)
	require.Empty(t, splitAllowedHosts(""))
	require.Empty(t, splitAllowedHosts(" , , "))

	if !hostAllowed("api.openai.com", []string{"api.openai.com"}) {
		t.Fatal("exact host match")
	}
	if !hostAllowed("a.example.com", []string{"*.example.com"}) {
		t.Fatal("wildcard host match")
	}
	if hostAllowed("example.com", []string{"*.example.com"}) {
		t.Fatal("bare domain should not match wildcard")
	}
	if hostAllowed("evil.com", []string{"*.example.com"}) {
		t.Fatal("non-matching host")
	}
	if !hostAllowed("API.OPENAI.COM", []string{"api.openai.com"}) {
		t.Fatal("case-insensitive host match")
	}

	if !privateHTTPHostAllowed("10.1.2.3", []string{"10.1.2.3"}) {
		t.Fatal("private ip allowlisted")
	}
	if privateHTTPHostAllowed("8.8.8.8", []string{"8.8.8.8"}) {
		t.Fatal("public ip should not be allowed for http")
	}
	if privateHTTPHostAllowed("10.1.2.3", []string{"10.1.2.4"}) {
		t.Fatal("non-allowlisted private ip")
	}
	if privateHTTPHostAllowed("example.com", []string{"example.com"}) {
		t.Fatal("hostname should not be allowed for http")
	}

	if !supportedProvider(ProviderOpenAICompatible) || !supportedProvider(ProviderAzureOpenAI) ||
		!supportedProvider(ProviderAnthropic) || supportedProvider("other") {
		t.Fatal("supportedProvider mismatch")
	}
}

func TestAIGatewayTestConnectionValidation(t *testing.T) {
	svc, ctx := newTestAIGateway(t)
	cmd := SaveCommand{
		WorkspaceID: 1,
		Provider:    "unsupported",
		BaseURL:     "https://api.openai.com/v1",
		APIKey:      "k",
	}
	require.ErrorIs(t, svc.TestConnection(ctx, cmd), ErrInvalid)
	cmd.Provider = ""
	cmd.APIKey = ""
	require.ErrorIs(t, svc.TestConnection(ctx, cmd), ErrInvalid)
	cmd.APIKey = "k"
	cmd.BaseURL = ""
	require.ErrorIs(t, svc.TestConnection(ctx, cmd), ErrInvalid)
	cmd.BaseURL = "https://api.openai.com/v1"
	cmd.Provider = ProviderOpenAICompatible
	// 校验通过后进入真实 HTTP（不可达 → ErrConnectionFailed，不触达外部）
	require.ErrorIs(t, svc.TestConnection(ctx, cmd), ErrConnectionFailed)
}

func TestAIGatewayNormalizeNotFound(t *testing.T) {
	require.ErrorIs(t, normalizeNotFound(repo.ErrNotFound), ErrNotFound)
	plain := errors.New("x")
	require.ErrorIs(t, normalizeNotFound(plain), plain)
}

func boolPtr(value bool) *bool {
	return &value
}

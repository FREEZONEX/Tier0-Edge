package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"backend/internal/domain/aigateway"
)

func TestWriteAIResolveErrorDistinguishesHostPolicyFailures(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantMessage string
	}{
		{
			name:        "trusted host missing",
			err:         aigateway.ErrUpstreamNotAllowed,
			wantMessage: "AI provider is not trusted",
		},
		{
			name:        "system policy restriction",
			err:         aigateway.ErrUpstreamRestricted,
			wantMessage: "AI provider access is restricted by system policy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeAIResolveError(recorder, tt.err)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
			}
			var body map[string]any
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["msg"] != tt.wantMessage {
				t.Fatalf("message = %q, want %q", body["msg"], tt.wantMessage)
			}
		})
	}
}

func TestJoinAIPath(t *testing.T) {
	tests := []struct {
		name   string
		base   string
		suffix string
		want   string
	}{
		{name: "empty base", suffix: "/chat/completions", want: "/chat/completions"},
		{name: "regular provider path", base: "/v1", suffix: "/chat/completions", want: "/v1/chat/completions"},
		{name: "duplicate version", base: "/v1", suffix: "/v1/responses", want: "/v1/responses"},
		{name: "duplicate nested version", base: "/openai/v1/", suffix: "v1/models", want: "/openai/v1/models"},
		{name: "exact duplicate version", base: "/v1", suffix: "/v1", want: "/v1"},
		{name: "keep non-version segments", base: "/custom", suffix: "/custom/models", want: "/custom/custom/models"},
		{name: "keep different versions", base: "/v1", suffix: "/v2/models", want: "/v1/v2/models"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := joinAIPath(tt.base, tt.suffix); got != tt.want {
				t.Fatalf("joinAIPath(%q, %q) = %q, want %q", tt.base, tt.suffix, got, tt.want)
			}
		})
	}
}

func TestOverrideAIRequestModel(t *testing.T) {
	req, err := http.NewRequest(
		http.MethodPost,
		"http://backend/api/internal/ai/workspaces/1/chat/completions",
		strings.NewReader(`{"model":"openai/tier0-enterprise","messages":[{"role":"user","content":"hello"}]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	if err := overrideAIRequestModel(req, "/chat/completions", " gpt-5.6 "); err != nil {
		t.Fatalf("overrideAIRequestModel() error = %v", err)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	var model string
	if err := json.Unmarshal(payload["model"], &model); err != nil {
		t.Fatal(err)
	}
	if model != "gpt-5.6" {
		t.Fatalf("model = %q, want %q", model, "gpt-5.6")
	}
	if len(payload["messages"]) == 0 {
		t.Fatal("messages were removed while rewriting model")
	}
	if req.ContentLength != int64(len(body)) {
		t.Fatalf("ContentLength = %d, want %d", req.ContentLength, len(body))
	}
	retryBody, err := req.GetBody()
	if err != nil {
		t.Fatal(err)
	}
	defer retryBody.Close()
	retried, err := io.ReadAll(retryBody)
	if err != nil {
		t.Fatal(err)
	}
	if string(retried) != string(body) {
		t.Fatalf("retry body = %q, want rewritten body %q", retried, body)
	}
}

func TestOverrideAIRequestModelRejectsInvalidJSON(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "http://backend/", strings.NewReader(`{"model":`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := overrideAIRequestModel(req, "/chat/completions", "gpt-5.6"); err == nil {
		t.Fatal("overrideAIRequestModel() error = nil, want invalid JSON error")
	}
}

func TestOverrideAIRequestModelKeepsBodyWithoutConfiguredModel(t *testing.T) {
	const original = `{"model":"client-model"}`
	req, err := http.NewRequest(http.MethodPost, "http://backend/", strings.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	if err := overrideAIRequestModel(req, "/chat/completions", ""); err != nil {
		t.Fatalf("overrideAIRequestModel() error = %v", err)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Fatalf("body = %q, want %q", body, original)
	}
}

func TestOverrideAIRequestModelReplacesEmbeddingModel(t *testing.T) {
	// /embeddings 已纳入模型替换：网关以 AI 设置里的 embeddingModel 为准。
	const original = `{"model":"client-alias","input":"hello"}`
	req, err := http.NewRequest(http.MethodPost, "http://backend/", strings.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := overrideAIRequestModel(req, "/embeddings", "text-embedding-3-small"); err != nil {
		t.Fatalf("overrideAIRequestModel() error = %v", err)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "text-embedding-3-small" || payload["input"] != "hello" {
		t.Fatalf("body = %q, want model replaced with text-embedding-3-small", body)
	}
}

func TestOverrideAIRequestModelKeepsEmbeddingModelWhenUnconfigured(t *testing.T) {
	// embeddingModel 未配置时，客户端显式指定的真实模型仍可透传。
	const original = `{"model":"text-embedding-3-small","input":"hello"}`
	req, err := http.NewRequest(http.MethodPost, "http://backend/", strings.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := overrideAIRequestModel(req, "/embeddings", ""); err != nil {
		t.Fatalf("overrideAIRequestModel() error = %v", err)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != original {
		t.Fatalf("body = %q, want %q", body, original)
	}
}

// 覆盖聊天、Responses、Messages 与向量请求的占位名拦截和实际模型替换。
// 使用真实 HTTP 上游计数，确保缺配时请求在网关终止，而不是转发后再改写错误。
func TestAIProxyDoesNotForwardUnresolvedModel(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		configured  string
		requested   string
		contentType string
		wantStatus  int
		wantModel   string
		wantMessage string
	}{
		{name: "chat missing", path: "/v1/chat/completions", requested: "gateway-managed", wantStatus: 503, wantMessage: "chat model is not configured"},
		{name: "responses missing", path: "/v1/responses", requested: "gateway-managed", wantStatus: 503, wantMessage: "chat model is not configured"},
		{name: "messages missing", path: "/v1/messages", requested: "gateway-managed", wantStatus: 503, wantMessage: "chat model is not configured"},
		{name: "embedding missing", path: "/v1/embeddings", requested: "gateway-managed", wantStatus: 503, wantMessage: "embedding model is not configured"},
		{name: "embedding whitespace", path: "/embeddings", configured: "  ", requested: " GATEWAY-MANAGED ", wantStatus: 503, wantMessage: "embedding model is not configured"},
		{name: "configured placeholder", path: "/chat/completions", configured: "gateway-managed", requested: "gpt-5.6-sol", wantStatus: 503, wantMessage: "chat model is not configured"},
		{name: "configured embedding placeholder", path: "/embeddings", configured: " GATEWAY-MANAGED ", requested: "text-embedding-3-small", wantStatus: 503, wantMessage: "embedding model is not configured"},
		{name: "chat resolved", path: "/v1/chat/completions", configured: "gpt-5.6-sol", requested: "gateway-managed", wantStatus: 200, wantModel: "gpt-5.6-sol"},
		{name: "responses resolved", path: "/v1/responses", configured: "gpt-5.6-sol", requested: "gateway-managed", wantStatus: 200, wantModel: "gpt-5.6-sol"},
		{name: "messages resolved", path: "/v1/messages", configured: "configured-chat-model", requested: "gateway-managed", wantStatus: 200, wantModel: "configured-chat-model"},
		{name: "embedding resolved", path: "/v1/embeddings", configured: "text-embedding-3-small", requested: "gateway-managed", wantStatus: 200, wantModel: "text-embedding-3-small"},
		{name: "explicit model retained", path: "/v1/embeddings", requested: "text-embedding-3-small", wantStatus: 200, wantModel: "text-embedding-3-small"},
		{name: "content type cannot bypass guard", path: "/v1/embeddings", requested: "gateway-managed", contentType: "text/plain", wantStatus: 400, wantMessage: "invalid AI request body"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var upstreamCalls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, "{}")
			}))
			defer upstream.Close()
			target, err := url.Parse(upstream.URL)
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(map[string]any{"model": tt.requested, "input": "test"})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "http://backend/api/internal/ai/workspaces/1"+tt.path, strings.NewReader(string(body)))
			contentType := tt.contentType
			if contentType == "" {
				contentType = "application/json; charset=utf-8"
			}
			req.Header.Set("Content-Type", contentType)
			if tt.wantModel != "" {
				// 成功请求只验证模型改写；网关的真实转发另有 SSRF 限制，不应为测试放开本地上游。
				if err := overrideAIRequestModel(req, tt.path, tt.configured); err != nil {
					t.Fatal(err)
				}
				var payload struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if payload.Model != tt.wantModel {
					t.Fatalf("rewritten model = %q, want %q", payload.Model, tt.wantModel)
				}
				return
			}
			response := httptest.NewRecorder()
			NewAIProxy(&aigateway.Service{}).proxy(response, req, aigateway.ResolvedConfig{
				ID: 1, BaseURL: target, Model: tt.configured,
				Provider: aigateway.ProviderOpenAICompatible, APIKey: "test-provider-key",
			}, tt.path)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, tt.wantStatus, response.Body.String())
			}
			if calls := upstreamCalls.Load(); calls != 0 {
				t.Fatalf("upstream received %d requests for an unresolved internal model", calls)
			}
			if !strings.Contains(response.Body.String(), tt.wantMessage) {
				t.Fatalf("response = %s, want %q", response.Body.String(), tt.wantMessage)
			}
		})
	}
}

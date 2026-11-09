package auth

// 测试目标:登录有效期设置的纯函数映射(session_duration.go)。
// 覆盖的正常路径:全部合法枚举值(12h/1d/7d/30d/permanent)到 TTL 的映射。
// 覆盖的异常情况:空字符串、未知值回退默认 12 小时;IsLoginSessionDuration 对非法值的判定。

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestTTLForLoginSessionDuration(t *testing.T) {
	cases := []struct {
		value string
		want  time.Duration
	}{
		{"12h", 12 * time.Hour},
		{"1d", 24 * time.Hour},
		{"7d", 7 * 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"permanent", 100 * 365 * 24 * time.Hour},
		// 未知/空值回退默认 12h
		{"", 12 * time.Hour},
		{"999h", 12 * time.Hour},
		{"1w", 12 * time.Hour},
	}
	for _, tc := range cases {
		if got := TTLForLoginSessionDuration(tc.value); got != tc.want {
			t.Fatalf("TTLForLoginSessionDuration(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestIsLoginSessionDuration(t *testing.T) {
	for _, valid := range []string{"12h", "1d", "7d", "30d", "permanent"} {
		if !IsLoginSessionDuration(valid) {
			t.Fatalf("IsLoginSessionDuration(%q) = false, want true", valid)
		}
	}
	for _, invalid := range []string{"", "999h", "1w", "PERMANENT"} {
		if IsLoginSessionDuration(invalid) {
			t.Fatalf("IsLoginSessionDuration(%q) = true, want false", invalid)
		}
	}
}

func TestMicrosoftUsernameValidationAndSuggestion(t *testing.T) {
	for _, valid := range []string{"alice", "Alice.Smith", "张三_01", "user@example"} {
		if err := validateMicrosoftUsername(valid); err != nil {
			t.Fatalf("validateMicrosoftUsername(%q) = %v, want nil", valid, err)
		}
	}
	for _, invalid := range []string{"ab", "has space", "user/one", ""} {
		if err := validateMicrosoftUsername(invalid); err == nil {
			t.Fatalf("validateMicrosoftUsername(%q) = nil, want error", invalid)
		}
	}
	if got := suggestMicrosoftUsername("alice.smith@example.com", "Alice Smith"); got != "alice.smith" {
		t.Fatalf("suggestMicrosoftUsername() = %q, want alice.smith", got)
	}
}

func TestNormalizeMicrosoftRedirect(t *testing.T) {
	for input, want := range map[string]string{
		"/project/42?tab=apps":  "/project/42?tab=apps",
		"https://evil.example":  "",
		"//evil.example/path":   "",
		"/\\evil.example/path":  "",
		"/%5cevil.example/path": "",
		"/tier0-login":          "",
		"":                      "",
	} {
		if got := normalizeMicrosoftRedirect(input); got != want {
			t.Fatalf("normalizeMicrosoftRedirect(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMicrosoftPendingBinding(t *testing.T) {
	binding := "browser-secret"
	if !validMicrosoftBinding(hashMicrosoftBinding(binding), binding) {
		t.Fatal("valid browser binding was rejected")
	}
	if validMicrosoftBinding(hashMicrosoftBinding(binding), "other-browser") {
		t.Fatal("mismatched browser binding was accepted")
	}
}

func TestMicrosoftCommonAuthorityAndTenantHint(t *testing.T) {
	if got := normalizeMicrosoftAuthority(" COMMON "); got != microsoftCommonAuthority {
		t.Fatalf("normalizeMicrosoftAuthority() = %q, want %q", got, microsoftCommonAuthority)
	}
	if got := normalizeMicrosoftAuthority(""); got != microsoftCommonAuthority {
		t.Fatalf("normalizeMicrosoftAuthority(empty) = %q, want %q", got, microsoftCommonAuthority)
	}
	if got := normalizeMicrosoftAuthority("tenant-id"); got != "tenant-id" {
		t.Fatalf("normalizeMicrosoftAuthority() = %q, want tenant-id", got)
	}

	tenantID := "9188040d-6c67-4c5b-b112-36a304b66dad"
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"tid":"` + tenantID + `"}`))
	got, err := microsoftTenantIDHint("header." + payload + ".signature")
	if err != nil || got != tenantID {
		t.Fatalf("microsoftTenantIDHint() = %q, %v, want %q, nil", got, err, tenantID)
	}

	for _, raw := range []string{
		"invalid",
		"header.not-base64.signature",
		"header." + base64.RawURLEncoding.EncodeToString([]byte(`{"tid":"not-a-guid"}`)) + ".signature",
		"header." + base64.RawURLEncoding.EncodeToString([]byte(`{"tid":"00000000-0000-0000-0000-000000000000"}`)) + ".signature",
	} {
		if _, err := microsoftTenantIDHint(raw); err == nil {
			t.Fatalf("microsoftTenantIDHint(%q) returned nil error", raw)
		}
	}
}

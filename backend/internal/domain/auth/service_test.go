package auth

// 测试目标：auth 服务纯函数（service.go 中的无 DB 函数）。
// 覆盖的正常路径：bearerToken 解析、APIKeyFromRequest 多来源优先级、toSet/intersect、
// UsesDefaultPassword 判定、issueToken 签发并回读。
// 覆盖的异常情况：无凭证、格式非法的 Authorization 头、密码为空/非默认密码。
// DB 路径（Login/CurrentUser/AuthenticateRequest/AuthenticateAPIKey 等）见 service_db_test.go。

import (
	"backend/internal/config"
	"backend/internal/infra/cache"
	"context"
	"encoding/json"
	"errors"
	"github.com/alicebob/miniredis/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/repo"

	"github.com/golang-jwt/jwt/v5"
)

func TestBearerToken(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	if token := bearerToken(req); token != "" {
		t.Fatalf("empty header token = %q", token)
	}
	req.Header.Set("Authorization", "Bearer abc123")
	if token := bearerToken(req); token != "abc123" {
		t.Fatalf("bearer token = %q", token)
	}
	req.Header.Set("Authorization", "bearer abc123")
	if token := bearerToken(req); token != "abc123" {
		t.Fatalf("lowercase bearer token = %q", token)
	}
	req.Header.Set("Authorization", "Bearer   spaced  ")
	if token := bearerToken(req); token != "spaced" {
		t.Fatalf("trimmed bearer token = %q", token)
	}
	req.Header.Set("Authorization", "Basic abc")
	if token := bearerToken(req); token != "" {
		t.Fatalf("basic auth token = %q", token)
	}
}

func TestAPIKeyFromRequest(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "/", nil)
	if key := APIKeyFromRequest(req); key != "" {
		t.Fatalf("empty = %q", key)
	}
	// X-API-Key 头
	req.Header.Set("X-API-Key", "key-1")
	if key := APIKeyFromRequest(req); key != "key-1" {
		t.Fatalf("X-API-Key = %q", key)
	}
	// query apikey
	req, _ = http.NewRequest(http.MethodGet, "/?apikey=key-2", nil)
	if key := APIKeyFromRequest(req); key != "key-2" {
		t.Fatalf("apikey query = %q", key)
	}
	req, _ = http.NewRequest(http.MethodGet, "/?apiKey=key-3", nil)
	if key := APIKeyFromRequest(req); key != "key-3" {
		t.Fatalf("apiKey query = %q", key)
	}
	// Authorization: apikey
	req, _ = http.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "apikey key-4")
	if key := APIKeyFromRequest(req); key != "key-4" {
		t.Fatalf("apikey auth header = %q", key)
	}
	// Authorization: Bearer 兜底
	req, _ = http.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer key-5")
	if key := APIKeyFromRequest(req); key != "key-5" {
		t.Fatalf("bearer fallback = %q", key)
	}
	// X-API-Key 头优先于 query apikey（生产行为）
	req.Header.Set("X-API-Key", "header-key")
	req.URL.RawQuery = url.Values{"apikey": []string{"query-key"}}.Encode()
	if key := APIKeyFromRequest(req); key != "header-key" {
		t.Fatalf("header precedence = %q", key)
	}
}

func TestToSetAndIntersect(t *testing.T) {
	set := toSet([]string{"a", "b", "a"})
	if len(set) != 2 {
		t.Fatalf("toSet = %v", set)
	}
	if _, ok := set["a"]; !ok {
		t.Fatal("toSet missing a")
	}
	if len(toSet(nil)) != 0 {
		t.Fatal("toSet(nil) should be empty")
	}
	out := intersect([]string{"a", "b", "c"}, []string{"b", "c", "d"})
	if strings.Join(out, ",") != "b,c" {
		t.Fatalf("intersect = %v", out)
	}
	if len(intersect([]string{"a"}, []string{"b"})) != 0 {
		t.Fatal("disjoint intersect should be empty")
	}
}

func TestUsesDefaultPassword(t *testing.T) {
	// 导入时即使指定了自定义初始密码，也必须首次修改。
	if !UsesDefaultPassword(repo.User{Password: bcryptHashForTest(t, "custom"), IsRandomPwd: true}) {
		t.Fatal("imported initial password should require password change")
	}
	// 空密码
	if UsesDefaultPassword(repo.User{Password: ""}) {
		t.Fatal("empty password should not be default")
	}
	// 非默认密码
	if UsesDefaultPassword(repo.User{Password: "$2a$10$abcdefghijklmnopqrstuvwx"}) {
		t.Fatal("random hash should not be default")
	}
	// 真实默认密码 hash（bcrypt of "tier0"）
	if !UsesDefaultPassword(repo.User{Password: bcryptHashForTest(t, "tier0")}) {
		t.Fatal("default password should be detected")
	}
	if UsesDefaultPassword(repo.User{Password: bcryptHashForTest(t, "other")}) {
		t.Fatal("non-default password detected as default")
	}
}

func bcryptHashForTest(t *testing.T, password string) string {
	t.Helper()
	// 直接 import bcrypt 计算（避免引入额外依赖）
	hash, err := bcryptGenerate(password)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	return hash
}

// settingSourceStub 模拟 sys_setting 读取结果,验证签发时长实际取自存储值。
type settingSourceStub struct {
	value string
	err   error
}

func (s settingSourceStub) GetSetting(context.Context, string) (string, error) {
	return s.value, s.err
}

func TestIssueToken(t *testing.T) {
	svc := New(nil, "test-secret")
	// 设置源读取失败时走默认回退路径,exp 仍有效
	svc.systemSettings = settingSourceStub{err: errors.New("settings unavailable")}
	token, expireAt, err := svc.issueToken(context.Background(), repo.User{ID: 42, UserName: "u"})
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (interface{}, error) {
		return []byte("test-secret"), nil
	})
	if err != nil || !parsed.Valid {
		t.Fatalf("parse issued token: %v", err)
	}
	if claims["uid"] != float64(42) {
		t.Fatalf("uid = %v", claims["uid"])
	}
	if claims["sub"] != "42" {
		t.Fatalf("sub = %v", claims["sub"])
	}
	exp, ok := claims["exp"].(float64)
	if !ok {
		t.Fatalf("exp missing: %v", claims["exp"])
	}
	if int64(exp) != expireAt.Unix() {
		t.Fatalf("claims exp = %v, returned expireAt = %v", exp, expireAt.Unix())
	}
	if diff := time.Duration(int64(exp)-time.Now().Unix()) * time.Second; diff < 11*time.Hour || diff > 13*time.Hour {
		t.Fatalf("fallback exp offset = %v, want ~12h", diff)
	}
}

func TestIssueTokenUsesStoredDuration(t *testing.T) {
	cases := []struct {
		value    string
		wantNear time.Duration
	}{
		{"7d", 7 * 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"1d", 24 * time.Hour},
	}
	for _, tc := range cases {
		svc := New(nil, "test-secret")
		svc.systemSettings = settingSourceStub{value: tc.value}
		_, expireAt, err := svc.issueToken(context.Background(), repo.User{ID: 42})
		if err != nil {
			t.Fatalf("issueToken(%q): %v", tc.value, err)
		}
		offset := time.Until(expireAt)
		margin := 5 * time.Second
		if offset < tc.wantNear-margin || offset > tc.wantNear+margin {
			t.Fatalf("stored %q produced exp offset %v, want ~%v", tc.value, offset, tc.wantNear)
		}
	}

	// permanent:远超默认 12h(取 100 年量级)
	svc := New(nil, "test-secret")
	svc.systemSettings = settingSourceStub{value: SessionDurationPermanent}
	_, expireAt, err := svc.issueToken(context.Background(), repo.User{ID: 42})
	if err != nil {
		t.Fatalf("issueToken(permanent): %v", err)
	}
	if offset := time.Until(expireAt); offset < loginSessionPermanentTTL-time.Hour {
		t.Fatalf("permanent exp offset = %v, want >= %v", offset, loginSessionPermanentTTL)
	}

	// 存储非法值回退默认 12h
	broken := New(nil, "test-secret")
	broken.systemSettings = settingSourceStub{value: "1w"}
	brokenToken, brokenExp, brokenErr := broken.issueToken(context.Background(), repo.User{ID: 42})
	if brokenErr != nil {
		t.Fatalf("issueToken(invalid): %v", brokenErr)
	}
	if brokenToken == "" {
		t.Fatal("empty token for invalid stored value")
	}
	if offset := time.Until(brokenExp); offset < 11*time.Hour || offset > 13*time.Hour {
		t.Fatalf("invalid value exp offset = %v, want ~12h", offset)
	}
}

// Concurrent redemption and expiry must not produce another usable session.
func TestMiniEntrySingleUseAndExpiry(t *testing.T) {
	r := miniredis.RunT(t)
	client := cache.Open(config.RedisConf{Addr: r.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	service := &Service{}
	ticket := strings.Repeat("a", 43)
	grant := MiniEntryGrant{Token: "test-session", ProjectID: 1, AppID: 2}
	payload, _ := json.Marshal(grant)
	seed := func() {
		if err := client.SetString(ctx, miniEntryKey(ticket), string(payload), miniEntryTTL); err != nil {
			t.Fatal(err)
		}
	}
	seed()
	var wins atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			got, err := service.TakeMiniEntry(ctx, client, ticket)
			if err == nil && got == grant {
				wins.Add(1)
			}
		}()
	}
	group.Wait()
	if wins.Load() != 1 {
		t.Fatalf("redemptions=%d", wins.Load())
	}
	seed()
	r.FastForward(61 * time.Second)
	if _, err := service.TakeMiniEntry(ctx, client, ticket); err == nil {
		t.Fatal("expired ticket accepted")
	}
	if _, err := service.TakeMiniEntry(ctx, client, "invalid"); err == nil {
		t.Fatal("malformed ticket accepted")
	}
	if err := client.SetString(ctx, miniEntryKey(ticket), "{}", miniEntryTTL); err != nil {
		t.Fatal(err)
	}
	if _, err := service.TakeMiniEntry(ctx, client, ticket); err == nil {
		t.Fatal("corrupt entry accepted")
	}
}
func TestMiniEntryRejectsInvalidSessionBeforeDB(t *testing.T) {
	service := &Service{jwtSecret: []byte("test-secret")}
	for _, tc := range []struct {
		name   string
		method jwt.SigningMethod
		claims jwt.MapClaims
		key    string
	}{
		{"expired", jwt.SigningMethodHS256, jwt.MapClaims{"sub": "1", "exp": time.Now().Add(-time.Minute).Unix()}, "test-secret"},
		{"no expiry", jwt.SigningMethodHS256, jwt.MapClaims{"sub": "1"}, "test-secret"},
		{"wrong signature", jwt.SigningMethodHS256, jwt.MapClaims{"sub": "1", "exp": time.Now().Add(time.Hour).Unix()}, "different"},
		{"wrong algorithm", jwt.SigningMethodHS384, jwt.MapClaims{"sub": "1", "exp": time.Now().Add(time.Hour).Unix()}, "test-secret"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token, err := jwt.NewWithClaims(tc.method, tc.claims).SignedString([]byte(tc.key))
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = service.ValidateMiniEntrySession(context.Background(), token); err == nil {
				t.Fatal("invalid JWT accepted")
			}
		})
	}
}

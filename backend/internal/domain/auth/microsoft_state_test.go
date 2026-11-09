package auth

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"backend/internal/config"
	"backend/internal/infra/cache"
	"github.com/alicebob/miniredis/v2"
)

// 浏览器绑定不匹配、重放和过期 state 均应在任何 OIDC 网络调用之前被拒绝。
func TestMicrosoftCallbackStateIsBoundAndSingleUse(t *testing.T) {
	redis := miniredis.RunT(t)
	client := cache.Open(config.RedisConf{Addr: redis.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	service := &Service{}
	service.ConfigureMicrosoft(client, MicrosoftConfig{Enabled: true, ClientID: "client", ClientSecret: "secret", RedirectURL: "https://example.com/api/core/auth/sso/microsoft/callback"})
	ctx := context.Background()
	seed := func(state string) {
		body, _ := json.Marshal(microsoftLoginState{Nonce: "nonce", CodeVerifier: "verifier", BindingHash: hashMicrosoftBinding("browser")})
		if err := client.SetString(ctx, microsoftStateKey(state), string(body), microsoftStateTTL); err != nil {
			t.Fatal(err)
		}
	}
	seed("one-use")
	if _, err := service.MicrosoftCallback(ctx, "one-use", "code", "", "different-browser"); !errors.Is(err, ErrMicrosoftStateInvalid) {
		t.Fatalf("wrong binding: %v", err)
	}
	if _, err := service.MicrosoftCallback(ctx, "one-use", "code", "", "browser"); !errors.Is(err, ErrMicrosoftStateInvalid) {
		t.Fatalf("replay: %v", err)
	}
	seed("cancelled")
	if _, err := service.MicrosoftCallback(ctx, "cancelled", "", "access_denied", "browser"); !errors.Is(err, ErrMicrosoftIdentityInvalid) {
		t.Fatalf("cancelled: %v", err)
	}
	seed("expired")
	redis.FastForward(11 * time.Minute)
	if _, err := service.MicrosoftCallback(ctx, "expired", "code", "", "browser"); !errors.Is(err, ErrMicrosoftStateInvalid) {
		t.Fatalf("expired: %v", err)
	}
}

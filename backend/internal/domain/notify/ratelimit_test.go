package notify

import (
	"context"
	"testing"
	"time"

	"backend/internal/contextx"
)

func TestAppRateLimitScope_UsesAuthenticatedAppOnly(t *testing.T) {
	if got := appRateLimitScope(contextx.Subject{APIKeyAppID: " app-1 "}); got != "app:app-1" {
		t.Fatalf("authenticated app scope = %q", got)
	}
	if got := appRateLimitScope(contextx.Subject{}); got != "" {
		t.Fatalf("key without app association must not get a shared app scope: %q", got)
	}
}

func TestMemoryRateLimiter_Threshold(t *testing.T) {
	rl := NewMemoryRateLimiter()
	ctx := context.Background()
	over := false
	for i := 0; i < RateLimitPerMinute; i++ {
		limited, err := rl.Hit(ctx, "key:1")
		if err != nil {
			t.Fatalf("hit: %v", err)
		}
		if limited {
			over = true
			break
		}
	}
	if over {
		t.Fatal("first 1000 hits should pass")
	}
	limited, err := rl.Hit(ctx, "key:1")
	if err != nil {
		t.Fatalf("hit: %v", err)
	}
	if !limited {
		t.Fatal("1001st hit should be limited")
	}
	// 不同 Key 独立计数
	limited, err = rl.Hit(ctx, "key:2")
	if err != nil || limited {
		t.Fatalf("other key should be independent: limited=%v err=%v", limited, err)
	}
}

func TestMemoryRateLimiter_WindowReset(t *testing.T) {
	rl := NewMemoryRateLimiter().(*memoryRateLimiter)
	ctx := context.Background()
	now := time.Now()
	rl.now = func() time.Time { return now }
	for i := 0; i < RateLimitPerMinute; i++ {
		if _, err := rl.Hit(ctx, "key:1"); err != nil {
			t.Fatalf("hit: %v", err)
		}
	}
	if limited, _ := rl.Hit(ctx, "key:1"); !limited {
		t.Fatal("should be limited at threshold")
	}
	// 窗口滑动后重置
	now = now.Add(2 * time.Minute)
	if limited, _ := rl.Hit(ctx, "key:1"); limited {
		t.Fatal("window should reset after slide")
	}
}

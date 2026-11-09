package notify

import (
	"context"
	"sync"
	"time"
)

// RateLimiter 限流器：按调用方提供的稳定作用域固定窗口计数（1000 次/分钟，对齐云端阈值）。
// 企业版为单体部署，进程内窗口计数语义等价；接口预留 Redis 实现位，
// 降级实现必须通过同一组兼容性测试用例（05-测试方案 3）。
type RateLimiter interface {
	// Hit 记一次请求；返回 true 表示超限（应返回 429）。
	Hit(ctx context.Context, scope string) (bool, error)
}

// memoryRateLimiter 进程内固定窗口计数实现（窗口 60s，条目 65s 过期兜底清理）。
type memoryRateLimiter struct {
	mu      sync.Mutex
	windows map[string]*rateWindow
	now     func() time.Time
}

type rateWindow struct {
	start    time.Time
	counter  int
	expireAt time.Time
}

// NewMemoryRateLimiter 构造进程内限流器。
func NewMemoryRateLimiter() RateLimiter {
	return &memoryRateLimiter{
		windows: make(map[string]*rateWindow),
		now:     time.Now,
	}
}

func (m *memoryRateLimiter) Hit(_ context.Context, scope string) (bool, error) {
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.windows[scope]
	if !ok || now.Sub(w.start) >= time.Minute || now.After(w.expireAt) {
		m.windows[scope] = &rateWindow{start: now, counter: 1, expireAt: now.Add(rateLimitWindowExpireSeconds * time.Second)}
		return false, nil
	}
	w.counter++
	if w.counter > RateLimitPerMinute {
		return true, nil
	}
	return false, nil
}

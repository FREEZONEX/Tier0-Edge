package cache

import (
	"context"
	"time"
)

// Store exposes atomic cache semantics independently of Redis wire commands.
type Store interface {
	Close() error
	Ping(context.Context) error
	GetString(context.Context, string) (string, error)
	TakeString(context.Context, string) (string, error)
	MGetStrings(context.Context, ...string) ([]string, error)
	SetString(context.Context, string, string, time.Duration) error
	SetNX(context.Context, string, string, time.Duration) (bool, error)
	Del(context.Context, ...string) error
	Increment(context.Context, string, time.Duration) (int64, error)
	UpdateIfNewer(context.Context, string, string, int64) (bool, error)
}

type Client struct{ Store Store }

func (c *Client) backend() Store {
	if c == nil || c.Store == nil {
		return &memoryStore{closed: true}
	}
	return c.Store
}
func (c *Client) Close() error {
	if c == nil || c.Store == nil {
		return nil
	}
	return c.Store.Close()
}
func (c *Client) Ping(ctx context.Context) error { return c.backend().Ping(ctx) }
func (c *Client) GetString(ctx context.Context, k string) (string, error) {
	return c.backend().GetString(ctx, k)
}
func (c *Client) TakeString(ctx context.Context, k string) (string, error) {
	return c.backend().TakeString(ctx, k)
}
func (c *Client) MGetStrings(ctx context.Context, k ...string) ([]string, error) {
	return c.backend().MGetStrings(ctx, k...)
}
func (c *Client) SetString(ctx context.Context, k, v string, ttl time.Duration) error {
	return c.backend().SetString(ctx, k, v, ttl)
}
func (c *Client) SetNX(ctx context.Context, k, v string, ttl time.Duration) (bool, error) {
	return c.backend().SetNX(ctx, k, v, ttl)
}
func (c *Client) Del(ctx context.Context, k ...string) error { return c.backend().Del(ctx, k...) }
func (c *Client) Increment(ctx context.Context, k string, ttl time.Duration) (int64, error) {
	return c.backend().Increment(ctx, k, ttl)
}
func (c *Client) UpdateIfNewer(ctx context.Context, k, v string, version int64) (bool, error) {
	return c.backend().UpdateIfNewer(ctx, k, v, version)
}

func (c *redisStore) Increment(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	return c.EvalInt64(ctx, `local v=redis.call('INCR',KEYS[1]); if v==1 and tonumber(ARGV[1])>0 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end; return v`, []string{key}, ttl.Milliseconds())
}
func (c *redisStore) UpdateIfNewer(ctx context.Context, key, value string, version int64) (bool, error) {
	n, err := c.EvalInt64(ctx, `local current=redis.call('GET',KEYS[1]); if current then local ok,obj=pcall(cjson.decode,current); if ok and tonumber(obj.timeStamp or obj.updateTime or 0)>tonumber(ARGV[2]) then return 0 end end; redis.call('SET',KEYS[1],ARGV[1]); return 1`, []string{key}, value, version)
	return n == 1, err
}

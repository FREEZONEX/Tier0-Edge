package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"backend/internal/config"

	"github.com/redis/go-redis/v9"
)

type redisStore struct {
	rdb *redis.Client
}

type EvalRequest struct {
	Keys []string
	Args []any
}

var ErrNotFound = errors.New("cache not found")

func Open(c config.RedisConf) *Client {
	if c.Driver != "redis" && !(c.Driver == "" && c.Addr != "") {
		return &Client{Store: newMemoryStore(c.MaxEntries)}
	}
	return &Client{Store: &redisStore{rdb: redis.NewClient(&redis.Options{
		Addr:     c.Addr,
		Password: c.Password,
		DB:       c.DB,
	})}}
}

func (c *redisStore) Close() error {
	if c == nil || c.rdb == nil {
		return nil
	}
	return c.rdb.Close()
}

func (c *redisStore) Ping(ctx context.Context) error {
	if c == nil || c.rdb == nil {
		return errors.New("redis not initialized")
	}
	return c.rdb.Ping(ctx).Err()
}

func (c *redisStore) Publish(ctx context.Context, channel string, payload any) error {
	if c == nil || c.rdb == nil {
		return errors.New("redis not initialized")
	}
	return c.rdb.Publish(ctx, channel, payload).Err()
}

func (c *redisStore) GetString(ctx context.Context, key string) (string, error) {
	if c == nil || c.rdb == nil {
		return "", errors.New("redis not initialized")
	}
	value, err := c.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrNotFound
	}
	return value, err
}

// TakeString atomically reads and removes a short-lived value. It is used for
// one-time login state so concurrent/replayed callbacks cannot consume it twice.
func (c *redisStore) TakeString(ctx context.Context, key string) (string, error) {
	if c == nil || c.rdb == nil {
		return "", errors.New("redis not initialized")
	}
	const script = `
local value = redis.call('GET', KEYS[1])
if not value then
  return nil
end
redis.call('DEL', KEYS[1])
return value
`
	value, err := c.rdb.Eval(ctx, script, []string{key}).Text()
	if errors.Is(err, redis.Nil) {
		return "", ErrNotFound
	}
	return value, err
}

func (c *redisStore) MGetStrings(ctx context.Context, keys ...string) ([]string, error) {
	if len(keys) == 0 {
		return []string{}, nil
	}
	if c == nil || c.rdb == nil {
		return nil, errors.New("redis not initialized")
	}
	values, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	out := make([]string, len(values))
	for idx, value := range values {
		if value == nil {
			continue
		}
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("redis mget value at index %d is %T", idx, value)
		}
		out[idx] = text
	}
	return out, nil
}

func (c *redisStore) SetString(ctx context.Context, key, value string, ttl time.Duration) error {
	if c == nil || c.rdb == nil {
		return errors.New("redis not initialized")
	}
	return c.rdb.Set(ctx, key, value, ttl).Err()
}

func (c *redisStore) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if c == nil || c.rdb == nil {
		return false, errors.New("redis not initialized")
	}
	return c.rdb.SetNX(ctx, key, value, ttl).Result()
}

func (c *redisStore) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	if c == nil || c.rdb == nil {
		return errors.New("redis not initialized")
	}
	return c.rdb.Del(ctx, keys...).Err()
}

func (c *redisStore) EvalInt64(ctx context.Context, script string, keys []string, args ...any) (int64, error) {
	if c == nil || c.rdb == nil {
		return 0, errors.New("redis not initialized")
	}
	return c.rdb.Eval(ctx, script, keys, args...).Int64()
}

func (c *redisStore) EvalBatch(ctx context.Context, script string, requests []EvalRequest) error {
	if len(requests) == 0 {
		return nil
	}
	if c == nil || c.rdb == nil {
		return errors.New("redis not initialized")
	}
	_, err := c.rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, request := range requests {
			pipe.Eval(ctx, script, request.Keys, request.Args...)
		}
		return nil
	})
	return err
}

// EvalStrings executes a bounded script returning a flat string array.
func (c *redisStore) EvalStrings(ctx context.Context, script string, keys []string, args ...any) ([]string, error) {
	if c == nil || c.rdb == nil {
		return nil, errors.New("redis not initialized")
	}
	return c.rdb.Eval(ctx, script, keys, args...).StringSlice()
}

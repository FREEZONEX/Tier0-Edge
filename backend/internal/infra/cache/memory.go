package cache

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"
)

var ErrUnavailable = errors.New("cache unavailable")
var ErrCapacity = errors.New("cache capacity reached")

type memoryEntry struct {
	value   string
	expires time.Time
}
type memoryStore struct {
	mu      sync.Mutex
	entries map[string]memoryEntry
	max     int
	closed  bool
	stop    chan struct{}
	done    chan struct{}
}

func newMemoryStore(max int) *memoryStore {
	if max <= 0 {
		max = 100000
	}
	s := &memoryStore{entries: make(map[string]memoryEntry), max: max, stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case now := <-t.C:
				s.mu.Lock()
				s.prune(now)
				s.mu.Unlock()
			}
		}
	}()
	return s
}
func (s *memoryStore) prune(now time.Time) {
	for k, v := range s.entries {
		if !v.expires.IsZero() && !now.Before(v.expires) {
			delete(s.entries, k)
		}
	}
}
func (s *memoryStore) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return ErrUnavailable
	}
	return nil
}
func (s *memoryStore) entry(key string) (memoryEntry, bool) {
	v, ok := s.entries[key]
	if ok && !v.expires.IsZero() && !time.Now().Before(v.expires) {
		delete(s.entries, key)
		return memoryEntry{}, false
	}
	return v, ok
}
func (s *memoryStore) put(key, value string, ttl time.Duration) error {
	if _, ok := s.entry(key); !ok && len(s.entries) >= s.max {
		s.prune(time.Now())
		if len(s.entries) >= s.max {
			return ErrCapacity
		}
	}
	e := memoryEntry{value: value}
	if ttl > 0 {
		e.expires = time.Now().Add(ttl)
	}
	s.entries[key] = e
	return nil
}
func (s *memoryStore) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.entries = nil
	if s.stop != nil {
		close(s.stop)
	}
	s.mu.Unlock()
	if s.done != nil {
		<-s.done
	}
	return nil
}
func (s *memoryStore) Ping(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready(ctx)
}
func (s *memoryStore) GetString(ctx context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return "", err
	}
	v, ok := s.entry(key)
	if !ok {
		return "", ErrNotFound
	}
	return v.value, nil
}
func (s *memoryStore) TakeString(ctx context.Context, key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return "", err
	}
	v, ok := s.entry(key)
	if !ok {
		return "", ErrNotFound
	}
	delete(s.entries, key)
	return v.value, nil
}
func (s *memoryStore) MGetStrings(ctx context.Context, keys ...string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	out := make([]string, len(keys))
	for i, k := range keys {
		if v, ok := s.entry(k); ok {
			out[i] = v.value
		}
	}
	return out, nil
}
func (s *memoryStore) SetString(ctx context.Context, k, v string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	return s.put(k, v, ttl)
}
func (s *memoryStore) SetNX(ctx context.Context, k, v string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return false, err
	}
	if _, ok := s.entry(k); ok {
		return false, nil
	}
	err := s.put(k, v, ttl)
	return err == nil, err
}
func (s *memoryStore) Del(ctx context.Context, keys ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	for _, k := range keys {
		delete(s.entries, k)
	}
	return nil
}
func (s *memoryStore) Increment(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return 0, err
	}
	e, ok := s.entry(key)
	var n int64
	var err error
	if ok {
		n, err = strconv.ParseInt(e.value, 10, 64)
		if err != nil {
			return 0, err
		}
	}
	if n == int64(^uint64(0)>>1) {
		return 0, errors.New("cache counter overflow")
	}
	n++
	if ok {
		e.value = strconv.FormatInt(n, 10)
		s.entries[key] = e
		return n, nil
	}
	err = s.put(key, strconv.FormatInt(n, 10), ttl)
	return n, err
}
func (s *memoryStore) UpdateIfNewer(ctx context.Context, key, value string, version int64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return false, err
	}
	if e, ok := s.entry(key); ok {
		var old struct {
			Timestamp int64 `json:"timeStamp"`
			Updated   int64 `json:"updateTime"`
		}
		if json.Unmarshal([]byte(e.value), &old) == nil {
			stamp := old.Timestamp
			if stamp == 0 {
				stamp = old.Updated
			}
			if stamp > version {
				return false, nil
			}
		}
	}
	err := s.put(key, value, 0)
	return err == nil, err
}

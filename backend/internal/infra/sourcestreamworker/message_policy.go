package sourcestreamworker

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"backend/internal/domain/dataingest"
	"backend/internal/domain/sourcestream"
	"github.com/nats-io/nats.go"
)

// One bounded, lossy retention policy applies to all four business streams.
// Expiry is measured from ingress, not renewed by outbox/retry hops.
type messagePolicy struct {
	maxAge   time.Duration
	maxMsgs  int64
	maxBytes int64
}

func loadMessagePolicy() (messagePolicy, error) {
	p := messagePolicy{maxAge: time.Hour, maxMsgs: 100000, maxBytes: 1024 * 1024 * 1024}
	for _, field := range []struct {
		name string
		set  func(int64)
		max  int64
	}{
		{"SOURCE_STREAM_MESSAGE_MAX_AGE_SECONDS", func(v int64) { p.maxAge = time.Duration(v) * time.Second }, int64((365 * 24 * time.Hour) / time.Second)},
		{"SOURCE_STREAM_MESSAGE_MAX_MSGS", func(v int64) { p.maxMsgs = v }, 1<<63 - 1},
		{"SOURCE_STREAM_MESSAGE_MAX_BYTES", func(v int64) { p.maxBytes = v }, 1<<63 - 1},
	} {
		value := strings.TrimSpace(os.Getenv(field.name))
		if value == "" {
			continue
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n <= 0 || n > field.max {
			return p, fmt.Errorf("invalid positive message limit: %s", field.name)
		}
		field.set(n)
	}
	return p, nil
}

func (p messagePolicy) apply(config *nats.StreamConfig) {
	config.MaxAge, config.MaxMsgs, config.MaxBytes = p.maxAge, p.maxMsgs, p.maxBytes
	config.Discard = nats.DiscardOld
	// DuplicateWindow cannot exceed MaxAge when testing short retention.
	if config.Duplicates > p.maxAge {
		config.Duplicates = p.maxAge
	}
}

func (p messagePolicy) deadline(subject string, payload []byte, published time.Time) time.Time {
	at := published
	switch {
	case strings.HasPrefix(subject, "tier0.raw.source."):
		var raw sourcestream.RawInput
		if json.Unmarshal(payload, &raw) == nil && raw.Input.ReceivedAt > 0 {
			at = time.UnixMilli(raw.Input.ReceivedAt)
		}
	case strings.HasPrefix(subject, "tier0.events.topic."):
		var event dataingest.StreamEvent
		if json.Unmarshal(payload, &event) == nil && !event.ReceivedAt.IsZero() {
			at = event.ReceivedAt
		}
	case strings.HasPrefix(subject, "tier0.delivery.task."):
		var task struct {
			ReceivedAt int64 `json:"receivedAt"`
		}
		if json.Unmarshal(payload, &task) == nil && task.ReceivedAt > 0 {
			at = time.UnixMilli(task.ReceivedAt)
		}
	}
	// A producer timestamp cannot extend the server's retention window.
	if at.IsZero() || (!published.IsZero() && at.After(published)) {
		at = published
	}
	return at.Add(p.maxAge)
}

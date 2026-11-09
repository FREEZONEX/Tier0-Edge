package outbox

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"backend/internal/repo"

	"gorm.io/gorm"
)

type Service struct {
	store *repo.OutboxRepo
}

func New() *Service {
	return &Service{store: repo.NewOutboxRepo(context.Background())}
}

func (s *Service) Enqueue(ctx context.Context, eventType, aggregateType, aggregateID string, payload any) error {
	return enqueueOnDB(ctx, s.store, eventType, aggregateType, aggregateID, payload)
}

// EnqueueOnTx 在指定事务上落 outbox 行（调用方事务提交时任务随之原子可见）。
// 供需要"业务行与任务行同事务"的场景使用（如 notify.create_inbox 与请求行同事务）。
func (s *Service) EnqueueOnTx(tx *gorm.DB, eventType, aggregateType, aggregateID string, payload any) error {
	return enqueueOnDB(context.Background(), repo.NewOutboxRepo(tx), eventType, aggregateType, aggregateID, payload)
}

func enqueueOnDB(ctx context.Context, store *repo.OutboxRepo, eventType, aggregateType, aggregateID string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	eventID := fmt.Sprintf("%s:%s:%d", eventType, aggregateID, time.Now().UTC().UnixNano())
	// event_id 列为 VARCHAR(64)；aggregateID 较长时（如 UNS 异步任务的 36 位 jobKey）
	// 组合 ID 会超长导致入队失败，超长时按既有约定（见 user_sync.go）退化为 sha256 摘要。
	if len(eventID) > 64 {
		digest := sha256.Sum256([]byte(eventID))
		eventID = fmt.Sprintf("%x", digest[:])
	}
	return store.EnqueueOutbox(ctx, repo.OutboxEvent{
		EventID:       eventID,
		EventType:     eventType,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		Payload:       body,
	})
}

func NextRetryTime(attempts int, now time.Time) int64 {
	delay := 30 * time.Second
	switch attempts {
	case 0:
		delay = 0
	case 1:
		delay = 30 * time.Second
	case 2:
		delay = 2 * time.Minute
	case 3:
		delay = 10 * time.Minute
	default:
		delay = 30 * time.Minute
	}
	return now.Add(delay).UTC().UnixMilli()
}

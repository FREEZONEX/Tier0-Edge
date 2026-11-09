package outbox

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"backend/internal/repo"

	"github.com/zeromicro/go-zero/core/logx"
)

type EventHandler func(ctx context.Context, event repo.OutboxEvent) error
type BatchHandler func(context.Context, []repo.OutboxEvent) map[string]error

type Worker struct {
	store              *repo.OutboxRepo
	handlers           map[string]EventHandler
	batchHandlers      map[string]BatchHandler
	workerID           string
	batchSize          int
	lockTimeout        time.Duration
	interval           time.Duration
	completedRetention time.Duration

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

type Option func(*Worker)

// WithBatchSize bounds the number of leased events waiting behind a slow handler.
func WithStore(store *repo.OutboxRepo) Option { return func(w *Worker) { w.store = store } }

func WithBatchSize(size int) Option {
	return func(w *Worker) {
		if size > 0 {
			w.batchSize = size
		}
	}
}

// 仅对显式启用的消费者清理已完成中转记录；失败和待处理记录不受影响。
func WithCompletedRetention(retention time.Duration) Option {
	return func(w *Worker) { w.completedRetention = retention }
}

func NewWorker(ctx context.Context, options ...Option) *Worker {
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	if host == "" {
		host = "backend"
	}
	worker := &Worker{
		store:         repo.NewOutboxRepo(ctx),
		handlers:      map[string]EventHandler{},
		batchHandlers: map[string]BatchHandler{},
		workerID:      fmt.Sprintf("%s-%d", host, os.Getpid()),
		batchSize:     10,
		lockTimeout:   2 * time.Minute,
		interval:      2 * time.Second,
	}
	for _, option := range options {
		option(worker)
	}
	return worker
}

func (w *Worker) Register(eventType string, handler EventHandler) {
	eventType = strings.TrimSpace(eventType)
	if w == nil || eventType == "" || handler == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handlers[eventType] = handler
}

// RegisterBatch reuses durable leasing, retry scheduling and completion records.
// Missing per-event results are failures, never implicit acknowledgements.
func (w *Worker) RegisterBatch(eventType string, handler BatchHandler) {
	if w == nil || handler == nil || strings.TrimSpace(eventType) == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.batchHandlers[eventType] = handler
	w.handlers[eventType] = func(context.Context, repo.OutboxEvent) error { return fmt.Errorf("batch handler required") }
}

func (w *Worker) Start(ctx context.Context) {
	if w == nil {
		return
	}
	w.mu.Lock()
	if w.cancel != nil {
		w.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.wg.Add(1)
	w.mu.Unlock()

	go w.loop(runCtx)
}

func (w *Worker) Stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	cancel := w.cancel
	w.cancel = nil
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	w.wg.Wait()
}

func (w *Worker) loop(ctx context.Context) {
	defer w.wg.Done()
	nextCleanup := time.Time{}
	for {
		if ctx.Err() != nil {
			return
		}
		if w.completedRetention > 0 && !time.Now().Before(nextCleanup) {
			// 每批最多 1,000 条，即使持续有待消费事件也按时清理。
			nextCleanup = time.Now().Add(time.Minute)
			if count, err := w.pruneCompleted(ctx); err != nil {
				logx.WithContext(ctx).Errorf("outbox completed cleanup failed: %v", err)
			} else if count == 1000 {
				// 满批说明仍可能有积压，下一轮消费后继续清理。
				nextCleanup = time.Time{}
			}
		}
		claimed, err := w.drain(ctx)
		if err != nil {
			logx.WithContext(ctx).Errorf("outbox worker drain failed: %v", err)
		}
		if claimed > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.interval):
		}
	}
}

func (w *Worker) pruneCompleted(ctx context.Context) (int64, error) {
	if w.completedRetention <= 0 {
		return 0, nil
	}
	eventTypes, _ := w.snapshotHandlers()
	cleanupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return w.store.PruneCompletedOutbox(cleanupCtx, eventTypes, time.Now().UTC().Add(-w.completedRetention), 1000)
}

func (w *Worker) drain(ctx context.Context) (int, error) {
	eventTypes, handlers := w.snapshotHandlers()
	if len(eventTypes) == 0 {
		return 0, nil
	}
	events, err := w.store.ClaimOutboxByTypes(ctx, w.workerID, eventTypes, w.batchSize, int64(w.lockTimeout/time.Millisecond))
	if err != nil {
		return 0, err
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].CreatedTime.Equal(events[j].CreatedTime) {
			return events[i].ID < events[j].ID
		}
		return events[i].CreatedTime.Before(events[j].CreatedTime)
	})
	w.mu.Lock()
	batchHandlers := map[string]BatchHandler{}
	for k, h := range w.batchHandlers {
		batchHandlers[k] = h
	}
	w.mu.Unlock()
	for eventType, handler := range batchHandlers {
		batch := []repo.OutboxEvent{}
		for _, e := range events {
			if e.EventType == eventType {
				batch = append(batch, e)
			}
		}
		if len(batch) > 0 {
			w.handleBatch(ctx, batch, handler)
		}
	}
	for _, event := range events {
		if batchHandlers[event.EventType] != nil {
			continue
		}
		handler := handlers[event.EventType]
		if handler == nil {
			continue
		}
		w.handle(ctx, event, handler)
	}
	return len(events), nil
}

func (w *Worker) snapshotHandlers() ([]string, map[string]EventHandler) {
	w.mu.Lock()
	defer w.mu.Unlock()
	handlers := make(map[string]EventHandler, len(w.handlers))
	eventTypes := make([]string, 0, len(w.handlers))
	for eventType, handler := range w.handlers {
		handlers[eventType] = handler
		eventTypes = append(eventTypes, eventType)
	}
	sort.Strings(eventTypes)
	return eventTypes, handlers
}

func (w *Worker) handle(ctx context.Context, event repo.OutboxEvent, handler EventHandler) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err := fmt.Errorf("panic: %v\n%s", recovered, debug.Stack())
			w.markRetry(ctx, event, err)
		}
	}()
	if err := handler(ctx, event); err != nil {
		w.markRetry(ctx, event, err)
		return
	}
	if err := w.store.MarkOutboxDone(ctx, event.EventID); err != nil {
		logx.WithContext(ctx).Errorf("outbox mark done failed eventID=%s err=%v", event.EventID, err)
	}
}

func (w *Worker) markRetry(ctx context.Context, event repo.OutboxEvent, err error) {
	attempts := event.Attempts + 1
	next := NextRetryTime(attempts, time.Now().UTC())
	message := ""
	if err != nil {
		message = err.Error()
	}
	if markErr := w.store.MarkOutboxRetry(ctx, event.EventID, attempts, next, message); markErr != nil {
		logx.WithContext(ctx).Errorf("outbox mark retry failed eventID=%s err=%v", event.EventID, markErr)
	}
	logx.WithContext(ctx).Errorf("outbox handler failed eventID=%s eventType=%s attempts=%d err=%v", event.EventID, event.EventType, attempts, err)
}

func (w *Worker) handleBatch(ctx context.Context, events []repo.OutboxEvent, handler BatchHandler) {
	defer func() {
		if recovered := recover(); recovered != nil {
			for _, e := range events {
				w.markRetry(ctx, e, fmt.Errorf("batch handler panic"))
			}
		}
	}()
	results := handler(ctx, events)
	for _, e := range events {
		err, ok := results[e.EventID]
		if !ok {
			err = fmt.Errorf("batch receipt missing")
		}
		if err != nil {
			w.markRetry(ctx, e, err)
			continue
		}
		if err := w.store.MarkOutboxDone(ctx, e.EventID); err != nil {
			logx.WithContext(ctx).Errorf("outbox mark done failed eventID=%s err=%v", e.EventID, err)
		}
	}
}

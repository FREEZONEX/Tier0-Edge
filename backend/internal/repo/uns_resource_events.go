package repo

import (
	"context"
	"database/sql"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

// Resource notifications are best effort after commit, not a durable queue.
// Never return a publish failure as a failed data write or retry device effects.
type ResourceEventPublisher func(context.Context, string, string, []byte) error

type resourceEventTransport struct{ publisher atomic.Value }

func (*resourceEventTransport) Name() string              { return "uns-direct-resource-events" }
func (*resourceEventTransport) Initialize(*gorm.DB) error { return nil }

// Initial registration runs before serving requests. A restarted Worker replaces
// only the publisher, so existing GORM sessions retain the same transport without
// mutating the plugin registry while transactions are publishing.
func ConfigureResourceEventPublisher(db *gorm.DB, publish ResourceEventPublisher) error {
	if db == nil || publish == nil {
		return ErrInvalidArgument
	}
	if transport, ok := db.Config.Plugins["uns-direct-resource-events"].(*resourceEventTransport); ok {
		transport.publisher.Store(publish)
		return nil
	}
	transport := &resourceEventTransport{}
	transport.publisher.Store(publish)
	return db.Use(transport)
}

type resourceNotification struct {
	subject, id string
	data        []byte
}
type resourceNotifications struct {
	messages []resourceNotification
	bytes    int
}
type resourceNotificationsKey struct{}

// MetadataEventTransaction scopes notifications to the actual outer commit.
// A rolled-back savepoint discards its notifications too. Callers opening an
// outer UNS write transaction must use this helper, not plain GORM Transaction.
func MetadataEventTransaction(ctx context.Context, db *gorm.DB, fn func(*gorm.DB) error, options ...*sql.TxOptions) error {
	pending, _ := ctx.Value(resourceNotificationsKey{}).(*resourceNotifications)
	if pending == nil && db.Statement != nil && db.Statement.Context != nil {
		pending, _ = db.Statement.Context.Value(resourceNotificationsKey{}).(*resourceNotifications)
	}
	owner := pending == nil
	if owner {
		if db.Statement != nil {
			if _, inside := db.Statement.ConnPool.(gorm.TxCommitter); inside {
				return ErrInvalidArgument // cannot observe an unwrapped outer commit
			}
		}
		pending = &resourceNotifications{}
	}
	start, size := len(pending.messages), pending.bytes
	ctx = context.WithValue(ctx, resourceNotificationsKey{}, pending)
	err := db.WithContext(ctx).Transaction(fn, options...)
	if err != nil {
		pending.messages, pending.bytes = pending.messages[:start], size
		return err
	}
	if owner {
		if notifier, _ := db.Config.Plugins["record-change-notifier"].(*recordChangeNotifier); notifier != nil {
			notifier.notify()
		}
	}
	if !owner || len(pending.messages) == 0 {
		return nil
	}
	transport, _ := db.Config.Plugins["uns-direct-resource-events"].(*resourceEventTransport)
	if transport == nil {
		logx.WithContext(ctx).Errorf("UNS resource notifications not published: transport unavailable count=%d", len(pending.messages))
		return nil
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	publish := transport.publisher.Load().(ResourceEventPublisher)
	for i, message := range pending.messages {
		if bounded.Err() != nil {
			logx.WithContext(ctx).Errorf("UNS resource notifications discarded after commit: deadline count=%d", len(pending.messages)-i)
			break
		}
		if err := publish(bounded, message.subject, message.id, message.data); err != nil {
			// Do not log payloads, credentials, or transport error bodies.
			logx.WithContext(ctx).Errorf("UNS resource notification failed after commit: event=%s; no database retry", message.id)
		}
	}
	return nil
}

func stageResourceNotification(tx *gorm.DB, subject, id string, data []byte) error {
	pending, _ := tx.Statement.Context.Value(resourceNotificationsKey{}).(*resourceNotifications)
	if pending == nil {
		return ErrInvalidArgument
	}
	// Transaction-local memory only. Overflow drops notifications, not committed
	// business data; no queue or goroutine survives the request.
	if len(pending.messages) >= 4096 || pending.bytes+len(data) > 4*1024*1024 {
		logx.WithContext(tx.Statement.Context).Error("UNS resource notification discarded: transaction notification budget exceeded")
		return nil
	}
	pending.messages = append(pending.messages, resourceNotification{subject: subject, id: id, data: data})
	pending.bytes += len(data)
	return nil
}

// ConfigureRecordChangeNotifier adds an after-commit wake hint; durable events live in PostgreSQL.
type recordChangeNotifier struct{ notify func() }

func (*recordChangeNotifier) Name() string              { return "record-change-notifier" }
func (*recordChangeNotifier) Initialize(*gorm.DB) error { return nil }
func ConfigureRecordChangeNotifier(db *gorm.DB, notify func()) error {
	if db == nil || notify == nil {
		return ErrInvalidArgument
	}
	return db.Use(&recordChangeNotifier{notify: notify})
}

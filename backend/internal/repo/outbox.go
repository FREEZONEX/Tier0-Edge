package repo

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OutboxEvent struct {
	ID              int64           `gorm:"column:id;primaryKey" json:"id"`
	EventID         string          `gorm:"column:event_id;uniqueIndex:idx_sys_outbox_event_event_id" json:"eventId"`
	EventType       string          `gorm:"column:event_type" json:"eventType"`
	AggregateType   string          `gorm:"column:aggregate_type" json:"aggregateType"`
	AggregateID     string          `gorm:"column:aggregate_id" json:"aggregateId"`
	Payload         json.RawMessage `gorm:"column:payload;type:jsonb" json:"payload"`
	Status          string          `gorm:"column:status" json:"status"`
	Attempts        int             `gorm:"column:attempts" json:"attempts"`
	NextRetryTime   int64           `gorm:"column:next_retry_time" json:"nextRetryTime"`
	LastError       string          `gorm:"column:last_error" json:"lastError"`
	LockedBy        string          `gorm:"column:locked_by" json:"lockedBy"`
	LockedUntilTime int64           `gorm:"column:locked_until_time" json:"lockedUntilTime"`
	OnlyTime
}

func (OutboxEvent) TableName() string { return "sys_outbox_event" }

func (r *OutboxRepo) EnqueueOutbox(ctx context.Context, event OutboxEvent) error {
	now := time.Now().UTC().UnixMilli()
	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage(`{}`)
	}
	event.Status = "pending"
	event.Attempts = 0
	event.NextRetryTime = now
	return r.query(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_id"}}, DoNothing: true}).
		Create(&event).Error
}

// ClaimOutbox atomically locks and returns a batch of pending events. The
// SELECT ... FOR UPDATE SKIP LOCKED self-join has no gorm builder equivalent,
// so it stays as raw SQL.
func (r *OutboxRepo) ClaimOutbox(ctx context.Context, worker string, limit int, lockMs int64) ([]OutboxEvent, error) {
	if limit <= 0 {
		limit = 10
	}
	now := time.Now().UTC().UnixMilli()
	var out []OutboxEvent
	err := r.query(ctx).Raw(r.sql(`
UPDATE sys_outbox_event
SET locked_by=?, locked_until_time=?, updated_time=?
WHERE id IN (
    SELECT id FROM sys_outbox_event
    WHERE status='pending'
      AND next_retry_time <= ?
      AND (locked_until_time=0 OR locked_until_time < ?)
    ORDER BY created_time, id
    LIMIT ?
    FOR UPDATE SKIP LOCKED
)
RETURNING id, event_id, event_type, aggregate_type, aggregate_id, payload, status, attempts, next_retry_time, last_error, locked_by, locked_until_time, created_time, updated_time`),
		worker, now+lockMs, repoTimeFromMilli(now), now, now, limit).Scan(&out).Error
	return out, err
}

func (r *OutboxRepo) ClaimOutboxByTypes(ctx context.Context, worker string, eventTypes []string, limit int, lockMs int64) ([]OutboxEvent, error) {
	if len(eventTypes) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	if r.table == "uns_collection_storage_outbox" {
		return r.claimStorageBatch(ctx, worker, limit, lockMs)
	}
	now := time.Now().UTC().UnixMilli()
	var out []OutboxEvent
	err := r.query(ctx).Raw(r.sql(`
UPDATE sys_outbox_event
SET locked_by=?, locked_until_time=?, updated_time=?
WHERE id IN (
    SELECT id FROM sys_outbox_event
    WHERE status='pending'
      AND event_type IN ?
      AND next_retry_time <= ?
      AND (locked_until_time=0 OR locked_until_time < ?)
    ORDER BY created_time, id
    LIMIT ?
    FOR UPDATE SKIP LOCKED
)
RETURNING id, event_id, event_type, aggregate_type, aggregate_id, payload, status, attempts, next_retry_time, last_error, locked_by, locked_until_time, created_time, updated_time`),
		worker, now+lockMs, repoTimeFromMilli(now), eventTypes, now, now, limit).Scan(&out).Error
	return out, err
}

func (r *OutboxRepo) MarkOutboxDone(ctx context.Context, eventID string) error {
	now := time.Now().UTC().UnixMilli()
	return r.query(ctx).Model(&OutboxEvent{}).
		Where("event_id = ?", eventID).
		Updates(touchValues(map[string]any{"status": "done", "locked_by": "", "locked_until_time": 0}, now)).Error
}

func (r *OutboxRepo) MarkOutboxRetry(ctx context.Context, eventID string, attempts int, nextRetryTime int64, lastError string) error {
	now := time.Now().UTC().UnixMilli()
	status := "pending"
	if attempts >= 5 && r.table != "uns_collection_storage_outbox" {
		status = "dead"
	}
	return r.query(ctx).Model(&OutboxEvent{}).
		Where("event_id = ?", eventID).
		Updates(touchValues(map[string]any{
			"status":            gorm.Expr("CASE WHEN event_type = ? THEN 'pending' ELSE ? END", "fleet.user.sync.requested", status),
			"attempts":          attempts,
			"next_retry_time":   nextRetryTime,
			"last_error":        lastError,
			"locked_by":         "",
			"locked_until_time": 0,
		}, now)).Error
}

type OutboxRepo struct {
	claimMu        sync.Mutex
	lastCollection string
	db             *gorm.DB
	table          string
}

func (r *OutboxRepo) query(ctx context.Context) *gorm.DB { return r.db.WithContext(ctx).Table(r.table) }
func (r *OutboxRepo) sql(s string) string                { return strings.ReplaceAll(s, "sys_outbox_event", r.table) }
func NewStorageOutboxRepo(in any) *OutboxRepo {
	return &OutboxRepo{db: GetUnsConn(in), table: "uns_collection_storage_outbox"}
}

// 已完成事件保留重试窗口后分批清理；幂等性仍由目标表的 event_id 保证。
func (r *OutboxRepo) PruneCompletedOutbox(ctx context.Context, eventTypes []string, before time.Time, limit int) (int64, error) {
	if len(eventTypes) == 0 {
		return 0, nil
	}
	if before.IsZero() || limit <= 0 || limit > 1000 {
		return 0, ErrInvalidArgument
	}
	result := r.query(ctx).Exec(r.sql(`
DELETE FROM sys_outbox_event
WHERE id IN (
    SELECT id FROM sys_outbox_event
    WHERE status = 'done' AND event_type IN ? AND updated_time < ?
    ORDER BY updated_time, id
    LIMIT ?
    FOR UPDATE SKIP LOCKED
)`), eventTypes, before, limit)
	return result.RowsAffected, result.Error
}

func NewOutboxRepo(in any) *OutboxRepo {
	return &OutboxRepo{db: GetCommonConn(in), table: "sys_outbox_event"}
}

func (r *OutboxRepo) GetOutboxEvent(ctx context.Context, eventID string) (OutboxEvent, error) {
	var event OutboxEvent
	err := r.query(ctx).Where("event_id = ?", eventID).First(&event).Error
	return event, err
}

func (r *OutboxRepo) UpdateOutboxPayload(ctx context.Context, eventID string, payload json.RawMessage) error {
	result := r.query(ctx).Model(&OutboxEvent{}).Where("event_id = ?", eventID).Update("payload", payload)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// Rotate Collection heads so an older backlog cannot starve another Collection.
// Only a contiguous eligible prefix is leased; a retry/lease blocks later events
// in the same Collection, while other Collections continue. Lease/ack semantics
// are the same as the general outbox. The cursor is only a fairness hint.
func (r *OutboxRepo) claimStorageBatch(ctx context.Context, worker string, limit int, lockMs int64) ([]OutboxEvent, error) {
	r.claimMu.Lock()
	defer r.claimMu.Unlock()
	now := time.Now().UTC().UnixMilli()
	var rows []OutboxEvent
	err := r.query(ctx).Raw(`
WITH heads AS (
 SELECT DISTINCT ON (aggregate_id) aggregate_id, id, next_retry_time, locked_until_time
 FROM uns_collection_storage_outbox WHERE status='pending' ORDER BY aggregate_id,id
), chosen AS (
 SELECT aggregate_id FROM heads WHERE next_retry_time<=? AND locked_until_time<?
 ORDER BY CASE WHEN aggregate_id>? THEN 0 ELSE 1 END,aggregate_id LIMIT 1
), candidates AS (
 SELECT o.id FROM uns_collection_storage_outbox o JOIN chosen c USING(aggregate_id)
 WHERE o.status='pending' AND o.id<COALESCE((
  SELECT min(b.id) FROM uns_collection_storage_outbox b WHERE b.aggregate_id=o.aggregate_id AND b.status='pending'
  AND (b.next_retry_time>? OR b.locked_until_time>=?)
 ),9223372036854775807)
 ORDER BY o.id LIMIT ? FOR UPDATE OF o SKIP LOCKED
)
UPDATE uns_collection_storage_outbox SET locked_by=?,locked_until_time=?,updated_time=?
WHERE id IN (SELECT id FROM candidates)
RETURNING id,event_id,event_type,aggregate_type,aggregate_id,payload,status,attempts,next_retry_time,last_error,locked_by,locked_until_time,created_time,updated_time`,
		now, now, r.lastCollection, now, now, limit, worker, now+lockMs, repoTimeFromMilli(now)).Scan(&rows).Error
	if len(rows) > 0 {
		r.lastCollection = rows[0].AggregateID
	}
	return rows, err
}

package repo

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type StreamProgramIntervalCursor struct {
	StreamID   int64  `gorm:"column:stream_id;primaryKey"`
	VersionID  int64  `gorm:"column:version_id;primaryKey"`
	EntryID    string `gorm:"column:entry_id;primaryKey"`
	IntervalMs int64  `gorm:"column:interval_ms"`
	NextDueAt  int64  `gorm:"column:next_due_at"`
	UpdatedAt  int64  `gorm:"column:updated_at"`
}

func (StreamProgramIntervalCursor) TableName() string { return "public.uns_stream_interval_cursor" }

type StreamProgramIntervalOutbox struct {
	ID        string `gorm:"column:id;primaryKey"`
	StreamID  int64  `gorm:"column:stream_id"`
	VersionID int64  `gorm:"column:version_id"`
	EntryID   string `gorm:"column:entry_id"`
	DueAt     int64  `gorm:"column:due_at"`
	Payload   string `gorm:"column:payload"`
	CreatedAt int64  `gorm:"column:created_at"`
	SentAt    int64  `gorm:"column:sent_at"`
}

func (StreamProgramIntervalOutbox) TableName() string { return "public.uns_stream_interval_outbox" }

type ProgramIntervalSchedule struct {
	StreamID, VersionID         int64
	EntryID                     string
	IntervalMs, FirstDueAt, Now int64
	MaxSlots                    int
}

// Cursor advancement and each publication intent commit in one control DB
// transaction. Publish must reuse outbox.ID and mark sent only after PubAck.
func (r *SourceStreamRepo) ScheduleProgramInterval(ctx context.Context, request ProgramIntervalSchedule) ([]StreamProgramIntervalOutbox, error) {
	items := []StreamProgramIntervalOutbox{}
	if request.StreamID <= 0 || request.VersionID <= 0 || request.EntryID == "" || len(request.EntryID) > 128 ||
		request.IntervalMs <= 0 || request.FirstDueAt <= 0 || request.Now <= 0 || request.MaxSlots < 1 || request.MaxSlots > 100 {
		return items, ErrInvalidArgument
	}
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		var stream Stream
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND deleted_at=0", request.StreamID).Take(&stream).Error; err != nil {
			return err
		}
		if stream.Model != "program" || stream.Status != "enabled" || stream.ActiveVersion != request.VersionID {
			return ErrConflict
		}
		var version StreamVersion
		if err := tx.Where("id=? AND stream_id=?", request.VersionID, request.StreamID).Take(&version).Error; err != nil {
			return err
		}
		if version.ActivatedAt <= 0 {
			return ErrConflict
		}
		manifest, err := version.ProgramV2Manifest()
		if err != nil {
			return err
		}
		found := false
		for _, entry := range manifest.Entries {
			if entry.ID == request.EntryID && entry.Kind == "interval" && int64(entry.IntervalMs) == request.IntervalMs {
				found = true
			}
		}
		if !found {
			return ErrConflict
		}
		cursor := StreamProgramIntervalCursor{StreamID: request.StreamID, VersionID: request.VersionID, EntryID: request.EntryID,
			IntervalMs: request.IntervalMs, NextDueAt: request.FirstDueAt, UpdatedAt: time.Now().UnixMilli()}
		if err := insertRuntimeIgnore(tx, &cursor).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("stream_id=? AND version_id=? AND entry_id=?", request.StreamID, request.VersionID, request.EntryID).Take(&cursor).Error; err != nil {
			return err
		}
		if cursor.IntervalMs != request.IntervalMs {
			return ErrConflict
		}
		for len(items) < request.MaxSlots && cursor.NextDueAt <= request.Now {
			if cursor.NextDueAt > math.MaxInt64-cursor.IntervalMs {
				return ErrInvalidArgument
			}
			payload, _ := json.Marshal(struct {
				StreamID  int64  `json:"streamId"`
				VersionID int64  `json:"versionId"`
				EntryID   string `json:"entryId"`
				DueAt     int64  `json:"dueAt"`
			}{request.StreamID, request.VersionID, request.EntryID, cursor.NextDueAt})
			item := StreamProgramIntervalOutbox{ID: programIdentity("program-interval-v2", request.StreamID, request.VersionID, request.EntryID, cursor.NextDueAt),
				StreamID: request.StreamID, VersionID: request.VersionID, EntryID: request.EntryID, DueAt: cursor.NextDueAt, Payload: string(payload), CreatedAt: time.Now().UnixMilli()}
			if err := insertRuntimeIgnore(tx, &item).Error; err != nil {
				return err
			}
			items = append(items, item)
			cursor.NextDueAt += cursor.IntervalMs
		}
		cursor.UpdatedAt = time.Now().UnixMilli()
		return tx.Save(&cursor).Error
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (r *SourceStreamRepo) PendingProgramIntervals(ctx context.Context, limit int) ([]StreamProgramIntervalOutbox, error) {
	if limit < 1 || limit > 200 {
		return nil, ErrInvalidArgument
	}
	items := []StreamProgramIntervalOutbox{}
	// An old/disabled version's intent is retained for history, never published
	// as a current trigger. Enabling an old version can resume its own cursor.
	err := r.db.WithContext(ctx).Model(&StreamProgramIntervalOutbox{}).
		Joins("JOIN public.program_stream_projection s ON s.id=uns_stream_interval_outbox.stream_id").
		Where("uns_stream_interval_outbox.sent_at=0 AND s.deleted_at=0 AND s.status='enabled' AND s.model='program' AND s.active_version=uns_stream_interval_outbox.version_id").
		Order("uns_stream_interval_outbox.created_at, uns_stream_interval_outbox.id").Limit(limit).Find(&items).Error
	return items, err
}

func (r *SourceStreamRepo) MarkProgramIntervalSent(ctx context.Context, id string) error {
	if id == "" || len(id) > 64 {
		return ErrInvalidArgument
	}
	q := r.db.WithContext(ctx).Model(&StreamProgramIntervalOutbox{}).Where("id=? AND sent_at=0", id).Update("sent_at", time.Now().UnixMilli())
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected == 1 {
		return nil
	}
	var item StreamProgramIntervalOutbox
	if err := r.db.WithContext(ctx).Where("id=?", id).Take(&item).Error; err != nil {
		return err
	}
	if item.SentAt <= 0 {
		return ErrConflict
	}
	return nil
}

// -1 is a distinct expired/discarded terminal intent, never a JetStream PubAck.
// Keep its original due_at/payload for audit; Pending only selects sent_at=0.
func (r *SourceStreamRepo) MarkProgramIntervalDiscarded(ctx context.Context, id string) error {
	if id == "" || len(id) > 64 {
		return ErrInvalidArgument
	}
	q := r.db.WithContext(ctx).Model(&StreamProgramIntervalOutbox{}).Where("id=? AND sent_at=0", id).Update("sent_at", -1)
	if q.Error != nil {
		return q.Error
	}
	if q.RowsAffected == 1 {
		return nil
	}
	var item StreamProgramIntervalOutbox
	if err := r.db.WithContext(ctx).Where("id=?", id).Take(&item).Error; err != nil {
		return err
	}
	if item.SentAt != -1 {
		return ErrConflict
	}
	return nil
}

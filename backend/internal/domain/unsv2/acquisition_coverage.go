package unsv2

import (
	"backend/internal/repo"
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Native subscriptions do not imply durable upstream history. In particular,
// the existing MQTT driver starts clean sessions. A failed commit is not ACKed,
// but reconnecting still cannot promise redelivery. Record uncertainty without
// fabricating a lost event count or advancing the durable acquisition cursor.
type acquisitionGap struct {
	StartedAt    int64  `json:"startedAt"`
	RecoveredAt  int64  `json:"recoveredAt"`
	Reason       string `json:"reason"`
	LossPossible bool   `json:"lossPossible"`
	LostEvents   *int64 `json:"lostEvents"`
}
type acquisitionCoverage struct {
	WatchStartedAt   int64            `json:"watchStartedAt"`
	GapCount         int64            `json:"gapCount"`
	PriorGapsFrom    int64            `json:"priorGapsFrom"`
	PriorGapsThrough int64            `json:"priorGapsThrough"`
	Gaps             []acquisitionGap `json:"gaps"`
}

func (c *acquisitionCoverage) interrupt(at int64, reason string) {
	if n := len(c.Gaps); n > 0 && c.Gaps[n-1].RecoveredAt == 0 {
		return
	}
	c.GapCount++
	c.Gaps = append(c.Gaps, acquisitionGap{StartedAt: at, Reason: reason, LossPossible: true})
	if len(c.Gaps) > 20 {
		old := c.Gaps[0]
		if c.PriorGapsFrom == 0 {
			c.PriorGapsFrom = old.StartedAt
		}
		c.PriorGapsThrough = old.RecoveredAt
		c.Gaps = c.Gaps[1:]
	}
}
func (c *acquisitionCoverage) ready(at int64) {
	if n := len(c.Gaps); n > 0 && c.Gaps[n-1].RecoveredAt == 0 {
		c.Gaps[n-1].RecoveredAt = at
	}
}

// No subscription was opened: report the bounded admission failure and the
// uncertain upstream interval. Do not overwrite a live lease or a local pause.
func (s *Service) markAcquisitionCapacityWait(ctx context.Context, row repo.UnsMeta, bindingID string) {
	waitCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_ = repo.GetUnsConn(waitCtx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock_shared(814206108)").Error; err != nil {
			return err
		}
		var source repo.UnsMeta
		if err := tx.Where("kind='source' AND id=? AND revision=? AND deleted_at=0", row.ID, row.Revision).Take(&source).Error; err != nil {
			return err
		}
		var stateRow repo.UnsMeta
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("kind='source_state' AND id=?", row.ID).Take(&stateRow).Error; err != nil {
			return err
		}
		var document struct {
			Streams map[string]acquisitionState `json:"streams"`
		}
		if err := json.Unmarshal(stateRow.Data, &document); err != nil {
			return err
		}
		state := document.Streams[bindingID]
		now := time.Now().UnixMilli()
		if state.LeaseUntil > now || state.LastError == "ACQUISITION_CONCURRENCY_LIMIT" {
			return nil
		}
		state.Status = "failed"
		state.LastError = "ACQUISITION_CONCURRENCY_LIMIT"
		state.Coverage.interrupt(now, state.LastError)
		return saveAcquisitionState(tx, row.ID, bindingID, state)
	})
}

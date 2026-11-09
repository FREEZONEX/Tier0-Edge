package repo

import (
	"context"
	"encoding/json"
	"time"

	"backend/internal/streamexec"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ProgramOutputRecoveryMaxAttempts = 8
const ProgramOutputRecoveryLease = 30 * time.Second
const programOutputRecoveryGrace = 5 * time.Second

// JSONB text adds spaces to encoded JSON. This storage-only bound does not
// raise the 8 MiB script/response budget or permit batching large results.
const MaxFrozenProgramOutputBytes = 2 * streamexec.MaxProgramDataBytes
const programRecoveryClockSQL = `floor(extract(epoch FROM clock_timestamp())*1000)::bigint`

const (
	ProgramRecoveryUnavailable    = "outputUnavailable"
	ProgramRecoveryInvalid        = "invalidFrozenOutput"
	ProgramRecoveryConflict       = "outputConflict"
	ProgramRecoveryCallerIdentity = "callerIdentityUnavailable"
)

type ProgramOutputRecoveryClaim struct {
	Run         StreamRun
	Token       string
	Attempt     int
	Quarantined bool
}

func programRecoveryTime(tx *gorm.DB) (int64, error) {
	var now int64
	err := tx.Raw("SELECT " + programRecoveryClockSQL).Scan(&now).Error
	return now, err
}

func scheduleProgramOutputRecovery(tx *gorm.DB, id string) error {
	return tx.Model(&programRunOwner{}).Where("run_id=? AND recovery_state='waiting'", id).
		Updates(map[string]any{"recovery_state": "pending", "recovery_after": gorm.Expr(programRecoveryClockSQL+"+?", programOutputRecoveryGrace.Milliseconds())}).Error
}

func completeProgramOutputRecovery(tx *gorm.DB, id string) error {
	var attempts []int
	if err := tx.Raw(`UPDATE public.uns_stream_run_owner SET recovery_state='completed',recovery_after=0,recovery_token=NULL
		WHERE run_id=? RETURNING recovery_attempts`, id).Scan(&attempts).Error; err != nil {
		return err
	}
	if len(attempts) == 1 && attempts[0] > 0 {
		return annotateProgramOutputRecovery(tx, id, "completed", attempts[0], "")
	}
	return nil
}

func annotateProgramOutputRecovery(tx *gorm.DB, id, state string, attempt int, reason string) error {
	fields := map[string]any{"state": state, "attempts": attempt, "reason": reason}
	if reason == ProgramRecoveryCallerIdentity {
		fields["repair"] = "Automatic recovery stopped because the original caller cannot be verified. Inspect the original receipt and external effects before explicitly submitting a new business intent; never automatically change its idempotency key."
	}
	data, _ := json.Marshal(fields)
	return tx.Model(&StreamRun{}).Where("id=? AND model='program'", id).
		Update("summary", gorm.Expr(`jsonb_set(summary,'{outputRecovery}',?::jsonb,true)`, string(data))).Error
}

func quarantineProgramOutputRecovery(tx *gorm.DB, id string, attempt int, reason string) error {
	if err := tx.Model(&programRunOwner{}).Where("run_id=?", id).Updates(map[string]any{
		"recovery_state": "quarantined", "recovery_after": int64(0), "recovery_token": nil}).Error; err != nil {
		return err
	}
	if err := tx.Model(&StreamTask{}).Where("id=? AND model='program' AND status IN ('pending','partial-failure')", id).Update("status", "partial-failure").Error; err != nil {
		return err
	}
	if err := tx.Model(&StreamRun{}).Where("id=? AND model='program' AND status='processing'", id).Update("status", "partial-failure").Error; err != nil {
		return err
	}
	return annotateProgramOutputRecovery(tx, id, "quarantined", attempt, reason)
}

// Run -> association is the same order as Task finalization. No execution-owner
// row or script is needed once immutable output has been frozen.
func (r *SourceStreamRepo) ClaimProgramOutputRecovery(ctx context.Context) (ProgramOutputRecoveryClaim, bool, error) {
	var result ProgramOutputRecoveryClaim
	found := false
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		now, err := programRecoveryTime(tx)
		if err != nil {
			return err
		}
		var run StreamRun
		q := tx.Table("public.uns_stream_run AS run").Select("run.*").
			Joins("JOIN public.uns_stream_run_owner binding ON binding.run_id=run.id").
			Where("binding.recovery_state IN ('pending','claimed') AND binding.recovery_after<=? AND run.model='program' AND run.status IN ('processing','partial-failure')", now).
			Order("binding.recovery_after,run.id").Limit(1).
			Clauses(clause.Locking{Strength: "UPDATE", Table: clause.Table{Name: "run"}, Options: "SKIP LOCKED"}).Find(&run)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected == 0 {
			return nil
		}
		var binding programRunOwner
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id=?", run.ID).Take(&binding).Error; err != nil {
			return err
		}
		if (binding.RecoveryState != "pending" && binding.RecoveryState != "claimed") || binding.RecoveryAfter > now {
			return nil
		}
		result.Run, result.Attempt = run, binding.RecoveryAttempts
		found = true
		if binding.RecoveryAttempts >= ProgramOutputRecoveryMaxAttempts {
			result.Quarantined = true
			return quarantineProgramOutputRecovery(tx, run.ID, binding.RecoveryAttempts, "retryExhausted")
		}
		result.Token, result.Attempt = uuid.NewString(), binding.RecoveryAttempts+1
		return tx.Model(&binding).Updates(map[string]any{"recovery_state": "claimed", "recovery_attempts": result.Attempt,
			"recovery_after": now + ProgramOutputRecoveryLease.Milliseconds(), "recovery_token": result.Token}).Error
	})
	if err != nil {
		return ProgramOutputRecoveryClaim{}, false, normalizeDBError(err)
	}
	return result, found, nil
}

func validProgramOutputClaim(claim ProgramOutputRecoveryClaim) bool {
	return claim.Run.ID != "" && len(claim.Run.ID) <= 64 && claim.Run.Model == "program" && validProgramOwner(claim.Token) &&
		claim.Attempt >= 1 && claim.Attempt <= ProgramOutputRecoveryMaxAttempts && !claim.Quarantined
}

// Only one bounded result crosses the DB boundary. Oversized/non-object JSONB
// stays in PostgreSQL and returns an empty projection, never a huge Go string.
func (r *SourceStreamRepo) LoadProgramOutputRecovery(ctx context.Context, claim ProgramOutputRecoveryClaim) (StreamTask, error) {
	var task StreamTask
	if !validProgramOutputClaim(claim) {
		return task, ErrInvalidArgument
	}
	q := r.db.WithContext(ctx).Raw(`SELECT task.id,task.source_id,task.stream_id,task.version_id,task.source_revision,task.entry_id,task.model,
		CASE WHEN jsonb_typeof(task.output)='object' AND octet_length(task.output::text)<=? THEN task.output::text ELSE '' END AS output,
		task.received_at,task.status,task.sent_at,task.created_at
		FROM public.uns_stream_task task JOIN public.uns_stream_run_owner binding ON binding.run_id=task.id
		WHERE task.id=? AND binding.recovery_state='claimed' AND binding.recovery_token=?::uuid AND binding.recovery_attempts=?
		AND task.model='program' AND task.stream_id=? AND task.version_id=? AND task.entry_id=? AND COALESCE(task.source_id,0)=?
		AND task.status IN ('pending','partial-failure')`, MaxFrozenProgramOutputBytes, claim.Run.ID, claim.Token, claim.Attempt,
		claim.Run.StreamID, claim.Run.VersionID, claim.Run.EntryID, claim.Run.SourceID).Scan(&task)
	if q.Error != nil {
		return StreamTask{}, q.Error
	}
	if q.RowsAffected == 0 {
		return StreamTask{}, ErrConflict
	}
	if task.Output == "" || task.ReceivedAt <= 0 {
		return StreamTask{}, ErrInvalidArgument
	}
	return task, nil
}

// A stale failure is only a stale observation. It cannot reschedule another
// claim or overwrite a success committed by the original caller.
func (r *SourceStreamRepo) FailProgramOutputRecovery(ctx context.Context, claim ProgramOutputRecoveryClaim, permanent bool, reason string) (bool, error) {
	if !validProgramOutputClaim(claim) || (reason != ProgramRecoveryUnavailable && reason != ProgramRecoveryInvalid && reason != ProgramRecoveryConflict && reason != ProgramRecoveryCallerIdentity) {
		return false, ErrInvalidArgument
	}
	accepted := false
	err := streamRuntimeTransaction(ctx, r.db, false, func(tx *gorm.DB) error {
		var run StreamRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", claim.Run.ID).Take(&run).Error; err != nil {
			return err
		}
		var binding programRunOwner
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("run_id=?", run.ID).Take(&binding).Error; err != nil {
			return err
		}
		if binding.RecoveryState != "claimed" || binding.RecoveryToken == nil || *binding.RecoveryToken != claim.Token || binding.RecoveryAttempts != claim.Attempt {
			return nil
		}
		if !sameProgramRunIdentity(run, claim.Run) {
			return ErrConflict
		}
		accepted = true
		if run.Status == "succeeded" {
			return completeProgramOutputRecovery(tx, run.ID)
		}
		if permanent || binding.RecoveryAttempts >= ProgramOutputRecoveryMaxAttempts {
			if !permanent {
				reason = "retryExhausted"
			}
			return quarantineProgramOutputRecovery(tx, run.ID, binding.RecoveryAttempts, reason)
		}
		delay := time.Second * time.Duration(1<<binding.RecoveryAttempts)
		if delay > time.Minute {
			delay = time.Minute
		}
		if err := tx.Model(&binding).Updates(map[string]any{"recovery_state": "pending", "recovery_after": gorm.Expr(programRecoveryClockSQL+"+?", delay.Milliseconds()), "recovery_token": nil}).Error; err != nil {
			return err
		}
		return annotateProgramOutputRecovery(tx, run.ID, "retrying", binding.RecoveryAttempts, reason)
	})
	if err != nil {
		return false, normalizeDBError(err)
	}
	return accepted, nil
}

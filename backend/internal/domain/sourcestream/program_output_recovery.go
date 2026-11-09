package sourcestream

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

const programOutputRecoveryInterval = 2 * time.Second
const programOutputRecoveryBudget = 10 * time.Second
const programOutputRecoveryBatch = 16

// Execution and protocol runtimes are deliberately absent. This loop can only
// commit one immutable stored result, including while its original owner lives.
func (s *Service) runProgramOutputRecovery(ctx context.Context) {
	ticker := time.NewTicker(programOutputRecoveryInterval)
	defer ticker.Stop()
	for {
		for i := 0; i < programOutputRecoveryBatch && ctx.Err() == nil; i++ {
			claimCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			claim, found, err := repo.NewSourceStreamRepo(claimCtx).ClaimProgramOutputRecovery(claimCtx)
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					logx.WithContext(ctx).Error("Stream frozen-output claim failed")
				}
				break
			}
			if !found {
				break
			}
			if claim.Quarantined {
				continue
			}
			opCtx, stop := context.WithTimeout(ctx, programOutputRecoveryBudget)
			err = s.recoverProgramOutput(opCtx, claim)
			stop()
			if ctx.Err() != nil {
				return
			} // Keep an interrupted claim durable until expiry.
			if err == nil {
				continue
			} // Task completion also completes its queue row.
			permanent, reason := programOutputRecoveryDisposition(err)
			finishCtx, finish := context.WithTimeout(ctx, 3*time.Second)
			accepted, finishErr := repo.NewSourceStreamRepo(finishCtx).FailProgramOutputRecovery(finishCtx, claim, permanent, reason)
			finish()
			if finishErr != nil && ctx.Err() == nil {
				logx.WithContext(ctx).Errorf("Stream frozen-output finalization failed: run=%s", claim.Run.ID)
			} else if accepted && permanent {
				logx.WithContext(ctx).Errorf("Stream frozen-output quarantined: run=%s reason=%s", claim.Run.ID, reason)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) recoverProgramOutput(ctx context.Context, claim repo.ProgramOutputRecoveryClaim) error {
	r := repo.NewSourceStreamRepo(ctx)
	task, err := r.LoadProgramOutputRecovery(ctx, claim)
	if err != nil {
		return err
	}
	var summary struct {
		UsesNATS *bool `json:"usesNATS"`
	}
	if json.Unmarshal([]byte(claim.Run.Summary), &summary) != nil || summary.UsesNATS == nil {
		return repo.ErrInvalidArgument
	}
	lineage, owned, err := r.ProgramRunLineage(ctx, task.ID)
	if err != nil {
		return err
	}
	if !owned {
		return repo.ErrInvalidArgument
	}
	version, err := r.Version(ctx, task.StreamID, task.VersionID)
	if err != nil {
		return err
	}
	manifest, err := version.ProgramV2Manifest()
	if err != nil {
		return err
	}
	for _, entry := range manifest.Entries {
		if entry.ID == task.EntryID && entry.Request != nil {
			ctx, err = s.restoreAppRequestSubject(ctx, claim.Run.Summary)
			if err != nil {
				return err
			}
		}
	}
	_, err = s.commitProgramV2Task(ctx, task, lineage, s.programSink, *summary.UsesNATS)
	return err
}

// A transient sibling must not be hidden by a permanent error on another
// target. Persist only fixed reason codes, never SQL, credentials or payloads.
func programOutputRecoveryDisposition(err error) (bool, string) {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		reason := repo.ProgramRecoveryConflict
		for _, cause := range joined.Unwrap() {
			if cause == nil {
				continue
			}
			permanent, itemReason := programOutputRecoveryDisposition(cause)
			if !permanent {
				return false, repo.ProgramRecoveryUnavailable
			}
			if itemReason != repo.ProgramRecoveryConflict {
				reason = repo.ProgramRecoveryInvalid
			}
		}
		return true, reason
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return programOutputRecoveryDisposition(wrapped.Unwrap())
	}
	switch {
	case errors.Is(err, errRequestRecoveryIdentity):
		return true, repo.ProgramRecoveryCallerIdentity
	case errors.Is(err, repo.ErrConflict):
		return true, repo.ProgramRecoveryConflict
	case errors.Is(err, repo.ErrInvalidArgument), errors.Is(err, repo.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound),
		errors.Is(err, unsmeta.ErrInvalid), errors.Is(err, unsmeta.ErrData), errors.Is(err, unsmeta.ErrSchema),
		errors.Is(err, streamexec.ErrSourceDenied), errors.Is(err, ErrForbidden):
		return true, repo.ProgramRecoveryInvalid
	default:
		return false, repo.ProgramRecoveryUnavailable
	}
}

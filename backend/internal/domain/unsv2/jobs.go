package unsv2

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"github.com/google/uuid"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type jobData struct {
	PurgeTargets []collectionPurgeTarget `json:"purgeTargets,omitempty"`
	Partial      *partialRequest         `json:"partial,omitempty"`
	Type         string                  `json:"type"`
	Digest       string                  `json:"digest"`
	Identity     string                  `json:"identity"`
	Status       string                  `json:"status"`
	OwnerID      int64                   `json:"ownerId"`
	Actor        contextx.Subject        `json:"actor"`
	Batch        Batch                   `json:"batch"`
	Plan         *plan                   `json:"plan"`
	LeaseUntil   int64                   `json:"leaseUntil"`
	LeaseToken   string                  `json:"leaseToken"`
	Attempts     int                     `json:"attempts"`
	StartedAt    int64                   `json:"startedAt,omitempty"`
	FinishedAt   int64                   `json:"finishedAt,omitempty"`
	Result       *Outcome                `json:"result,omitempty"`
	Error        *Fault                  `json:"error,omitempty"`
}

// jobExecutionOutcome separates worker completion from durable write results.
// In particular, a failed job can contain committed items or an ambiguous commit.
func jobExecutionOutcome(job jobData) (executionStatus, outcome string) {
	if job.Status == "queued" || job.Status == "running" {
		return job.Status, "pending"
	}
	if result := job.Result; result != nil {
		switch result.Status {
		case "committed", "partiallyCommitted":
			return "finished", result.Status
		case "unknown":
			return "finished", "unknown"
		case "failed":
			// Only infer no committed writes when every item has positive evidence.
			rolledBack := len(result.Results) > 0
			for _, item := range result.Results {
				if item.Status != "notExecuted" && (item.Status != "failed" || item.Error == nil || !item.Error.RolledBack) {
					rolledBack = false
				}
			}
			if rolledBack {
				return "finished", "rolledBack"
			}
		}
		return "finished", "unknown"
	}
	if job.Error != nil && job.Error.RolledBack {
		return "finished", "rolledBack"
	}
	return "finished", "unknown"
}

func (s *Service) enqueue(ctx context.Context, b Batch, p *plan, identity, digest string) (Outcome, error) {
	return s.enqueueWork(ctx, b, p, identity, digest, nil)
}

func (s *Service) enqueueWork(ctx context.Context, b Batch, p *plan, identity, digest string, partial *partialRequest) (Outcome, error) {
	if actor, ok := contextx.SubjectFrom(ctx); ok && actor.FleetTask != nil {
		return Outcome{}, problem("UNSUPPORTED_OPERATION", "remote operations require bounded synchronous execution; reduce the batch size")
	}
	var out Outcome
	err := repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,812300001))", identity).Error; err != nil {
			return err
		}
		tctx := repo.WithUnsTransaction(ctx, tx)
		if previous, found, err := s.existing(tctx, identity, digest); found || err != nil {
			out = previous
			return err
		}
		actor, _ := contextx.SubjectFrom(ctx)
		actor.ResourceKeys = nil
		actor.UIActions = nil
		data := jobData{Type: "apiBatchJob", Digest: digest, Identity: identity, Status: "queued", OwnerID: actor.UserID, Actor: actor, Batch: b, Plan: p, Partial: partial}
		raw, _ := json.Marshal(data)
		now := time.Now().UnixMilli()
		id := "api-job-" + identity
		if err := tx.Create(&repo.UnsMeta{Kind: "task", ID: id, Data: raw, Revision: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			return err
		}
		out = Outcome{Status: "accepted", JobID: id}
		return nil
	})
	return out, err
}

func (s *Service) Start(ctx context.Context) {
	ctx, s.stop = context.WithCancel(ctx)
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		acquisitionDone := make(chan struct{})
		go func() { defer close(acquisitionDone); s.runAcquisition(ctx) }()
		defer func() { <-acquisitionDone }()
		indexesDone := make(chan struct{})
		go func() { defer close(indexesDone); unsmeta.RunQueryIndexWorker(ctx) }()
		defer func() { <-indexesDone }()
		retention := time.NewTicker(time.Minute)
		defer retention.Stop()
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-retention.C:
				if err := s.purgeRecordChanges(ctx); err != nil && ctx.Err() == nil {
					logx.Error("UNS record notification retention failed")
				}
			case <-timer.C:
				if err := s.work(ctx); err != nil && ctx.Err() == nil {
					logx.Errorf("UNS batch worker: %v", err)
				}
			}
		}
	}()
}
func (s *Service) Close() {
	if s.stop != nil {
		s.stop()
		<-s.done
	}
}
func (s *Service) work(ctx context.Context) error {
	return s.workJob(ctx, "")
}

func (s *Service) workJob(ctx context.Context, jobID string) error {
	var row repo.UnsMeta
	var job jobData
	now := time.Now().UnixMilli()
	err := repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("kind='task' AND data->>'type'='apiBatchJob' AND (data->>'status'='queued' OR (data->>'status'='running' AND (data->>'leaseUntil')::bigint<?))", now)
		if jobID != "" {
			query = query.Where("id=?", jobID)
		}
		if err := query.Order("created_at,id").Take(&row).Error; err != nil {
			return err
		}
		if err := json.Unmarshal(row.Data, &job); err != nil {
			return err
		}
		job.Status = "running"
		job.Attempts++
		job.StartedAt = now
		job.LeaseToken = uuid.NewString()
		job.LeaseUntil = now + 6*60*1000
		raw, _ := json.Marshal(job)
		return tx.Model(&row).Updates(map[string]any{"data": raw, "updated_at": now, "revision": row.Revision + 1}).Error
	})
	if errors.Is(err, repo.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	actor, err := s.refresh(bounded, job.Actor)
	if err != nil {
		err = problem("FORBIDDEN", "job identity or authorization is no longer valid")
	}
	var result Outcome
	if err == nil {
		jctx := contextx.WithSubject(bounded, actor)
		if len(job.PurgeTargets) > 0 {
			result, err = s.executeCollectionPurge(jctx, row.ID, &job)
		} else if job.Partial != nil {
			result = s.executePartial(jctx, *job.Partial, job.Identity)
			result.JobID = row.ID
		} else {
			result, err = s.execute(jctx, job.Batch, job.Plan, job.Identity, job.Digest, true)
		}
	}
	// Persist terminal result with a separate short context even if execution timed out.
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	job.FinishedAt = time.Now().UnixMilli()
	job.LeaseUntil = 0
	job.Status = "succeeded"
	job.Result = &result
	if job.Partial != nil && result.Status != "committed" {
		job.Status = "failed"
	}
	if err != nil {
		if len(job.PurgeTargets) == 0 {
			job.Result = nil
		}
		job.Status = "failed"
		classified := classify(err)
		var f *Fault
		if errors.As(classified, &f) {
			job.Error = f
		} else {
			job.Error = &Fault{Code: "TEMPORARILY_UNAVAILABLE", Message: "batch execution failed", Retryable: true}
		}
		// Retry infrastructure failures without losing fixed IDs or the receipt.
		// A failed query transaction must not silently select a different target
		// on a fresh snapshot. Keep its error for the caller to inspect/replan.
		if job.Error.Retryable && job.Attempts < 3 && !hasRecordQuery(job.Batch.Items) {
			job.Status = "queued"
			job.FinishedAt = 0
		}
	}
	if len(job.PurgeTargets) > 0 {
		return savePurgeJob(finishCtx, row.ID, &job, true)
	}
	raw, _ := json.Marshal(job)
	return repo.GetUnsConn(finishCtx).Model(&repo.UnsMeta{}).Where("kind='task' AND id=? AND data->>'leaseToken'=?", row.ID, job.LeaseToken).Updates(map[string]any{"data": raw, "updated_at": time.Now().UnixMilli(), "revision": gorm.Expr("revision+1")}).Error
}

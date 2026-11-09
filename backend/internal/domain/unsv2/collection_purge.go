package unsv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const collectionPurgeLimit = 200

type collectionPurgeRequest struct {
	Kind           string   `json:"kind"`
	IDs            []string `json:"ids"`
	DryRun         bool     `json:"dryRun,omitempty"`
	IdempotencyKey string   `json:"idempotencyKey,omitempty"`
}

type collectionPurgeTarget struct {
	Kind    string `json:"kind,omitempty"`
	ID      string `json:"id"`
	Done    bool   `json:"done,omitempty"`
	Missing bool   `json:"missing,omitempty"`
	Error   *Fault `json:"error,omitempty"`
}

// purgeCollections 将明确的回收站目标与目录后代原子固定。
func (s *Service) purgeCollections(ctx context.Context, raw []byte) (any, error) {
	if actor, ok := contextx.SubjectFrom(ctx); ok && actor.FleetTask != nil {
		return nil, purgeRejected(problem("UNSUPPORTED_OPERATION", "remote operations require bounded synchronous execution"))
	}
	op, ok := FindResourceOperation("metadataPurge")
	if !ok || !ResourceAllowed(ctx, op) {
		return nil, purgeRejected(problem("FORBIDDEN", "metadata purge permission is required"))
	}
	if _, err := validateResourceRequest(op, raw); err != nil {
		return nil, purgeRejected(err)
	}
	var request collectionPurgeRequest
	if err := Decode(raw, &request); err != nil {
		return nil, purgeRejected(err)
	}
	if !metadataKind(request.Kind) || len(request.IDs) == 0 || len(request.IDs) > collectionPurgeLimit {
		return nil, purgeRejected(invalid("ids", "1 to 200 explicit namespace IDs required"))
	}
	batch := Batch{}
	seen := map[string]bool{}
	for _, id := range request.IDs {
		if !unsmeta.ValidID(id) || seen[id] {
			return nil, purgeRejected(invalid("ids", "unique valid namespace IDs required"))
		}
		seen[id] = true
		filter, _ := json.Marshal(map[string]string{"_id": id})
		change := metadataChangeEnvelope("deleteOne", metadataRequest{Kind: request.Kind, Filter: filter})
		change.OpID = id
		if err := metadataDocumentPermissions(ctx, change); err != nil {
			return nil, purgeRejected(err)
		}
		batch.Items = append(batch.Items, change)
	}
	key := request.IdempotencyKey
	if key == "" {
		key = uuid.NewString()
	}
	identity, err := requestIdentity(ctx, key)
	if err != nil {
		return nil, purgeRejected(err)
	}
	canonical, _ := json.Marshal(struct {
		Kind string
		IDs  []string
	}{request.Kind, request.IDs})
	hash := sha256.Sum256(append([]byte("metadataPurge/"), canonical...))
	digest := hex.EncodeToString(hash[:])
	jobID := "api-job-" + identity
	var out Outcome
	transactionBodyCompleted := false
	err = repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,812300001))", identity).Error; err != nil {
			return err
		}
		tctx := repo.WithUnsTransaction(ctx, tx)
		if err := tx.Exec("SELECT pg_advisory_xact_lock(814206108)").Error; err != nil {
			return err
		}
		if !request.DryRun {
			if previous, found, err := s.existing(tctx, identity, digest); found || err != nil {
				out = previous
				if err != nil {
					return err
				}
				// 同键再次请求可续做已失败的持久任务，目标与已完成项不重新选择。
				err = tx.Exec(`UPDATE public.uns_meta SET data=jsonb_set(jsonb_set(data,'{status}','"queued"'),'{attempts}','0')-'error',updated_at=? WHERE kind='task' AND id=? AND data->>'status'='failed' AND data->'purgeTargets' IS NOT NULL`, time.Now().UnixMilli(), jobID).Error
				transactionBodyCompleted = err == nil
				return err
			}
		}
		targets := make([]collectionPurgeTarget, 0, len(request.IDs))
		selected := map[string]bool{}
		for _, id := range request.IDs {
			members, err := unsmeta.NamespacePurgeMembers(tctx, id, request.Kind)
			if err != nil {
				return err
			}
			if len(members) == 0 {
				members = []repo.UnsMeta{{ID: id, StorageKind: request.Kind}}
			}
			for _, member := range members {
				kind := member.StorageKind
				if kind == "" {
					kind = member.Kind
				}
				if kind == "folder" {
					kind = "directory"
				}
				key := kind + ":" + member.ID
				if selected[key] {
					continue
				}
				selected[key] = true
				filter, _ := json.Marshal(map[string]string{"_id": member.ID})
				change := metadataChangeEnvelope("deleteOne", metadataRequest{Kind: kind, Filter: filter})
				if err := metadataDocumentPermissions(ctx, change); err != nil {
					return err
				}
				exists, err := unsmeta.FenceNamespacePurge(tctx, member.ID, kind, jobID, request.DryRun)
				if errors.Is(err, repo.ErrStorageOperation) {
					return &Fault{Code: "UNSUPPORTED_OPERATION", Reason: "REMOTE_COLLECTION_PURGE_UNSUPPORTED", ObjectID: member.ID, ObjectKind: kind, Message: "permanent deletion requires local Collection storage"}
				}
				if errors.Is(err, repo.ErrConflict) {
					reason := "NAMESPACE_NOT_ARCHIVED"
					if kind == "collection" {
						reason = "COLLECTION_NOT_ARCHIVED"
					}
					return &Fault{Code: "CONDITION_FAILED", Reason: reason, ObjectID: member.ID, ObjectKind: kind, Message: "move every target to the recycle bin before permanently deleting it"}
				}
				if err != nil {
					return err
				}
				targets = append(targets, collectionPurgeTarget{Kind: kind, ID: member.ID, Done: !exists, Missing: !exists})
			}
		}
		if request.DryRun {
			out = purgeOutcome(targets, "")
			out.Status = "validated"
			out.Summary = nil
			for i := range out.Results {
				out.Results[i].Status = "validated"
			}
			return nil
		}
		actor, _ := contextx.SubjectFrom(ctx)
		actor.ResourceKeys, actor.UIActions = nil, nil
		job := jobData{Type: "apiBatchJob", Digest: digest, Identity: identity, Status: "queued", OwnerID: actor.UserID, Actor: actor, Batch: batch, PurgeTargets: targets}
		progress := purgeOutcome(targets, jobID)
		job.Result = &progress
		data, _ := json.Marshal(job)
		now := time.Now().UnixMilli()
		if err := tx.Create(&repo.UnsMeta{Kind: "task", ID: jobID, Data: data, Revision: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			return err
		}
		out = Outcome{Status: "accepted", JobID: jobID}
		transactionBodyCompleted = true
		return nil
	})
	if err != nil {
		if transactionBodyCompleted {
			return Outcome{Status: "unknown", JobID: jobID}, &Fault{Code: "TEMPORARILY_UNAVAILABLE", Message: "purge acceptance commit could not be confirmed; retry with the same idempotency key", Retryable: request.IdempotencyKey != ""}
		}
		return nil, purgeRejected(classify(err))
	}
	return out, nil
}

func purgeOutcome(targets []collectionPurgeTarget, jobID string) Outcome {
	out := Outcome{Status: "committed", JobID: jobID, Summary: &OutcomeSummary{Total: len(targets)}, Results: make([]Result, len(targets))}
	for i, target := range targets {
		status := "notExecuted"
		if target.Done {
			status = "deleted"
			if target.Missing {
				status = "notFound"
			}
			out.Summary.Succeeded++
		} else if target.Error != nil {
			status = "failed"
			out.Summary.Failed++
			out.Status = "partiallyCommitted"
		} else {
			out.Summary.NotExecuted++
			out.Status = "partiallyCommitted"
		}
		out.Results[i] = Result{OpID: target.ID, Action: "meta.purge", Kind: target.resourceKind(), ID: target.ID, Status: status, Error: target.Error}
	}
	return out
}

// executeCollectionPurge 每一批独立提交，最终删除与完成标记同事务保存，重放不再碰已完成的 ID。
func (s *Service) executeCollectionPurge(ctx context.Context, jobID string, job *jobData) (Outcome, error) {
	var firstError error
	for index, target := range job.PurgeTargets {
		if target.Done {
			continue
		}
		for {
			finished := false
			err := repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec("SELECT pg_advisory_xact_lock(814206108)").Error; err != nil {
					return err
				}
				var current repo.UnsMeta
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("kind='task' AND id=? AND data->>'leaseToken'=?", jobID, job.LeaseToken).Take(&current).Error; err != nil {
					return err
				}
				tctx := repo.WithUnsTransaction(ctx, tx)
				filter, _ := json.Marshal(map[string]string{"_id": target.ID})
				if err := metadataDocumentPermissions(tctx, metadataChangeEnvelope("deleteOne", metadataRequest{Kind: target.resourceKind(), Filter: filter})); err != nil {
					return err
				}

				if target.resourceKind() == "collection" {
					drained, err := unsmeta.PurgeCollectionBatch(tctx, target.ID, jobID)
					if err != nil || !drained {
						return err
					}
					if err := unsmeta.FinishCollectionPurge(tctx, target.ID, jobID); err != nil {
						return err
					}
					// 授权是集合的访问入口，集合永久删除后同步撤销；引用方对象本身保留。
					if err := revokePurgedCollectionGrants(tx, target.ID); err != nil {
						return err
					}
				} else if err := unsmeta.FinishNamespacePurge(tctx, target.ID, target.resourceKind(), jobID); err != nil {
					if target.resourceKind() == "directory" && errors.Is(err, unsmeta.ErrReferenced) {
						return &Fault{Code: "CONDITION_FAILED", Reason: "NAMESPACE_CHILDREN_REMAIN", ObjectID: target.ID, ObjectKind: "directory", Message: "finish deleting the directory descendants before removing the directory"}
					}
					return err
				}
				targets := append([]collectionPurgeTarget(nil), job.PurgeTargets...)
				targets[index].Done = true
				targets[index].Error = nil
				progress := purgeOutcome(targets, jobID)
				rawTargets, _ := json.Marshal(targets)
				rawResult, _ := json.Marshal(progress)
				if err := tx.Exec("UPDATE public.uns_meta SET data=jsonb_set(jsonb_set(data,'{purgeTargets}',?::jsonb),'{result}',?::jsonb),revision=revision+1,updated_at=? WHERE kind='task' AND id=?", string(rawTargets), string(rawResult), time.Now().UnixMilli(), jobID).Error; err != nil {
					return err
				}
				finished = true
				return nil
			})
			if err != nil {
				fault, ok := classify(err).(*Fault)
				if !ok {
					fault = problem("TEMPORARILY_UNAVAILABLE", "namespace purge interrupted").(*Fault)
				}
				copy := *fault
				copy.Message = fmt.Sprintf("namespace purge interrupted for %s: %s", target.ID, copy.Message)
				job.PurgeTargets[index].Error = &copy
				if firstError == nil {
					firstError = &copy
				}
				if ctx.Err() != nil {
					return purgeOutcome(job.PurgeTargets, jobID), firstError
				}
				if saveErr := savePurgeProgress(ctx, jobID, job); saveErr != nil {
					return purgeOutcome(job.PurgeTargets, jobID), saveErr
				}
				break
			}
			if finished {
				job.PurgeTargets[index].Done = true
				job.PurgeTargets[index].Error = nil
				break
			}
		}
	}
	return purgeOutcome(job.PurgeTargets, jobID), firstError
}

func savePurgeProgress(ctx context.Context, jobID string, job *jobData) error {
	return savePurgeJob(ctx, jobID, job, false)
}

// savePurgeJob 先合并数据库已提交进度，提交响应丢失时不得用旧内存把 Done 回退。
func savePurgeJob(ctx context.Context, jobID string, job *jobData, final bool) error {
	return repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
		var row repo.UnsMeta
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("kind='task' AND id=? AND data->>'leaseToken'=?", jobID, job.LeaseToken).Take(&row).Error; err != nil {
			return err
		}
		var stored jobData
		if err := json.Unmarshal(row.Data, &stored); err != nil {
			return err
		}
		for i := range job.PurgeTargets {
			if i >= len(stored.PurgeTargets) || stored.PurgeTargets[i].ID != job.PurgeTargets[i].ID || stored.PurgeTargets[i].resourceKind() != job.PurgeTargets[i].resourceKind() {
				return repo.ErrConflict
			}
			if stored.PurgeTargets[i].Done {
				job.PurgeTargets[i] = stored.PurgeTargets[i]
			}
		}
		progress := purgeOutcome(job.PurgeTargets, jobID)
		job.Result = &progress
		if final && progress.Status == "committed" {
			job.Status = "succeeded"
			job.Error = nil
		}
		if !final {
			stored.PurgeTargets, stored.Result = job.PurgeTargets, &progress
		} else {
			stored = *job
		}
		data, _ := json.Marshal(stored)
		return tx.Model(&row).Updates(map[string]any{"data": string(data), "revision": gorm.Expr("revision+1"), "updated_at": time.Now().UnixMilli()}).Error
	})
}

// revokePurgedCollectionGrants 仅移除目标授权，保留活动和已归档 App 的其他策略字段。
func revokePurgedCollectionGrants(tx *gorm.DB, id string) error {
	filter, _ := json.Marshal([]map[string]string{{"collectionId": id}})
	return tx.Exec(`UPDATE public.uns_meta a SET data=jsonb_set(data,'{collections}',
COALESCE((SELECT jsonb_agg(g) FROM jsonb_array_elements(a.data->'collections') g WHERE g->>'collectionId'<>?), '[]'::jsonb)),
revision=revision+1,updated_at=? WHERE kind='app' AND data->'collections' @> ?::jsonb`, id, time.Now().UnixMilli(), string(filter)).Error
}

// Keep target identity explicit throughout durable job progress.
func (t collectionPurgeTarget) resourceKind() string {
	return t.Kind
}

func purgeRejected(err error) error {
	var fault *Fault
	if errors.As(err, &fault) {
		copy := *fault
		copy.RolledBack = true
		return &copy
	}
	return err
}

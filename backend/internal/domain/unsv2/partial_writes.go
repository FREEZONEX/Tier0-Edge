package unsv2

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// Partial work is only constructed by ordinary resource-array endpoints. It is
// deliberately absent from Batch: transactions cannot opt out of rollback.
type partialRequest struct {
	Operation string                     `json:"operation"`
	Fields    map[string]json.RawMessage `json:"fields"`
}

func (r partialRequest) items() []json.RawMessage {
	var items []json.RawMessage
	_ = json.Unmarshal(r.Fields["items"], &items)
	return items
}

func (r partialRequest) single(item json.RawMessage) map[string]json.RawMessage {
	fields := make(map[string]json.RawMessage, len(r.Fields))
	for key, value := range r.Fields {
		fields[key] = value
	}
	delete(fields, "allowPartial")
	delete(fields, "idempotencyKey")
	fields["mode"] = json.RawMessage(`"sync"`)
	fields["items"], _ = json.Marshal([]json.RawMessage{item})
	return fields
}

func partialDigest(value any) string {
	raw, _ := json.Marshal(value)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func (s *Service) partialResourceWrite(ctx context.Context, op ResourceOperation, fields map[string]json.RawMessage) (Outcome, error) {
	recordID := ""
	if op.Mutation == "sample.append" {
		var err error
		recordID, err = validateSampleAppendItems(fields)
		if err != nil {
			return Outcome{}, err
		}
	}
	r := partialRequest{Operation: op.Name, Fields: fields}
	items := r.items()
	mode := resourceString(fields, "mode")
	var dryRun bool
	_ = json.Unmarshal(fields["dryRun"], &dryRun)
	if mode == "sync" && len(items) > SyncItems {
		return Outcome{}, invalid("mode", "sync supports at most 200 items")
	}
	// Permissions are also rechecked when looking up the saved result.
	b := Batch{}
	seen := map[string]bool{}
	for _, raw := range items {
		var item map[string]json.RawMessage
		_ = json.Unmarshal(raw, &item)
		change := Change{Action: op.Mutation, Kind: op.Resource, ID: Identity{ID: resourceString(item, "id")}}
		change.ReturnDocument = resourceString(item, "returnDocument")
		if op.Mutation == "meta.resourcePatch" {
			change.Data, _ = json.Marshal(resourcePatch{Operation: op.Name})
		}
		objectKey := change.ID.ID
		if strings.HasPrefix(op.Name, "sourceBindings") {
			objectKey += "/" + resourceString(item, "bindingId")
		}
		if objectKey != "" {
			if seen[objectKey] {
				return Outcome{}, invalid("items", "each independent target may appear only once; use an atomic batch for dependent changes")
			}
			seen[objectKey] = true
		}
		if op.Resource == "record" || op.Resource == "sample" {
			change.Kind = ""
			change.CollectionID.ID = resourceString(fields, "collectionId")
			change.RecordID.ID = recordID
		}
		if err := mutationPermissions(ctx, change); err != nil {
			return Outcome{}, err
		}
		b.Items = append(b.Items, change)
	}
	if dryRun {
		limit := SyncItems
		if actor, ok := contextx.SubjectFrom(ctx); ok && actor.MCPRestricted {
			limit = 100
		}
		if len(items) > limit {
			return Outcome{}, invalid("items", fmt.Sprintf("partial preview supports at most %d items; preview independent subsets", limit))
		}
		bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		return s.executePartial(bounded, r, ""), nil
	}
	// Retain a recoverable job even for synchronous partial work. A timeout may
	// follow earlier commits; switching to a fresh whole-request retry is unsafe.
	key := resourceString(fields, "idempotencyKey")
	if key == "" {
		key = uuid.NewString()
	}
	identity, err := requestIdentity(ctx, key)
	if err != nil {
		return Outcome{}, err
	}
	hashFields := r.single(nil)
	delete(hashFields, "mode")
	hashFields["items"] = fields["items"]
	digest := partialDigest(partialRequest{Operation: op.Name, Fields: hashFields})
	out, err := s.enqueueWork(ctx, b, nil, identity, digest, &r)
	if err != nil {
		return Outcome{}, err
	}
	if out.JobID == "" {
		return Outcome{}, problem("IDEMPOTENCY_CONFLICT", "key belongs to another write operation")
	}
	if mode != "async" && len(items) <= SyncItems {
		bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = s.workJob(bounded, out.JobID)
		cancel()
		// The durable job, not a speculative rollback error, is the recovery handle.
		if err != nil {
			return out, nil
		}
	}
	var row repo.UnsMeta
	if err = repo.GetUnsConn(ctx).Where("kind='task' AND id=?", out.JobID).Take(&row).Error; err != nil {
		return out, nil
	}
	var job jobData
	if json.Unmarshal(row.Data, &job) != nil {
		return out, nil
	}
	if job.Result != nil {
		result := *job.Result
		result.Replayed = out.Replayed
		return result, nil
	}
	return out, nil
}

func (s *Service) executePartial(ctx context.Context, r partialRequest, identity string) Outcome {
	op, found := FindResourceOperation(r.Operation)
	items := r.items()
	out := Outcome{Results: make([]Result, len(items))}
	var dryRun bool
	_ = json.Unmarshal(r.Fields["dryRun"], &dryRun)
	stopped := !found || !arrayResource(op) || !ResourceAllowed(ctx, op)
	written := map[repo.UnsRecordLock]bool{}
	resultLimit := MaxQueryResponseBytes
	if actor, ok := contextx.SubjectFrom(ctx); ok && actor.MCPRestricted {
		resultLimit = 120 << 10
	}
	// Reserve framing and a bounded terminal diagnostic for every item. This
	// also bounds asynchronous receipts, not just the synchronous HTTP response.
	resultBytes := len(items) * 768
	for i, raw := range items {
		itemCtx := context.WithValue(ctx, recordWriteTargetsKey{}, written)
		itemCtx = context.WithValue(itemCtx, recordResultBudgetKey{}, resultLimit-resultBytes)
		var item map[string]json.RawMessage
		_ = json.Unmarshal(raw, &item)
		result := Result{OpID: fmt.Sprintf("item-%d", i), Action: op.Mutation, ID: resourceString(item, "id"), CollectionID: resourceString(r.Fields, "collectionId")}
		if stopped || ctx.Err() != nil {
			result.Status = "notExecuted"
			result.Error = partialFault(i, problem("NOT_EXECUTED", "execution stopped before this item; inspect the job and prior results"), false)
			out.Results[i] = result
			continue
		}
		batch, prepareErr := resourceArrayBatch(op, r.single(raw), false)
		if prepareErr == nil {
			prepareErr = batch.Items[0].validate()
			if prepareErr == nil {
				prepareErr = mutationPermissions(ctx, batch.Items[0])
			}
		}
		var p *plan
		if prepareErr == nil {
			p, prepareErr = newPlan(batch)
		}
		var permissionFault *Fault
		if errors.As(prepareErr, &permissionFault) && (permissionFault.Code == "FORBIDDEN" || permissionFault.Code == "UNAUTHENTICATED") {
			result.Status, result.Error = "failed", partialFault(i, prepareErr, true)
			out.Results[i], stopped = result, true
			continue
		}
		if dryRun {
			var child Outcome
			if prepareErr == nil {
				child, prepareErr = s.execute(itemCtx, batch, p, "", "", false)
			}
			if prepareErr != nil {
				result.Status, result.Error = "failed", partialFault(i, prepareErr, true)
			} else {
				result = child.Results[0]
				result.OpID, result.Status = fmt.Sprintf("item-%d", i), "validated"
			}
		} else {
			childIdentity := partialDigest([]any{"partial-item", identity, i})
			intent := []any{r.Operation, raw, result.CollectionID}
			if op.Mutation == "sample.append" {
				intent = append(intent, resourceString(r.Fields, "recordId"))
			}
			result = s.partialItem(itemCtx, batch, p, childIdentity, partialDigest(intent), result, i, prepareErr)
		}
		out.Results[i] = result
		encoded, _ := json.Marshal(result)
		if len(encoded) > 768 {
			resultBytes += len(encoded) - 768
		}
		if op.Resource == "record" && result.ID != "" && (result.Status == "committed" || result.Status == "validated") {
			written[repo.UnsRecordLock{CollectionID: result.CollectionID, RecordID: result.ID}] = true
		}
		stopped = result.Status == "unknown" || result.Error != nil && (result.Error.Code == "RESPONSE_TOO_LARGE" || result.Error.Code == "TEMPORARILY_UNAVAILABLE" || result.Error.Code == "INTERNAL_ERROR" || result.Error.Code == "UNAUTHENTICATED")
	}
	out.Summary = &OutcomeSummary{Total: len(items)}
	for _, result := range out.Results {
		switch result.Status {
		case "committed", "validated":
			out.Summary.Succeeded++
		case "failed":
			out.Summary.Failed++
		case "unknown":
			out.Summary.Unknown++
		default:
			out.Summary.NotExecuted++
		}
	}
	switch {
	case out.Summary.Unknown > 0:
		out.Status = "unknown"
	case out.Summary.Succeeded == len(items):
		out.Status = "committed"
	case out.Summary.Succeeded > 0:
		out.Status = "partiallyCommitted"
	default:
		out.Status = "failed"
	}
	for i, result := range out.Results {
		if result.Status == "unknown" || result.Status == "notExecuted" {
			out.NextIndex = &i
			if result.Status == "unknown" {
				until := i + 1
				out.UnknownUntil = &until
			}
			break
		}
	}
	if dryRun {
		out.Status = "validationFailed"
		if out.Summary.Succeeded == len(items) {
			out.Status = "validated"
		}
	}
	return out
}

func (s *Service) partialItem(ctx context.Context, batch Batch, p *plan, identity, digest string, result Result, index int, prepareErr error) Result {
	// The outer item transaction keeps its receipt lock while an inner
	// savepoint rolls back a business failure. Success AND failure are durable,
	// so a worker restart cannot re-execute an already decided item.
	bodyCompleted := false
	var options *sql.TxOptions
	if hasRecordQuery(batch.Items) {
		// execute runs inside a savepoint here; isolation must be chosen by
		// this outer transaction before any receipt or selector is read.
		options = &sql.TxOptions{Isolation: sql.LevelSerializable}
	}
	err := repo.MetadataEventTransaction(ctx, repo.GetUnsConn(ctx), func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,812300001))", identity).Error; err != nil {
			return err
		}
		tctx := repo.WithUnsTransaction(ctx, tx)
		if saved, found, err := s.existing(tctx, identity, digest); found || err != nil {
			if err != nil {
				return err
			}
			if len(saved.Results) != 1 {
				return problem("INTERNAL_ERROR", "invalid partial item receipt")
			}
			result = saved.Results[0]
			bodyCompleted = true
			return nil
		}
		var child Outcome
		itemErr := prepareErr
		if itemErr == nil {
			// The outer transaction owns the receipt. Do not create a second one
			// in execute; its savepoint still enforces all normal domain checks.
			batch.IdempotencyKey = ""
			child, itemErr = s.execute(tctx, batch, p, identity, digest, false)
		}
		if itemErr != nil {
			result.Status, result.Error = "failed", partialFault(index, itemErr, true)
			if result.Error.Code == "TEMPORARILY_UNAVAILABLE" || result.Error.Code == "INTERNAL_ERROR" {
				return itemErr
			}
		} else {
			result = child.Results[0]
			result.OpID, result.Status = fmt.Sprintf("item-%d", index), "committed"
		}
		raw, _ := json.Marshal(receipt{Type: "apiReceipt", Digest: digest, Outcome: Outcome{Status: result.Status, Results: []Result{result}}, Permissions: permissionDescriptors(batch.Items)})
		now := time.Now().UnixMilli()
		if err := tx.Create(&repo.UnsMeta{Kind: "task", ID: "api-receipt-" + identity, Data: raw, Revision: 1, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
			return err
		}
		bodyCompleted = true
		return nil
	}, options)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "40001" || pg.Code == "40P01") {
		bodyCompleted = false
	}
	if err != nil {
		result.Status = "failed"
		if bodyCompleted {
			result.Status = "unknown"
		}
		result.Error = partialFault(index, err, !bodyCompleted)
	}
	return result
}

func partialFault(index int, err error, rolledBack bool) *Fault {
	var fault *Fault
	if !errors.As(classify(err), &fault) {
		fault = &Fault{Code: "TEMPORARILY_UNAVAILABLE", Reason: "storage or execution unavailable; inspect the job before retrying"}
	}
	copy := *fault
	copy.OpID = fmt.Sprintf("item-%d", index)
	path := copy.Path
	for strings.HasPrefix(path, "/items/0/") {
		path = strings.TrimPrefix(path, "/items/0/")
	}
	path = strings.Trim(strings.TrimPrefix(path, "items[0]"), "./")
	path = strings.ReplaceAll(path, "/", ".")
	copy.Path = fmt.Sprintf("items[%d]", index)
	if path != "" {
		copy.Path += "." + path
	}
	if copy.Reason == "" {
		copy.Reason = copy.Message
	}
	if copy.Reason == "" {
		copy.Reason = "item could not be completed"
	}
	copy.Repair = "Inspect this item and business state; submit only corrected failed items as a new request"
	copy.Retryable, copy.RolledBack = false, rolledBack
	copy.RetryAdvice = &RetryAdvice{RecommendedAction: "inspectItemResultAndJobBeforeRetry"}
	copy.Candidates = nil
	return &copy
}

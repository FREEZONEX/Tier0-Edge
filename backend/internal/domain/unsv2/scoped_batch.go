package unsv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"backend/internal/domain/unsmeta"
)

// BatchScope is an API boundary, not a different transaction engine. All scopes
// retain the existing authorization, dependency planner, receipts and workers.
func BatchScope(operation string) string {
	switch operation {
	case "metaBatchExecute", "metaBatchValidate", "metaBatchStatus":
		return "meta"
	case "recordBatchExecute", "recordBatchValidate", "recordBatchStatus", "recordTransactionExecute", "recordTransactionValidate":
		return "records"
	case "sampleAppendBatch", "sampleAppendBatchStatus":
		return "samples"
	}
	return ""
}

func BatchActionAllowed(scope, action string) bool {
	switch scope {
	case "meta":
		return contains([]string{"meta.create", "meta.replace", "meta.patch", "meta.resourcePatch", "meta.copy", "meta.archive", "meta.restore", "stream.publish"}, action)
	case "records":
		return contains([]string{"record.create", "record.replace", "record.update", "record.modify", "record.delete", "record.check", "record.read", "record.query"}, action)
	case "samples":
		return action == "sample.append"
	}
	return false
}

func scopedBatchKey(ctx context.Context, scope, key string) (string, error) {
	// Validate the caller's key before hashing; use the same identity rules as
	// existing batches. Scope-specific status cannot expose another scope's job.
	if _, err := requestIdentity(ctx, key); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte("uns-scoped-batch/" + scope + "/" + key))
	return "scoped-" + hex.EncodeToString(sum[:]), nil
}

func (s *Service) ScopedBatch(ctx context.Context, operation string, batch Batch) (value any, err error) {
	if batch.IdempotencyKey == "" {
		defer func() { err = keylessWriteError(err) }()
	}
	scope := BatchScope(operation)
	if scope == "" {
		return nil, invalid("operation", "unknown scoped batch operation")
	}
	if batch.Atomic != nil {
		return nil, invalid("atomic", "scoped batches are always atomic; omit atomic")
	}
	if batch.Mode != "" && batch.Mode != "auto" && batch.Mode != "sync" && batch.Mode != "async" {
		return nil, invalid("mode", "expected auto, sync or async")
	}
	for i, item := range batch.Items {
		if !BatchActionAllowed(scope, item.Action) {
			return nil, itemFault(i, item, invalid("action", "this endpoint accepts only "+scope+" operations; cross-scope atomic batches are not supported"))
		}
	}
	if strings.HasSuffix(operation, "Validate") || operation == "recordTransactionExecute" && batch.DryRun {
		return s.ValidateTransaction(ctx, batch)
	}
	if batch.DryRun && scope != "samples" {
		return nil, invalid("dryRun", "use the matching batch/validate endpoint for previews")
	}
	if batch.IdempotencyKey == "" {
		return s.Apply(ctx, batch)
	}
	key, err := scopedBatchKey(ctx, scope, batch.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	batch.IdempotencyKey = key
	return s.Apply(ctx, batch)
}

type SampleAppendBatchInput struct {
	CollectionID   string            `json:"collectionId"`
	RecordID       string            `json:"recordId"`
	IdempotencyKey string            `json:"idempotencyKey"`
	DryRun         *bool             `json:"dryRun"`
	Mode           string            `json:"mode,omitempty"`
	Samples        []SampleBatchItem `json:"samples"`
}

type SampleBatchItem struct {
	ID      string          `json:"id,omitempty"`
	Time    int64           `json:"time"`
	Quality string          `json:"quality,omitempty"`
	Data    json.RawMessage `json:"data"`
}

func (s *Service) executeScopedBatch(ctx context.Context, operation string, raw []byte) (any, error) {
	scope := BatchScope(operation)
	if strings.HasSuffix(operation, "Status") {
		var input TransactionStatusInput
		if err := Decode(raw, &input); err != nil {
			return nil, err
		}
		key, err := scopedBatchKey(ctx, scope, input.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		return s.TransactionStatus(ctx, TransactionStatusInput{IdempotencyKey: key})
	}
	if operation == "sampleAppendBatch" {
		var input SampleAppendBatchInput
		if err := Decode(raw, &input); err != nil {
			return nil, err
		}
		if !unsmeta.ValidID(input.RecordID) {
			return nil, invalid("recordId", "a valid request-level recordId is required for all samples")
		}
		if input.DryRun == nil {
			return nil, invalid("dryRun", "explicit dryRun is required")
		}
		if len(input.Samples) == 0 || len(input.Samples) > MaxItems {
			return nil, invalid("samples", "1 to 10000 samples required")
		}
		batch := Batch{IdempotencyKey: input.IdempotencyKey, Mode: input.Mode, DryRun: *input.DryRun}
		for i, sample := range input.Samples {
			batch.Items = append(batch.Items, Change{OpID: fmt.Sprintf("sample-%d", i), Action: "sample.append", CollectionID: Identity{ID: input.CollectionID}, ID: Identity{ID: sample.ID}, RecordID: Identity{ID: input.RecordID}, Time: sample.Time, Quality: sample.Quality, Data: sample.Data})
		}
		return s.ScopedBatch(ctx, operation, batch)
	}
	var input struct {
		AssertLocks    []RecordLockAssertion `json:"assertLocks,omitempty"`
		IdempotencyKey string                `json:"idempotencyKey"`
		Mode           string                `json:"mode,omitempty"`
		DryRun         *bool                 `json:"dryRun,omitempty"`
		Items          []Change              `json:"items"`
	}
	if err := Decode(raw, &input); err != nil {
		return nil, err
	}
	preview := strings.HasSuffix(operation, "Validate")
	if operation == "recordTransactionExecute" && input.DryRun != nil {
		preview = *input.DryRun
	}
	if input.DryRun != nil && *input.DryRun != preview {
		return nil, invalid("dryRun", "use the matching validate endpoint for previews and the batch endpoint for commits")
	}
	batch := Batch{AssertLocks: input.AssertLocks, IdempotencyKey: input.IdempotencyKey, Mode: input.Mode, DryRun: preview, Items: input.Items}
	return s.ScopedBatch(ctx, operation, batch)
}

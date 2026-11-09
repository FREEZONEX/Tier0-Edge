package unsv2

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"backend/internal/repo"
	"github.com/google/uuid"
)

// RetryAdvice describes recovery, never an authorization to change business intent.
type RetryAdvice struct {
	RecommendedAction   string `json:"recommendedAction"`
	CanRetrySameRequest bool   `json:"canRetrySameRequest"`
	KeepIdempotencyKey  bool   `json:"keepIdempotencyKey"`
}

func AdviceForFault(f *Fault) RetryAdvice {
	if f != nil && f.RetryAdvice != nil {
		return *f.RetryAdvice
	}
	a := RetryAdvice{RecommendedAction: "inspectErrorAndCorrectRequest", KeepIdempotencyKey: true}
	if f == nil || f.Code == "INTERNAL_ERROR" || f.Code == "TEMPORARILY_UNAVAILABLE" {
		return RetryAdvice{RecommendedAction: "queryTransactionStatusBeforeRetry", CanRetrySameRequest: true, KeepIdempotencyKey: true}
	}
	switch f.Code {
	case "VERSION_CONFLICT", "CONDITION_FAILED", "DUPLICATE_KEY", "DUPLICATE":
		if f.RolledBack {
			a.RecommendedAction = "rereadAndReplan"
		} else {
			a.RecommendedAction = "queryTransactionStatusBeforeReplanning"
		}
	case "IDEMPOTENCY_CONFLICT":
		a.RecommendedAction = "queryOriginalTransactionStatusDoNotReplaceKey"
	case "FORBIDDEN", "UNAUTHENTICATED":
		a.RecommendedAction = "restoreAuthorizationBeforeRetry"
	case "QUERY_TIMEOUT":
		a.RecommendedAction = "narrowQueryAndRetry"
	}
	return a
}

type TransactionPlanStep struct {
	OpID            string   `json:"opId"`
	Action          string   `json:"action"`
	Kind            string   `json:"kind,omitempty"`
	CollectionID    string   `json:"collectionId,omitempty"`
	TargetID        string   `json:"targetId,omitempty"`
	DependsOn       []string `json:"dependsOn,omitempty"`
	ChangedFields   []string `json:"changedFields,omitempty"`
	ConditionFields []string `json:"conditionFields,omitempty"`
	BindingSources  []string `json:"bindingSources,omitempty"`
	ConditionCount  int      `json:"conditionCount"`
	ChecksRevision  bool     `json:"checksRevision"`
}
type TransactionValidation struct {
	Status                 string                `json:"status"`
	Plan                   []TransactionPlanStep `json:"plan"`
	ReadCollections        []string              `json:"readCollections"`
	WriteCollections       []string              `json:"writeCollections"`
	WritesCommitted        bool                  `json:"writesCommitted"`
	LocksHeldAfterResponse bool                  `json:"locksHeldAfterResponse"`
	ExecutionGuaranteed    bool                  `json:"executionGuaranteed"`
	Warnings               []string              `json:"warnings"`
}

func (s *Service) ValidateTransaction(ctx context.Context, batch Batch) (TransactionValidation, error) {
	// Never reuse the caller's final execution identity, even after it committed.
	batch.IdempotencyKey = "validation-" + uuid.NewString()
	batch.DryRun = true
	batch.Mode = "auto"
	p, err := newPlan(batch)
	if err != nil {
		return TransactionValidation{}, err
	}
	// Reuse exactly these generated IDs during rollback validation.
	batch.Items = p.Items
	if _, err = s.Apply(ctx, batch); err != nil {
		return TransactionValidation{}, err
	}
	out := TransactionValidation{Status: "validated", Plan: []TransactionPlanStep{}, ReadCollections: []string{}, WriteCollections: []string{}, Warnings: []string{
		"Validation executed with rollback; no business writes were committed and no execution idempotency key was reserved.",
		"Locks are released before this response. Data, permissions and conditions are checked again when executing; validation does not guarantee future success.",
		"Generated IDs in this plan are provisional. External device, ERP and WMS execution is outside this database transaction.",
	}}
	reads, writes := map[string]bool{}, map[string]bool{}
	for _, index := range p.Order {
		c := p.Items[index]
		step := TransactionPlanStep{OpID: c.OpID, Action: c.Action, Kind: c.Kind, CollectionID: c.CollectionID.ID, TargetID: c.ID.ID, DependsOn: c.DependsOn, ConditionCount: len(c.Conditions), ChecksRevision: c.ExpectedRevision > 0}
		for _, op := range c.Operations {
			step.ChangedFields = append(step.ChangedFields, op.Path)
		}
		for _, condition := range c.Conditions {
			step.ConditionFields = append(step.ConditionFields, condition.Field)
		}
		if len(c.Filter) > 0 {
			step.ConditionFields = filterFieldNames(c.Filter)
			step.ConditionCount = len(step.ConditionFields)
			reads[c.CollectionID.ID] = true
		}
		for _, binding := range c.Bindings {
			step.BindingSources = append(step.BindingSources, binding.Ref+"."+binding.ResultPath)
		}
		if c.Action == "record.create" || c.Action == "record.replace" || c.Action == "sample.append" {
			var fields map[string]json.RawMessage
			if json.Unmarshal(c.Data, &fields) == nil {
				for name := range fields {
					step.ChangedFields = append(step.ChangedFields, "data."+name)
				}
				sort.Strings(step.ChangedFields)
			}
		}
		if c.Action == "stream.publish" {
			step.TargetID = c.StreamID.ID
		}
		out.Plan = append(out.Plan, step)
		if c.CollectionID.ID != "" {
			if transactionAction(c.Action) {
				reads[c.CollectionID.ID] = true
			}
			if c.Action != "record.read" && c.Action != "record.check" && c.Action != "record.query" {
				writes[c.CollectionID.ID] = true
			}
		}
	}
	for id := range reads {
		out.ReadCollections = append(out.ReadCollections, id)
	}
	sort.Strings(out.ReadCollections)
	for id := range writes {
		out.WriteCollections = append(out.WriteCollections, id)
	}
	sort.Strings(out.WriteCollections)
	return out, nil
}

type TransactionStatusInput struct {
	Scope          string `json:"scope,omitempty"`
	IdempotencyKey string `json:"idempotencyKey"`
}
type TransactionStatusResult struct {
	Deliveries  []WriteDelivery `json:"deliveries,omitempty"`
	Status      string          `json:"status"`
	JobID       string          `json:"jobId,omitempty"`
	ItemCount   int             `json:"itemCount,omitempty"`
	CommittedAt int64           `json:"committedAt,omitempty"`
	RetryAdvice RetryAdvice     `json:"retryAdvice"`
}

func permissionDescriptors(items []Change) []Change {
	result := make([]Change, len(items))
	for i, c := range items {
		result[i] = Change{OpID: c.OpID, Action: c.Action, Kind: c.Kind, ID: c.ID, CollectionID: c.CollectionID, RecordID: c.RecordID, StreamID: c.StreamID, ManagedBy: c.ManagedBy, ReturnDocument: c.ReturnDocument}
		if c.Action == "meta.resourcePatch" {
			var patch resourcePatch
			if json.Unmarshal(c.Data, &patch) == nil {
				result[i].Data, _ = json.Marshal(map[string]string{"operation": patch.Operation})
			}
		}
	}
	return result
}

func (s *Service) TransactionStatus(ctx context.Context, input TransactionStatusInput) (TransactionStatusResult, error) {
	key := input.IdempotencyKey
	if input.Scope != "" {
		if !contains([]string{"meta", "records", "samples"}, input.Scope) {
			return TransactionStatusResult{}, invalid("scope", "unknown receipt scope")
		}
		var err error
		key, err = scopedBatchKey(ctx, input.Scope, key)
		if err != nil {
			return TransactionStatusResult{}, err
		}
	}
	identity, err := requestIdentity(ctx, key)
	if err != nil {
		return TransactionStatusResult{}, err
	}
	unknown := TransactionStatusResult{Status: "unknown", RetryAdvice: RetryAdvice{RecommendedAction: "retryOriginalRequestWithSameKey", CanRetrySameRequest: true, KeepIdempotencyKey: true}}
	var row repo.UnsMeta
	err = repo.GetUnsConn(ctx).Where("kind='task' AND id=?", "api-receipt-"+identity).Take(&row).Error
	if err == nil {
		var saved receipt
		if json.Unmarshal(row.Data, &saved) != nil {
			return unknown, problem("INTERNAL_ERROR", "invalid saved receipt")
		}
		// Old receipts lack authorization evidence. Never expose them using key possession alone.
		if len(saved.Permissions) == 0 {
			return unknown, problem("FORBIDDEN", "receipt has no permission descriptor; retry the original request with the same key")
		}
		for _, c := range saved.Permissions {
			if err := mutationPermissions(ctx, c); err != nil {
				return unknown, err
			}
		}
		deliveries, err := writeDeliveryStatuses(ctx, saved.Outcome.Results)
		if err != nil {
			return unknown, err
		}
		return TransactionStatusResult{Status: saved.Outcome.Status, Deliveries: deliveries, ItemCount: len(saved.Permissions), CommittedAt: row.CreatedAt, RetryAdvice: RetryAdvice{RecommendedAction: "useCommittedOutcomeDoNotExecuteAgain", KeepIdempotencyKey: true}}, nil
	}
	if !errors.Is(err, repo.ErrNotFound) {
		return unknown, err
	}
	jobID := "api-job-" + identity
	jobs, err := s.jobs(ctx, Query{JobIDs: []string{jobID}})
	if errors.Is(err, repo.ErrNotFound) {
		return unknown, nil
	}
	if err != nil {
		return unknown, err
	}
	list, ok := jobs["list"].([]map[string]any)
	if !ok || len(list) != 1 {
		return unknown, problem("INTERNAL_ERROR", "invalid job status")
	}
	status, _ := list[0]["status"].(string)
	action := "pollTransactionStatus"
	if status == "succeeded" {
		action = "useCommittedOutcomeDoNotExecuteAgain"
	}
	if strings.EqualFold(status, "failed") {
		action = "inspectJobFailureBeforeReplanning"
	}
	return TransactionStatusResult{Status: status, JobID: jobID, RetryAdvice: RetryAdvice{RecommendedAction: action, KeepIdempotencyKey: true}}, nil
}

func writeDeliveryStatuses(ctx context.Context, results []Result) ([]WriteDelivery, error) {
	values := []WriteDelivery{}
	for _, result := range results {
		if result.Delivery == nil {
			continue
		}
		delivery := *result.Delivery
		if len(delivery.EventIDs) == 0 {
			values = append(values, delivery)
			continue
		}
		var rows []repo.OutboxEvent
		if err := repo.GetUnsConn(ctx).Table("uns_collection_storage_outbox").Where("event_id IN ?", delivery.EventIDs).Find(&rows).Error; err != nil {
			return nil, err
		}
		delivery.Pending = len(delivery.EventIDs)
		delivery.Failed = 0
		for _, row := range rows {
			if row.Status == "done" {
				delivery.Pending--
			} else if row.LastError != "" {
				delivery.Failed++
			}
		}
		delivery.CenterCommitted = delivery.Pending == 0
		switch {
		case delivery.CenterCommitted:
			delivery.State = "centerCommitted"
		case delivery.Failed > 0:
			delivery.State = "failed"
		default:
			delivery.State = "pendingUpload"
		}
		values = append(values, delivery)
	}
	return values, nil
}

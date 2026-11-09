package unsv2

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"backend/internal/domain/unsmeta"
)

func metadataArrayResource(operation ResourceOperation) bool {
	return !operation.Runtime && (strings.HasPrefix(operation.Mutation, "meta.") || operation.Mutation == "stream.publish")
}
func arrayResource(operation ResourceOperation) bool {
	return metadataArrayResource(operation) || operation.Resource == "record" && contains([]string{"record.create", "record.update", "record.replace", "record.delete"}, operation.Mutation) || operation.Mutation == "sample.append"
}

// Legacy single-object HTTP requests remain readable, but discovery, MCP and
// generated clients advertise only the canonical items array contract.
func normalizeResourceWrite(operation ResourceOperation, raw []byte) ([]byte, bool, string, error) {
	if operation.Mutation == "" || operation.Runtime {
		return raw, false, "", nil
	}
	var fields map[string]json.RawMessage
	if err := Decode(raw, &fields); err != nil {
		return nil, false, "", err
	}
	if operation.Mutation == "sample.append" {
		if _, err := sampleAppendRecordID(fields); err != nil {
			return nil, false, "", err
		}
	}
	var legacyKey string
	if value, ok := fields["idempotencyKey"]; ok {
		if err := json.Unmarshal(value, &legacyKey); err != nil {
			return nil, false, "", invalid("idempotencyKey", "optional key must be a string")
		}
		delete(fields, "idempotencyKey")
	}
	legacySingle := false
	if arrayResource(operation) {
		if _, ok := fields["items"]; !ok {
			legacySingle = true
			item := map[string]json.RawMessage{}
			for key, value := range fields {
				if (key != "collectionId" || metadataArrayResource(operation)) && (key != "recordId" || operation.Mutation != "sample.append") && key != "dryRun" && key != "mode" && key != "allowPartial" {
					item[key] = value
					delete(fields, key)
				}
			}
			fields["items"], _ = json.Marshal([]any{item})
		}
	}
	normalized, err := json.Marshal(fields)
	return normalized, legacySingle, legacyKey, err
}

func (s *Service) arrayResourceWrite(ctx context.Context, operation ResourceOperation, fields map[string]json.RawMessage, legacySingle bool) (any, error) {
	var partial bool
	_ = json.Unmarshal(fields["allowPartial"], &partial)
	if partial {
		return s.partialResourceWrite(ctx, operation, fields)
	}
	batch, err := resourceArrayBatch(operation, fields, legacySingle)
	if err != nil {
		return nil, err
	}
	out, err := s.Apply(ctx, batch)
	if err != nil {
		return nil, err
	}
	if legacySingle {
		response := map[string]any{"status": out.Status, "replayed": out.Replayed}
		if out.JobID != "" {
			response["jobId"] = out.JobID
		}
		if len(out.Results) > 0 {
			response["result"] = out.Results[0]
		}
		return response, nil
	}
	return out, nil
}

func resourceArrayBatch(operation ResourceOperation, fields map[string]json.RawMessage, legacySingle bool) (Batch, error) {
	recordID := ""
	if operation.Mutation == "sample.append" {
		var err error
		recordID, err = validateSampleAppendItems(fields)
		if err != nil {
			return Batch{}, err
		}
	}
	var items []struct {
		ID               string           `json:"id"`
		ExpectedRevision int64            `json:"expectedRevision"`
		Data             json.RawMessage  `json:"data"`
		Operations       []FieldOperation `json:"operations"`
		Conditions       []RecordFilter   `json:"conditions"`
		Filter           json.RawMessage  `json:"filter"`
		Selection        string           `json:"selection"`
		Sort             []RecordOrder    `json:"sort"`
		ReturnDocument   string           `json:"returnDocument"`
		Time             int64            `json:"time"`
		Quality          string           `json:"quality"`
	}
	if err := json.Unmarshal(fields["items"], &items); err != nil {
		return Batch{}, invalid("items", "invalid write items")
	}
	batch := Batch{IdempotencyKey: resourceString(fields, "idempotencyKey"), Mode: resourceString(fields, "mode")}
	_ = json.Unmarshal(fields["dryRun"], &batch.DryRun)
	if metadataArrayResource(operation) {
		var metadataItems []map[string]json.RawMessage
		if err := json.Unmarshal(fields["items"], &metadataItems); err != nil {
			return Batch{}, invalid("items", "invalid metadata items")
		}
		for i, item := range metadataItems {
			change, err := resourceChange(operation, item)
			if err != nil {
				return Batch{}, itemFault(i, change, err)
			}
			if !legacySingle {
				change.OpID = fmt.Sprintf("item-%d", i)
			}
			batch.Items = append(batch.Items, change)
		}
	} else {
		for i, item := range items {
			opID := fmt.Sprintf("item-%d", i)
			if legacySingle {
				opID = operation.Name
			}
			batch.Items = append(batch.Items, Change{OpID: opID, Action: operation.Mutation, CollectionID: Identity{ID: resourceString(fields, "collectionId")}, ID: Identity{ID: item.ID}, RecordID: Identity{ID: recordID}, ExpectedRevision: item.ExpectedRevision, Data: item.Data, Operations: item.Operations, Conditions: item.Conditions, Filter: item.Filter, Selection: item.Selection, Sort: item.Sort, ReturnDocument: item.ReturnDocument, Time: item.Time, Quality: item.Quality})
		}
	}
	return batch, nil
}

func sampleAppendRecordID(fields map[string]json.RawMessage) (string, error) {
	var recordID string
	if json.Unmarshal(fields["recordId"], &recordID) != nil || !unsmeta.ValidID(recordID) {
		return "", invalid("recordId", "a valid request-level recordId is required for all samples")
	}
	return recordID, nil
}

// Validate the fixed target before any partial job is queued. Item-level
// identities are rejected even when they equal the outer recordId: there is
// only one target rule and no silent fallback or override.
func validateSampleAppendItems(fields map[string]json.RawMessage) (string, error) {
	recordID, err := sampleAppendRecordID(fields)
	if err != nil {
		return "", err
	}
	var items []SampleBatchItem
	if err := Decode(fields["items"], &items); err != nil {
		return "", transactionPath(err, "items/")
	}
	if len(items) < 1 || len(items) > MaxItems {
		return "", invalid("items", "1 to 10000 samples required")
	}
	return recordID, nil
}

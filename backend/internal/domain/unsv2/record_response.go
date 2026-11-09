package unsv2

import (
	"encoding/json"
	"errors"
	"strconv"
)

// 查询语言使用平铺字段，普通资源响应统一还原为持久化文档结构。
func recordQueryDocument(value map[string]any) *RecordDocument {
	metadata, _ := value["_meta"].(map[string]any)
	data := make(map[string]any, len(value))
	for key, item := range value {
		if key != "_id" && key != "_meta" {
			data[key] = item
		}
	}
	raw, _ := json.Marshal(data)
	doc := &RecordDocument{Data: raw}
	doc.ID, _ = value["_id"].(string)
	doc.CollectionID, _ = metadata["collectionId"].(string)
	if id, ok := metadata["sourceId"].(string); ok {
		doc.SourceID = &id
	}
	doc.Revision = recordResponseInteger(metadata["revision"])
	doc.CreatedAt = recordResponseInteger(metadata["createdAt"])
	doc.UpdatedAt = recordResponseInteger(metadata["updatedAt"])
	if value := metadata["sourceTime"]; value != nil {
		at := recordResponseInteger(value)
		doc.SourceTime = &at
	}
	if lock, ok := metadata["lock"].(map[string]any); ok {
		doc.Lock = &RecordLeaseView{ExpiresAt: recordResponseInteger(lock["expiresAt"]), Fence: recordResponseInteger(lock["fence"]), Since: recordResponseInteger(lock["since"])}
		doc.Lock.Label, _ = lock["label"].(string)
	}
	return doc
}
func recordResponseInteger(value any) int64 {
	switch v := value.(type) {
	case json.Number:
		n, _ := v.Int64()
		return n
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
}

func recordSingleWriteResponse(action string, upsert bool, returnDocument string, out Outcome) map[string]any {
	result := map[string]any{"status": out.Status, "replayed": out.Replayed}
	var row Result
	if len(out.Results) > 0 {
		row = out.Results[0]
	}
	if row.Delivery != nil {
		result["delivery"] = row.Delivery
	}
	if row.Status == "pendingUpload" {
		result["status"] = "accepted"
		result["deliveryState"] = "pendingUpload"
		result["acceptedId"] = row.ID
		result["centerCommitted"] = false
		return result
	}
	if returnDocument != "" {
		result["value"] = row.Document
	}
	if action == "insertOne" {
		result["insertedId"] = row.ID
		if row.Document != nil {
			result["value"] = row.Document
		}
		return result
	}
	if upsert && row.Status == "created" {
		result["upsertedId"] = row.ID
	}
	switch action {
	case "findOneAndUpdate", "findOneAndReplace", "findOneAndDelete":
		result["value"] = row.Document
	case "deleteOne":
		count := 0
		if row.Status == "deleted" {
			count = 1
		}
		result["deletedCount"] = count
	default:
		matched, modified := 0, 0
		if row.Status == "updated" {
			matched, modified = 1, 1
		} else if row.Status == "unchanged" {
			matched = 1
		}
		result["matchedCount"], result["modifiedCount"] = matched, modified
	}
	return result
}

func recordBatchResponse(action string, value any) any {
	input, ok := value.(map[string]any)
	if !ok {
		return value
	}
	result := map[string]any{}
	for _, key := range []string{"status", "replayed", "writeErrors", "hasMore", "resumeAfter", "nextIndex", "unknownUntil"} {
		if v, ok := input[key]; ok {
			result[key] = v
		}
	}
	if action == "insertMany" || action == "bulkWrite" {
		delete(result, "resumeAfter")
	} else {
		if _, unknown := result["unknownUntil"]; unknown {
			result["outcomeUnknown"] = true
			delete(result, "resumeAfter")
		}
		delete(result, "nextIndex")
		delete(result, "unknownUntil")
	}
	keys := []string{"matchedCount", "modifiedCount", "deletedCount", "insertedCount", "upsertedCount", "insertedIds", "upsertedIds"}
	switch action {
	case "insertMany":
		keys = []string{"insertedCount", "insertedIds"}
	case "updateMany":
		keys = []string{"matchedCount", "modifiedCount"}
	case "deleteMany":
		keys = []string{"deletedCount"}
	}
	for _, key := range keys {
		if v, ok := input[key]; ok {
			result[key] = v
		}
	}
	return result
}

func recordIndexedIDs(out Outcome, items []Change) (map[string]string, map[string]string) {
	positions := map[string]int{}
	upserts := map[string]bool{}
	for i, item := range items {
		positions[item.OpID] = i
		upserts[item.OpID] = item.Upsert
	}
	inserted, updated := map[string]string{}, map[string]string{}
	for _, row := range out.Results {
		if row.Status != "created" {
			continue
		}
		key := strconv.Itoa(positions[row.OpID])
		if upserts[row.OpID] {
			updated[key] = row.ID
		} else {
			inserted[key] = row.ID
		}
	}
	return inserted, updated
}

// Preserve cumulative progress in the same shape as the successful write response.
func recordBatchFault(action string, err error) error {
	var fault *Fault
	if !errors.As(err, &fault) || fault.Data == nil {
		return err
	}
	copy := *fault
	copy.Data = recordBatchResponse(action, fault.Data)
	if action != "insertMany" && action != "bulkWrite" {
		copy.Repair = "Inspect cumulative counts. Retry the unchanged request with the same idempotency key. Without a key, inspect business state before retrying; outcomeUnknown forbids resuming by record ID."
	}
	return &copy
}

func partialBatchFault(data map[string]any, err error, mayHaveCommitted bool, errorsField string) error {
	var fault *Fault
	if !errors.As(classify(err), &fault) {
		fault = problem("TEMPORARILY_UNAVAILABLE", "batch execution interrupted; inspect cumulative progress").(*Fault)
	}
	copy := *fault
	copy.Data, copy.Retryable, copy.RolledBack = data, false, !mayHaveCommitted
	copy.Repair = "Operations below nextIndex that are not listed in " + errorsField + " committed. If unknownUntil is present, [nextIndex,unknownUntil) has an unknown outcome: retry the unchanged request with the same idempotency key, or inspect business state before continuing. Without unknownUntil, remaining operations start at nextIndex."
	copy.RetryAdvice = &RetryAdvice{RecommendedAction: "inspectCumulativeProgressAndRetrySameIntent", CanRetrySameRequest: true, KeepIdempotencyKey: true}
	return &copy
}

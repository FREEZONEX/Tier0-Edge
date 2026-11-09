package unsv2

import (
	"context"
	"encoding/json"
	"strings"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/domain/unsquery"
	"backend/internal/repo"
)

func storageRead(ctx context.Context, id, operation string, body any) (map[string]any, bool, error) {
	raw, routed, err := repo.ReadCollectionStorage(ctx, id, operation, body)
	var result map[string]any
	if routed && err == nil {
		err = decodeNumberJSON(raw, &result)
	}
	return result, routed, err
}

// CollectionStorageRead is called only after verifying the exact node/target
// grant. It exposes bounded reads of that target, never arbitrary peer queries.
func (s *Service) CollectionStorageRead(ctx context.Context, id, operation string, body json.RawMessage) (any, error) {
	ctx = contextx.WithSubject(ctx, contextx.Subject{ResourceKeys: map[string]struct{}{"uns.read": {}}})
	if err := repo.RequireLocalCollection(ctx, id); err != nil {
		return nil, err
	}
	switch operation {
	case "records":
		var r struct {
			Search string `json:"search"`
			Page   int    `json:"page"`
			Size   int    `json:"size"`
		}
		if err := Decode(body, &r); err != nil {
			return nil, err
		}
		return s.metadata.Records(ctx, id, r.Search, r.Page, r.Size)
	case "samples":
		var r struct {
			RecordID string `json:"recordId"`
			Start    int64  `json:"start"`
			End      int64  `json:"end"`
			Page     int    `json:"page"`
			Size     int    `json:"size"`
		}
		if err := Decode(body, &r); err != nil {
			return nil, err
		}
		return s.metadata.Samples(ctx, id, r.RecordID, r.Start, r.End, r.Page, r.Size)
	case "programQuery":
		var r struct {
			Options             unsquery.ProgramOptions `json:"options"`
			ResourceKey         string                  `json:"resourceKey"`
			LogicalCollectionID string                  `json:"logicalCollectionId"`
		}
		if err := Decode(body, &r); err != nil {
			return nil, err
		}
		if !unsmeta.ValidID(r.LogicalCollectionID) || len(r.ResourceKey) > 1024 {
			return nil, repo.ErrInvalidArgument
		}
		r.Options.ResourceKey = r.ResourceKey
		r.Options.StorageCursorCollectionID = r.LogicalCollectionID
		return unsquery.QueryProgramCollection(ctx, id, r.Options)
	case "program":
		var r unsmeta.ProgramReadOptions
		if err := Decode(body, &r); err != nil {
			return nil, err
		}
		return unsmeta.ReadProgramCollection(ctx, id, r)
	case "query.records", "query.samples", "query.collections":
		var q Query
		if err := Decode(body, &q); err != nil {
			return nil, err
		}
		if q.Relational != nil || q.TopicID != "" || len(q.Targets) != 0 {
			return nil, repo.ErrStorageOperation
		}
		q.CollectionID = id
		return s.query(ctx, strings.TrimPrefix(operation, "query."), q)
	case "record.get", "record.find", "record.count", "record.distinct", "record.aggregate":
		var q novaRecordRequest
		if err := Decode(body, &q); err != nil {
			return nil, err
		}
		if !storagePipeline(q.Pipeline) {
			return nil, repo.ErrStorageOperation
		}
		q.CollectionID = id
		return s.novaRecordRead(ctx, strings.TrimPrefix(operation, "record."), q)
	case "sample.find", "sample.latest", "sample.history", "sample.buckets", "sample.aggregate":
		var q novaSampleRequest
		if err := Decode(body, &q); err != nil {
			return nil, err
		}
		if !storagePipeline(q.Pipeline) {
			return nil, repo.ErrStorageOperation
		}
		q.CollectionID = id
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(body, &fields); err != nil {
			return nil, err
		}
		fields["collectionId"], _ = json.Marshal(id)
		raw, _ := json.Marshal(fields)
		return s.novaSampleOperation(ctx, operation, raw)
	default:
		return nil, repo.ErrStorageOperation
	}
}

// These stages cannot introduce a second Collection. Cross-collection operators
// are rejected even when nested; no peer token authorizes joins or unions.
func storagePipeline(stages []map[string]json.RawMessage) bool {
	allowed := map[string]bool{"$match": true, "$sort": true, "$project": true, "$limit": true, "$skip": true, "$count": true, "$group": true, "$unwind": true, "$set": true, "$unset": true, "$addFields": true, "$replaceWith": true, "$replaceRoot": true, "$bucket": true, "$bucketAuto": true}
	for _, stage := range stages {
		if len(stage) != 1 {
			return false
		}
		for key := range stage {
			if !allowed[key] {
				return false
			}
		}
	}
	return true
}

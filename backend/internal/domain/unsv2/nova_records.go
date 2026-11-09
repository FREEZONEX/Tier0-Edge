package unsv2

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	docquery "backend/internal/domain/document/query"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"gorm.io/gorm"
)

// Collection document actions retain the platform transaction and authorization
// boundary. They use the Nova document language rather than inventing a second
// set of update operators for business Apps.
type novaRecordRequest struct {
	AssertLocks      []RecordLockAssertion        `json:"assertLocks,omitempty"`
	IDs              []string                     `json:"ids,omitempty"`
	LockToken        string                       `json:"lockToken,omitempty"`
	LockTokens       map[string]string            `json:"lockTokens,omitempty"`
	Release          bool                         `json:"release,omitempty"`
	Page             *PipelinePage                `json:"page,omitempty"`
	Op               string                       `json:"op,omitempty"`
	CollectionID     string                       `json:"collectionId"`
	ID               string                       `json:"id,omitempty"`
	Data             json.RawMessage              `json:"data,omitempty"`
	Filter           json.RawMessage              `json:"filter,omitempty"`
	Sort             []RecordOrder                `json:"sort,omitempty"`
	Projection       json.RawMessage              `json:"projection,omitempty"`
	Pipeline         []map[string]json.RawMessage `json:"pipeline,omitempty"`
	Limit            int                          `json:"limit,omitempty"`
	Cursor           string                       `json:"cursor,omitempty"`
	Field            string                       `json:"field,omitempty"`
	Update           json.RawMessage              `json:"update,omitempty"`
	Replacement      json.RawMessage              `json:"replacement,omitempty"`
	ArrayFilters     []any                        `json:"arrayFilters,omitempty"`
	Upsert           bool                         `json:"upsert,omitempty"`
	ExpectedRevision int64                        `json:"expectedRevision,omitempty"`
	ReturnDocument   string                       `json:"returnDocument,omitempty"`
	Documents        []novaInsertDocument         `json:"documents,omitempty"`
	Operations       []map[string]json.RawMessage `json:"operations,omitempty"`
	Ordered          *bool                        `json:"ordered,omitempty"`
	Atomic           bool                         `json:"atomic,omitempty"`
	MaxCount         int                          `json:"maxCount,omitempty"`
	MaxDocs          int                          `json:"maxDocs,omitempty"`
	ResumeAfter      string                       `json:"resumeAfter,omitempty"`
	IdempotencyKey   string                       `json:"idempotencyKey,omitempty"`
	DryRun           bool                         `json:"dryRun,omitempty"`
}

type novaInsertDocument struct {
	ID   string          `json:"id,omitempty"`
	Data json.RawMessage `json:"data"`
}

func (s *Service) novaRecordOperation(ctx context.Context, action string, raw []byte) (any, error) {
	if action == "lock" || action == "renew" || action == "unlock" {
		return s.recordLeaseOperation(ctx, action, raw)
	}
	if strings.HasPrefix(action, "sample.") {
		return s.novaSampleOperation(ctx, action, raw)
	}
	var req novaRecordRequest
	if err := Decode(raw, &req); err != nil {
		return nil, err
	}
	if !unsmeta.ValidID(req.CollectionID) {
		return nil, invalid("collectionId", "valid Collection ID required")
	}
	if len(req.Filter) == 0 {
		req.Filter = json.RawMessage(`{}`)
	}
	if action == "restore" {
		return s.recordRestore(ctx, req)
	}
	permission := "write"
	if contains([]string{"find", "get", "count", "distinct", "aggregate", "explain"}, action) {
		permission = "read"
	} else if strings.Contains(action, "Delete") || strings.HasPrefix(action, "delete") {
		permission = "delete"
	}
	if err := authorizeCollection(ctx, permission, req.CollectionID); err != nil {
		return nil, err
	}
	if contains([]string{"find", "get", "count", "distinct", "aggregate", "explain"}, action) {
		return s.novaRecordRead(ctx, action, req)
	}
	if action != "insertOne" {
		if err := repo.RequireLocalCollection(ctx, req.CollectionID); err != nil {
			return nil, classify(err)
		}
	}
	if action == "updateMany" || action == "deleteMany" {
		return s.novaRecordMany(ctx, action, req)
	}
	if action == "insertMany" || action == "bulkWrite" {
		return s.novaRecordBulk(ctx, action, req)
	}
	change, err := novaRecordChange(action, req)
	if err != nil {
		return nil, err
	}
	if len(req.Projection) > 0 && change.ReturnDocument == "" {
		return nil, invalid("projection", singleWriteProjectionMessage)
	}
	projection, err := recordWriteProjection(ctx, req)
	if err != nil {
		return nil, err
	}
	out, err := s.Apply(ctx, Batch{AssertLocks: req.AssertLocks, IdempotencyKey: req.IdempotencyKey, Mode: "sync", DryRun: req.DryRun, Items: []Change{change}})
	if err != nil {
		return nil, err
	}
	if projection != nil {
		for i := range out.Results {
			if doc := out.Results[i].Document; doc != nil {
				var data map[string]any
				_ = decodeNumberJSON(doc.Data, &data)
				doc.Data, _ = json.Marshal(projection.Apply(data))
			}
		}
	}
	return recordSingleWriteResponse(action, req.Upsert, change.ReturnDocument, out), nil
}

func recordWriteProjection(ctx context.Context, r novaRecordRequest) (*docquery.Projection, error) {
	if len(r.Projection) == 0 {
		return nil, nil
	}
	var value any
	if err := decodeNumberJSON(r.Projection, &value); err != nil {
		return nil, invalid("projection", "object required")
	}
	p, err := docquery.ParseProjection(value)
	if err != nil {
		return nil, invalid("projection", err.Error())
	}
	if p == nil {
		return nil, nil
	}
	schema, err := pipelineSchema(ctx, "records", r.CollectionID)
	if err != nil {
		return nil, err
	}
	c := pipelineCompiler{ctx: ctx}
	for _, field := range p.Paths {
		if _, err := c.field("base:r", field.Raw, schema); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func novaRecordChange(action string, r novaRecordRequest) (Change, error) {
	c := Change{OpID: "document", CollectionID: Identity{ID: r.CollectionID}, Filter: r.Filter, Selection: "first", Sort: r.Sort, Update: r.Update, ArrayFilters: r.ArrayFilters, Upsert: r.Upsert, ExpectedRevision: r.ExpectedRevision}
	c.LockToken, c.Release = r.LockToken, r.Release
	c.LockTokens = r.LockTokens
	switch action {
	case "insertOne":
		c.Action = "record.create"
		c.ID = Identity{ID: r.ID}
		c.Data = r.Data
		c.Filter = nil
		c.Selection = ""
		c.Sort = nil
	case "updateOne", "findOneAndUpdate":
		c.Action = "record.update"
	case "replaceOne", "findOneAndReplace":
		c.Action = "record.replace"
		c.Data = r.Replacement
	case "deleteOne", "findOneAndDelete":
		c.Action = "record.delete"
	default:
		return c, invalid("operation", "unsupported document action")
	}
	var err error
	c.ReturnDocument, err = singleWriteReturnDocument(action, r.ReturnDocument)
	return c, err
}

func (s *Service) novaRecordRead(ctx context.Context, action string, r novaRecordRequest) (any, error) {
	if out, routed, err := storageRead(ctx, r.CollectionID, "record."+action, r); routed || err != nil {
		return out, classify(err)
	}
	if action == "explain" {
		return s.recordExplain(ctx, r)
	}
	if action == "get" {
		return s.recordGet(ctx, r)
	}
	query, shape, err := recordReadQuery(ctx, action, r)
	if err != nil {
		return nil, err
	}
	out, err := s.DataQuery(ctx, "records", DataQueries{Queries: []DataQuery{query}})
	if err != nil {
		return nil, err
	}
	result := out.(map[string]any)["results"].([]map[string]any)[0]
	return shape(result), nil
}

func recordReadQuery(ctx context.Context, action string, r novaRecordRequest) (DataQuery, func(map[string]any) any, error) {
	stages := []map[string]json.RawMessage{{"$match": r.Filter}}
	if len(r.Sort) > 0 {
		v, _ := json.Marshal(r.Sort)
		stages = append(stages, map[string]json.RawMessage{"$sort": v})
	}
	var projection *docquery.Projection
	if len(r.Projection) > 0 {
		var value any
		if err := decodeNumberJSON(r.Projection, &value); err != nil {
			return DataQuery{}, nil, invalid("projection", "object required")
		}
		var err error
		projection, err = docquery.ParseProjection(value)
		if err != nil {
			return DataQuery{}, nil, invalid("projection", err.Error())
		}
		if projection != nil {
			schema, err := pipelineSchema(ctx, "records", r.CollectionID)
			if err != nil {
				return DataQuery{}, nil, err
			}
			compiler := pipelineCompiler{ctx: ctx}
			for _, field := range projection.Paths {
				if _, err := compiler.field("base:r", field.Raw, schema); err != nil {
					return DataQuery{}, nil, err
				}
			}
		}
	}
	if action == "aggregate" {
		stages = r.Pipeline
		if stages == nil {
			return DataQuery{}, nil, invalid("pipeline", "pipeline array required")
		}
	}
	if action == "count" {
		if r.MaxCount == 0 {
			r.MaxCount = 100000
		}
		if r.MaxCount < 1 || r.MaxCount > 100000 {
			return DataQuery{}, nil, invalid("maxCount", "must be 1 to 100000")
		}
		bound, _ := json.Marshal(r.MaxCount + 1)
		stages = append(stages, map[string]json.RawMessage{"$limit": bound})
		stages = append(stages, map[string]json.RawMessage{"$count": json.RawMessage(`"count"`)})
	}
	if action == "distinct" {
		v, _ := json.Marshal(map[string]any{"_id": "$" + r.Field})
		stages = append(stages, map[string]json.RawMessage{"$group": v})
	}
	limit := r.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 200 {
		return DataQuery{}, nil, invalid("limit", "limit must be 1 to 200")
	}
	query := DataQuery{QueryID: "documents", CollectionID: r.CollectionID, Pipeline: stages, Page: &PipelinePage{Size: limit, Cursor: r.Cursor}}
	if action == "aggregate" && r.Limit == 0 && r.Cursor == "" {
		query.Page = nil
	}
	if r.Page != nil {
		if action != "aggregate" && action != "distinct" || r.Limit != 0 || r.Cursor != "" {
			return DataQuery{}, nil, invalid("page", "page is only accepted by aggregate/distinct, without limit/cursor")
		}
		query.Page = r.Page
	}
	if projection != nil {
		query.projection = projection.String()
	}
	if action == "count" {
		query.Page = nil
	}
	return query, func(result map[string]any) any {

		if projection != nil {
			items, _ := result["items"].([]any)
			for i, item := range items {
				document := item.(map[string]any)
				projected := projection.Apply(document)
				projected["_id"], projected["_meta"] = document["_id"], document["_meta"]
				items[i] = projected
			}
			result["items"] = items
		}
		delete(result, "queryId")
		delete(result, "resultMode")
		if action == "count" {
			items, _ := result["items"].([]any)
			var count any = int64(0)
			if len(items) > 0 {
				count = items[0].(map[string]any)["count"]
			}
			n := recordResponseInteger(count)
			return map[string]any{"count": min(n, int64(r.MaxCount)), "capped": n > int64(r.MaxCount)}
		}
		if action == "find" {
			items, _ := result["items"].([]any)
			documents := make([]*RecordDocument, 0, len(items))
			for _, item := range items {
				documents = append(documents, recordQueryDocument(item.(map[string]any)))
			}
			result["items"] = documents
		}
		if action == "distinct" {
			items, _ := result["items"].([]any)
			values := make([]any, 0, len(items))
			for _, item := range items {
				values = append(values, item.(map[string]any)["_id"])
			}
			return map[string]any{"values": values, "pageInfo": result["pageInfo"]}
		}
		return result
	}, nil
}

func novaWriteResult(out Outcome) map[string]any {
	result := map[string]any{"matchedCount": 0, "modifiedCount": 0, "deletedCount": 0, "insertedCount": 0, "upsertedCount": 0, "status": out.Status, "replayed": out.Replayed, "document": nil}
	matched, modified, deleted, inserted := 0, 0, 0, 0
	ids := []string{}
	for _, row := range out.Results {
		switch row.Status {
		case "updated":
			matched++
			modified++
		case "unchanged":
			matched++
		case "deleted":
			deleted++
		case "created":
			inserted++
			ids = append(ids, row.ID)
		}
		if row.Document != nil {
			result["document"] = row.Document
		}
	}
	result["matchedCount"], result["modifiedCount"], result["deletedCount"], result["insertedCount"] = matched, modified, deleted, inserted
	if len(ids) > 0 {
		result["insertedIds"] = ids
	}
	return result
}

func (s *Service) novaRecordMany(ctx context.Context, action string, r novaRecordRequest) (any, error) {
	if r.Upsert {
		return nil, invalid("upsert", "many operations do not upsert; use updateOne or replaceOne")
	}
	candidateAction := "updateOne"
	if action == "deleteMany" {
		candidateAction = "deleteOne"
	}
	change, err := novaRecordChange(candidateAction, r)
	if err != nil {
		return nil, err
	}
	if err := change.validate(); err != nil {
		return nil, err
	}
	if err := mutationPermissions(ctx, change); err != nil {
		return nil, err
	}
	if r.MaxDocs == 0 {
		r.MaxDocs = 1000
		if r.Atomic {
			r.MaxDocs = SyncItems
		}
	}
	if r.MaxDocs < 1 || r.MaxDocs > MaxItems {
		return nil, invalid("maxDocs", "maxDocs must be 1 to 10000")
	}
	if r.Atomic && r.MaxDocs > SyncItems {
		return nil, invalid("maxDocs", fmt.Sprintf("atomic writes support at most %d documents", SyncItems))
	}
	predicate, args, err := CompileDataFilter(ctx, "records", r.CollectionID, "base:r", r.Filter)
	if err != nil {
		return nil, err
	}
	var rows []repo.UnsRecord
	err = repo.GetUnsConn(ctx).WithContext(ctx).Table("public.uns_record AS r").Where("r.collection_id=? AND r.deleted_at IS NULL", r.CollectionID).Where(predicate, args...).Where("r.id>?", r.ResumeAfter).Select("r.id").Order("r.id").Limit(r.MaxDocs + 1).Find(&rows).Error
	if err != nil {
		return nil, classify(err)
	}
	hasMore := len(rows) > r.MaxDocs
	if hasMore && r.Atomic {
		return nil, invalid("maxDocs", "atomic filter exceeds maxDocs; no writes committed")
	}
	if hasMore {
		rows = rows[:r.MaxDocs]
	}
	batch := Batch{AssertLocks: r.AssertLocks, Mode: "sync", DryRun: r.DryRun, IdempotencyKey: r.IdempotencyKey}
	for i, row := range rows {
		c := change
		if token := r.LockTokens[row.ID]; token != "" {
			c.LockToken = token
		}
		c.ID = Identity{ID: row.ID}
		c.OpID = fmt.Sprintf("document-%d", i)
		batch.Items = append(batch.Items, c)
	}
	value, err := s.novaPersistedWrites(ctx, action, r, batch, hasMore)
	return recordBatchResponse(action, value), recordBatchFault(action, err)
}

func (s *Service) novaRecordBulk(ctx context.Context, action string, r novaRecordRequest) (any, error) {
	batch := Batch{AssertLocks: r.AssertLocks, Mode: "sync", DryRun: r.DryRun, IdempotencyKey: r.IdempotencyKey}
	if action == "insertMany" {
		for i, d := range r.Documents {
			c, err := novaRecordChange("insertOne", novaRecordRequest{CollectionID: r.CollectionID, ID: d.ID, Data: d.Data})
			if err != nil {
				return nil, err
			}
			c.OpID = fmt.Sprintf("document-%d", i)
			batch.Items = append(batch.Items, c)
		}
	} else {
		for i, op := range r.Operations {
			if len(op) != 1 {
				return nil, invalid("operations", "each bulk entry requires one document action")
			}
			for kind, raw := range op {
				var item novaRecordRequest
				if err := Decode(raw, &item); err != nil {
					return nil, err
				}
				item.CollectionID = r.CollectionID
				item.LockTokens = r.LockTokens
				if len(item.Filter) == 0 {
					item.Filter = json.RawMessage(`{}`)
				}
				if !contains([]string{"insertOne", "updateOne", "replaceOne", "deleteOne"}, kind) {
					return nil, invalid("operations", "bulk supports insertOne, updateOne, replaceOne and deleteOne")
				}
				c, err := novaRecordChange(kind, item)
				if err != nil {
					return nil, err
				}
				c.OpID = fmt.Sprintf("document-%d", i)
				batch.Items = append(batch.Items, c)
			}
		}
	}
	if len(batch.Items) == 0 || len(batch.Items) > MaxItems {
		return nil, invalid("documents/operations", "1 to 10000 entries required")
	}
	if r.Atomic && len(batch.Items) > SyncItems {
		return nil, invalid("atomic", fmt.Sprintf("atomic writes support at most %d documents", SyncItems))
	}
	value, err := s.novaPersistedWrites(ctx, action, r, batch, false)
	return recordBatchResponse(action, value), recordBatchFault(action, err)
}

func (s *Service) novaRunWrites(ctx context.Context, batch Batch, atomic bool, ordered *bool, hasMore bool) (any, error) {
	if len(batch.Items) == 0 {
		status := "committed"
		if batch.DryRun {
			status = "validated"
		}
		return novaBatchWriteResult(Outcome{Status: status}, batch.Items), nil
	}
	if atomic {
		out, err := s.Apply(ctx, batch)
		if err != nil {
			return nil, err
		}
		return novaBatchWriteResult(out, batch.Items), nil
	}
	out := Outcome{Status: "committed"}
	if batch.DryRun {
		out.Status = "validated"
	}
	writeErrors := []map[string]any{}
	lastID := ""
	next := 0
	interrupted := func(err error, unknownUntil int) (any, error) {
		if batch.DryRun {
			return nil, err
		}
		result := novaBatchWriteResult(out, batch.Items)
		result["status"] = "failed"
		if len(out.Results) > 0 {
			result["status"] = "partiallyCommitted"
		} else if unknownUntil > next {
			result["status"] = "unknown"
		}
		result["writeErrors"], result["hasMore"], result["nextIndex"] = writeErrors, true, next
		if lastID != "" {
			result["resumeAfter"] = lastID
		}
		if unknownUntil > next {
			result["unknownUntil"] = unknownUntil
		}
		return nil, partialBatchFault(result, err, len(out.Results) > 0 || unknownUntil > next, "writeErrors")
	}
	for i, c := range batch.Items {
		key := ""
		if batch.IdempotencyKey != "" {
			key = fmt.Sprintf("%s/%d", batch.IdempotencyKey, i)
		}
		one, err := s.Apply(ctx, Batch{AssertLocks: batch.AssertLocks, Mode: "sync", DryRun: batch.DryRun, IdempotencyKey: key, Items: []Change{c}})
		if err != nil {
			var fault *Fault
			if one.Status == "unknown" {
				return interrupted(err, i+1)
			}
			if !errors.As(classify(err), &fault) || fault.Code == "TEMPORARILY_UNAVAILABLE" || fault.Code == "INTERNAL_ERROR" || ctx.Err() != nil {
				return interrupted(err, next)
			}
			if len(writeErrors) == 100 {
				hasMore = true
				break
			}
			writeErrors = append(writeErrors, map[string]any{"index": i, "id": c.ID.ID, "error": classify(err)})
			next = i + 1
			if ordered == nil || *ordered {
				hasMore = i < len(batch.Items)-1 || hasMore
				break
			}
			continue
		}
		out.Results = append(out.Results, one.Results...)
		lastID = c.ID.ID
		next = i + 1
	}
	result := novaBatchWriteResult(out, batch.Items)
	if hasMore {
		result["nextIndex"] = next
	}
	if len(writeErrors) > 0 {
		if len(out.Results) > 0 {
			result["status"] = "partiallyCommitted"
		} else {
			result["status"] = "failed"
		}
	}
	result["writeErrors"] = writeErrors
	result["hasMore"] = hasMore
	if hasMore && lastID != "" {
		result["resumeAfter"] = lastID
	}
	return result, nil
}

type novaWritePlan struct {
	Type     string          `json:"type"`
	Digest   string          `json:"digest"`
	Batch    Batch           `json:"batch"`
	Atomic   bool            `json:"atomic"`
	Ordered  *bool           `json:"ordered,omitempty"`
	HasMore  bool            `json:"hasMore"`
	Response json.RawMessage `json:"response,omitempty"`
}

// Commit the selected identities before any per-document write. A retry keeps
// its original plan even when prior partial writes changed the filter's result.
// Child receipts survive a process failure; the root receipt records the final
// response only after all attempted writes have their own committed outcome.
func (s *Service) novaPersistedWrites(ctx context.Context, action string, r novaRecordRequest, batch Batch, hasMore bool) (any, error) {
	if r.IdempotencyKey == "" || r.DryRun {
		return s.novaRunWrites(ctx, batch, r.Atomic, r.Ordered, hasMore)
	}
	identity, err := requestIdentity(ctx, r.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal([]any{action, r})
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	id := "api-receipt-" + identity
	lock := func(tx *gorm.DB) error {
		return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,812300001))", identity).Error
	}
	err = repo.NewUnsMetaRepo(ctx).Transaction(ctx, func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		var rows []repo.UnsMeta
		if err := tx.Where("kind='task' AND id IN ?", []string{id, "api-job-" + identity}).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) > 0 {
			var plan novaWritePlan
			if len(rows) != 1 || rows[0].ID != id || json.Unmarshal(rows[0].Data, &plan) != nil || plan.Type != "apiNovaRecordPlan" || plan.Digest != digest {
				return problem("IDEMPOTENCY_CONFLICT", "same key belongs to another request")
			}
			return nil
		}
		plan := novaWritePlan{Type: "apiNovaRecordPlan", Digest: digest, Batch: batch, Atomic: r.Atomic, Ordered: r.Ordered, HasMore: hasMore}
		data, _ := json.Marshal(plan)
		now := time.Now().UnixMilli()
		return tx.Create(&repo.UnsMeta{ID: id, Kind: "task", Data: data, Revision: 1, CreatedAt: now, UpdatedAt: now}).Error
	})
	if err != nil {
		return nil, classify(err)
	}
	var result any
	// This transaction holds only the principal intent lock. Child writes use
	// the original context and independent transactions, retaining partial commit.
	err = repo.NewUnsMetaRepo(ctx).Transaction(ctx, func(tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		var row repo.UnsMeta
		if err := tx.Where("kind='task' AND id=?", id).Take(&row).Error; err != nil {
			return err
		}
		var plan novaWritePlan
		if err := json.Unmarshal(row.Data, &plan); err != nil {
			return err
		}
		if plan.Type != "apiNovaRecordPlan" || plan.Digest != digest {
			return problem("IDEMPOTENCY_CONFLICT", "request plan changed")
		}
		for _, change := range plan.Batch.Items {
			if err := mutationPermissions(ctx, change); err != nil {
				return err
			}
		}
		if len(plan.Response) > 0 {
			var response map[string]any
			if err := decodeNumberJSON(plan.Response, &response); err != nil {
				return err
			}
			response["replayed"] = true
			result = response
			return nil
		}
		work := plan.Batch
		if plan.Atomic {
			work.IdempotencyKey += "/atomic"
		}
		value, err := s.novaRunWrites(ctx, work, plan.Atomic, plan.Ordered, plan.HasMore)
		if err != nil {
			return err
		}
		result = value
		plan.Response, err = json.Marshal(value)
		if err != nil {
			return err
		}
		data, err := json.Marshal(plan)
		if err != nil {
			return err
		}
		updated := tx.Model(&repo.UnsMeta{}).Where("kind='task' AND id=? AND revision=?", id, row.Revision).Updates(map[string]any{"data": data, "revision": row.Revision + 1, "updated_at": time.Now().UnixMilli()})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return repo.ErrConflict
		}
		return nil
	})
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil, &Fault{Code: "TEMPORARILY_UNAVAILABLE", Message: "bulk response interrupted; retry the unchanged request with the same key", Retryable: true}
	}
	return result, classify(err)
}

// Bulk upserts are distinguished from ordinary inserts using the original plan.
func novaBatchWriteResult(out Outcome, items []Change) map[string]any {
	result := novaWriteResult(out)
	inserted, ids := recordIndexedIDs(out, items)
	result["upsertedCount"] = len(ids)
	result["insertedCount"] = len(inserted)
	result["insertedIds"] = inserted
	result["upsertedIds"] = ids
	return result
}

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
	docquery "backend/internal/domain/document/query"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/unsref"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 文档适配只负责选择和返回形状，实际写入仍通过元数据领域服务和原有回执事务。
const metadataDocumentAction = "meta.document"
const metadataDocumentLimit = 200

type metadataRequest struct {
	Kind             string                       `json:"kind,omitempty"`
	Kinds            []string                     `json:"kinds,omitempty"`
	ID               string                       `json:"id,omitempty"`
	Data             json.RawMessage              `json:"data,omitempty"`
	Filter           json.RawMessage              `json:"filter,omitempty"`
	Sort             []RecordOrder                `json:"sort,omitempty"`
	Projection       json.RawMessage              `json:"projection,omitempty"`
	Pipeline         []map[string]json.RawMessage `json:"pipeline,omitempty"`
	Page             *PipelinePage                `json:"page,omitempty"`
	Limit            int                          `json:"limit,omitempty"`
	Field            string                       `json:"field,omitempty"`
	State            string                       `json:"state,omitempty"`
	Update           json.RawMessage              `json:"update,omitempty"`
	Replacement      json.RawMessage              `json:"replacement,omitempty"`
	ExpectedRevision int64                        `json:"expectedRevision,omitempty"`
	Upsert           bool                         `json:"upsert,omitempty"`
	ReturnDocument   string                       `json:"returnDocument,omitempty"`
	Documents        []novaInsertDocument         `json:"documents,omitempty"`
	Items            []metadataRestoreItem        `json:"items,omitempty"`
	Operations       []json.RawMessage            `json:"operations,omitempty"`
	Atomic           *bool                        `json:"atomic,omitempty"`
	Ordered          *bool                        `json:"ordered,omitempty"`
	MaxDocs          int                          `json:"maxDocs,omitempty"`
	DryRun           bool                         `json:"dryRun,omitempty"`
	IdempotencyKey   string                       `json:"idempotencyKey,omitempty"`
}

type metadataRestoreItem struct {
	ID               string `json:"id"`
	ExpectedRevision int64  `json:"expectedRevision,omitempty"`
}
type metadataEnvelope struct {
	Action  string          `json:"action"`
	Request metadataRequest `json:"request"`
}
type metadataTransactionItem struct {
	OpID             string          `json:"opId"`
	Action           string          `json:"action"`
	Kind             string          `json:"kind"`
	ID               string          `json:"id,omitempty"`
	Data             json.RawMessage `json:"data,omitempty"`
	Filter           json.RawMessage `json:"filter,omitempty"`
	Sort             []RecordOrder   `json:"sort,omitempty"`
	Update           json.RawMessage `json:"update,omitempty"`
	Replacement      json.RawMessage `json:"replacement,omitempty"`
	ExpectedRevision int64           `json:"expectedRevision,omitempty"`
	DependsOn        []string        `json:"dependsOn,omitempty"`
}

func metadataKind(kind string) bool {
	return contains([]string{"directory", "topic", "collection"}, kind)
}

func retiredMetadataOperation(operation string) bool {
	if contains([]string{"metadata", "collections", "metaBatchExecute", "metaBatchValidate", "metaBatchStatus", "collectionSchemaUpdate", "topicViewUpdate"}, operation) {
		return true
	}
	for _, kind := range []string{"directory", "topic", "collection"} {
		for _, action := range []string{"List", "Get", "Create", "Update", "Archive", "Restore"} {
			if operation == kind+action {
				return true
			}
		}
	}
	return false
}
func metadataReadAction(action string) bool {
	return contains([]string{"find", "findOne", "count", "distinct", "aggregate"}, action)
}

func (s *Service) metadataOperation(ctx context.Context, operation string, raw []byte) (any, error) {
	descriptor, ok := FindResourceOperation(operation)
	if !ok || !ResourceAllowed(ctx, descriptor) {
		return nil, metadataRejected(problem("FORBIDDEN", "metadata operation permission is required"))
	}
	if _, err := validateResourceRequest(descriptor, raw); err != nil {
		return nil, metadataRejected(err)
	}
	var r metadataRequest
	if err := Decode(raw, &r); err != nil {
		return nil, metadataRejected(err)
	}
	action := descriptor.MetadataAction
	if action == "" {
		return nil, metadataRejected(invalid("operation", "metadata action required"))
	}
	if r.State == "" {
		r.State = "active"
	}
	if r.State != "active" && r.State != "deleted" && r.State != "all" {
		return nil, metadataRejected(invalid("state", "active, deleted or all required"))
	}
	actor, ok := contextx.SubjectFrom(ctx)
	if !ok {
		return nil, metadataRejected(problem("FORBIDDEN", "metadata management requires a management credential"))
	}
	if metadataAppCredential(actor) && (action != "find" && action != "findOne" || r.State != "active" || r.Kind != "collection" && !(r.Kind == "" && len(r.Kinds) == 1 && r.Kinds[0] == "collection")) {
		return nil, metadataRejected(problem("FORBIDDEN", "App credentials may only read active authorized collections"))
	}
	if metadataReadAction(action) {
		return s.metadataRead(ctx, action, r)
	}
	if r.State != "active" {
		return nil, metadataRejected(invalid("state", "writes target active metadata; use restore for deleted metadata"))
	}
	if action != "transact" && !metadataKind(r.Kind) {
		return nil, metadataRejected(invalid("kind", "directory, topic or collection required"))
	}
	if r.ExpectedRevision < 0 {
		return nil, metadataRejected(invalid("expectedRevision", "positive revision required"))
	}
	if r.Atomic != nil && !*r.Atomic && action != "bulkWrite" {
		return nil, metadataRejected(invalid("atomic", "only bulkWrite supports independent commits"))
	}
	if action == "bulkWrite" && (r.Atomic == nil || *r.Atomic) && r.Ordered != nil && !*r.Ordered {
		return nil, metadataRejected(invalid("ordered", "atomic bulk writes require ordered execution"))
	}
	if action == "bulkWrite" && r.Atomic != nil && !*r.Atomic {
		return s.metadataPartialBulk(ctx, r)
	}
	return s.metadataAtomic(ctx, action, r)
}

func metadataChangeEnvelope(action string, r metadataRequest) Change {
	raw, _ := json.Marshal(metadataEnvelope{Action: action, Request: r})
	return Change{OpID: "metadata", Action: metadataDocumentAction, Kind: "directory", ID: Identity{ID: "metadata-request"}, Data: raw}
}

func validateMetadataDocumentChange(c Change) error {
	var e metadataEnvelope
	if err := Decode(c.Data, &e); err != nil {
		return err
	}
	if c.Kind != "directory" || c.ID.ID != "metadata-request" || c.ExpectedRevision != 0 || c.ManagedBy != nil || len(c.Filter) != 0 {
		return invalid("action", "invalid metadata adapter envelope")
	}
	if !contains([]string{"insertOne", "insertMany", "updateOne", "updateMany", "findOneAndUpdate", "replaceOne", "findOneAndReplace", "deleteOne", "deleteMany", "findOneAndDelete", "restore", "bulkWrite", "transact"}, e.Action) {
		return invalid("action", "unsupported metadata document action")
	}
	if e.Action != "transact" && !metadataKind(e.Request.Kind) {
		return invalid("kind", "writable metadata kind required")
	}
	return nil
}

func metadataDocumentPermissions(ctx context.Context, c Change) error {
	if err := validateMetadataDocumentChange(c); err != nil {
		return err
	}
	actor, ok := contextx.SubjectFrom(ctx)
	if !ok || metadataAppCredential(actor) {
		return problem("FORBIDDEN", "metadata management credential required")
	}
	var e metadataEnvelope
	_ = Decode(c.Data, &e)
	check := func(action, kind string) error {
		if !metadataKind(kind) {
			return invalid("kind", "writable metadata kind required")
		}
		mapped := "meta.replace"
		if action == "insertOne" || action == "insertMany" {
			mapped = "meta.create"
		}
		if action == "restore" || action == "restoreOne" {
			mapped = "meta.restore"
		}
		if strings.Contains(strings.ToLower(action), "delete") {
			mapped = "meta.archive"
		}
		if err := require(ctx, "uns.read"); err != nil {
			return err
		}
		return mutationPermissions(ctx, Change{Action: mapped, Kind: kind})
	}
	if e.Action == "transact" {
		for _, raw := range e.Request.Operations {
			var item metadataTransactionItem
			if err := Decode(raw, &item); err != nil {
				return err
			}
			if err := check(item.Action, item.Kind); err != nil {
				return err
			}
		}
		return nil
	}
	if e.Action == "bulkWrite" {
		for _, raw := range e.Request.Operations {
			action, _, err := metadataBulkItem(raw)
			if err != nil {
				return err
			}
			if err = check(action, e.Request.Kind); err != nil {
				return err
			}
		}
		return nil
	}
	return check(e.Action, e.Request.Kind)
}

func (s *Service) metadataAtomic(ctx context.Context, action string, r metadataRequest) (any, error) {
	change := metadataChangeEnvelope(action, r)
	out, err := s.Apply(ctx, Batch{Mode: "sync", DryRun: r.DryRun, IdempotencyKey: r.IdempotencyKey, Items: []Change{change}})
	if err != nil {
		return nil, err
	}
	if len(out.Results) != 1 {
		return nil, problem("INTERNAL_ERROR", "metadata result unavailable")
	}
	var result map[string]any
	if err := decodeNumberJSON(out.Results[0].Data, &result); err != nil {
		return nil, err
	}
	if action == "insertOne" {
		delete(result, "insertedCount")
	}
	if contains([]string{"updateOne", "replaceOne", "findOneAndUpdate", "findOneAndReplace"}, action) {
		delete(result, "upsertedCount")
	}
	result["status"], result["replayed"] = out.Status, out.Replayed
	if r.DryRun {
		if failures, ok := result["errors"].([]any); ok && len(failures) > 0 {
			result["status"] = "failed"
		}
	}
	return result, nil
}

func metadataSchema() *unsmeta.Schema {
	str := unsmeta.Schema{Type: "string"}
	integer := unsmeta.Schema{Type: "integer"}
	obj := func(p map[string]unsmeta.Schema) unsmeta.Schema {
		return unsmeta.Schema{Type: "object", Properties: p, AdditionalProperties: pipelineClosedObject()}
	}
	schema := obj(map[string]unsmeta.Schema{"type": str, "format": str, "title": str, "description": str, "unit": str, "required": {Type: "array", Items: &str}, "additionalProperties": {Type: "boolean"}})
	field := obj(map[string]unsmeta.Schema{"name": str, "displayName": str, "kind": str, "collectionId": str, "recordId": str, "fieldPath": str})
	index := obj(map[string]unsmeta.Schema{"path": str, "paths": {Type: "array", Items: &str}, "unique": {Type: "boolean"}, "target": str, "kind": str})
	semanticField := obj(map[string]unsmeta.Schema{"kind": str, "path": str, "aggregationNote": str})
	relationship := obj(map[string]unsmeta.Schema{"name": str, "sourceField": str, "targetCollectionId": str, "targetField": str, "cardinality": str, "description": str})
	root := obj(map[string]unsmeta.Schema{
		"directoryTrashId": str, "purgeJobId": str, "name": str, "description": str, "parentId": str, "schemaVersion": integer, "viewMode": str, "collectionId": str,
		"recordSchema": schema, "sampleSchema": schema, "fields": {Type: "array", Items: &field}, "indexes": {Type: "array", Items: &index},
		"semantics": obj(map[string]unsmeta.Schema{"recordGrain": str, "sampleGrain": str, "fields": {Type: "array", Items: &semanticField}, "relationships": {Type: "array", Items: &relationship}}), "_id": str,
		"_meta": obj(map[string]unsmeta.Schema{"kind": str, "mountId": str, "revision": integer, "createdAt": integer, "updatedAt": integer, "deletedAt": integer}),
	})
	return &root
}

func metadataDocumentSQL() string {
	return "((b.data-'audit'-'nodeType') || CASE WHEN b.kind IN ('directory','topic','collection') THEN jsonb_build_object('parentId',b.data->'parentId') ELSE '{}'::jsonb END || jsonb_build_object('_id',b.id,'_meta',jsonb_build_object('kind',b.kind,'mountId',b.mount_id,'revision',b.revision,'createdAt',b.created_at,'updatedAt',b.updated_at,'deletedAt',b.deleted_at)))"
}

func metadataQuerySchema(ctx context.Context) *unsmeta.Schema {
	schema := metadataSchema()
	if actor, _ := contextx.SubjectFrom(ctx); metadataAppCredential(actor) {
		// 查询条件、排序和投影均不能用隐藏定义探测当前 App 不可见的字段。
		for name := range schema.Properties {
			if !contains([]string{"_id", "_meta", "name", "parentId"}, name) {
				delete(schema.Properties, name)
			}
		}
	}
	return schema
}

func metadataReadPlan(ctx context.Context, r metadataRequest) (pipelinePlan, error) {
	kinds := r.Kinds
	if r.Kind != "" {
		if len(kinds) > 0 {
			return pipelinePlan{}, invalid("kinds", "kind and kinds cannot be combined")
		}
		kinds = []string{r.Kind}
	}
	if len(kinds) == 0 || len(kinds) > 3 {
		return pipelinePlan{}, invalid("kinds", "one to three metadata kinds required")
	}
	seen := map[string]bool{}
	for _, kind := range kinds {
		if !metadataKind(kind) || seen[kind] {
			return pipelinePlan{}, invalid("kinds", "unique directory/topic/collection kinds required")
		}
		seen[kind] = true
		if err := readPermission(ctx, kind); err != nil {
			return pipelinePlan{}, err
		}
	}
	where := "b.kind IN ? AND b.id NOT LIKE ?"
	switch r.State {
	case "", "active":
		where += " AND b.deleted_at=0"
	case "deleted":
		where += " AND b.deleted_at>0"
	case "all":
	default:
		return pipelinePlan{}, invalid("state", "active, deleted or all required")
	}
	args := []any{kinds, unsref.StreamRuntimePrefix + "%"}
	if actor, _ := contextx.SubjectFrom(ctx); metadataAppCredential(actor) {
		if len(kinds) != 1 || kinds[0] != "collection" || r.State != "active" {
			return pipelinePlan{}, problem("FORBIDDEN", "App metadata queries require active collections")
		}
		ids, err := appMetadataCollectionIDs(ctx)
		if err != nil {
			return pipelinePlan{}, err
		}
		where += " AND b.id IN ?"
		args = append(args, ids)
	}
	doc := metadataDocumentSQL()
	return pipelinePlan{sql: "SELECT " + doc + " AS doc,row_number() OVER (ORDER BY b.kind,b.id) AS ord FROM public.uns_meta b WHERE " + where, args: args, schema: metadataQuerySchema(ctx)}, nil
}

func metadataFilter(r metadataRequest) json.RawMessage {
	if len(r.Filter) == 0 {
		return json.RawMessage(`{}`)
	}
	return r.Filter
}

func metadataQueryRows(ctx context.Context, p pipelinePlan, size, offset int) ([]map[string]any, error) {
	rows, err := repo.GetUnsConn(ctx).Raw("SELECT p.doc FROM ("+p.sql+") p ORDER BY p.ord LIMIT ? OFFSET ?", append(append([]any{}, p.args...), size, offset)...).Rows()
	if err != nil {
		return nil, pipelineRuntimeError(err)
	}
	defer rows.Close()
	items := []map[string]any{}
	bytes := 0
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		bytes += len(raw)
		if bytes > MaxQueryResponseBytes {
			return nil, problem("RESPONSE_TOO_LARGE", "metadata result exceeds response budget")
		}
		var item map[string]any
		if err = decodeNumberJSON(raw, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func metadataResource(doc map[string]any, projection *docquery.Projection) map[string]any {
	id := doc["_id"]
	system, _ := doc["_meta"].(map[string]any)
	data := map[string]any{}
	for k, v := range doc {
		if k != "_id" && k != "_meta" {
			data[k] = v
		}
	}
	if projection != nil {
		projected := projection.Apply(doc)
		data = map[string]any{}
		for k, v := range projected {
			if k != "_id" && k != "_meta" {
				data[k] = v
			}
		}
	}
	return map[string]any{"id": id, "kind": system["kind"], "mountId": system["mountId"], "revision": system["revision"], "createdAt": system["createdAt"], "updatedAt": system["updatedAt"], "deletedAt": system["deletedAt"], "data": data}
}

func metadataProjection(ctx context.Context, raw json.RawMessage) (*docquery.Projection, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	v, err := pipelineDecode(raw)
	if err != nil {
		return nil, err
	}
	p, err := docquery.ParseProjection(v)
	if err != nil {
		return nil, invalid("projection", err.Error())
	}
	if p != nil {
		c := pipelineCompiler{ctx: ctx}
		for _, path := range p.Paths {
			if _, err = c.field("p.doc", path.Raw, metadataQuerySchema(ctx)); err != nil {
				return nil, err
			}
		}
	}
	return p, nil
}

func (s *Service) metadataRead(ctx context.Context, action string, r metadataRequest) (any, error) {
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var result any
	err := repo.GetUnsConn(ctx).WithContext(bounded).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL statement_timeout='15000ms'").Error; err != nil {
			return err
		}
		qctx := repo.WithUnsTransaction(bounded, tx)
		p, err := metadataReadPlan(qctx, r)
		if err != nil {
			return err
		}
		c := pipelineCompiler{ctx: qctx}
		stages := []map[string]json.RawMessage{{"$match": metadataFilter(r)}}
		if len(r.Sort) > 0 {
			raw, _ := json.Marshal(r.Sort)
			stages = append(stages, map[string]json.RawMessage{"$sort": raw})
		}
		if action == "aggregate" {
			if len(r.Filter) > 0 {
				p, err = c.compile(p, []map[string]json.RawMessage{{"$match": r.Filter}}, 0)
				if err != nil {
					return err
				}
			}
			stages = r.Pipeline
			if stages == nil {
				return invalid("pipeline", "pipeline array required")
			}
			for _, stage := range stages {
				for op := range stage {
					if !contains([]string{"$match", "$project", "$group", "$sort", "$skip", "$limit", "$distinct"}, op) {
						return invalid("pipeline", "unsupported metadata stage: "+op)
					}
				}
			}
		}
		p, err = metadataCompileStages(&c, p, stages)
		if err != nil {
			return err
		}
		if action == "count" {
			var count int64
			err = tx.Raw("SELECT count(*) FROM ("+p.sql+") p", p.args...).Scan(&count).Error
			result = map[string]any{"count": count}
			return err
		}
		if action == "distinct" {
			e, err := c.field("p.doc", r.Field, p.schema)
			if err != nil {
				return err
			}
			p = pipelinePlan{sql: "SELECT jsonb_build_object('value',v) AS doc,row_number() OVER (ORDER BY v) AS ord FROM (SELECT DISTINCT " + e.sql + " AS v FROM (" + p.sql + ") p) d WHERE v IS NOT NULL", args: p.args}
			size, offset, err := metadataPagination(r)
			if err != nil {
				return err
			}
			values, err := metadataQueryRows(qctx, p, size+1, offset)
			if err != nil {
				return err
			}
			more := len(values) > size
			if more {
				values = values[:size]
			}
			out := []any{}
			for _, v := range values {
				out = append(out, v["value"])
			}
			result = map[string]any{"values": out, "pageInfo": metadataPageInfo(size, offset, more)}
			return nil
		}
		size, offset, err := metadataPagination(r)
		if err != nil {
			return err
		}
		if action == "findOne" {
			size, offset = 1, 0
		}
		items, err := metadataQueryRows(qctx, p, size+1, offset)
		if err != nil {
			return err
		}
		more := len(items) > size
		if more {
			items = items[:size]
		}
		projection, err := metadataProjection(qctx, r.Projection)
		if err != nil {
			return err
		}
		values := []any{}
		for _, item := range items {
			if action == "aggregate" {
				values = append(values, item)
			} else if actor, _ := contextx.SubjectFrom(qctx); metadataAppCredential(actor) {
				id, _ := item["_id"].(string)
				row, err := meta(qctx, "collection", id, true)
				if err != nil {
					return err
				}
				view, err := metadataRowDocument(qctx, row, r.Projection)
				if err != nil {
					return err
				}
				if view == nil {
					return problem("FORBIDDEN", "collection metadata is no longer readable")
				}
				values = append(values, view)
			} else {
				values = append(values, metadataResource(item, projection))
			}
		}
		if action == "findOne" {
			result = map[string]any{"value": nil}
			if len(values) > 0 {
				result.(map[string]any)["value"] = values[0]
			}
			return nil
		}
		result = map[string]any{"items": values, "pageInfo": metadataPageInfo(size, offset, more)}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, pipelineRuntimeError(err)
}

func metadataDistinctPlan(c *pipelineCompiler, p pipelinePlan, field string) (pipelinePlan, error) {
	e, err := c.field("p.doc", field, p.schema)
	if err != nil {
		return p, err
	}
	return pipelinePlan{
		sql:    "SELECT jsonb_build_object('value',v) AS doc,row_number() OVER (ORDER BY v) AS ord FROM (SELECT DISTINCT " + e.sql + " AS v FROM (" + p.sql + ") p) d WHERE v IS NOT NULL",
		args:   p.args,
		schema: &unsmeta.Schema{Type: "object", AdditionalProperties: pipelineClosedObject(), Properties: map[string]unsmeta.Schema{"value": *e.schema}},
	}, nil
}

// 只在元数据管线增加已有 distinct 能力，其他阶段继续使用公共编译器与同一阶段预算。
func metadataCompileStages(c *pipelineCompiler, p pipelinePlan, stages []map[string]json.RawMessage) (pipelinePlan, error) {
	for _, stage := range stages {
		var err error
		if raw, ok := stage["$distinct"]; ok {
			if len(stage) != 1 {
				return p, pipelineError("each stage must have exactly one operator")
			}
			c.stages++
			if c.stages > 32 {
				return p, pipelineError("pipeline exceeds 32 stages")
			}
			var field string
			if json.Unmarshal(raw, &field) != nil || field == "" {
				return p, pipelineError("$distinct requires a declared field path")
			}
			p, err = metadataDistinctPlan(c, p, field)
		} else {
			p, err = c.compile(p, []map[string]json.RawMessage{stage}, 0)
		}
		if err != nil {
			return p, err
		}
	}
	return p, nil
}

func metadataPagination(r metadataRequest) (int, int, error) {
	page := &PipelinePage{Size: 20, Page: 1, Mode: "offset"}
	if r.Page != nil {
		copy := *r.Page
		page = &copy
	}
	if r.Limit > 0 {
		if r.Page != nil {
			return 0, 0, invalid("limit", "limit and page cannot be combined")
		}
		page.Size = r.Limit
	}
	if page.Mode == "cursor" || page.Cursor != "" {
		return 0, 0, invalid("page", "metadata supports offset pagination")
	}
	return pipelinePagination("records", DataQuery{Page: page})
}

func metadataPageInfo(size, offset int, more bool) map[string]any {
	var next any
	if more {
		next = offset/size + 2
	}
	return map[string]any{"mode": "offset", "hasMore": more, "nextPage": next, "nextCursor": nil, "snapshotScope": "thisRequestOnly"}
}

func (s *Service) metadataDocumentChange(ctx context.Context, tx *gorm.DB, c Change) (Result, error) {
	var e metadataEnvelope
	if err := Decode(c.Data, &e); err != nil {
		return Result{}, err
	}
	if err := tx.Exec("SET LOCAL statement_timeout='10000ms'").Error; err != nil {
		return Result{}, err
	}
	value, err := s.metadataWrite(ctx, tx, e.Action, e.Request)
	if err != nil {
		return Result{}, err
	}
	raw, err := json.Marshal(value)
	return Result{OpID: c.OpID, Action: c.Action, Kind: c.Kind, ID: c.ID.ID, Status: "updated", Data: raw}, err
}

func metadataSelected(ctx context.Context, r metadataRequest, max int) ([]repo.UnsMeta, error) {
	p, err := metadataReadPlan(ctx, r)
	if err != nil {
		return nil, err
	}
	c := pipelineCompiler{ctx: ctx}
	stages := []map[string]json.RawMessage{{"$match": metadataFilter(r)}}
	if len(r.Sort) > 0 {
		raw, _ := json.Marshal(r.Sort)
		stages = append(stages, map[string]json.RawMessage{"$sort": raw})
	}
	p, err = c.compile(p, stages, 0)
	if err != nil {
		return nil, err
	}
	docs, err := metadataQueryRows(ctx, p, max, 0)
	if err != nil {
		return nil, err
	}
	rows := []repo.UnsMeta{}
	for _, doc := range docs {
		id, _ := doc["_id"].(string)
		system, _ := doc["_meta"].(map[string]any)
		kind, _ := system["kind"].(string)
		row, err := meta(ctx, kind, id, true)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func metadataRowDocument(ctx context.Context, row repo.UnsMeta, raw json.RawMessage) (map[string]any, error) {
	view := CollectionProjection(ctx, row)
	if len(raw) == 0 || view == nil {
		return view, nil
	}
	p, err := metadataProjection(ctx, raw)
	if err != nil {
		return nil, err
	}
	data, _ := view["data"].(map[string]any)
	if data == nil {
		return nil, problem("INTERNAL_ERROR", "metadata projection unavailable")
	}
	delete(data, "audit")
	delete(data, "nodeType")
	data["_id"] = row.ID
	data["_meta"] = map[string]any{"kind": view["kind"], "mountId": row.MountID, "revision": row.Revision, "createdAt": row.CreatedAt, "updatedAt": row.UpdatedAt, "deletedAt": row.DeletedAt}
	return metadataResource(data, p), nil
}

func metadataCreateData(raw json.RawMessage) (json.RawMessage, error) {
	var data map[string]any
	if err := decodeNumberJSON(raw, &data); err != nil || data == nil {
		return nil, invalid("data", "metadata configuration object required")
	}
	if _, ok := data["schemaVersion"]; !ok {
		data["schemaVersion"] = 1
	}
	return json.Marshal(data)
}

func metadataApplyUpdate(raw json.RawMessage, data map[string]any, insert bool) (map[string]any, error) {
	var update map[string]any
	if err := decodeNumberJSON(raw, &update); err != nil || len(update) == 0 {
		return nil, invalid("update", "nonempty update object required")
	}
	for op, fields := range update {
		if !contains([]string{"$set", "$unset", "$setOnInsert"}, op) {
			return nil, invalid("update", "only $set/$unset/$setOnInsert supported")
		}
		m, ok := fields.(map[string]any)
		if !ok {
			return nil, invalid("update", "operator value must be an object")
		}
		for path := range m {
			parts, err := pipelinePath(path)
			if err != nil {
				return nil, err
			}
			if len(parts) == 0 || contains([]string{"_id", "_meta", "schemaVersion", "id", "kind", "mountId", "revision", "createdAt", "updatedAt", "deletedAt", "audit", "nodeType", "directoryTrashId", "purgeJobId"}, parts[0]) {
				return nil, invalid("update", "server fields are read-only")
			}
			if _, known := metadataSchema().Properties[parts[0]]; !known {
				return nil, invalid("update", "unknown metadata field: "+parts[0])
			}
			if len(parts) > 1 && contains([]string{"recordSchema", "sampleSchema", "indexes", "fields", "semantics"}, parts[0]) {
				return nil, invalid("update", "replace configuration field as a whole: "+parts[0])
			}
		}
	}
	u, err := docquery.ParseUpdate(update, nil)
	if err != nil {
		return nil, invalid("update", err.Error())
	}
	out, err := u.Apply(data, docquery.ApplyContext{Insert: insert, NowMs: time.Now().UnixMilli()})
	if err != nil {
		return nil, invalid("update", err.Error())
	}
	return out, nil
}

func metadataUpsertID(filter json.RawMessage) (string, error) {
	var value map[string]any
	if err := decodeNumberJSON(filter, &value); err != nil {
		return "", invalid("filter", "object required")
	}
	id, ok := value["_id"].(string)
	if !ok {
		if eq, ok := value["_id"].(map[string]any); ok && len(eq) == 1 {
			id, _ = eq["$eq"].(string)
		}
	}
	if !unsmeta.ValidID(id) {
		return "", invalid("upsert", "explicit _id equality required")
	}
	return id, nil
}

func (s *Service) metadataOne(ctx context.Context, tx *gorm.DB, action string, r metadataRequest) (map[string]any, error) {
	if !metadataKind(r.Kind) {
		return nil, invalid("kind", "writable metadata kind required")
	}
	// 新增快照仅开放给 Record；元数据新增保留原有契约。
	if action == "insertOne" && r.ReturnDocument != "" {
		return nil, invalid("returnDocument", singleWriteReturnUnsupportedMessage)
	}
	returnDocument, err := singleWriteReturnDocument(action, r.ReturnDocument)
	if err != nil {
		return nil, err
	}
	if len(r.Projection) > 0 && returnDocument == "" {
		return nil, invalid("projection", singleWriteProjectionMessage)
	}
	r.ReturnDocument = returnDocument
	if action == "insertOne" {
		data, err := metadataCreateData(r.Data)
		if err != nil {
			return nil, err
		}
		id := r.ID
		if id == "" {
			id = uuid.NewString()
		}
		if !unsmeta.ValidID(id) {
			return nil, invalid("id", "valid metadata ID required")
		}
		if r.ExpectedRevision != 0 {
			return nil, invalid("expectedRevision", "creation must not specify a revision")
		}
		change := Change{OpID: "create", Action: "meta.create", Kind: r.Kind, ID: Identity{ID: id}, Data: data}
		if err = mutationPermissions(ctx, change); err != nil {
			return nil, err
		}
		saved, err := s.metadataChange(ctx, tx, change)
		if err != nil {
			return nil, err
		}
		return map[string]any{"insertedId": saved.ID, "insertedCount": 1}, nil
	}
	if r.ID != "" {
		raw, _ := json.Marshal(map[string]string{"_id": r.ID})
		if len(r.Filter) > 0 {
			return nil, invalid("filter", "id and filter cannot be combined")
		}
		r.Filter = raw
	}
	if len(r.Filter) == 0 {
		return nil, invalid("filter", "explicit filter required")
	}
	if action == "restoreOne" {
		r.State = "deleted"
	}
	if r.Upsert && r.ExpectedRevision != 0 {
		return nil, invalid("upsert", "upsert cannot specify expectedRevision")
	}
	if r.Upsert {
		if _, err := metadataUpsertID(r.Filter); err != nil {
			return nil, err
		}
	}
	if action == "updateOne" || action == "findOneAndUpdate" {
		// 即使没有匹配对象也拒绝未知操作符、冲突路径及系统字段写入。
		if _, err := metadataApplyUpdate(r.Update, map[string]any{}, true); err != nil {
			return nil, err
		}
	}
	rows, err := metadataSelected(ctx, r, 1)
	if err != nil {
		return nil, err
	}
	result := map[string]any{}
	returnValue := returnDocument != ""
	if returnValue {
		result["value"] = nil
	}
	if len(rows) == 0 {
		if r.Upsert {
			if !contains([]string{"updateOne", "replaceOne", "findOneAndUpdate", "findOneAndReplace"}, action) {
				return nil, invalid("upsert", "unsupported upsert action")
			}
			id, err := metadataUpsertID(r.Filter)
			if err != nil {
				return nil, err
			}
			var existing int64
			if err = tx.Model(&repo.UnsMeta{}).Where("kind=? AND id=?", r.Kind, id).Count(&existing).Error; err != nil {
				return nil, err
			}
			if existing > 0 {
				return nil, problem("VERSION_CONFLICT", "metadata identity exists but does not match; deleted objects require restore")
			}
			data := r.Replacement
			if strings.Contains(action, "Update") || action == "updateOne" {
				updated, err := metadataApplyUpdate(r.Update, map[string]any{}, true)
				if err != nil {
					return nil, err
				}
				data, _ = json.Marshal(updated)
			}
			_, err = s.metadataOne(ctx, tx, "insertOne", metadataRequest{Kind: r.Kind, ID: id, Data: data})
			if err != nil {
				return nil, err
			}
			if returnValue {
				result["upsertedId"] = id
				if r.ReturnDocument == "after" {
					row, err := meta(ctx, r.Kind, id, false)
					if err != nil {
						return nil, err
					}
					result["value"], err = metadataRowDocument(ctx, row, r.Projection)
					return metadataSingleReturningResult(action, result, 0, 0, 1), err
				}
				return metadataSingleReturningResult(action, result, 0, 0, 1), nil
			}
			return map[string]any{"matchedCount": 0, "modifiedCount": 0, "upsertedId": id, "upsertedCount": 1}, nil
		}
		if r.ExpectedRevision > 0 {
			return nil, problem("VERSION_CONFLICT", "metadata target does not match the expected version")
		}
		if returnValue {
			return metadataSingleReturningResult(action, result, 0, 0, 0), nil
		}
		if action == "deleteOne" {
			return map[string]any{"deletedCount": 0}, nil
		}
		if action == "restoreOne" {
			return map[string]any{"restoredCount": 0, "items": []any{}}, nil
		}
		return map[string]any{"matchedCount": 0, "modifiedCount": 0, "upsertedCount": 0}, nil
	}
	row := rows[0]
	if r.ExpectedRevision > 0 && row.Revision != r.ExpectedRevision {
		return nil, problem("VERSION_CONFLICT", "metadata revision changed")
	}
	before, err := metadataRowDocument(ctx, row, r.Projection)
	if err != nil {
		return nil, err
	}
	change := Change{OpID: "document", Kind: r.Kind, ID: Identity{ID: row.ID}, ExpectedRevision: row.Revision}
	switch action {
	case "updateOne", "findOneAndUpdate":
		var data map[string]any
		if err = decodeNumberJSON(row.Data, &data); err != nil {
			return nil, err
		}
		delete(data, "audit")
		delete(data, "nodeType")
		data, err = metadataApplyUpdate(r.Update, data, false)
		if err != nil {
			return nil, err
		}
		change.Data, _ = json.Marshal(data)
		change.Action = "meta.replace"
	case "replaceOne", "findOneAndReplace":
		change.Data, err = metadataCreateData(r.Replacement)
		if err != nil {
			return nil, err
		}
		change.Action = "meta.replace"
	case "deleteOne", "findOneAndDelete":
		change.Action = "meta.archive"
	case "restoreOne":
		change.Action = "meta.restore"
	default:
		return nil, invalid("action", "unsupported metadata action")
	}
	if err = mutationPermissions(ctx, change); err != nil {
		return nil, err
	}
	saved, err := s.metadataChange(ctx, tx, change)
	if err != nil {
		return nil, err
	}
	if returnValue {
		modified := 0
		if saved.Revision != row.Revision {
			modified = 1
		}
		result["value"] = before
		if r.ReturnDocument == "after" {
			current, err := meta(ctx, r.Kind, row.ID, true)
			if err != nil {
				return nil, err
			}
			result["value"], err = metadataRowDocument(ctx, current, r.Projection)
			return metadataSingleReturningResult(action, result, 1, modified, 0), err
		}
		return metadataSingleReturningResult(action, result, 1, modified, 0), nil
	}
	if action == "deleteOne" {
		return map[string]any{"deletedCount": 1}, nil
	}
	if action == "restoreOne" {
		current, err := meta(ctx, r.Kind, row.ID, true)
		if err != nil {
			return nil, err
		}
		return map[string]any{"restoredCount": 1, "items": []any{CollectionProjection(ctx, current)}}, nil
	}
	modified := 0
	if saved.Revision != row.Revision {
		modified = 1
	}
	return map[string]any{"matchedCount": 1, "modifiedCount": modified, "upsertedCount": 0}, nil
}

// 统一入口保留原计数并附带 value；兼容入口继续返回原有的文档响应形状。
func metadataSingleReturningResult(action string, result map[string]any, matched, modified, upserted int) map[string]any {
	if strings.HasPrefix(action, "findOneAnd") {
		return result
	}
	if action == "deleteOne" {
		result["deletedCount"] = matched
	} else {
		result["matchedCount"], result["modifiedCount"], result["upsertedCount"] = matched, modified, upserted
	}
	return result
}

func metadataBulkItem(raw json.RawMessage) (string, metadataRequest, error) {
	var fields map[string]json.RawMessage
	if err := Decode(raw, &fields); err != nil {
		return "", metadataRequest{}, err
	}
	if len(fields) != 1 {
		return "", metadataRequest{}, invalid("operations", "one action per bulk operation required")
	}
	for action, value := range fields {
		if !contains([]string{"insertOne", "updateOne", "replaceOne", "deleteOne", "restoreOne"}, action) {
			return "", metadataRequest{}, invalid("operations", "unsupported bulk action")
		}
		var r metadataRequest
		if err := Decode(value, &r); err != nil {
			return "", r, err
		}
		return action, r, nil
	}
	return "", metadataRequest{}, invalid("operations", "operation required")
}

func metadataBatchResult() map[string]any {
	return map[string]any{"insertedCount": 0, "matchedCount": 0, "modifiedCount": 0, "deletedCount": 0, "restoredCount": 0, "upsertedCount": 0, "insertedIds": map[string]string{}, "upsertedIds": map[string]string{}, "results": []any{}, "errors": []any{}}
}
func metadataAccumulate(out, mapResult map[string]any, index int) {
	for _, key := range []string{"insertedCount", "matchedCount", "modifiedCount", "deletedCount", "restoredCount", "upsertedCount"} {
		v := 0
		switch n := mapResult[key].(type) {
		case int:
			v = n
		case json.Number:
			i, _ := n.Int64()
			v = int(i)
		}
		if key == "insertedCount" && v == 0 && mapResult["insertedId"] != nil {
			v = 1
		}
		if key == "upsertedCount" && v == 0 && mapResult["upsertedId"] != nil {
			v = 1
		}
		out[key] = out[key].(int) + v
	}
	for _, pair := range [][2]string{{"insertedId", "insertedIds"}, {"upsertedId", "upsertedIds"}} {
		if id, ok := mapResult[pair[0]].(string); ok {
			out[pair[1]].(map[string]string)[fmt.Sprint(index)] = id
		}
	}
	entry := map[string]any{"index": index}
	for key, value := range metadataItemResult(mapResult) {
		entry[key] = value
	}
	out["results"] = append(out["results"].([]any), entry)
}

func metadataItemResult(result map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"insertedId", "matchedCount", "modifiedCount", "deletedCount", "restoredCount", "upsertedId", "value"} {
		if value, ok := result[key]; ok {
			out[key] = value
		}
	}
	return out
}

func (s *Service) metadataWrite(ctx context.Context, tx *gorm.DB, action string, r metadataRequest) (map[string]any, error) {
	if action == "transact" {
		return s.metadataTransact(ctx, tx, r)
	}
	if action == "updateMany" || action == "deleteMany" {
		if len(r.Filter) == 0 {
			return nil, invalid("filter", "explicit filter required")
		}
		max := r.MaxDocs
		if max == 0 {
			max = metadataDocumentLimit
		}
		if max < 1 || max > metadataDocumentLimit {
			return nil, invalid("maxDocs", "1 to 200 required")
		}
		rows, err := metadataSelected(ctx, r, max+1)
		if err != nil {
			return nil, err
		}
		if len(rows) > max {
			return nil, invalid("maxDocs", "matching objects exceed the atomic limit; narrow the filter")
		}
		if action == "deleteMany" && r.Kind == "directory" {
			rows, err = directoryDeleteRoots(tx, rows)
			if err != nil {
				return nil, err
			}
		}
		out := metadataBatchResult()
		for i, row := range rows {
			one := r
			one.Filter, _ = json.Marshal(map[string]string{"_id": row.ID})
			one.ExpectedRevision = row.Revision
			result, err := s.metadataOne(ctx, tx, strings.Replace(action, "Many", "One", 1), one)
			if err != nil {
				return nil, err
			}
			metadataAccumulate(out, result, i)
		}
		if action == "deleteMany" {
			return map[string]any{"deletedCount": out["deletedCount"]}, nil
		}
		return map[string]any{"matchedCount": out["matchedCount"], "modifiedCount": out["modifiedCount"]}, nil
	}
	if action == "insertMany" {
		if len(r.Documents) < 1 || len(r.Documents) > metadataDocumentLimit {
			return nil, invalid("documents", "1 to 200 documents required")
		}
		out := metadataBatchResult()
		for i, doc := range r.Documents {
			result, err := s.metadataOne(ctx, tx, "insertOne", metadataRequest{Kind: r.Kind, ID: doc.ID, Data: doc.Data})
			if err != nil {
				return nil, err
			}
			metadataAccumulate(out, result, i)
		}
		return map[string]any{"insertedCount": out["insertedCount"], "insertedIds": out["insertedIds"]}, nil
	}
	if action == "restore" {
		if len(r.Items) < 1 || len(r.Items) > metadataDocumentLimit {
			return nil, invalid("items", "1 to 200 restore identities required")
		}
		if r.Kind == "directory" {
			return s.restoreDirectories(ctx, tx, r.Items)
		}
		count := 0
		items := []any{}
		seen := map[string]bool{}
		for _, item := range r.Items {
			if !unsmeta.ValidID(item.ID) || seen[item.ID] {
				return nil, invalid("items", "unique valid IDs required")
			}
			seen[item.ID] = true
			result, err := s.metadataOne(ctx, tx, "restoreOne", metadataRequest{Kind: r.Kind, ID: item.ID, ExpectedRevision: item.ExpectedRevision})
			if err != nil {
				return nil, err
			}
			count += result["restoredCount"].(int)
			items = append(items, result["items"].([]any)...)
		}
		return map[string]any{"restoredCount": count, "items": items}, nil
	}
	if action == "bulkWrite" {
		if len(r.Operations) < 1 || len(r.Operations) > metadataDocumentLimit {
			return nil, invalid("operations", "1 to 200 operations required")
		}
		out := metadataBatchResult()
		for i, raw := range r.Operations {
			partialDryRun := r.DryRun && r.Atomic != nil && !*r.Atomic
			if partialDryRun {
				if err := tx.SavePoint("metadata_item").Error; err != nil {
					return nil, err
				}
			}
			action, one, err := metadataBulkItem(raw)
			if err != nil {
				return nil, err
			}
			one.Kind = r.Kind
			result, err := s.metadataOne(ctx, tx, action, one)
			if err != nil {
				if partialDryRun {
					if rollbackErr := tx.RollbackTo("metadata_item").Error; rollbackErr != nil {
						return nil, rollbackErr
					}
					var fault *Fault
					if !errors.As(classify(err), &fault) {
						return nil, err
					}
					copy := *fault
					copy.RolledBack = true
					failure := map[string]any{"index": i, "error": &copy}
					out["errors"] = append(out["errors"].([]any), failure)
					out["results"] = append(out["results"].([]any), failure)
					if r.Ordered == nil || *r.Ordered {
						break
					}
					continue
				}
				return nil, err
			}
			metadataAccumulate(out, result, i)
		}
		return out, nil
	}
	return s.metadataOne(ctx, tx, action, r)
}

func (s *Service) metadataTransact(ctx context.Context, tx *gorm.DB, r metadataRequest) (map[string]any, error) {
	if len(r.Operations) < 1 || len(r.Operations) > metadataDocumentLimit {
		return nil, invalid("operations", "1 to 200 operations required")
	}
	items := make([]metadataTransactionItem, len(r.Operations))
	changes := []Change{}
	for i, raw := range r.Operations {
		item := &items[i]
		if err := Decode(raw, item); err != nil {
			return nil, err
		}
		if !metadataKind(item.Kind) || !contains([]string{"insertOne", "updateOne", "replaceOne", "deleteOne", "restoreOne"}, item.Action) {
			return nil, invalid("operations", "supported metadata kind/action required")
		}
		change := Change{OpID: item.OpID, Kind: item.Kind, ID: Identity{ID: item.ID}, DependsOn: item.DependsOn, ExpectedRevision: item.ExpectedRevision}
		if item.Action == "insertOne" {
			change.Action = "meta.create"
			data, err := metadataCreateData(item.Data)
			if err != nil {
				return nil, err
			}
			change.Data = data
		} else {
			change.Action = metadataDocumentAction
			if change.ID.ID == "" {
				change.ID.ID = "selector-" + item.OpID
			}
			// 复用现有引用解析器，替换和 $set 中的父目录/集合引用同样参与依赖排序。
			if item.Action == "replaceOne" {
				change.Action = "meta.replace"
				data, err := metadataCreateData(item.Replacement)
				if err != nil {
					return nil, err
				}
				change.Data = data
			} else if item.Action == "updateOne" {
				var update map[string]json.RawMessage
				if err := Decode(item.Update, &update); err != nil {
					return nil, err
				}
				if set, ok := update["$set"]; ok {
					change.Action = "meta.replace"
					change.Data = set
				}
			}
		}
		changes = append(changes, change)
	}
	p, err := newPlan(Batch{Mode: "sync", Items: changes})
	if err != nil {
		return nil, err
	}
	results := make([]any, len(items))
	for _, index := range p.Order {
		item := items[index]
		one := metadataRequest{Kind: item.Kind, ID: item.ID, Filter: item.Filter, Sort: item.Sort, Data: item.Data, Update: item.Update, Replacement: item.Replacement, ExpectedRevision: item.ExpectedRevision}
		if item.Action == "insertOne" {
			one.ID = p.Items[index].ID.ID
			one.Data = p.Items[index].Data
		} else if item.Action == "replaceOne" {
			one.Replacement = p.Items[index].Data
		} else if item.Action == "updateOne" && p.Items[index].Action == "meta.replace" {
			var update map[string]json.RawMessage
			_ = Decode(item.Update, &update)
			update["$set"] = p.Items[index].Data
			one.Update, _ = json.Marshal(update)
		}
		value, err := s.metadataOne(ctx, tx, item.Action, one)
		if err != nil {
			return nil, itemFault(index, p.Items[index], err)
		}
		value = metadataItemResult(value)
		value["opId"] = item.OpID
		results[index] = value
	}
	return map[string]any{"results": results}, nil
}

// 非原子批次先提交原始意图；每项复用独立回执，进程中断后重试不会重复已提交的写。
type metadataBulkReceipt struct {
	Type     string          `json:"type"`
	Digest   string          `json:"digest"`
	Request  metadataRequest `json:"request"`
	Response json.RawMessage `json:"response,omitempty"`
}

func (s *Service) metadataPartialBulk(ctx context.Context, r metadataRequest) (any, error) {
	if len(r.Operations) < 1 || len(r.Operations) > metadataDocumentLimit {
		return nil, invalid("operations", "1 to 200 operations required")
	}
	change := metadataChangeEnvelope("bulkWrite", r)
	if err := metadataDocumentPermissions(ctx, change); err != nil {
		return nil, err
	}
	if r.DryRun {
		// 在一个最终回滚的事务中保留前项的模拟结果，避免 insert 后 update 被误判未匹配。
		return s.metadataAtomic(ctx, "bulkWrite", r)
	}
	key := r.IdempotencyKey
	if key == "" {
		key = uuid.NewString()
	}
	identity, err := requestIdentity(ctx, key)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(r)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	id := "api-receipt-" + identity
	lock := func(tx *gorm.DB) error {
		return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,812300001))", identity).Error
	}
	if !r.DryRun {
		err = repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
			if err := lock(tx); err != nil {
				return err
			}
			var row repo.UnsMeta
			err := tx.Where("kind='task' AND id=?", id).Take(&row).Error
			if err == nil {
				var receipt metadataBulkReceipt
				if json.Unmarshal(row.Data, &receipt) != nil || receipt.Type != "metadataBulk" || receipt.Digest != digest {
					return problem("IDEMPOTENCY_CONFLICT", "same key belongs to a different write")
				}
				return nil
			}
			if !errors.Is(err, repo.ErrNotFound) {
				return err
			}
			var jobs int64
			if err := tx.Model(&repo.UnsMeta{}).Where("kind='task' AND id=?", "api-job-"+identity).Count(&jobs).Error; err != nil {
				return err
			}
			if jobs > 0 {
				return problem("IDEMPOTENCY_CONFLICT", "same key belongs to an existing job")
			}
			data, _ := json.Marshal(metadataBulkReceipt{Type: "metadataBulk", Digest: digest, Request: r})
			return tx.Create(&repo.UnsMeta{Kind: "task", ID: id, Data: data, Revision: 1}).Error
		})
		if err != nil {
			return nil, err
		}
	}
	var out map[string]any
	run := func(tx *gorm.DB) error {
		if !r.DryRun {
			if err := lock(tx); err != nil {
				return err
			}
			var row repo.UnsMeta
			if err := tx.Where("kind='task' AND id=?", id).Take(&row).Error; err != nil {
				return err
			}
			var receipt metadataBulkReceipt
			if json.Unmarshal(row.Data, &receipt) != nil || receipt.Type != "metadataBulk" || receipt.Digest != digest {
				return problem("IDEMPOTENCY_CONFLICT", "metadata intent changed")
			}
			if len(receipt.Response) > 0 {
				if err := s.metadataRecheckWrite(ctx, []Change{change}); err != nil {
					return err
				}
				if err := decodeNumberJSON(receipt.Response, &out); err != nil {
					return err
				}
				out["replayed"] = true
				return nil
			}
		}
		out = metadataBatchResult()
		failed := 0
		next := 0
		for index, raw := range r.Operations {
			action, one, err := metadataBulkItem(raw)
			if err != nil {
				return err
			}
			one.Kind = r.Kind
			one.DryRun = r.DryRun
			child := sha256.Sum256([]byte("metadata-bulk/" + key + "/" + fmt.Sprint(index)))
			one.IdempotencyKey = hex.EncodeToString(child[:])
			if action == "restoreOne" {
				one.Items = []metadataRestoreItem{{ID: one.ID, ExpectedRevision: one.ExpectedRevision}}
				one.ID = ""
				action = "restore"
			}
			value, err := s.metadataAtomic(ctx, action, one)
			if err != nil {
				var f *Fault
				if !errors.As(classify(err), &f) || !f.RolledBack || f.Code == "TEMPORARILY_UNAVAILABLE" || f.Code == "INTERNAL_ERROR" {
					unknown := f == nil || !f.RolledBack
					out["nextIndex"], out["replayed"], out["status"] = next, false, "failed"
					succeeded := len(out["results"].([]any)) - failed
					if succeeded > 0 {
						out["status"] = "partiallyCommitted"
					} else if unknown {
						out["status"] = "unknown"
					}
					if unknown {
						out["unknownUntil"] = index + 1
					}
					return partialBatchFault(out, err, succeeded > 0 || unknown, "errors")
				}
				failed++
				failure := map[string]any{"index": index, "error": f}
				out["errors"] = append(out["errors"].([]any), failure)
				out["results"] = append(out["results"].([]any), failure)
				next = index + 1
				if r.Ordered == nil || *r.Ordered {
					if next < len(r.Operations) {
						out["nextIndex"] = next
					}
					break
				}
				continue
			}
			metadataAccumulate(out, value.(map[string]any), index)
			next = index + 1
		}
		out["status"], out["replayed"] = "committed", false
		if r.DryRun {
			out["status"] = "validated"
		}
		if failed > 0 {
			out["status"] = "partiallyCommitted"
			if len(out["results"].([]any)) == failed {
				out["status"] = "failed"
			}
		}
		if !r.DryRun {
			data, _ := json.Marshal(metadataBulkReceipt{Type: "metadataBulk", Digest: digest, Request: r})
			var receipt metadataBulkReceipt
			_ = json.Unmarshal(data, &receipt)
			receipt.Response, _ = json.Marshal(out)
			data, _ = json.Marshal(receipt)
			return tx.Model(&repo.UnsMeta{}).Where("kind='task' AND id=?", id).Update("data", json.RawMessage(data)).Error
		}
		return nil
	}
	if r.DryRun {
		err = run(nil)
	} else {
		err = repo.GetUnsConn(ctx).Transaction(run)
	}
	return out, err
}

func metadataAppCredential(subject contextx.Subject) bool {
	return subject.APIKeyUsageType == repo.APIKeyUsageAppData
}

// 复制层级和专用配置编辑保留原领域能力，通用元数据写入不获得这些资源的权限。
func (s *Service) metadataSpecializedWrite(ctx context.Context, operation ResourceOperation, raw []byte) (any, error) {
	if !ResourceAllowed(ctx, operation) {
		return nil, metadataRejected(problem("FORBIDDEN", "resource operation permission is required"))
	}
	fields, err := validateResourceRequest(operation, raw)
	if err != nil {
		return nil, metadataRejected(err)
	}
	change := Change{OpID: operation.Name, Action: "meta.replace", Kind: operation.Resource, ID: Identity{ID: resourceString(fields, "id")}}
	_ = json.Unmarshal(fields["expectedRevision"], &change.ExpectedRevision)
	if operation.Name == "namespaceCopy" {
		change.Action, change.Kind, change.Data = "meta.copy", resourceString(fields, "kind"), fields["data"]
	} else {
		change.Data, err = metadataCreateData(fields["replacement"])
		if err != nil {
			return nil, metadataRejected(err)
		}
	}
	batch := Batch{Mode: "sync", IdempotencyKey: resourceString(fields, "idempotencyKey"), Items: []Change{change}}
	_ = json.Unmarshal(fields["dryRun"], &batch.DryRun)
	out, err := s.Apply(ctx, batch)
	if err != nil {
		return nil, err
	}
	if len(out.Results) != 1 {
		return nil, problem("INTERNAL_ERROR", "metadata result unavailable")
	}
	result := map[string]any{"status": out.Status, "replayed": out.Replayed}
	if operation.Name == "namespaceCopy" {
		result["insertedId"] = out.Results[0].ID
	} else {
		result["matchedCount"], result["modifiedCount"] = 1, 1
	}
	return result, nil
}

func metadataRejected(err error) error {
	var fault *Fault
	if !errors.As(err, &fault) {
		return err
	}
	copy := *fault
	copy.RolledBack = true
	return &copy
}

func appMetadataCollectionIDs(ctx context.Context) ([]string, error) {
	subject, _ := contextx.SubjectFrom(ctx)
	if !metadataAppCredential(subject) {
		return nil, problem("FORBIDDEN", "App metadata read requires an App credential")
	}
	if subject.APIKeyAppID == "" {
		return nil, problem("FORBIDDEN", "App identity is required")
	}
	_, app, err := appDefinition(ctx, subject.APIKeyAppID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, grant := range app.Collections {
		if contains(grant.Permissions, "record:read") {
			ids = append(ids, grant.CollectionID)
		}
	}
	return ids, nil
}

func (s *Service) metadataRecheckWrite(ctx context.Context, changes []Change) error {
	actor, ok := contextx.SubjectFrom(ctx)
	if !ok {
		return problem("UNAUTHENTICATED", "authenticated user required")
	}
	if actor.MCPRestricted {
		if s.refresh == nil {
			return problem("FORBIDDEN", "MCP authorization cannot be refreshed")
		}
		current, err := s.refresh(ctx, actor)
		if err != nil {
			return problem("FORBIDDEN", "MCP authorization is no longer valid")
		}
		ctx = contextx.WithSubject(ctx, current)
	}
	for _, change := range changes {
		if err := mutationPermissions(ctx, change); err != nil {
			return err
		}
	}
	return nil
}

// 目录回收会覆盖整棵子树，只执行最外层目标，避免父目录改变子目录版本后重复删除。
func directoryDeleteRoots(tx *gorm.DB, rows []repo.UnsMeta) ([]repo.UnsMeta, error) {
	if len(rows) < 2 {
		return rows, nil
	}
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	var covered []string
	if err := tx.Raw(`WITH RECURSIVE ancestors AS (
	 SELECT id AS target_id, data->>'parentId' AS parent_id FROM public.uns_meta WHERE kind='directory' AND id IN ?
	 UNION
	 SELECT a.target_id, p.data->>'parentId' FROM ancestors a JOIN public.uns_meta p ON p.kind='directory' AND p.id=a.parent_id
	) SELECT DISTINCT target_id FROM ancestors WHERE parent_id IN ?`, ids, ids).Scan(&covered).Error; err != nil {
		return nil, err
	}
	roots := make([]repo.UnsMeta, 0, len(rows))
	for _, row := range rows {
		if !contains(covered, row.ID) {
			roots = append(roots, row)
		}
	}
	return roots, nil
}

// 同批根目录与子目录可一起选择恢复；先校验原版本，再由根恢复一次，结果保留请求顺序。
func (s *Service) restoreDirectories(ctx context.Context, tx *gorm.DB, requested []metadataRestoreItem) (map[string]any, error) {
	seen := map[string]bool{}
	rows := make([]repo.UnsMeta, 0, len(requested))
	roots := map[string]string{}
	for _, item := range requested {
		if !unsmeta.ValidID(item.ID) || seen[item.ID] {
			return nil, invalid("items", "unique valid IDs required")
		}
		seen[item.ID] = true
		row, err := meta(ctx, "directory", item.ID, true)
		if errors.Is(err, repo.ErrNotFound) || err == nil && row.DeletedAt == 0 {
			if item.ExpectedRevision > 0 {
				return nil, problem("VERSION_CONFLICT", "metadata target does not match the expected version")
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		if item.ExpectedRevision > 0 && row.Revision != item.ExpectedRevision {
			return nil, problem("VERSION_CONFLICT", "metadata revision changed")
		}
		var doc unsmeta.Document
		if err := json.Unmarshal(row.Data, &doc); err != nil {
			return nil, err
		}
		rows = append(rows, row)
		roots[row.ID] = doc.DirectoryTrashID
	}
	for _, row := range rows {
		if root := roots[row.ID]; root != "" && root != row.ID {
			if _, selected := roots[root]; selected {
				continue
			}
		}
		if _, err := s.metadataOne(ctx, tx, "restoreOne", metadataRequest{Kind: "directory", ID: row.ID, ExpectedRevision: row.Revision}); err != nil {
			return nil, err
		}
	}
	items := make([]any, 0, len(rows))
	for _, row := range rows {
		current, err := meta(ctx, "directory", row.ID, false)
		if err != nil {
			return nil, err
		}
		items = append(items, CollectionProjection(ctx, current))
	}
	return map[string]any{"restoredCount": len(rows), "items": items}, nil
}

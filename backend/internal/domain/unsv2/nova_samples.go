package unsv2

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	docquery "backend/internal/domain/document/query"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"gorm.io/gorm"
)

type novaSampleRequest struct {
	CollectionID string                       `json:"collectionId"`
	RecordIDs    []string                     `json:"recordIds"`
	From         int64                        `json:"from,omitempty"`
	To           int64                        `json:"to,omitempty"`
	Bucket       string                       `json:"bucket,omitempty"`
	Fields       []string                     `json:"fields,omitempty"`
	Quality      []string                     `json:"quality,omitempty"`
	Limit        int                          `json:"limit,omitempty"`
	Cursors      map[string]string            `json:"cursors,omitempty"`
	DryRun       bool                         `json:"dryRun,omitempty"`
	Filter       json.RawMessage              `json:"filter,omitempty"`
	Sort         []RecordOrder                `json:"sort,omitempty"`
	Projection   json.RawMessage              `json:"projection,omitempty"`
	Pipeline     []map[string]json.RawMessage `json:"pipeline,omitempty"`
	Cursor       string                       `json:"cursor,omitempty"`
	Page         *PipelinePage                `json:"page,omitempty"`
}

func (s *Service) novaSampleOperation(ctx context.Context, action string, raw []byte) (any, error) {
	var r novaSampleRequest
	if err := Decode(raw, &r); err != nil {
		return nil, err
	}
	queryAction := action == "sample.find" || action == "sample.aggregate"
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	allowed := map[string]bool{"collectionId": true, "recordIds": true}
	switch action {
	case "sample.find":
		for _, key := range []string{"from", "to", "filter", "sort", "projection", "limit", "cursor", "page"} {
			allowed[key] = true
		}
	case "sample.aggregate":
		for _, key := range []string{"from", "to", "pipeline", "limit", "page"} {
			allowed[key] = true
		}
	case "sample.delete":
		for _, key := range []string{"from", "to", "dryRun"} {
			allowed[key] = true
		}
	case "sample.latest":
	default:
		allowed = nil
	}
	if allowed != nil {
		for key := range fields {
			if !allowed[key] {
				return nil, invalid(key, "field is not accepted by this Sample operation")
			}
		}
	}
	if !unsmeta.ValidID(r.CollectionID) {
		return nil, invalid("collectionId", "valid Collection ID required")
	}
	if len(r.RecordIDs) > MaxQueries || len(r.RecordIDs) == 0 && (!queryAction || fields["recordIds"] != nil) {
		return nil, invalid("recordIds", "1 to 20 unique Record IDs required when supplied")
	}
	seen := map[string]bool{}
	for _, id := range r.RecordIDs {
		if !unsmeta.ValidID(id) || seen[id] {
			return nil, invalid("recordIds", "valid unique Record IDs required")
		}
		seen[id] = true
	}
	permission := "sample:read"
	if action == "sample.delete" {
		permission = "sample:write"
		if err := require(ctx, "uns.delete"); err != nil {
			return nil, err
		}
	} else if err := require(ctx, "uns.read"); err != nil {
		return nil, err
	}
	if err := appCollectionAccess(ctx, r.CollectionID, permission); err != nil {
		return nil, err
	}
	if action == "sample.delete" {
		if err := repo.RequireLocalCollection(ctx, r.CollectionID); err != nil {
			return nil, classify(err)
		}
	} else if out, routed, err := storageRead(ctx, r.CollectionID, action, json.RawMessage(raw)); routed || err != nil {
		return out, classify(err)
	}
	doc, err := readDefinition(ctx, r.CollectionID)
	if err != nil {
		return nil, err
	}
	if doc.SampleSchema == nil {
		return nil, invalid("collectionId", "Collection has no Sample schema")
	}
	if queryAction {
		if fields["from"] == nil || fields["to"] == nil || string(fields["from"]) == "null" || string(fields["to"]) == "null" || r.From < 0 || r.To <= r.From || r.To > 253402300799999 {
			return nil, invalid("from/to", "explicit [from,to) Unix millisecond range required")
		}
		return s.sampleRead(ctx, action, r)
	}
	if r.Limit == 0 {
		r.Limit = min(200, 2000/len(r.RecordIDs))
	}
	if r.Limit < 1 || r.Limit > 200 {
		return nil, invalid("limit", "limit must be 1 to 200")
	}
	if action == "sample.latest" {
		values := make([]map[string]any, 0, len(r.RecordIDs))
		for _, id := range r.RecordIDs {
			row, err := repo.NewUnsMetaRepo(ctx).Latest(ctx, r.CollectionID, id)
			if errors.Is(err, repo.ErrNotFound) {
				values = append(values, map[string]any{"recordId": id, "sample": nil})
				continue
			}
			if err != nil {
				return nil, err
			}
			values = append(values, map[string]any{"recordId": id, "sample": novaSampleView(row)})
		}
		return map[string]any{"series": values}, nil
	}
	if fields["from"] == nil || fields["to"] == nil || string(fields["from"]) == "null" || string(fields["to"]) == "null" || r.From < 0 || r.To <= r.From || r.To > 253402300799999 {
		return nil, invalid("from/to", "explicit [from,to) Unix millisecond range required")
	}
	if action == "sample.delete" {
		meta, err := meta(ctx, "collection", r.CollectionID, false)
		if err != nil {
			return nil, err
		}
		if meta.MountID != nil {
			return nil, problem("FORBIDDEN", "mounted Samples are read-only")
		}
		var deleted int64
		err = repo.NewUnsMetaRepo(ctx).Transaction(ctx, func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT pg_advisory_xact_lock_shared(814206108)").Error; err != nil {
				return err
			}
			if _, err := definition(repo.WithUnsTransaction(ctx, tx), r.CollectionID); err != nil {
				return err
			}
			locks := make([]repo.UnsRecordLock, 0, len(r.RecordIDs))
			for _, id := range r.RecordIDs {
				locks = append(locks, repo.UnsRecordLock{CollectionID: r.CollectionID, RecordID: id})
			}
			if err := repo.LockUnsRecords(tx, locks...); err != nil {
				return err
			}
			q := tx.Where("collection_id=? AND record_id IN ? AND time>=? AND time<?", r.CollectionID, r.RecordIDs, time.UnixMilli(r.From), time.UnixMilli(r.To))
			if r.DryRun {
				return q.Model(&repo.UnsSample{}).Count(&deleted).Error
			}
			result := q.Delete(&repo.UnsSample{})
			deleted = result.RowsAffected
			return result.Error
		})
		return map[string]any{"deletedCount": deleted, "dryRun": r.DryRun}, classify(err)
	}
	if r.Bucket != "" {
		return s.novaSampleBuckets(ctx, r, doc.SampleSchema)
	}
	queries := make([]DataQuery, 0, len(r.RecordIDs))
	for _, id := range r.RecordIDs {
		filter := map[string]any{"_meta.time": map[string]any{"$gte": r.From, "$lt": r.To}}
		if len(r.Quality) > 0 {
			filter["_meta.quality"] = map[string]any{"$in": r.Quality}
		}
		match, _ := json.Marshal(filter)
		sort, _ := json.Marshal([]RecordOrder{{Field: "_meta.time", Direction: "desc"}, {Field: "_id", Direction: "desc"}})
		queries = append(queries, DataQuery{QueryID: id, CollectionID: r.CollectionID, RecordID: id, Pipeline: []map[string]json.RawMessage{{"$match": match}, {"$sort": sort}}, Page: &PipelinePage{Size: r.Limit, Mode: "cursor", Cursor: r.Cursors[id]}})
	}
	out, err := s.DataQuery(ctx, "samples", DataQueries{Queries: queries})
	if err != nil {
		return nil, err
	}
	series := out.(map[string]any)["results"].([]map[string]any)
	for _, value := range series {
		value["recordId"] = value["queryId"]
		delete(value, "queryId")
	}
	return map[string]any{"series": series, "range": map[string]int64{"from": r.From, "to": r.To}}, nil
}

func (s *Service) sampleRead(ctx context.Context, action string, r novaSampleRequest) (any, error) {
	limit := r.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 200 {
		return nil, invalid("limit", "limit must be 1 to 200")
	}
	stages := r.Pipeline
	var projection *docquery.Projection
	if action == "sample.find" {
		if r.Pipeline != nil {
			return nil, invalid("pipeline", "find accepts filter, sort, projection and pagination")
		}
		filter := r.Filter
		if len(filter) == 0 {
			filter = json.RawMessage(`{}`)
		}
		stages = []map[string]json.RawMessage{{"$match": filter}}
		order := r.Sort
		if len(order) == 0 {
			order = []RecordOrder{{Field: "_meta.time", Direction: "desc"}}
		}
		sort, _ := json.Marshal(order)
		stages = append(stages, map[string]json.RawMessage{"$sort": sort})
		if len(r.Projection) > 0 {
			var value any
			if err := decodeNumberJSON(r.Projection, &value); err != nil {
				return nil, invalid("projection", "object required")
			}
			var err error
			projection, err = docquery.ParseProjection(value)
			if err != nil {
				return nil, invalid("projection", err.Error())
			}
			if projection != nil {
				schema, err := pipelineSchema(ctx, "samples", r.CollectionID)
				if err != nil {
					return nil, err
				}
				compiler := pipelineCompiler{ctx: ctx}
				for _, field := range projection.Paths {
					if _, err := compiler.field("base:b", field.Raw, schema); err != nil {
						return nil, err
					}
				}
			}
		}
	} else {
		if stages == nil {
			return nil, invalid("pipeline", "pipeline array required")
		}
		if len(r.Filter) > 0 || len(r.Sort) > 0 || len(r.Projection) > 0 || r.Cursor != "" {
			return nil, invalid("pipeline", "aggregate uses pipeline and page; find options are not accepted")
		}
	}
	page := &PipelinePage{Size: limit, Cursor: r.Cursor}
	// Preserve an omitted page for aggregate; scalar $count/$exists results
	// reject explicit pagination in the shared query validator.
	if action == "sample.aggregate" && r.Limit == 0 {
		page = nil
	}
	if r.Page != nil {
		if r.Limit != 0 || r.Cursor != "" {
			return nil, invalid("page", "page cannot be combined with limit or cursor")
		}
		copy := *r.Page
		page = &copy
		if page.Size == 0 {
			page.Size = limit
		}
	}
	q := DataQuery{QueryID: "samples", CollectionID: r.CollectionID, Pipeline: stages, Page: page, sampleScope: &sampleQueryScope{RecordIDs: r.RecordIDs, From: r.From, To: r.To}}
	if projection != nil {
		q.projection = projection.String()
	}
	out, err := s.DataQuery(ctx, "samples", DataQueries{Queries: []DataQuery{q}})
	if err != nil {
		return nil, err
	}
	result := out.(map[string]any)["results"].([]map[string]any)[0]
	if projection != nil {
		items := result["items"].([]any)
		for i, item := range items {
			doc := item.(map[string]any)
			projected := projection.Apply(doc)
			projected["_id"], projected["_meta"] = doc["_id"], doc["_meta"]
			items[i] = projected
		}
	}
	if action == "sample.find" {
		items := result["items"].([]any)
		for i, item := range items {
			view, err := sampleQueryView(item.(map[string]any))
			if err != nil {
				return nil, err
			}
			items[i] = view
		}
	}
	delete(result, "queryId")
	delete(result, "resultMode")
	return result, nil
}

// 查询语言保留扁平业务字段；仅在公开查询响应处恢复采样资源结构。
type SampleDocument struct {
	ID           string          `json:"id"`
	CollectionID string          `json:"collectionId"`
	RecordID     string          `json:"recordId"`
	Time         int64           `json:"time"`
	ReceivedAt   int64           `json:"receivedAt"`
	Quality      string          `json:"quality"`
	Data         json.RawMessage `json:"data"`
	SourceID     *string         `json:"sourceId"`
	StreamID     string          `json:"streamId,omitempty"`
	BindingID    string          `json:"bindingId,omitempty"`
}

func sampleQueryView(document map[string]any) (SampleDocument, error) {
	metadata := document["_meta"].(map[string]any)
	data := make(map[string]any, len(document))
	for key, value := range document {
		if key != "_id" && key != "_meta" {
			data[key] = value
		}
	}
	value := map[string]any{"id": document["_id"], "collectionId": metadata["collectionId"], "recordId": metadata["recordId"], "time": metadata["time"], "receivedAt": metadata["receivedAt"], "quality": metadata["quality"], "data": data, "sourceId": metadata["sourceId"], "streamId": metadata["streamId"], "bindingId": metadata["bindingId"]}
	raw, err := json.Marshal(value)
	if err != nil {
		return SampleDocument{}, err
	}
	var result SampleDocument
	err = json.Unmarshal(raw, &result)
	return result, err
}

func novaSampleView(row repo.UnsSample) SampleDocument {
	return SampleDocument{ID: row.ID, CollectionID: row.CollectionID, RecordID: row.RecordID, Time: row.Time.UnixMilli(), ReceivedAt: row.ReceivedAt.UnixMilli(), Quality: row.Quality, Data: row.Data, SourceID: row.SourceID, StreamID: row.StreamID, BindingID: row.BindingID}
}

func (s *Service) novaSampleBuckets(ctx context.Context, r novaSampleRequest, schema *unsmeta.Schema) (any, error) {
	if len(r.Cursors) > 0 {
		return nil, invalid("cursors", "bucket queries do not accept raw Sample cursors")
	}
	width, err := sampleBucketWidth(r.Bucket)
	if err != nil {
		return nil, err
	}
	if len(r.Fields) < 1 || len(r.Fields) > 8 {
		return nil, invalid("fields", "bucket queries require 1 to 8 declared numeric Sample fields")
	}
	columns := []string{}
	pairs := []string{}
	seen := map[string]bool{}
	compiler := pipelineCompiler{ctx: ctx}
	fullSchema, err := pipelineSchema(ctx, "samples", r.CollectionID)
	if err != nil {
		return nil, err
	}
	for i, field := range r.Fields {
		if seen[field] {
			return nil, invalid("fields", "duplicate bucket field")
		}
		seen[field] = true
		parts, err := unsmeta.ParseFieldPath(field)
		if err != nil {
			return nil, invalid("fields", "invalid field path")
		}
		declared, ok := unsmeta.SchemaField(schema, parts)
		if !ok || declared.Type != "integer" && declared.Type != "number" {
			return nil, invalid("fields", "fields must name declared numeric business data")
		}
		expr, err := compiler.field("base:b", field, fullSchema)
		if err != nil {
			return nil, err
		}
		name := "v" + strconv.Itoa(i)
		columns = append(columns, pipelineScalar(expr)+" AS "+name)
		stats := []string{"'avg',avg(" + name + ")", "'min',min(" + name + ")", "'max',max(" + name + ")", "'count',count(" + name + ")", "'sum',sum(" + name + ")", "'first',max(" + name + ") FILTER(WHERE first_rank=1)", "'last',max(" + name + ") FILTER(WHERE last_rank=1)"}
		pairs = append(pairs, pipelineLiteral(field)+",jsonb_build_object("+strings.Join(stats, ",")+")")
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	series := []map[string]any{}
	err = repo.GetUnsConn(ctx).WithContext(bounded).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL statement_timeout='15000ms'").Error; err != nil {
			return err
		}
		for _, record := range r.RecordIDs {
			quality := ""
			args := []any{width, width, r.CollectionID, record, time.UnixMilli(r.From), time.UnixMilli(r.To)}
			if len(r.Quality) > 0 {
				quality = " AND b.quality IN ?"
				args = append(args, r.Quality)
			}
			query := "WITH points AS (SELECT (floor(extract(epoch FROM b.time)*1000/?)*?)::bigint AS bucket,b.time,b.id," + strings.Join(columns, ",") + " FROM public.uns_sample b WHERE b.collection_id=? AND b.record_id=? AND b.time>=? AND b.time<?" + quality + "), ranked AS (SELECT *,row_number() OVER(PARTITION BY bucket ORDER BY time,id) AS first_rank,row_number() OVER(PARTITION BY bucket ORDER BY time DESC,id DESC) AS last_rank FROM points) SELECT bucket AS time,jsonb_build_object(" + strings.Join(pairs, ",") + ") AS data FROM ranked GROUP BY bucket ORDER BY bucket LIMIT ?"
			args = append(args, r.Limit+1)
			var rows []struct {
				Time int64
				Data json.RawMessage
			}
			if err := tx.Raw(query, args...).Scan(&rows).Error; err != nil {
				return err
			}
			hasMore := len(rows) > r.Limit
			if hasMore {
				rows = rows[:r.Limit]
			}
			items := []map[string]any{}
			for _, row := range rows {
				items = append(items, map[string]any{"time": row.Time, "data": row.Data})
			}
			value := map[string]any{"recordId": record, "items": items, "hasMore": hasMore}
			if hasMore {
				value["nextFrom"] = rows[len(rows)-1].Time + width
			}
			series = append(series, value)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, pipelineRuntimeError(err)
	}
	return map[string]any{"series": series, "bucket": r.Bucket, "range": map[string]int64{"from": r.From, "to": r.To}}, nil
}

func sampleBucketWidth(value string) (int64, error) {
	var width time.Duration
	var err error
	if strings.HasSuffix(value, "d") {
		days, e := strconv.ParseInt(strings.TrimSuffix(value, "d"), 10, 32)
		if e != nil || days < 1 || days > 365 {
			return 0, invalid("bucket", "bucket must be a duration from 1ms to 365d")
		}
		width = time.Duration(days) * 24 * time.Hour
	} else {
		width, err = time.ParseDuration(value)
	}
	if err != nil || width < time.Millisecond || width > 365*24*time.Hour || width%time.Millisecond != 0 {
		return 0, invalid("bucket", fmt.Sprintf("invalid fixed UTC bucket %q; use 1ms through 365d", value))
	}
	return width.Milliseconds(), nil
}

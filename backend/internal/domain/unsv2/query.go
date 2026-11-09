package unsv2

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/unsref"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

func bounds(p Pagination, max int) (int, int, error) {
	if p.Page < 0 || p.Size < 0 || p.Size > max || len(p.Search) > 128 {
		return 0, 0, invalid("page/size/search", "invalid pagination")
	}
	if p.Page == 0 {
		p.Page = 1
	}
	if p.Size == 0 {
		p.Size = 20
	}
	return p.Page, p.Size, nil
}
func pageResult(list any, total int64, page, size int) map[string]any {
	hasMore := int64(page)*int64(size) < total
	out := map[string]any{"list": list, "total": total, "page": page, "size": size, "hasMore": hasMore, "complete": !hasMore && page == 1, "truncated": false, "snapshotScope": "thisRequestOnly"}
	if hasMore {
		out["nextPage"] = page + 1
	}
	return out
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func readPermission(ctx context.Context, kind string) error {
	switch kind {
	case "directory", "topic", "collection":
		return require(ctx, "uns.read")
	case "source", "source_state":
		return require(ctx, "uns.source")
	case "stream", "source_plan", "source_version":
		return require(ctx, "uns.stream")
	case "app":
		return require(ctx, "uns.app.read")
	default:
		return problem("FORBIDDEN", "system tasks are available only through their dedicated query")
	}
}
func projection(row repo.UnsMeta) map[string]any {
	if row.Kind == "folder" {
		row.Kind = "directory"
	}
	var data map[string]any
	_ = decodeNumberJSON(row.Data, &data)
	delete(data, "credentials")
	delete(data, "programManifestJSON")
	delete(data, "nodeType")
	if row.Kind == "directory" || row.Kind == "topic" || row.Kind == "collection" {
		if _, exists := data["parentId"]; !exists {
			data["parentId"] = nil
		}
	}
	out := map[string]any{"kind": row.Kind, "id": row.ID, "mountId": row.MountID, "data": data, "revision": row.Revision, "createdAt": row.CreatedAt, "updatedAt": row.UpdatedAt, "deletedAt": row.DeletedAt}
	if row.Kind == "app" {
		out["launch"] = appLaunch(data["entry"])
		if data["status"] == "disabled" || row.DeletedAt != 0 {
			out["launch"] = map[string]string{"status": "disabled"}
		}
	}
	return out
}
func (s *Service) Query(ctx context.Context, operation string, request Queries) (any, error) {
	if len(request.Queries) < 1 || len(request.Queries) > MaxQueries {
		return nil, invalid("queries", "1 to 20 queries required")
	}
	seen := map[string]bool{}
	budget := 0
	complex := false
	for _, q := range request.Queries {
		if q.CursorOptions.present() && (q.PaginationMode != "cursor" || operation != "records" && operation != "samples" || q.Relational != nil || operation == "samples" && q.Mode != "" && q.Mode != "history") {
			return nil, invalid("paginationMode", "cursor options require ordinary records or samples history in cursor mode")
		}
		if operation == "records" && len(q.IDs) > 0 {
			return nil, invalid("ids", "Record queries use recordIds; ids selects metadata objects")
		}
		complex = complex || q.Relational != nil
		if q.QueryID == "" || len(q.QueryID) > 128 || seen[q.QueryID] {
			return nil, invalid("queryId", "unique queryId required")
		}
		seen[q.QueryID] = true
		_, size, err := bounds(q.Pagination, 1000)
		if err != nil {
			return nil, err
		}
		budget += size
		if operation == "samples" && q.Mode == "trend" {
			points, err := trendPointLimit(q.MaxPoints)
			if err != nil {
				return nil, err
			}
			budget += points
		} else if len(q.FieldPath) > 0 || q.MaxPoints != 0 {
			return nil, invalid("fieldPath/maxPoints", "only supported by samples trend")
		}
		if operation == "references" {
			budget += size * (len(q.Targets) - 1)
		}
		if operation == "samples" && q.Mode == "latest" {
			budget += len(q.RecordIDs)
		}
		if q.IncludeLatestSample {
			budget += size
		}
		if q.Records != nil {
			_, size, err = bounds(*q.Records, 200)
			if err != nil {
				return nil, err
			}
			budget += size
			if contains(q.Include, "latestSamples") {
				budget += size
			}
		}
		if q.SelectedRecord != nil && q.SelectedRecord.History != nil {
			_, size, err = bounds(q.SelectedRecord.History.Pagination, 1000)
			if err != nil {
				return nil, err
			}
			budget += size
		}
	}
	if budget > 2000 {
		return nil, invalid("queries", "query response row budget exceeds 2000")
	}
	results := make([]map[string]any, 0, len(request.Queries))
	if complex {
		bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		ctx = repo.WithUnsTransaction(bounded, repo.GetUnsConn(ctx).WithContext(bounded))
		release, err := s.admitDataWork(ctx)
		if err != nil {
			if !errors.Is(err, context.DeadlineExceeded) {
				return nil, classify(err)
			}
			return nil, &Fault{Code: "QUERY_TIMEOUT", Message: "Query budget expired waiting for execution capacity", Retryable: true, Repair: "Retry with lower concurrency or narrow the query"}
		}
		defer release()
	}
	err := repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
		qctx := repo.WithUnsTransaction(ctx, tx)
		for _, q := range request.Queries {
			result, err := s.query(qctx, operation, q)
			if err != nil {
				var pg *pgconn.PgError
				if complex && (errors.Is(err, context.DeadlineExceeded) || errors.As(err, &pg) && pg.Code == "57014") {
					return &Fault{Code: "QUERY_TIMEOUT", Message: "Query exceeded its execution budget", Retryable: true, Repair: "Reduce joins or narrow the source rows with where predicates"}
				}
				if q.Relational != nil && errors.As(err, &pg) && pg.Code == "22003" {
					return invalid("relational", "Numeric predicate or aggregate result exceeds the database numeric range")
				}
				return classify(err)
			}
			result["queryId"] = q.QueryID
			results = append(results, result)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		if complex && ctx.Err() == context.DeadlineExceeded {
			return nil, &Fault{Code: "QUERY_TIMEOUT", Message: "Query batch exceeded its execution budget", Retryable: true, Repair: "Reduce joins, predicates or the number of queries"}
		}
		return nil, err
	}
	out := map[string]any{"results": results}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	if len(encoded) > MaxQueryResponseBytes {
		return nil, problem("RESPONSE_TOO_LARGE", "query exceeds 8 MiB; reduce page size or included data")
	}
	return out, nil
}
func (s *Service) query(ctx context.Context, operation string, q Query) (map[string]any, error) {
	if err := appQueryAccess(ctx, operation, q); err != nil {
		return nil, err
	}
	if operation != "records" && (len(q.Filters) > 0 || len(q.OrderBy) > 0) {
		return nil, invalid("filters/orderBy", "supported by records query only")
	}
	if operation != "records" && q.Relational != nil {
		return nil, invalid("relational", "supported by records query only")
	}
	if operation == "jobs" {
		return s.jobs(ctx, q)
	}
	if operation == "graph" {
		return graphQuery(ctx, q)
	}
	if operation == "metadata" || operation == "namespace" || operation == "collections" && q.Mode != "detail" {
		return s.metadataQuery(ctx, operation, q)
	}
	if err := require(ctx, "uns.read"); err != nil {
		return nil, err
	}
	if (operation == "records" || operation == "samples" || operation == "collections") && q.CollectionID != "" {
		if out, routed, err := storageRead(ctx, q.CollectionID, "query."+operation, q); routed || err != nil {
			if err == nil && operation == "collections" {
				out["collectionId"] = q.CollectionID
				if contains(q.Include, "definition") {
					if row, e := meta(ctx, "collection", q.CollectionID, false); e == nil {
						out["definition"] = CollectionProjection(ctx, row)
					} else {
						return nil, e
					}
				}
			}
			return out, err
		}
	}
	switch operation {
	case "collections":
		return s.collectionQuery(ctx, q)
	case "records":
		if q.Relational != nil {
			return relationalQuery(ctx, q)
		}
		return recordsQuery(ctx, q)
	case "samples":
		return samplesQuery(ctx, q)
	case "topics":
		row, err := meta(ctx, "topic", q.TopicID, false)
		if err != nil {
			return nil, err
		}
		view, err := s.metadata.View(ctx, row.ID, "topic")
		if err != nil {
			return nil, err
		}
		view["meta"] = CollectionProjection(ctx, row)
		return view, nil
	case "references":
		return referencesQuery(ctx, q)
	default:
		return nil, invalid("operation", "unknown query")
	}
}
func (s *Service) metadataQuery(ctx context.Context, operation string, q Query) (map[string]any, error) {
	kinds := q.Kinds
	if operation == "namespace" {
		kinds = []string{"directory", "topic", "collection"}
	}
	if operation == "collections" {
		kinds = []string{"collection"}
	}
	if len(kinds) == 0 || len(kinds) > len(Kinds) {
		return nil, invalid("kinds", "explicit kinds required")
	}
	for _, kind := range kinds {
		if !contains(Kinds, kind) {
			return nil, invalid("kinds", "unknown kind")
		}
		if err := readPermission(ctx, kind); err != nil {
			return nil, err
		}
	}
	page, size, err := bounds(q.Pagination, 200)
	if err != nil {
		return nil, err
	}
	query := repo.GetUnsConn(ctx).Model(&repo.UnsMeta{}).Where("kind IN ?", kinds).
		Where("id NOT LIKE ?", unsref.StreamRuntimePrefix+"%")
	if contains(kinds, "app") {
		filter, args, err := nativeAppVisibility(ctx)
		if err != nil {
			return nil, err
		}
		query = query.Where(filter, args...)
	}
	if q.IncludeArchived {
		query = query.Where("deleted_at>0")
	} else {
		query = query.Where("deleted_at=0")
	}
	if len(q.IDs) > 0 {
		if len(q.IDs) > 200 {
			return nil, invalid("ids", "too many IDs")
		}
		query = query.Where("id IN ?", q.IDs)
	}
	if q.Search != "" {
		query = query.Where("(data->>'name' ILIKE ? OR id ILIKE ?)", "%"+q.Search+"%", "%"+q.Search+"%")
	}
	if operation == "namespace" {
		parent := ""
		if q.ParentID != nil {
			parent = *q.ParentID
		}
		query = query.Where("COALESCE(data->>'parentId','')=?", parent)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	rows := []repo.UnsMeta{}
	if err := query.Order("created_at,kind,id").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, err
	}
	list := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		list = append(list, CollectionProjection(ctx, row))
	}
	out := pageResult(list, total, page, size)
	if q.IncludeAncestors && operation == "namespace" {
		ancestors := []map[string]any{}
		parent := ""
		if q.ParentID != nil {
			parent = *q.ParentID
		}
		seen := map[string]bool{}
		for parent != "" && !seen[parent] && len(ancestors) < 128 {
			seen[parent] = true
			row, err := meta(ctx, "directory", parent, false)
			if err != nil {
				return nil, err
			}
			ancestors = append(ancestors, CollectionProjection(ctx, row))
			var d struct {
				ParentID string `json:"parentId"`
			}
			_ = json.Unmarshal(row.Data, &d)
			parent = d.ParentID
		}
		out["ancestors"] = ancestors
	}
	return out, nil
}
func recordsQuery(ctx context.Context, q Query) (map[string]any, error) {
	doc, err := readDefinition(ctx, q.CollectionID)
	if err != nil {
		return nil, err
	}
	page, size, err := bounds(q.Pagination, 200)
	if err != nil {
		return nil, err
	}
	query := repo.GetUnsConn(ctx).Model(&repo.UnsRecord{}).Where("collection_id=? AND deleted_at IS NULL", q.CollectionID)
	if len(q.RecordIDs) > 200 {
		return nil, invalid("recordIds", "too many record IDs")
	}
	if len(q.RecordIDs) > 0 {
		query = query.Where("id IN ?", q.RecordIDs)
	}
	if q.Search != "" {
		pattern := "%" + q.Search + "%"
		query = query.Where("(id ILIKE ? OR EXISTS (SELECT 1 FROM jsonb_each_text(data) field WHERE field.value ILIKE ?))", pattern, pattern)
	}
	query, err = recordFilters(query, doc.RecordSchema, q.Filters)
	if err != nil {
		return nil, err
	}
	if q.PaginationMode == "cursor" {
		return recordCursorQuery(ctx, query, q, size)
	}
	var total int64
	// An explicit ID set bounds the entire result. If it fits on the first
	// page, the returned rows themselves give the exact total, even with filters
	// or repeated/missing IDs. Other pages still require a separate count.
	boundedIDs := page == 1 && len(q.RecordIDs) > 0 && len(q.RecordIDs) <= size
	if !boundedIDs {
		if err := query.Count(&total).Error; err != nil {
			return nil, err
		}
	}
	rows := []repo.UnsRecord{}
	query, err = recordOrder(query, doc.RecordSchema, q.OrderBy)
	if err != nil {
		return nil, err
	}
	if err := query.Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, err
	}
	if boundedIDs {
		total = int64(len(rows))
	}
	result := pageResult(rows, total, page, size)
	if q.IncludeLatestSample {
		ids := make([]string, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		latest, err := latestQuery(ctx, q.CollectionID, ids)
		if err != nil {
			return nil, err
		}
		result["latestSamples"] = latest
	}
	return result, nil
}
func sampleView(row repo.UnsSample) map[string]any {
	return map[string]any{"id": row.ID, "collectionId": row.CollectionID, "recordId": row.RecordID, "time": row.Time.UnixMilli(), "receivedAt": row.ReceivedAt.UnixMilli(), "quality": row.Quality, "data": row.Data, "sourceId": row.SourceID}
}
func latestQuery(ctx context.Context, collection string, ids []string) ([]map[string]any, error) {
	list := make([]map[string]any, 0, len(ids))
	if len(ids) == 0 {
		return list, nil
	}
	rows := []repo.UnsSample{}
	if err := repo.GetUnsConn(ctx).Raw("SELECT DISTINCT ON(record_id) * FROM public.uns_sample WHERE collection_id=? AND record_id IN ? ORDER BY record_id,time DESC,id DESC", collection, ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	byID := map[string]repo.UnsSample{}
	for _, r := range rows {
		byID[r.RecordID] = r
	}
	for _, id := range ids {
		var value any
		if r, ok := byID[id]; ok {
			value = sampleView(r)
		}
		list = append(list, map[string]any{"recordId": id, "sample": value})
	}
	return list, nil
}
func samplesQuery(ctx context.Context, q Query) (map[string]any, error) {
	doc, err := readDefinition(ctx, q.CollectionID)
	if err != nil {
		return nil, err
	}
	if doc.SampleSchema == nil {
		return nil, problem("SAMPLES_NOT_ENABLED", "Collection has no sample schema")
	}
	if len(q.RecordIDs) > 200 {
		return nil, invalid("recordIds", "too many records")
	}
	if q.Mode == "trend" {
		return sampleTrendQuery(ctx, doc.SampleSchema, q)
	}
	if q.Mode == "latest" {
		if len(q.RecordIDs) == 0 || q.TimeRange != nil {
			return nil, invalid("recordIds", "latest requires record IDs and no time range")
		}
		var count int64
		if err := repo.GetUnsConn(ctx).Model(&repo.UnsRecord{}).Where("collection_id=? AND id IN ?", q.CollectionID, q.RecordIDs).Count(&count).Error; err != nil {
			return nil, err
		}
		if count != int64(len(q.RecordIDs)) {
			return nil, problem("NOT_FOUND", "one or more records do not exist")
		}
		latest, err := latestQuery(ctx, q.CollectionID, q.RecordIDs)
		return map[string]any{"collectionId": q.CollectionID, "latestSamples": latest}, err
	}
	if q.Mode != "" && q.Mode != "history" {
		return nil, invalid("mode", "history, latest or trend required")
	}
	page, size, err := bounds(q.Pagination, 1000)
	if err != nil {
		return nil, err
	}
	rangeValue := q.TimeRange
	if rangeValue == nil {
		now := time.Now().UnixMilli()
		rangeValue = &TimeRange{From: now - 24*60*60*1000, To: now + 1}
	}
	if rangeValue.From < 0 || rangeValue.To <= rangeValue.From {
		return nil, invalid("timeRange", "expected [from,to)")
	}
	query := repo.GetUnsConn(ctx).Model(&repo.UnsSample{}).Where("collection_id=? AND time>=? AND time<?", q.CollectionID, time.UnixMilli(rangeValue.From), time.UnixMilli(rangeValue.To))
	if len(q.RecordIDs) > 0 {
		query = query.Where("record_id IN ?", q.RecordIDs)
	}
	if q.PaginationMode == "cursor" {
		return sampleCursorQuery(query, q, size)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, err
	}
	rows := []repo.UnsSample{}
	if err := query.Order("time DESC,id DESC,record_id").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, err
	}
	list := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		list = append(list, sampleView(r))
	}
	return pageResult(list, total, page, size), nil
}
func (s *Service) collectionQuery(ctx context.Context, q Query) (map[string]any, error) {
	row, err := meta(ctx, "collection", q.CollectionID, false)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"collectionId": q.CollectionID}
	for _, inc := range q.Include {
		if !contains([]string{"definition", "records", "latestSamples", "selectedRecord"}, inc) {
			return nil, invalid("include", "unknown expansion")
		}
	}
	if contains(q.Include, "definition") {
		result["definition"] = CollectionProjection(ctx, row)
	}
	ids := []string{}
	if contains(q.Include, "records") {
		p := Pagination{}
		if q.Records != nil {
			p = *q.Records
		}
		records, err := recordsQuery(ctx, Query{CollectionID: q.CollectionID, Pagination: p})
		if err != nil {
			return nil, err
		}
		result["records"] = records
		for _, r := range records["list"].([]repo.UnsRecord) {
			ids = append(ids, r.ID)
		}
	}
	if q.SelectedRecord != nil && contains(q.Include, "selectedRecord") {
		selected := q.SelectedRecord
		record, err := repo.NewUnsMetaRepo(ctx).Record(ctx, q.CollectionID, selected.RecordID)
		if err != nil {
			return nil, err
		}
		value := map[string]any{"record": record}
		ids = append(ids, record.ID)
		if selected.History != nil {
			history, err := samplesQuery(ctx, Query{CollectionID: q.CollectionID, RecordIDs: []string{record.ID}, Mode: "history", Pagination: selected.History.Pagination, TimeRange: selected.History.TimeRange})
			if err != nil {
				return nil, err
			}
			value["history"] = history
		}
		result["selectedRecord"] = value
	}
	if contains(q.Include, "latestSamples") {
		latest, err := latestQuery(ctx, q.CollectionID, ids)
		if err != nil {
			return nil, err
		}
		result["latestSamples"] = latest
	}
	return result, nil
}
func referencesQuery(ctx context.Context, q Query) (map[string]any, error) {
	if len(q.Targets) == 0 || len(q.Targets) > 200 {
		return nil, invalid("targets", "1 to 200 targets required")
	}
	page, size, err := bounds(q.Pagination, 200)
	if err != nil {
		return nil, err
	}
	results := []map[string]any{}
	for _, target := range q.Targets {
		if target.Kind != "collection" && target.Kind != "topic" && target.Kind != "stream" {
			return nil, invalid("kind", "unsupported reference target")
		}
		if err := readPermission(ctx, target.Kind); err != nil {
			return nil, err
		}
		if _, err := meta(ctx, target.Kind, target.ID, false); err != nil {
			return nil, err
		}
		// Compare only fixed reference fields, not arbitrary user JSON content.
		rows := []repo.UnsMeta{}
		db := repo.GetUnsConn(ctx)
		if readPermission(ctx, "app") == nil {
			filter, args, err := nativeAppVisibility(ctx)
			if err != nil {
				return nil, err
			}
			db = db.Where(filter, args...)
		}
		predicate := `(kind='topic' AND (data->>'collectionId'=? OR EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(data->'fields')='array' THEN data->'fields' ELSE '[]'::jsonb END) f WHERE f->>'collectionId'=?))) OR
 (kind='source' AND EXISTS(SELECT 1 FROM jsonb_each(CASE WHEN jsonb_typeof(data->'config'->'streams')='object' THEN data->'config'->'streams' ELSE '{}'::jsonb END) s WHERE s.value->'target'->>'collectionId'=? OR s.value->>'topicId'=? OR s.value->>'streamId'=?)) OR
 (kind='stream' AND data->'draft'->'execution'->'target'->>'collectionId'=?) OR
 (kind='app' AND (EXISTS(SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(data->'collections')='array' THEN data->'collections' ELSE '[]'::jsonb END) g WHERE g->>'collectionId'=?) OR data->'navigation'->'topicIds' @> ?::jsonb))`
		// Different kinds may share an id; empty impossible IDs disable unrelated paths.
		collection, topic, stream := "", "", ""
		switch target.Kind {
		case "collection":
			collection = target.ID
		case "topic":
			topic = target.ID
		case "stream":
			stream = target.ID
		}
		nav, _ := json.Marshal([]string{topic})
		if err := db.Where("deleted_at=0 AND ("+predicate+")", collection, collection, collection, topic, stream, collection, collection, string(nav)).Find(&rows).Error; err != nil {
			return nil, err
		}
		visible := []map[string]any{}
		hidden := false
		for _, row := range rows {
			if readPermission(ctx, row.Kind) != nil {
				hidden = true
				continue
			}
			visible = append(visible, CollectionProjection(ctx, row))
		}
		start := (page - 1) * size
		if start > len(visible) {
			start = len(visible)
		}
		end := start + size
		if end > len(visible) {
			end = len(visible)
		}
		results = append(results, map[string]any{"target": target, "references": pageResult(visible[start:end], int64(len(visible)), page, size), "hasHiddenReferences": hidden})
	}
	return map[string]any{"targets": results}, nil
}
func (s *Service) jobs(ctx context.Context, q Query) (map[string]any, error) {
	if err := require(ctx, "uns.read"); err != nil {
		return nil, err
	}
	if len(q.JobIDs) == 0 || len(q.JobIDs) > 20 {
		return nil, invalid("jobIds", "1 to 20 jobs required")
	}
	subject, _ := contextx.SubjectFrom(ctx)
	list := []map[string]any{}
	for _, id := range q.JobIDs {
		var row repo.UnsMeta
		if err := repo.GetUnsConn(ctx).Where("kind='task' AND id=? AND data->>'type'='apiBatchJob'", id).Take(&row).Error; err != nil {
			return nil, err
		}
		var data jobData
		if err := json.Unmarshal(row.Data, &data); err != nil {
			return nil, err
		}
		if data.OwnerID != subject.UserID || appIdentity(ctx) != "" && (data.Actor.APIKeyAppID != appIdentity(ctx) || data.Actor.APIKeyID != subject.APIKeyID) {
			return nil, problem("NOT_FOUND", "job not found")
		}
		for _, c := range data.Batch.Items {
			if err := mutationPermissions(ctx, c); err != nil {
				return nil, err
			}
		}
		progress := map[string]any{"total": len(data.Batch.Items), "completed": 0}
		if data.Status == "succeeded" {
			progress["completed"] = len(data.Batch.Items)
		}
		if data.Result != nil && data.Result.Summary != nil {
			progress["completed"] = data.Result.Summary.Succeeded + data.Result.Summary.Failed
		}
		executionStatus, outcome := jobExecutionOutcome(data)
		list = append(list, map[string]any{"jobId": id, "status": data.Status, "executionStatus": executionStatus, "outcome": outcome, "progress": progress, "createdAt": row.CreatedAt, "startedAt": data.StartedAt, "finishedAt": data.FinishedAt, "attempts": data.Attempts, "result": data.Result, "error": data.Error})
	}
	return map[string]any{"list": list}, nil
}

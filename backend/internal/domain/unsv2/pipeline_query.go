package unsv2

import (
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"strings"
	"time"
)

type DataQueries struct {
	Queries []DataQuery `json:"queries"`
}
type DataQuery struct {
	projection   string
	sampleScope  *sampleQueryScope
	QueryID      string                       `json:"queryId"`
	CollectionID string                       `json:"collectionId"`
	RecordID     string                       `json:"recordId,omitempty"`
	Pipeline     []map[string]json.RawMessage `json:"pipeline"`
	ResultMode   string                       `json:"resultMode,omitempty"`
	Page         *PipelinePage                `json:"page,omitempty"`
}
type sampleQueryScope struct {
	RecordIDs []string
	From      int64
	To        int64
}

func (q DataQuery) validateScope(kind string) error {
	if q.sampleScope != nil && kind == "samples" {
		return nil
	}
	return validatePipelineRecordScope(kind, q.RecordID, "recordId")
}

type PipelinePage struct {
	Size   int    `json:"size,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Page   int    `json:"page,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// Sample scope is part of query identity, independent of document predicates.
// It cannot be broadened by $or or by replacing/projecting the recordId field.
func validatePipelineRecordScope(kind, recordID, path string) error {
	if kind == "samples" && !unsmeta.ValidID(recordID) {
		return invalid(path, "samples queries require one valid recordId outside the pipeline")
	}
	if kind == "records" && recordID != "" {
		return invalid(path, "recordId is only accepted by samples queries")
	}
	return nil
}

func pipelinePagination(kind string, q DataQuery) (int, int, error) {
	mode := q.ResultMode
	if mode == "" {
		mode = "all"
	}
	if mode != "all" && mode != "first" {
		return 0, 0, invalid("resultMode", "resultMode must be all or first")
	}
	if q.Page != nil && len(q.Pipeline) > 0 {
		last := q.Pipeline[len(q.Pipeline)-1]
		_, count := last["$count"]
		_, exists := last["$exists"]
		if len(last) == 1 && (count || exists) {
			return 0, 0, invalid("page", "a pipeline ending in $count or $exists returns one row and does not accept page")
		}
	}
	if mode == "first" {
		if q.Page != nil {
			return 0, 0, invalid("page", "first does not accept page")
		}
		return 1, 0, nil
	}
	if q.Page == nil {
		return 20, 0, nil
	}
	p := *q.Page
	if p.Size == 0 {
		p.Size = 20
	}
	if p.Size < 1 || p.Size > 200 || p.Page < 0 || p.Page > 1000000 {
		return 0, 0, invalid("page", "size must be 1 to 200 and page a nonnegative integer")
	}
	if p.Mode != "" && p.Mode != "offset" && p.Mode != "cursor" {
		return 0, 0, invalid("page.mode", "mode must be offset or cursor")
	}
	if p.Mode == "cursor" && p.Page != 0 || p.Mode == "offset" && p.Cursor != "" || p.Cursor != "" && p.Page != 0 {
		return 0, 0, invalid("page", "cursor and offset page cannot be combined")
	}
	offset := 0
	if p.Page > 0 {
		offset = (p.Page - 1) * p.Size
	}
	if len(p.Cursor) > pipelineCursorMaxBytes {
		return 0, 0, invalid("page.cursor", "invalid cursor")
	}
	if offset < 0 || offset > 1000000 {
		return 0, 0, invalid("page", "offset exceeds 1000000")
	}
	return p.Size, offset, nil
}
func (s *Service) DataQuery(ctx context.Context, kind string, request DataQueries) (any, error) {
	if kind != "records" && kind != "samples" {
		return nil, invalid("operation", "expected records or samples")
	}
	if err := require(ctx, "uns.read"); err != nil {
		return nil, err
	}
	if len(request.Queries) < 1 || len(request.Queries) > MaxQueries {
		return nil, invalid("queries", "1 to 20 queries required")
	}
	seen := map[string]bool{}
	budget := 0
	for i, q := range request.Queries {
		if err := q.validateScope(kind); err != nil {
			return nil, pipelineQueryFault(i, q.QueryID, err)
		}
		if q.Pipeline == nil {
			return nil, pipelineQueryFault(i, q.QueryID, invalid("pipeline", "pipeline must be an array; use [] to list all records"))
		}
		if q.QueryID == "" || len(q.QueryID) > 128 || seen[q.QueryID] {
			return nil, pipelineQueryFault(i, q.QueryID, invalid("queryId", "unique nonempty queryId up to 128 characters required"))
		}
		seen[q.QueryID] = true
		size, _, err := pipelinePagination(kind, q)
		if err != nil {
			return nil, pipelineQueryFault(i, q.QueryID, err)
		}
		budget += size
		if len(q.Pipeline) > 32 {
			return nil, pipelineQueryFault(i, q.QueryID, pipelineError("at most 32 stages"))
		}
	}
	if budget > 2000 {
		return nil, invalid("queries", "requested result row budget exceeds 2000")
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ctx = repo.WithUnsTransaction(bounded, repo.GetUnsConn(ctx).WithContext(bounded))
	release, err := s.admitDataWork(ctx)
	if err != nil {
		return nil, pipelineRuntimeError(err)
	}
	defer release()
	results := make([]map[string]any, 0, len(request.Queries))
	bytes := 0
	err = repo.GetUnsConn(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL statement_timeout='15000ms'").Error; err != nil {
			return err
		}
		qctx := repo.WithUnsTransaction(ctx, tx)
		for i, q := range request.Queries {
			result, err := s.runDataQuery(qctx, tx, kind, i, q, &bytes)
			if err != nil {
				return err
			}
			results = append(results, result)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, pipelineRuntimeError(err)
	}
	out := map[string]any{"results": results}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	if len(encoded) > MaxQueryResponseBytes {
		return nil, problem("RESPONSE_TOO_LARGE", "query exceeds 8 MiB; reduce page size or projected fields")
	}
	return out, nil
}
func pipelineRuntimeError(err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &pg) && pg.Code == "57014" {
		return &Fault{Code: "QUERY_TIMEOUT", Message: "Query exceeded its 15 second execution budget", Retryable: true, Repair: "Narrow filters, simplify joins, or reduce batch size"}
	}
	if errors.As(err, &pg) {
		if budget := expressionRuntimeError(pg.Message); budget != nil {
			return budget
		}
		if budget := lookupRuntimeError(pg.Message); budget != nil {
			return budget
		}
		if pg.Code == "22003" || pg.Code == "22012" {
			return pipelineError("numeric expression exceeds supported range")
		}
		if pg.Code == "22P02" && strings.Contains(pg.Message, "lookup result exceeds") {
			return pipelineError("lookup result exceeds 200 rows; narrow or aggregate its pipeline")
		}
		switch pg.Code {
		case "22007", "22008", "22023", "22P02", "42846":
			return pipelineError("expression value cannot be converted to the requested type or date; check the input value and expression arguments")
		}
	}
	return classify(err)
}
func (s *Service) ValidateDataQuery(ctx context.Context, kind string, request DataQueries) (any, error) {
	if kind != "records" && kind != "samples" {
		return nil, invalid("operation", "expected records or samples")
	}
	if len(request.Queries) < 1 || len(request.Queries) > MaxQueries {
		return nil, invalid("queries", "1 to 20 queries required")
	}
	seen := map[string]bool{}
	budget := 0
	for i, q := range request.Queries {
		if err := q.validateScope(kind); err != nil {
			return nil, pipelineQueryFault(i, q.QueryID, err)
		}
		if q.Pipeline == nil {
			return nil, pipelineQueryFault(i, q.QueryID, invalid("pipeline", "pipeline must be an array; use [] to list all records"))
		}
		if q.QueryID == "" || len(q.QueryID) > 128 || seen[q.QueryID] {
			return nil, invalid("queryId", "unique queryId required")
		}
		seen[q.QueryID] = true
		size, _, err := pipelinePagination(kind, q)
		budget += size
		if err != nil {
			return nil, pipelineQueryFault(i, q.QueryID, err)
		}
		c := &pipelineCompiler{ctx: ctx}
		p, err := c.source(kind, q.CollectionID, q.RecordID, q.sampleScope)
		if err == nil {
			var simple bool
			p, simple, err = c.simple(p, kind, q.Pipeline)
			if err == nil && !simple {
				p, err = c.compile(p, q.Pipeline, 0)
			}
		}
		if err == nil {
			var mode string
			mode, err = pipelinePageMode(q, p)
			if err == nil && mode == "cursor" && q.Page != nil && q.Page.Cursor != "" {
				var binding string
				binding, err = pipelineCursorBinding(ctx, kind, q, p)
				if err == nil {
					_, err = s.readPipelineCursor(q.Page.Cursor, binding, p.seek)
				}
			}
		}
		if err != nil {
			return nil, pipelineQueryFault(i, q.QueryID, err)
		}
	}
	if budget > 2000 {
		return nil, invalid("queries", "requested row budget exceeds 2000")
	}
	return map[string]any{"valid": true, "queryCount": len(request.Queries), "snapshotScope": "thisRequestOnly"}, nil
}

func (s *Service) runDataQuery(qctx context.Context, tx *gorm.DB, kind string, i int, q DataQuery, bytes *int) (map[string]any, error) {
	c := &pipelineCompiler{ctx: qctx}
	p, err := c.source(kind, q.CollectionID, q.RecordID, q.sampleScope)
	if err == nil {
		var simple bool
		p, simple, err = c.simple(p, kind, q.Pipeline)
		if err == nil && !simple {
			p, err = c.compile(p, q.Pipeline, 0)
		}
	}
	if err != nil {
		return nil, pipelineQueryFault(i, q.QueryID, err)
	}
	size, offset, _ := pipelinePagination(kind, q)
	pageMode, err := pipelinePageMode(q, p)
	if err != nil {
		return nil, pipelineQueryFault(i, q.QueryID, err)
	}
	binding := ""
	var after []*string
	if pageMode == "cursor" {
		binding, err = pipelineCursorBinding(qctx, kind, q, p)
		if err != nil {
			return nil, pipelineQueryFault(i, q.QueryID, err)
		}
		if q.Page != nil && q.Page.Cursor != "" {
			after, err = s.readPipelineCursor(q.Page.Cursor, binding, p.seek)
			if err != nil {
				return nil, pipelineQueryFault(i, q.QueryID, err)
			}
		}
	}
	limit := size + 1
	if q.ResultMode == "first" {
		limit = 1
	}
	args := append(append([]any{}, p.args...), limit, offset)
	statement := "SELECT p.doc FROM (" + p.sql + ") p ORDER BY p.ord LIMIT ? OFFSET ?"
	if p.direct {
		statement = p.sql + " LIMIT ? OFFSET ?"
	}
	if pageMode == "cursor" {
		from := p.from
		args = append([]any{}, p.args...)
		if after != nil {
			seekSQL, seekArgs := pipelineSeekAfter(p.seek, after)
			from += " AND (" + seekSQL + ")"
			args = append(args, seekArgs...)
		}
		statement = "SELECT " + DataDocumentSQL(kind, "b") + " AS doc," + pipelineSeekColumns(p.seek) + " AS seek" + from + p.order + " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := tx.Raw(statement, args...).Rows()
	if err != nil {
		return nil, pipelineQueryFault(i, q.QueryID, pipelineRuntimeError(err))
	}
	items := make([]any, 0, size)
	count := 0
	var lastValues []*string
	for rows.Next() {
		var raw []byte
		var seekRaw []byte
		if pageMode == "cursor" {
			err = rows.Scan(&raw, &seekRaw)
		} else {
			err = rows.Scan(&raw)
		}
		if err != nil {
			rows.Close()
			return nil, pipelineQueryFault(i, q.QueryID, err)
		}
		count++
		*bytes += len(raw)
		if *bytes > MaxQueryResponseBytes {
			rows.Close()
			return nil, pipelineQueryFault(i, q.QueryID, problem("RESPONSE_TOO_LARGE", "query exceeds 8 MiB; reduce page size or projected fields"))
		}
		if count <= size {
			if pageMode == "cursor" {
				if err = json.Unmarshal(seekRaw, &lastValues); err != nil {
					rows.Close()
					return nil, err
				}
			}
			var item any
			if err = decodeNumberJSON(raw, &item); err != nil {
				rows.Close()
				return nil, err
			}
			items = append(items, item)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, pipelineQueryFault(i, q.QueryID, pipelineRuntimeError(err))
	}
	result := map[string]any{"queryId": q.QueryID}
	if q.ResultMode == "first" {
		result["resultMode"] = "first"
		result["item"] = nil
		if len(items) > 0 {
			result["item"] = items[0]
		}
	} else {
		result["resultMode"] = "all"
		result["items"] = items
		var next any
		var nextPage any
		hasMore := count > size
		if hasMore {
			if pageMode == "offset" && offset+size > 1000000 {
				return nil, pipelineQueryFault(i, q.QueryID, invalid("page", "query exceeds maximum pageable offset; narrow the filter"))
			}
			if pageMode == "cursor" {
				next, err = s.nextPipelineCursor(binding, lastValues)
				if err != nil {
					return nil, pipelineQueryFault(i, q.QueryID, err)
				}
			} else {
				nextPage = pipelineOffsetNext(offset, size)
			}
		}
		result["pageInfo"] = map[string]any{"mode": pageMode, "hasMore": hasMore, "nextCursor": next, "nextPage": nextPage, "snapshotScope": "thisRequestOnly"}
	}
	return result, nil
}

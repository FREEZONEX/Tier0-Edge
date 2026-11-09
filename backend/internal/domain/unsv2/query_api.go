package unsv2

import (
	"backend/internal/repo"
	"context"
	"database/sql"
	"errors"
	"gorm.io/gorm"
)

// RecordQueryRequest is the single-query API. Unlike the advanced batch API,
// callers do not need a queryId or a relational wrapper.
type RecordQueryRequest struct {
	CollectionID string `json:"collectionId"`
	Pagination
	CursorOptions
	RecordIDs           []string        `json:"recordIds,omitempty"`
	IncludeLatestSample bool            `json:"includeLatestSample,omitempty"`
	Filters             []RecordFilter  `json:"filters,omitempty"`
	OrderBy             []RecordOrder   `json:"orderBy,omitempty"`
	Joins               []QueryJoin     `json:"joins,omitempty"`
	Subqueries          []QuerySubquery `json:"subqueries,omitempty"`
	Select              []QuerySelect   `json:"select,omitempty"`
	Where               *QueryPredicate `json:"where,omitempty"`
	GroupBy             []string        `json:"groupBy,omitempty"`
	Having              []RecordFilter  `json:"having,omitempty"`
	Distinct            bool            `json:"distinct,omitempty"`
}

func (r RecordQueryRequest) query(aggregate bool) (Query, error) {
	q := Query{QueryID: "result", CollectionID: r.CollectionID, Pagination: r.Pagination, CursorOptions: r.CursorOptions}
	if aggregate {
		if r.CursorOptions.present() {
			return q, invalid("paginationMode", "cursor options are only supported by ordinary Record and Sample history queries")
		}
		if len(r.Filters) > 0 || len(r.RecordIDs) > 0 || r.Search != "" || r.IncludeLatestSample {
			return q, invalid("filters/recordIds/search/includeLatestSample", "Aggregate queries use where and qualified fields; ordinary filters belong to records/search")
		}
		q.Relational = &RelationalQuery{Subqueries: r.Subqueries, Joins: r.Joins, Select: r.Select, Where: r.Where, GroupBy: r.GroupBy, Having: r.Having, OrderBy: r.OrderBy, Distinct: r.Distinct}
	} else {
		if len(r.Subqueries) > 0 || len(r.Select) > 0 || len(r.Joins) > 0 || r.Where != nil || len(r.GroupBy) > 0 || len(r.Having) > 0 || r.Distinct {
			return q, invalid("select/joins/where/groupBy/having/distinct", "Projection and aggregation belong to records/aggregate")
		}
		q.Filters = r.Filters
		q.RecordIDs = r.RecordIDs
		q.IncludeLatestSample = r.IncludeLatestSample
		q.OrderBy = r.OrderBy
	}
	return q, nil
}

func (s *Service) RecordQuery(ctx context.Context, operation string, r RecordQueryRequest) (any, error) {
	q, err := r.query(operation != "recordSearch")
	if err != nil {
		return nil, err
	}
	if operation == "recordQueryValidate" {
		if err := require(ctx, "uns.read"); err != nil {
			return nil, err
		}
		if err := appQueryAccess(ctx, "records", q); err != nil {
			return nil, err
		}
		var out map[string]any
		err = repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
			var inner error
			out, inner = compileRelationalQuery(repo.WithUnsTransaction(ctx, tx), q, true)
			return inner
		}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		return out, flatQueryError(classify(err))
	}
	response, err := s.Query(ctx, "records", Queries{Queries: []Query{q}})
	if err != nil {
		return nil, flatQueryError(err)
	}
	results := response.(map[string]any)["results"].([]map[string]any)
	out := results[0]
	delete(out, "queryId")
	if operation == "recordSearch" {
		out["rowShape"] = "record"
		out["nullMeaning"] = "Missing or null is not zero"
	}
	return out, nil
}

func flatQueryError(err error) error {
	var fault *Fault
	if errors.As(err, &fault) && fault.Path == "relational" {
		copy := *fault
		copy.Path = ""
		copy.Repair = "Check collectionId, subqueries, joins, select expressions, where, groupBy, having and orderBy against discovery/describe; validate the corrected request through records/validate-query"
		return &copy
	}
	return err
}

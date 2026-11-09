package unsv2

import "encoding/json"

type RelationalQuery struct {
	Subqueries []QuerySubquery `json:"subqueries,omitempty"`
	Joins      []QueryJoin     `json:"joins,omitempty"`
	Select     []QuerySelect   `json:"select"`
	Where      *QueryPredicate `json:"where,omitempty"`
	GroupBy    []string        `json:"groupBy,omitempty"`
	Having     []RecordFilter  `json:"having,omitempty"`
	OrderBy    []RecordOrder   `json:"orderBy,omitempty"`
	Distinct   bool            `json:"distinct,omitempty"`
}
type QueryJoin struct {
	CollectionID string        `json:"collectionId,omitempty"`
	Subquery     string        `json:"subquery,omitempty"`
	Alias        string        `json:"alias"`
	Type         string        `json:"type"`
	On           []QueryJoinOn `json:"on"`
}
type QueryJoinOn struct {
	Left  string `json:"left"`
	Right string `json:"right"`
}
type QuerySelect struct {
	Expression *QueryExpression `json:"expression,omitempty"`
	Field      string           `json:"field,omitempty"`
	As         string           `json:"as"`
	Aggregate  string           `json:"aggregate,omitempty"`
	Distinct   bool             `json:"distinct,omitempty"`
}

// QuerySubquery deliberately has no pagination, ordering, or nested subqueries.
// Each result is grouped before it can be joined to the outer record grain.
type QuerySubquery struct {
	Name         string          `json:"name"`
	CollectionID string          `json:"collectionId"`
	Joins        []QueryJoin     `json:"joins,omitempty"`
	Select       []QuerySelect   `json:"select"`
	Where        *QueryPredicate `json:"where,omitempty"`
	GroupBy      []string        `json:"groupBy"`
	Having       []RecordFilter  `json:"having,omitempty"`
}

type QueryExpression struct {
	Field string            `json:"field,omitempty"`
	Value json.RawMessage   `json:"value,omitempty"`
	Op    string            `json:"op,omitempty"`
	Args  []QueryExpression `json:"args,omitempty"`
}
type QueryPredicate struct {
	Field string           `json:"field,omitempty"`
	Op    string           `json:"op,omitempty"`
	Value json.RawMessage  `json:"value,omitempty"`
	And   []QueryPredicate `json:"and,omitempty"`
	Or    []QueryPredicate `json:"or,omitempty"`
	Not   *QueryPredicate  `json:"not,omitempty"`
}

package unsv2

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const MaxTransactionQueryRows = 100

func hasRecordQuery(items []Change) bool {
	for _, item := range items {
		if item.Action == "record.query" || len(item.Filter) > 0 {
			return true
		}
	}
	return false
}

type TransactionQueryResult struct {
	Records []*RecordDocument `json:"records"`
	Total   int64             `json:"total"`
	HasMore bool              `json:"hasMore"`
}

func hasTargetBinding(c Change) bool {
	for _, b := range c.Bindings {
		if b.Path == "id" {
			return true
		}
	}
	return false
}

// Only query row identities may supply a dynamic target. Collection identities
// remain static so permission checks cannot be redirected by a result value.
func queryResultPath(path string) (int, string, bool) {
	parts := strings.SplitN(path, ".", 4)
	if len(parts) < 4 || parts[0] != "query" || parts[1] != "records" {
		return 0, "", false
	}
	i, err := strconv.Atoi(parts[2])
	field := parts[3]
	_, data := bindingPath(field)
	return i, field, err == nil && i >= 0 && i < MaxTransactionQueryRows && strconv.Itoa(i) == parts[2] && (data || contains([]string{"id", "revision", "collectionId"}, field))
}

func validateTransactionQuery(c Change) error {
	if c.Action != "record.query" {
		if c.Query != nil {
			return invalid("query", "only record.query accepts query")
		}
		return nil
	}
	if c.Query == nil {
		return invalid("query", "query object required")
	}
	if c.ID.ID != "" || c.ID.Ref != "" || c.ExpectedRevision != 0 || c.Exists != nil || len(c.Conditions) > 0 || len(c.Operations) > 0 {
		return invalid("query", "query accepts filter, sort, limit and expectCount; no record identity or write fields")
	}
	q := c.Query
	if len(q.Filter) > 0 && len(q.Filters) > 0 || len(q.Sort) > 0 && len(q.OrderBy) > 0 {
		return invalid("query", "filter/sort cannot be combined with legacy filters/orderBy")
	}
	if len(q.Filters) > 16 || len(q.OrderBy) > 3 || len(q.Sort) > 16 || q.Limit < 0 || q.Limit > MaxTransactionQueryRows {
		return invalid("query", "at most 16 sort fields and 100 returned records; limit defaults to 20")
	}
	if e := q.ExpectCount; e != nil {
		if e.Min == nil && e.Max == nil || e.Min != nil && *e.Min < 0 || e.Max != nil && *e.Max < 0 || e.Min != nil && e.Max != nil && *e.Min > *e.Max {
			return invalid("query.expectCount", "nonnegative min and/or max required; min must not exceed max")
		}
	}
	return nil
}

func (s *Service) recordTransactionQuery(ctx context.Context, tx *gorm.DB, c Change) (Result, error) {
	r := Result{OpID: c.OpID, Action: c.Action, CollectionID: c.CollectionID.ID, Status: "queried"}
	if err := recordReadPermission(ctx, c.CollectionID.ID); err != nil {
		return r, err
	}
	doc, err := definition(ctx, c.CollectionID.ID)
	if err != nil {
		return r, err
	}
	q := c.Query
	base := tx.Model(&repo.UnsRecord{}).Where("collection_id=? AND deleted_at IS NULL", c.CollectionID.ID)
	filtered, err := recordFilters(base, doc.RecordSchema, q.Filters)
	if err != nil {
		return r, err
	}
	if len(q.Filter) > 0 {
		predicate, args, err := CompileDataFilter(ctx, "records", c.CollectionID.ID, "base:uns_record", q.Filter)
		if err != nil {
			return r, dataFilterPath(err, "query/filter")
		}
		filtered = filtered.Where(predicate, args...)
	}
	var ordered *gorm.DB
	if len(q.Sort) > 0 {
		order, err := CompileDataSort(ctx, "records", c.CollectionID.ID, "base:uns_record", q.Sort)
		if err != nil {
			return r, dataFilterPath(err, "query/sort")
		}
		ordered = filtered.Session(&gorm.Session{}).Order(order).Order("id ASC")
	} else {
		ordered, err = recordOrder(filtered.Session(&gorm.Session{}), doc.RecordSchema, q.OrderBy)
		if err != nil {
			return r, err
		}
	}
	var total int64
	if err = filtered.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return r, err
	}
	if e := q.ExpectCount; e != nil && (e.Min != nil && total < *e.Min || e.Max != nil && total > *e.Max) {
		return r, &Fault{Code: "CONDITION_FAILED", Path: "query.expectCount", Reason: fmt.Sprintf("query matched %d records, outside the expected count", total), Message: "query count assertion failed", Repair: "Inspect the matching records and revise the business transaction"}
	}
	limit := q.Limit
	if limit == 0 {
		limit = 20
	}
	// Always break sorting ties by the immutable identity. The serializable
	// snapshot covers both the unbounded count and this bounded selection.
	ordered = ordered.Limit(limit)
	var candidates []repo.UnsRecord
	if err = ordered.Session(&gorm.Session{}).Select("id").Find(&candidates).Error; err != nil {
		return r, err
	}
	locks := make([]repo.UnsRecordLock, 0, len(candidates))
	for _, row := range candidates {
		locks = append(locks, repo.UnsRecordLock{CollectionID: c.CollectionID.ID, RecordID: row.ID})
	}
	if err = repo.LockUnsRecords(tx, locks...); err != nil {
		return r, err
	}
	rows := []repo.UnsRecord{}
	// A concurrent update since the serializable snapshot raises 40001 here,
	// rather than returning stale values after waiting on a writer's lock.
	if err = ordered.Session(&gorm.Session{}).Clauses(clause.Locking{Strength: "UPDATE"}).Find(&rows).Error; err != nil {
		return r, err
	}
	documents := make([]*RecordDocument, 0, len(rows))
	for _, row := range rows {
		documents = append(documents, recordDocument(row))
	}
	r.Query = &TransactionQueryResult{Records: documents, Total: total, HasMore: total > int64(len(rows))}
	return r, nil
}

func validateDynamicTarget(c Change, result Result, field string, value any) (Identity, error) {
	id, ok := value.(string)
	if result.Action != "record.query" || field != "id" || result.CollectionID != c.CollectionID.ID || !ok || !unsmeta.ValidID(id) {
		return Identity{}, invalid("bindings", "id must reference a query record ID from the same Collection")
	}
	return Identity{ID: id}, nil
}

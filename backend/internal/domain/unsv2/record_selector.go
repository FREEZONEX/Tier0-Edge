package unsv2

import (
	docquery "backend/internal/domain/document/query"
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"sort"
	"strings"
	"time"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Partial writes execute one transaction per item. Carry earlier resolved
// targets into the next item so a filter cannot bypass the request's existing
// one-write-per-identity rule. This context never crosses request boundaries.
type recordWriteTargetsKey struct{}

// Partial writes share a response budget while retaining one transaction per
// item. The remaining limit is checked by execute before the item commits.
type recordResultBudgetKey struct{}

func priorRecordWriteTargets(ctx context.Context) map[repo.UnsRecordLock]bool {
	targets, _ := ctx.Value(recordWriteTargetsKey{}).(map[repo.UnsRecordLock]bool)
	return targets
}

func filterFieldNames(raw json.RawMessage) []string {
	var value any
	_ = json.Unmarshal(raw, &value)
	fields := map[string]bool{}
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			for key, child := range value {
				if !strings.HasPrefix(key, "$") {
					fields[key] = true
				} else {
					visit(child)
				}
			}
		case []any:
			for _, child := range value {
				visit(child)
			}
		}
	}
	visit(value)
	result := make([]string, 0, len(fields))
	for field := range fields {
		result = append(result, field)
	}
	sort.Strings(result)
	return result
}

func validateRecordFilter(c Change) error {
	if c.Action == "record.create" && c.ReturnDocument != "" {
		if c.ReturnDocument != "after" {
			return invalid("returnDocument", "creation only accepts after")
		}
		if c.Selection != "" || c.Sort != nil {
			return invalid("selection", "creation cannot select or sort existing records")
		}
	} else if c.Selection != "" || c.Sort != nil || c.ReturnDocument != "" {
		if !contains([]string{"record.update", "record.modify", "record.replace", "record.delete", "record.read"}, c.Action) {
			return invalid("selection", "selection, sort and returnDocument are only accepted by Record update and replace")
		}
		if c.Selection != "" && c.Selection != "exactOne" && c.Selection != "first" {
			return invalid("selection", "selection must be exactOne or first")
		}
		if c.ReturnDocument != "" && c.ReturnDocument != "before" && c.ReturnDocument != "after" {
			return invalid("returnDocument", "returnDocument must be before or after")
		}
		if c.Sort != nil && c.Selection != "first" {
			return invalid("sort", "sort requires selection first")
		}
		if c.Selection == "first" && len(c.Filter) == 0 {
			return invalid("filter", "selection first requires a nonempty filter")
		}
	}
	if len(c.Filter) == 0 {
		return nil
	}
	if !contains([]string{"record.update", "record.modify", "record.replace", "record.delete", "record.read", "record.check"}, c.Action) {
		return invalid("filter", "filter is only accepted by Record selection actions")
	}
	var filter map[string]json.RawMessage
	if json.Unmarshal(c.Filter, &filter) != nil || filter == nil {
		return invalid("filter", "filter must be an object using the query $match grammar")
	}
	if c.Selection != "first" && c.Action != "record.read" && c.Action != "record.check" && len(filter) == 0 {
		return invalid("filter", "writes require a nonempty filter selecting exactly one Record")
	}
	if len(c.Conditions) > 0 {
		return invalid("filter", "filter cannot be combined with legacy conditions")
	}
	if c.ID.ID != "" && !unsmeta.ValidID(c.ID.ID) {
		return invalid("id", "invalid legacy Record identity")
	}
	return nil
}

func dataFilterPath(err error, path string) error {
	var fault *Fault
	if errors.As(err, &fault) && fault.Path == "pipeline" {
		copy := *fault
		copy.Path = path
		return &copy
	}
	return transactionPath(err, path+"/")
}

// The caller has checked the operation's Collection grant and opened a
// serializable transaction. Selection never turns a single-item mutation into
// an implicit multi-record write. Retain the selected identity's advisory lock
// and row lock through all schema/revision checks, events, and commit.
func resolveRecordFilter(ctx context.Context, tx *gorm.DB, c Change) (Change, error) {
	if c.Upsert {
		node, err := docquery.ParseFilterJSON(c.Filter)
		if err != nil {
			return c, invalid("filter", err.Error())
		}
		paths, hasID := docquery.EqualityPaths(node)
		if !hasID {
			equal := map[string]bool{}
			for _, path := range paths {
				equal[unsmeta.FormatFieldPath(path.Segs)] = true
			}
			ready, err := unsmeta.ReadyUniqueIndex(ctx, c.CollectionID.ID, equal)
			if err != nil {
				return c, err
			}
			if !ready {
				return c, problem("UPSERT_REQUIRES_UNIQUE_INDEX", "upsert filter must contain _id equality or every field of a ready unique index")
			}
		}
	}

	predicate, args, err := CompileDataFilter(ctx, "records", c.CollectionID.ID, "base:r", c.Filter)
	if err != nil {
		return c, dataFilterPath(err, "filter")
	}
	base := tx.Table("public.uns_record AS r").Where("r.collection_id = ? AND r.deleted_at IS NULL", c.CollectionID.ID).Where(predicate, args...)
	if c.ID.ID != "" {
		base = base.Where("r.id = ?", c.ID.ID)
	}
	var candidates []repo.UnsRecord
	ordered := base.Session(&gorm.Session{})
	limit := 2
	if c.Selection == "first" {
		order, err := CompileDataSort(ctx, "records", c.CollectionID.ID, "base:r", c.Sort)
		if err != nil {
			return c, dataFilterPath(err, "sort")
		}
		if order != "" {
			ordered = ordered.Order(order)
		}
		limit = 1
	}
	if err := ordered.Select("r.id").Order("r.id ASC").Limit(limit).Find(&candidates).Error; err != nil {
		return c, err
	}
	if c.Action == "record.check" && c.Exists != nil && !*c.Exists {
		if len(candidates) > 0 {
			return c, conditionFailure(nil, "filter")
		}
		// recordTransaction retains the established absence-check result shape.
		c.ID = Identity{}
		return c, nil
	}
	if len(candidates) == 0 {
		if c.Upsert {
			node, err := docquery.ParseFilterJSON(c.Filter)
			if err != nil {
				return c, invalid("filter", err.Error())
			}
			data, id := docquery.UpsertBase(node)
			if id == "" {
				id = uuid.NewString()
			}
			if c.Action == "record.replace" {
				if err := decodeNumberJSON(c.Data, &data); err != nil {
					return c, invalid("replacement", "object required")
				}
			} else {
				var value any
				_ = decodeNumberJSON(c.Update, &value)
				u, err := docquery.ParseUpdate(value, c.ArrayFilters)
				if err != nil {
					return c, invalid("update", err.Error())
				}
				data, err = u.Apply(data, docquery.ApplyContext{Insert: true, NowMs: time.Now().UnixMilli(), Filter: node})
				if err != nil {
					return c, invalid("update", err.Error())
				}
			}
			c.Action = "record.create"
			c.ID = Identity{ID: id}
			c.Data, _ = json.Marshal(data)
			c.Filter = nil
			c.Update = nil
			c.Upsert = false
			if c.ReturnDocument == "before" {
				c.ReturnDocument = ""
			}
			return c, nil
		}
		if c.Selection == "first" && c.ExpectedRevision == 0 {
			c.noMatch = true
			return c, nil
		}
		if c.Action == "record.check" || c.ID.ID != "" {
			return c, conditionFailure(nil, "filter")
		}
		return c, problem("NOT_FOUND", "record filter matched no records")
	}
	if len(candidates) != 1 {
		return c, invalid("filter", "filter must match exactly one Record; narrow the predicate")
	}
	id := candidates[0].ID
	if err := repo.LockUnsRecords(tx, repo.UnsRecordLock{CollectionID: c.CollectionID.ID, RecordID: id}); err != nil {
		return c, err
	}
	var row repo.UnsRecord
	err = base.Session(&gorm.Session{}).Where("r.id = ?", id).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error
	if errors.Is(err, repo.ErrNotFound) {
		return c, conditionFailure(nil, "filter")
	}
	if err != nil {
		return c, err
	}
	c.ID = Identity{ID: id}
	return c, nil
}

func recordDocument(row repo.UnsRecord) *RecordDocument {
	doc := &RecordDocument{ID: row.ID, CollectionID: row.CollectionID,
		Data: append(json.RawMessage(nil), row.Data...), Revision: row.Revision,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, SourceID: row.SourceID}
	if row.SourceTime != nil {
		value := row.SourceTime.UnixMilli()
		doc.SourceTime = &value
	}
	if row.LockExpiresAt != nil && row.LockExpiresAt.After(time.Now()) {
		doc.Lock = &RecordLeaseView{Label: row.LockLabel, ExpiresAt: row.LockExpiresAt.UnixMilli(), Fence: row.LockFence}
		if row.LockSince != nil {
			doc.Lock.Since = row.LockSince.UnixMilli()
		}
	}
	return doc
}

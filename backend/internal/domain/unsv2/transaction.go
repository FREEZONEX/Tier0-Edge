package unsv2

import (
	docquery "backend/internal/domain/document/query"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/fieldpath"
	"backend/internal/mcpaccess"
	"backend/internal/repo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func transactionAction(action string) bool {
	return action == "record.query" || action == "record.check" || action == "record.read" || (action == "record.modify" || action == "record.update")
}

func itemFault(index int, item Change, err error) error {
	err = classify(err)
	var fault *Fault
	if !errors.As(err, &fault) {
		return err
	}
	copy := *fault
	copy.OpID, copy.ObjectKind, copy.ObjectID = item.OpID, item.Kind, item.ID.ID
	copy.Path = fmt.Sprintf("/items/%d/%s", index, strings.TrimPrefix(fault.Path, "/"))
	if copy.ObjectKind == "" {
		copy.ObjectKind = strings.Split(item.Action, ".")[0]
	}
	if copy.ObjectID == "" {
		copy.ObjectID = item.StreamID.ID
	}
	return &copy
}

func validateTransactionFields(c Change) error {
	if err := validateRecordFilter(c); err != nil {
		return err
	}
	if err := validateTransactionQuery(c); err != nil {
		return err
	}
	if len(c.Bindings) > 32 {
		return invalid("bindings", "at most 32 result bindings allowed")
	}
	if !transactionAction(c.Action) {
		if c.Exists != nil || len(c.Conditions) > 0 || len(c.Operations) > 0 {
			return invalid("action", "transaction fields require check, read or modify")
		}
		return nil
	}
	if c.Kind != "" || len(c.Data) > 0 && string(c.Data) != "null" || c.ManagedBy != nil || c.RecordID.ID != "" || c.RecordID.Ref != "" || c.StreamID.ID != "" || c.StreamID.Ref != "" || c.Time != 0 || c.Quality != "" {
		return invalid("action", "record transaction actions accept only their declared fields")
	}
	if len(c.Conditions) > 16 {
		return invalid("conditions", "at most 16 conditions allowed")
	}
	if c.Action != "record.check" && c.Exists != nil {
		return invalid("exists", "only record.check accepts exists")
	}
	if c.Exists != nil && !*c.Exists && (len(c.Conditions) > 0 || c.ExpectedRevision != 0) {
		return invalid("exists", "absence checks cannot compare data or revision")
	}
	if c.Action == "record.read" && (len(c.Conditions) > 0 || c.ExpectedRevision != 0) {
		return invalid("action", "record.read accepts identity and bindings only")
	}
	if (c.Action != "record.modify" && c.Action != "record.update") && len(c.Operations) != 0 {
		return invalid("operations", "only record.update (or legacy record.modify) accepts operations")
	}
	if len(c.Update) > 0 {
		if c.Action != "record.update" || len(c.Operations) > 0 {
			return invalid("update", "update operators are only accepted by record.update and cannot be combined with operations")
		}
		var value any
		if err := decodeNumberJSON(c.Update, &value); err != nil {
			return invalid("update", "object required")
		}
		if _, err := docquery.ParseUpdate(value, c.ArrayFilters); err != nil {
			return invalid("update", err.Error())
		}
	}
	if c.Upsert && (c.Action != "record.update" && c.Action != "record.replace" || len(c.Filter) == 0 || c.ExpectedRevision != 0) {
		return invalid("upsert", "upsert requires an update/replace filter without expectedRevision")
	}
	if (c.Action == "record.modify" || c.Action == "record.update") && (len(c.Update) == 0 && len(c.Operations) == 0 || len(c.Operations) > 32) {
		return invalid("operations", "1 to 32 field operations required")
	}
	return nil
}

func recordReadPermission(ctx context.Context, collectionID string) error {
	if err := require(ctx, "uns.read"); err != nil {
		return err
	}
	if subject, ok := contextx.SubjectFrom(ctx); ok && subject.MCPRestricted && !mcpaccess.Has(subject, "uns.read") {
		return problem("FORBIDDEN", "record reads require the MCP read scope")
	}
	return appCollectionAccess(ctx, collectionID, "record:read")
}

func conditionFailure(index *int, path string) error {
	return &Fault{Code: "CONDITION_FAILED", Message: "record condition failed", Reason: "The authorized record does not satisfy the requested condition", Path: path, ConditionIndex: index, Repair: "Read the current record and reconsider the whole transaction before retrying"}
}

// execute prelocked every Record identity in the batch before entering here.
// This excludes other writers of those identities, including inserts for absent
// IDs; unrelated Record writers retain shared access to the metadata gate.
func (s *Service) recordTransaction(ctx context.Context, tx *gorm.DB, c Change) (Result, error) {
	if err := repo.RequireLocalCollection(tx, c.CollectionID.ID); err != nil {
		return Result{}, err
	}
	ctx = unsmeta.WithRecordLease(ctx, c.LockToken, c.Release)
	result := Result{OpID: c.OpID, Action: c.Action, ID: c.ID.ID, CollectionID: c.CollectionID.ID}
	if !(c.Action == "record.update" && len(c.Update) > 0 && c.ReturnDocument == "") {
		if err := recordReadPermission(ctx, c.CollectionID.ID); err != nil {
			return result, err
		}
	}
	doc, err := definition(ctx, c.CollectionID.ID)
	if err != nil {
		return result, err
	}
	base := func() *gorm.DB {
		return tx.Model(&repo.UnsRecord{}).Where("collection_id=? AND id=? AND deleted_at IS NULL", c.CollectionID.ID, c.ID.ID)
	}
	// Validate predicates even if the row is absent; do not let absence hide an
	// invalid operator or a field outside the declared schema.
	for i, condition := range c.Conditions {
		if _, err := recordFilters(base(), doc.RecordSchema, []RecordFilter{condition}); err != nil {
			return result, conditionPath(err, i)
		}
	}
	var row repo.UnsRecord
	err = base().Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return result, err
	}
	found := err == nil
	if c.Action == "record.check" && c.Exists != nil && !*c.Exists {
		if found {
			return result, conditionFailure(nil, "exists")
		}
		result.Status = "checked"
		return result, nil
	}
	if !found {
		if c.Action == "record.check" {
			return result, conditionFailure(nil, "exists")
		}
		return result, problem("NOT_FOUND", "record not found")
	}
	if c.ExpectedRevision > 0 && row.Revision != c.ExpectedRevision {
		return result, problem("VERSION_CONFLICT", "record revision changed")
	}
	predicates := make([]string, 0, len(c.Conditions))
	arguments := make([]any, 0, len(c.Conditions))
	for i, condition := range c.Conditions {
		query, err := recordFilters(base(), doc.RecordSchema, []RecordFilter{condition})
		if err != nil {
			return result, conditionPath(err, i)
		}
		// Reuse the exact SQL predicate, including NULL and numeric semantics.
		// CASE preserves short-circuiting: a later SQL conversion failure must
		// not replace an earlier CONDITION_FAILED result.
		predicates = append(predicates, fmt.Sprintf("WHEN NOT EXISTS (?) THEN %d", i))
		arguments = append(arguments, query.Select("1"))
	}
	if len(predicates) > 0 {
		var failedIndex int
		if err := tx.Raw("SELECT CASE "+strings.Join(predicates, " ")+" ELSE -1 END", arguments...).Row().Scan(&failedIndex); err != nil {
			return result, err
		}
		if failedIndex >= 0 {
			return result, conditionFailure(&failedIndex, fmt.Sprintf("conditions/%d", failedIndex))
		}
	}
	result.Revision = row.Revision
	switch c.Action {
	case "record.check":
		result.Status = "checked"
	case "record.read":
		result.Status, result.Data = "read", row.Data
		result.Document = recordDocument(row)
	case "record.modify", "record.update":
		if row.SourceID != nil {
			return result, problem("REFERENCED", "Source-managed records cannot be edited")
		}
		var data map[string]any
		if err := decodeNumberJSON(row.Data, &data); err != nil {
			return result, err
		}
		for i, operation := range c.Operations {
			if err := checkRecordOperationPolicy(ctx, doc.RecordSchema, operation.Path); err != nil {
				return result, operationPath(err, i)
			}
			if err := applyFieldOperation(doc.RecordSchema, data, operation); err != nil {
				return result, operationPath(err, i)
			}
		}
		if len(c.Update) > 0 {
			if actor, ok := contextx.SubjectFrom(ctx); ok && actor.MCPRestricted {
				if err := checkMCPDataSchema(doc.RecordSchema, 0); err != nil {
					return result, err
				}
			}
			var value any
			if err := decodeNumberJSON(c.Update, &value); err != nil {
				return result, invalid("update", "object required")
			}
			u, err := docquery.ParseUpdate(value, c.ArrayFilters)
			if err != nil {
				return result, invalid("update", err.Error())
			}
			filter, err := docquery.ParseFilterJSON(c.Filter)
			if len(c.Filter) == 0 {
				filter, err = docquery.ParseFilter(nil)
			}
			if err != nil {
				return result, invalid("filter", err.Error())
			}
			data, err = u.Apply(data, docquery.ApplyContext{NowMs: time.Now().UnixMilli(), Filter: filter, Arrays: func(p docquery.Path) bool {
				f, ok := unsmeta.SchemaField(doc.RecordSchema, p.Segs)
				return ok && f.Type == "array"
			}})
			if err != nil {
				return result, invalid("update", err.Error())
			}
		}
		// Reuse every standard writer guard, final schema validation, revision
		// handling, domain event and database constraint in SaveRecord.
		saved, err := s.metadata.SaveRecord(ctx, c.CollectionID.ID, c.ID.ID, row.Revision, data)
		if err != nil {
			return result, err
		}
		result.Status, result.Revision = "updated", saved.Revision
		if saved.Revision == row.Revision {
			result.Status = "unchanged"
		}
		if c.ReturnDocument == "before" {
			result.Document = recordDocument(row)
		} else if c.ReturnDocument == "after" {
			result.Document = recordDocument(saved)
		}
	}
	return result, nil
}

// Array updates can inspect existing values (pull predicates, addToSet and
// uniqueness). Check the whole field schema before that inspection, including
// on workers where only the persisted MCP identity survives submission.
func checkRecordOperationPolicy(ctx context.Context, schema *unsmeta.Schema, path string) error {
	subject, _ := contextx.SubjectFrom(ctx)
	policy, restricted := ctx.Value(dataQueryFieldPolicyKey{}).(func(string, *unsmeta.Schema) error)
	if !subject.MCPRestricted && !restricted {
		return nil
	}
	_, field, err := declaredField(schema, path)
	if err != nil {
		return err
	}
	if subject.MCPRestricted {
		if err := CheckMCPDataQueryField(path, field); err != nil {
			return dataFilterPath(err, "path")
		}
	}
	if restricted {
		if err := policy(path, field); err != nil {
			return dataFilterPath(err, "path")
		}
	}
	return nil
}

func conditionPath(err error, i int) error {
	return transactionPath(err, fmt.Sprintf("conditions/%d/", i))
}
func operationPath(err error, i int) error {
	return transactionPath(err, fmt.Sprintf("operations/%d/", i))
}
func transactionPath(err error, prefix string) error {
	var fault *Fault
	if errors.As(err, &fault) {
		copy := *fault
		copy.Path = prefix + strings.TrimPrefix(copy.Path, "/")
		return &copy
	}
	return err
}

func decodeNumberJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func declaredField(schema *unsmeta.Schema, path string) ([]string, *unsmeta.Schema, error) {
	parts, err := unsmeta.ParseFieldPath(path)
	if err != nil || len(parts) == 0 || parts[0] == "_id" || parts[0] == "_meta" || schema == nil {
		return nil, nil, invalid("path", "declared data field required")
	}
	current, ok := unsmeta.SchemaField(schema, parts)
	if !ok {
		return nil, nil, invalid("path", "field must be declared in recordSchema")
	}
	return parts, current, nil
}

func applyFieldOperation(schema *unsmeta.Schema, data map[string]any, operation FieldOperation) error {
	parts, field, err := declaredField(schema, operation.Path)
	if err != nil {
		return err
	}
	if !contains([]string{"set", "unset", "increment", "append", "addToSet", "pull"}, operation.Op) {
		return invalid("op", "expected set, unset, increment, append, addToSet or pull")
	}
	var value any
	if operation.Op == "unset" {
		if len(operation.Value) > 0 {
			return invalid("value", "unset does not accept a value")
		}
	} else if len(operation.Value) == 0 || decodeNumberJSON(operation.Value, &value) != nil {
		return invalid("value", "JSON value required")
	}
	parent, found := fieldpath.Value(data, parts[:len(parts)-1])
	if !found {
		return invalid("path", "parent must already exist")
	}
	key := parts[len(parts)-1]
	var currentValue any
	var assign func(any, bool)
	switch container := parent.(type) {
	case map[string]any:
		currentValue = container[key]
		assign = func(v any, remove bool) {
			if remove {
				delete(container, key)
			} else {
				container[key] = v
			}
		}
	case []any:
		i, ok := fieldpath.Index(key)
		if !ok || i >= len(container) {
			return invalid("path", "array index must address an existing element")
		}
		if operation.Op == "unset" {
			return invalid("path", "unset does not remove array positions; use pull on the array")
		}
		currentValue = container[i]
		assign = func(v any, _ bool) { container[i] = v }
	default:
		return invalid("path", "parent must be an existing object or array")
	}
	switch operation.Op {
	case "set":
		assign(value, false)
	case "unset":
		assign(nil, true)
	case "append", "addToSet", "pull":
		updated, err := applyArrayOperation(field, currentValue, operation.Op, value)
		if err != nil {
			return err
		}
		assign(updated, false)
	case "increment":
		if field.Type != "integer" && field.Type != "number" {
			return invalid("path", "increment requires a declared numeric field")
		}
		current, ok := currentValue.(json.Number)
		delta, deltaOK := value.(json.Number)
		if !ok || !deltaOK {
			return invalid("value", "increment requires an existing number and numeric delta")
		}
		a, scaleA, ea := decimalNumber(current)
		b, scaleB, eb := decimalNumber(delta)
		if ea != nil || eb != nil {
			return invalid("value", "increment requires bounded decimal numbers (100 fractional places, magnitude <= 2^53-1)")
		}
		sum := new(big.Rat).Add(a, b)
		limit := new(big.Rat).SetInt64(9007199254740991)
		if new(big.Rat).Abs(sum).Cmp(limit) > 0 {
			return invalid("value", "increment exceeds the safe numeric range")
		}
		if field.Type == "integer" && (!a.IsInt() || !b.IsInt()) {
			return invalid("value", "integer increment requires integral values")
		}
		if scaleB > scaleA {
			scaleA = scaleB
		}
		text := sum.FloatString(scaleA)
		if strings.Contains(text, ".") {
			text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
		}
		assign(json.Number(text), false)
	}
	return nil
}

func decimalNumber(value json.Number) (*big.Rat, int, error) {
	text := value.String()
	if len(text) > 128 {
		return nil, 0, fmt.Errorf("number too long")
	}
	parts := strings.Split(strings.ToLower(text), "e")
	exponent := 0
	if len(parts) == 2 {
		var err error
		exponent, err = strconv.Atoi(parts[1])
		if err != nil || exponent < -100 || exponent > 100 {
			return nil, 0, fmt.Errorf("exponent out of range")
		}
	}
	scale := 0
	if dot := strings.IndexByte(parts[0], '.'); dot >= 0 {
		scale = len(parts[0]) - dot - 1
	}
	scale -= exponent
	if scale < 0 {
		scale = 0
	}
	if scale > 100 {
		return nil, 0, fmt.Errorf("scale out of range")
	}
	number, ok := new(big.Rat).SetString(text)
	if !ok || new(big.Rat).Abs(number).Cmp(new(big.Rat).SetInt64(9007199254740991)) > 0 {
		return nil, 0, fmt.Errorf("number out of range")
	}
	return number, scale, nil
}

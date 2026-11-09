package unsquery

import (
	"bytes"
	"encoding/json"
	"math"
	"math/big"
	"strings"

	"backend/internal/domain/unsmeta"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxRecordFilters = 16
const maxRecordOrder = 3

// Only fixed metadata columns and declared scalar JSON fields are addressable.
// JSON path segments and all values are parameters, never SQL identifiers.
func recordField(schema *unsmeta.Schema, field string) (string, []any, string, error) {
	columns := map[string]string{"id": "id", "createdAt": "created_at", "updatedAt": "updated_at", "revision": "revision"}
	if column, ok := columns[field]; ok {
		kind := "number"
		if field == "id" {
			kind = "string"
		}
		return column, nil, kind, nil
	}
	parts := strings.Split(field, ".")
	if len(parts) < 2 || len(parts) > 9 || parts[0] != "data" || schema == nil {
		return "", nil, "", invalid("field", "expected a declared data field or record metadata field")
	}
	current := *schema
	vars := []any{}
	for _, part := range parts[1:] {
		child, ok := current.Properties[part]
		if current.Type != "object" || !ok || !unsmeta.ValidID(part) || strings.Contains(part, ".") {
			return "", nil, "", invalid("field", "field is not declared in recordSchema")
		}
		vars = append(vars, part)
		current = child
	}
	expr := "jsonb_extract_path_text(data," + strings.TrimSuffix(strings.Repeat("?,", len(vars)), ",") + ")"
	switch current.Type {
	case "string":
	case "integer", "number":
		expr = "(" + expr + ")::numeric"
	case "boolean":
		expr = "(" + expr + ")::boolean"
	default:
		return "", nil, "", invalid("field", "only scalar fields support filters and ordering")
	}
	return expr, vars, current.Type, nil
}

func filterValue(kind string, value any) bool {
	if number, ok := value.(json.Number); ok {
		parsed, valid := new(big.Rat).SetString(number.String())
		return valid && (kind == "number" || kind == "integer" && parsed.IsInt())
	}
	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && !math.IsInf(number, 0) && !math.IsNaN(number) && math.Trunc(number) == number
	case "number":
		_, ok := value.(float64)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	}
	return false
}

func RecordFilters(query *gorm.DB, schema *unsmeta.Schema, filters []RecordFilter) (*gorm.DB, error) {
	if len(filters) > maxRecordFilters {
		return nil, invalid("filters", "at most 16 filters allowed")
	}
	operators := map[string]string{"eq": "=", "ne": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
	for _, filter := range filters {
		expr, args, kind, err := recordField(schema, filter.Field)
		if err != nil {
			return nil, err
		}
		var value any
		decoder := json.NewDecoder(bytes.NewReader(filter.Value))
		decoder.UseNumber()
		if !json.Valid(filter.Value) || decoder.Decode(&value) != nil {
			return nil, invalid("value", "filter value is required")
		}
		if filter.Op == "exists" {
			present, ok := value.(bool)
			if !ok {
				return nil, invalid("value", "exists requires a boolean")
			}
			op := " IS NOT NULL"
			if !present {
				op = " IS NULL"
			}
			query = query.Where(expr+op, args...)
			continue
		}
		if value == nil {
			if filter.Op != "eq" && filter.Op != "ne" {
				return nil, invalid("value", "null supports eq or ne only")
			}
			op := " IS NULL"
			if filter.Op == "ne" {
				op = " IS NOT NULL"
			}
			query = query.Where(expr+op, args...)
			continue
		}
		if filter.Op == "in" {
			values, ok := value.([]any)
			if !ok || len(values) == 0 || len(values) > 100 {
				return nil, invalid("value", "in requires 1 to 100 scalar values")
			}
			for _, item := range values {
				if !filterValue(kind, item) {
					return nil, invalid("value", "filter type does not match schema")
				}
			}
			query = query.Where(expr+" IN ?", append(args, values)...)
			continue
		}
		if !filterValue(kind, value) {
			return nil, invalid("value", "filter type does not match schema")
		}
		if filter.Op == "contains" {
			if kind != "string" {
				return nil, invalid("op", "contains requires a string field")
			}
			query = query.Where("strpos("+expr+",?)>0", append(args, value)...)
			continue
		}
		op, ok := operators[filter.Op]
		if !ok || kind == "boolean" && filter.Op != "eq" && filter.Op != "ne" {
			return nil, invalid("op", "unsupported filter operator")
		}
		query = query.Where(expr+" "+op+" ?", append(args, value)...)
	}
	return query, nil
}

func OrderRecords(query *gorm.DB, schema *unsmeta.Schema, orders []RecordOrder) (*gorm.DB, error) {
	if len(orders) > maxRecordOrder {
		return nil, invalid("orderBy", "at most 3 sort fields allowed")
	}
	parts := []string{}
	args := []any{}
	seen := map[string]bool{}
	for _, order := range orders {
		if seen[order.Field] || order.Direction != "asc" && order.Direction != "desc" {
			return nil, invalid("orderBy", "unique fields and asc/desc required")
		}
		seen[order.Field] = true
		expr, vars, _, err := recordField(schema, order.Field)
		if err != nil {
			return nil, err
		}
		parts = append(parts, expr+" "+strings.ToUpper(order.Direction)+" NULLS LAST")
		args = append(args, vars...)
	}
	if !seen["id"] {
		parts = append(parts, "id ASC")
	}
	return query.Clauses(clause.OrderBy{Expression: clause.Expr{SQL: strings.Join(parts, ","), Vars: args}}), nil
}

// Filter and Order form the shared API/Stream query contract.
type RecordFilter struct {
	Field string          `json:"field"`
	Op    string          `json:"op"`
	Value json.RawMessage `json:"value,omitempty"`
}
type RecordOrder struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}
type ValidationError struct{ Path, Message string }

func (e *ValidationError) Error() string { return e.Message }
func invalid(path, message string) error { return &ValidationError{Path: path, Message: message} }

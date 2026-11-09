package unsv2

import (
	"context"
	"encoding/json"
	"math/big"
	"strings"

	"backend/internal/domain/unsmeta"
	"backend/internal/fieldpath"
)

func applyArrayOperation(schema *unsmeta.Schema, current any, op string, value any) ([]any, error) {
	if schema == nil || schema.Type != "array" || schema.Items == nil {
		return nil, invalid("path", "array operation requires a declared array")
	}
	items, ok := current.([]any)
	if !ok {
		return nil, invalid("path", "array operation requires an existing array")
	}
	result := append([]any{}, items...)
	if op == "pull" {
		condition, ok := value.(map[string]any)
		if !ok || len(condition) == 0 {
			return nil, invalid("value", "pull requires a nonempty element condition object")
		}
		if containsPullExpression(condition) {
			return nil, invalid("value", "pull supports field conditions, not $expr")
		}
		compiler := pipelineCompiler{ctx: context.Background()}
		if _, err := compiler.elementPredicate(condition, "element", schema.Items, 0); err != nil {
			return nil, err
		}
		result = []any{}
		for _, item := range items {
			if !matchArrayElement(item, true, condition, schema.Items) {
				result = append(result, item)
			}
		}
		return result, nil
	}
	values, ok := value.([]any)
	if !ok || len(values) < 1 || len(values) > 200 {
		return nil, invalid("value", "append/addToSet requires 1 to 200 array elements")
	}
	for _, item := range values {
		if !schema.Items.Accepts(item) {
			return nil, invalid("value", "array element does not match declared item schema")
		}
		if op == "addToSet" {
			found := false
			for _, old := range result {
				if equalJSONValue(old, item) {
					found = true
					break
				}
			}
			if found {
				continue
			}
		}
		result = append(result, item)
	}
	return result, nil
}

func containsPullExpression(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "$expr" || containsPullExpression(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsPullExpression(child) {
				return true
			}
		}
	}
	return false
}

// JSON numbers compare by value, preserving exact decimals and large integers.
func equalJSONValue(a, b any) bool {
	switch x := a.(type) {
	case nil:
		return b == nil
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		left, lok := new(big.Rat).SetString(x.String())
		right, rok := new(big.Rat).SetString(y.String())
		return lok && rok && left.Cmp(right) == 0
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equalJSONValue(x[i], y[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			other, exists := y[k]
			if !exists || !equalJSONValue(v, other) {
				return false
			}
		}
		return true
	}
	return false
}

// Conditions reach this evaluator only after the shared SQL compiler validates
// operators, declared paths, types and complexity; it mirrors its null semantics.
func matchArrayElement(value any, exists bool, condition map[string]any, schema *unsmeta.Schema) bool {
	if schema.Type != "object" {
		return matchArrayComparisons(value, exists, condition, schema)
	}
	for key, operand := range condition {
		switch key {
		case "$not":
			if matchArrayElement(value, exists, operand.(map[string]any), schema) {
				return false
			}
		case "$and", "$or", "$nor":
			all, anyMatch := true, false
			for _, child := range operand.([]any) {
				matched := matchArrayElement(value, exists, child.(map[string]any), schema)
				all = all && matched
				anyMatch = anyMatch || matched
			}
			if key == "$and" && !all || key == "$or" && !anyMatch || key == "$nor" && anyMatch {
				return false
			}
		default:
			parts, _ := fieldpath.Parse(key)
			child, present := fieldpath.Value(value, parts)
			childSchema, _ := unsmeta.SchemaField(schema, parts)
			ops, ok := operand.(map[string]any)
			if !ok {
				ops = map[string]any{"$eq": operand}
			}
			if !matchArrayComparisons(child, present, ops, childSchema) {
				return false
			}
		}
	}
	return true
}

func matchArrayComparisons(value any, exists bool, ops map[string]any, schema *unsmeta.Schema) bool {
	for op, operand := range ops {
		matched := false
		switch op {
		case "$exists":
			matched = exists == operand.(bool)
		case "$not":
			matched = !matchArrayComparisons(value, exists, operand.(map[string]any), schema)
		case "$eq":
			matched = equalJSONValue(value, operand)
		case "$ne":
			matched = !equalJSONValue(value, operand)
		case "$in", "$nin":
			for _, candidate := range operand.([]any) {
				if equalJSONValue(value, candidate) {
					matched = true
					break
				}
			}
			if op == "$nin" {
				matched = !matched
			}
		case "$gt", "$gte", "$lt", "$lte":
			comparison, valid := compareJSONScalar(value, operand)
			matched = valid && (op == "$gt" && comparison > 0 || op == "$gte" && comparison >= 0 || op == "$lt" && comparison < 0 || op == "$lte" && comparison <= 0)
		case "$size":
			array, ok := value.([]any)
			size, _ := pipelineInteger(operand)
			matched = ok && len(array) == size
		case "$all":
			array, ok := value.([]any)
			matched = ok
			for _, required := range operand.([]any) {
				found := false
				for _, item := range array {
					if equalJSONValue(item, required) {
						found = true
						break
					}
				}
				matched = matched && found
			}
		case "$elemMatch":
			array, _ := value.([]any)
			for _, item := range array {
				if matchArrayElement(item, true, operand.(map[string]any), schema.Items) {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func compareJSONScalar(a, b any) (int, bool) {
	if x, ok := a.(json.Number); ok {
		y, ok := b.(json.Number)
		if !ok {
			return 0, false
		}
		left, lok := new(big.Rat).SetString(x.String())
		right, rok := new(big.Rat).SetString(y.String())
		if !lok || !rok {
			return 0, false
		}
		return left.Cmp(right), true
	}
	if x, ok := a.(string); ok {
		y, ok := b.(string)
		return strings.Compare(x, y), ok
	}
	return 0, false
}

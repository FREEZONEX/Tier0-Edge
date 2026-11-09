package unsv2

import (
	"encoding/json"
	"fmt"
	"strings"

	"backend/internal/domain/unsmeta"
)

func (c *pipelineCompiler) fieldComparison(f pipelineExpr, op string, value any, depth int) (pipelineExpr, error) {
	c.nodes++
	if depth > 8 || c.nodes > 128 {
		return f, pipelineError("filter exceeds 128 nodes or depth 8")
	}
	if op == "$not" {
		m, ok := value.(map[string]any)
		if !ok || len(m) == 0 || len(m) > 16 {
			return f, pipelineError("$not requires field comparisons")
		}
		parts := []string{}
		args := []any{}
		for _, key := range pipelineKeys(m) {
			if key == "$not" {
				return f, pipelineError("nested field $not is not supported")
			}
			e, err := c.fieldComparison(f, key, m[key], depth+1)
			if err != nil {
				return e, err
			}
			parts = append(parts, e.sql)
			args = append(args, e.args...)
		}
		return pipelineExpr{sql: "NOT (" + strings.Join(parts, " AND ") + ")", args: args}, nil
	}
	if op != "$size" && op != "$all" && op != "$elemMatch" {
		return pipelineComparison(f, op, value)
	}
	if f.schema == nil || f.schema.Type != "array" || f.schema.Items == nil {
		return f, pipelineError(op + " requires a declared array")
	}
	safe := "CASE WHEN jsonb_typeof(" + f.sql + ")='array' THEN " + f.sql + " ELSE '[]'::jsonb END"
	if op == "$size" {
		n, err := pipelineInteger(value)
		if err != nil {
			return f, err
		}
		return pipelineExpr{sql: "COALESCE((jsonb_typeof(" + f.sql + ")='array' AND jsonb_array_length(" + safe + ")=?),FALSE)", args: []any{n}}, nil
	}
	if op == "$all" {
		values, ok := value.([]any)
		if !ok || len(values) < 1 || len(values) > 200 {
			return f, pipelineError("$all requires 1 to 200 array elements")
		}
		parts := []string{}
		args := []any{}
		// Containment is a necessary, indexable prefilter. Exact equality below
		// prevents an object subset from matching a larger object element.
		rawAll, err := json.Marshal(values)
		if err != nil {
			return f, err
		}
		parts = append(parts, "("+f.sql+" @> ?::jsonb)")
		args = append(args, string(rawAll))
		for _, v := range values {
			if !f.schema.Items.Accepts(v) {
				return f, pipelineError("$all value does not match array item schema")
			}
			raw, err := json.Marshal(v)
			if err != nil {
				return f, err
			}
			parts = append(parts, "EXISTS (SELECT 1 FROM jsonb_array_elements("+safe+") AS ae(value) WHERE ae.value=?::jsonb)")
			args = append(args, string(raw))
		}
		return pipelineExpr{sql: "(" + strings.Join(parts, " AND ") + ")", args: args}, nil
	}
	m, ok := value.(map[string]any)
	if !ok || len(m) == 0 {
		return f, pipelineError("$elemMatch requires a nonempty element condition")
	}
	alias := fmt.Sprintf("ae%d", depth)
	e, err := c.elementPredicate(m, alias+".value", f.schema.Items, depth+1)
	if err != nil {
		return e, err
	}
	return pipelineExpr{sql: "EXISTS (SELECT 1 FROM jsonb_array_elements(" + safe + ") AS " + alias + "(value) WHERE " + e.sql + ")", args: e.args}, nil
}

func (c *pipelineCompiler) elementPredicate(m map[string]any, sql string, schema *unsmeta.Schema, depth int) (pipelineExpr, error) {
	if schema.Type == "object" {
		return c.predicate(m, sql, schema, depth)
	}
	f := pipelineExpr{sql: sql, schema: schema}
	parts := []string{}
	args := []any{}
	for _, op := range pipelineKeys(m) {
		e, err := c.fieldComparison(f, op, m[op], depth)
		if err != nil {
			return e, err
		}
		parts = append(parts, e.sql)
		args = append(args, e.args...)
	}
	return pipelineExpr{sql: "(" + strings.Join(parts, " AND ") + ")", args: args}, nil
}

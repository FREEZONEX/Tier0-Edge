package unsv2

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/fieldpath"
)

// A pipeline always operates on a public JSON document, never database columns.
// The same compiler is used by read predicates and guarded CRUD selectors.
type pipelineExpr struct {
	sql             string
	args            []any
	schema          *unsmeta.Schema
	native          string
	temporal        bool
	directReference bool
}
type pipelineCompiler struct {
	ctx    context.Context
	stages int
	nodes  int
}

type dataQueryFieldPolicyKey struct{}

// WithDataQueryFieldPolicy applies a caller's additional field restrictions to
// all resolved input fields, including nested lookup and CRUD predicates.
func WithDataQueryFieldPolicy(ctx context.Context, policy func(string, *unsmeta.Schema) error) context.Context {
	return context.WithValue(ctx, dataQueryFieldPolicyKey{}, policy)
}

func pipelineError(message string) error { return invalid("pipeline", message) }
func pipelineKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func pipelineLiteral(s string) string { return fieldpath.PGTextLiteral(s) }
func pipelinePath(path string) ([]string, error) {
	p, err := unsmeta.ParseFieldPath(path)
	if err != nil {
		return nil, pipelineError("invalid field path; escape literal dots and backslashes")
	}
	return p, nil
}
func (c *pipelineCompiler) field(doc, path string, schema *unsmeta.Schema) (pipelineExpr, error) {
	parts, err := pipelinePath(path)
	if err != nil {
		return pipelineExpr{}, err
	}
	cur, ok := unsmeta.SchemaField(schema, parts)
	if !ok {
		return pipelineExpr{}, pipelineError("unknown field: " + path)
	}
	quoted := []string{}
	for _, p := range parts {
		quoted = append(quoted, pipelineLiteral(p))
	}
	if subject, ok := contextx.SubjectFrom(c.ctx); ok && subject.MCPRestricted {
		if err := CheckMCPDataQueryField(path, cur); err != nil {
			return pipelineExpr{}, err
		}
	}
	if policy, ok := c.ctx.Value(dataQueryFieldPolicyKey{}).(func(string, *unsmeta.Schema) error); ok {
		if err := policy(path, cur); err != nil {
			return pipelineExpr{}, err
		}
	}
	e := pipelineExpr{sql: "jsonb_extract_path(" + doc + "," + strings.Join(quoted, ",") + ")", schema: cur}
	if strings.HasPrefix(doc, "base:") {
		alias := strings.TrimPrefix(doc, "base:")
		column := map[string]string{"_id": "id", "_meta.collectionId": "collection_id", "_meta.recordId": "record_id", "_meta.sourceId": "source_id", "_meta.sourceTime": "source_time", "_meta.streamId": "stream_id", "_meta.bindingId": "binding_id", "_meta.quality": "quality", "_meta.revision": "revision", "_meta.createdAt": "created_at", "_meta.updatedAt": "updated_at", "_meta.time": "time", "_meta.receivedAt": "received_at"}[path]
		if path == "_meta.locked" {
			e.native = "COALESCE((" + alias + ".lock_expires_at > now()),FALSE)"
			e.sql = "to_jsonb(COALESCE(" + e.native + ",FALSE))"
		} else if column != "" {
			e.native = alias + "." + column
			e.temporal = column == "time" || column == "received_at" || column == "source_time"
			e.sql = "COALESCE(to_jsonb(" + e.native + "),'null'::jsonb)"
		} else {
			e.sql = "jsonb_extract_path(" + alias + ".data," + strings.Join(quoted, ",") + ")"
		}
	}
	return e, nil
}
func pipelineScalar(e pipelineExpr) string {
	if e.native != "" {
		return e.native
	}
	s := "(" + e.sql + " #>> '{}')"
	if e.schema != nil {
		switch e.schema.Type {
		case "number", "integer":
			return "(" + s + ")::numeric"
		case "boolean":
			return "(" + s + ")::boolean"
		}
	}
	return s
}
func pipelineClone(s *unsmeta.Schema) *unsmeta.Schema {
	if s == nil {
		return nil
	}
	v := *s
	v.Properties = map[string]unsmeta.Schema{}
	for k, x := range s.Properties {
		v.Properties[k] = *pipelineClone(&x)
	}
	if s.Items != nil {
		v.Items = pipelineClone(s.Items)
	}
	return &v
}
func pipelineSchema(ctx context.Context, kind, id string) (*unsmeta.Schema, error) {
	d, err := readDefinition(ctx, id)
	if err != nil {
		return nil, err
	}
	data := d.RecordSchema
	if kind == "samples" {
		data = d.SampleSchema
	} else if kind != "records" {
		return nil, pipelineError("kind must be records or samples")
	}
	if data == nil {
		return nil, pipelineError("Collection does not declare the requested data schema")
	}
	p := pipelineClone(data)
	if p.Properties == nil {
		p.Properties = map[string]unsmeta.Schema{}
	}
	m := map[string]unsmeta.Schema{"collectionId": {Type: "string"}, "sourceId": {Type: "string"}}
	if kind == "records" {
		for _, key := range []string{"revision", "createdAt", "updatedAt", "sourceTime"} {
			m[key] = unsmeta.Schema{Type: "integer"}
		}
		m["locked"] = unsmeta.Schema{Type: "boolean"}
	} else {
		for _, key := range []string{"recordId", "streamId", "bindingId", "quality"} {
			m[key] = unsmeta.Schema{Type: "string"}
		}
		m["time"] = unsmeta.Schema{Type: "integer"}
		m["receivedAt"] = unsmeta.Schema{Type: "integer"}
	}
	p.Properties["_id"] = unsmeta.Schema{Type: "string"}
	p.Properties["_meta"] = unsmeta.Schema{Type: "object", AdditionalProperties: pipelineClosedObject(), Properties: m}
	return p, nil
}

// DataDocumentSQL produces the Nova query document: business fields at the root,
// with reserved identity and server metadata. Normal Record DTOs retain data.
func DataDocumentSQL(kind, alias string) string {
	pair := []string{"'collectionId'," + alias + ".collection_id", "'sourceId'," + alias + ".source_id"}
	if kind == "records" {
		pair = append(pair, "'revision',"+alias+".revision", "'createdAt',"+alias+".created_at", "'updatedAt',"+alias+".updated_at", "'sourceTime',floor(extract(epoch from "+alias+".source_time)*1000)::bigint", "'locked',COALESCE("+alias+".lock_expires_at>now(),FALSE)")
		pair = append(pair, "'lock',CASE WHEN "+alias+".lock_expires_at>now() THEN jsonb_build_object('label',COALESCE("+alias+".lock_label,''),'expiresAt',floor(extract(epoch from "+alias+".lock_expires_at)*1000)::bigint,'fence',"+alias+".lock_fence,'since',floor(extract(epoch from "+alias+".lock_since)*1000)::bigint) ELSE NULL END")
	} else {
		pair = append(pair, "'recordId',"+alias+".record_id", "'quality',"+alias+".quality", "'streamId',"+alias+".stream_id", "'bindingId',"+alias+".binding_id", "'time',floor(extract(epoch from "+alias+".time)*1000)::bigint", "'receivedAt',floor(extract(epoch from "+alias+".received_at)*1000)::bigint")
	}
	return "(" + alias + ".data || jsonb_build_object('_id'," + alias + ".id,'_meta',jsonb_build_object(" + strings.Join(pair, ",") + ")))"
}

// CompileDataFilter shares $match semantics with CRUD. The caller authorizes
// the operation; compiling a write selector must not require a read grant.
func CompileDataFilter(ctx context.Context, kind, collectionID, documentSQL string, filter json.RawMessage) (string, []any, error) {
	schema, err := pipelineSchema(ctx, kind, collectionID)
	if err != nil {
		return "", nil, err
	}
	var obj map[string]any
	if err = decodeNumberJSON(filter, &obj); err != nil || obj == nil {
		return "", nil, pipelineError("filter must be an object")
	}
	c := pipelineCompiler{ctx: ctx}
	e, err := c.predicate(obj, documentSQL, schema, 0)
	return e.sql, e.args, err
}
func (c *pipelineCompiler) predicate(obj map[string]any, doc string, schema *unsmeta.Schema, depth int) (pipelineExpr, error) {
	c.nodes++
	if depth > 8 || c.nodes > 128 {
		return pipelineExpr{}, pipelineError("filter exceeds 128 nodes or depth 8")
	}
	parts := []string{}
	args := []any{}
	for _, key := range pipelineKeys(obj) {
		v := obj[key]
		if strings.HasPrefix(key, "$") {
			if key == "$expr" {
				e, err := c.expression(v, doc, schema, depth+1)
				if err != nil {
					return e, err
				}
				if e.schema == nil || e.schema.Type != "boolean" {
					return e, pipelineError("$expr requires a boolean expression")
				}
				parts = append(parts, "COALESCE("+pipelineScalar(e)+",FALSE)")
				args = append(args, e.args...)
				continue
			}
			if key == "$not" {
				m, ok := v.(map[string]any)
				if !ok {
					return pipelineExpr{}, pipelineError("$not requires an object")
				}
				e, err := c.predicate(m, doc, schema, depth+1)
				if err != nil {
					return e, err
				}
				parts = append(parts, "NOT ("+e.sql+")")
				args = append(args, e.args...)
				continue
			}
			if key != "$and" && key != "$or" && key != "$nor" {
				return pipelineExpr{}, pipelineError("unsupported logical operator: " + key)
			}
			values, ok := v.([]any)
			if !ok || len(values) == 0 || len(values) > 64 {
				return pipelineExpr{}, pipelineError("logical operator requires 1 to 64 objects")
			}
			subs := []string{}
			for _, value := range values {
				m, ok := value.(map[string]any)
				if !ok {
					return pipelineExpr{}, pipelineError("logical operand must be an object")
				}
				e, err := c.predicate(m, doc, schema, depth+1)
				if err != nil {
					return e, err
				}
				subs = append(subs, "("+e.sql+")")
				args = append(args, e.args...)
			}
			join := " OR "
			if key == "$and" {
				join = " AND "
			}
			s := "(" + strings.Join(subs, join) + ")"
			if key == "$nor" {
				s = "NOT " + s
			}
			parts = append(parts, s)
			continue
		}
		f, err := c.field(doc, key, schema)
		if err != nil {
			return f, err
		}
		ops, ok := v.(map[string]any)
		if !ok {
			ops = map[string]any{"$eq": v}
		}
		if len(ops) == 0 {
			return f, pipelineError("empty field predicate")
		}
		for _, op := range pipelineKeys(ops) {
			c.nodes++
			if c.nodes > 128 {
				return f, pipelineError("filter exceeds 128 nodes")
			}
			e, err := c.fieldComparison(f, op, ops[op], depth+1)
			if err != nil {
				return e, err
			}
			parts = append(parts, e.sql)
			args = append(args, e.args...)
		}
	}
	if len(parts) == 0 {
		return pipelineExpr{sql: "TRUE"}, nil
	}
	return pipelineExpr{sql: "(" + strings.Join(parts, " AND ") + ")", args: args}, nil
}
func pipelineComparison(f pipelineExpr, op string, v any) (pipelineExpr, error) {
	if op == "$not" {
		m, ok := v.(map[string]any)
		if !ok || len(m) == 0 || len(m) > 16 {
			return f, pipelineError("$not requires field comparisons")
		}
		parts := []string{}
		args := []any{}
		for _, key := range pipelineKeys(m) {
			if key == "$not" {
				return f, pipelineError("nested field $not is not supported")
			}
			e, err := pipelineComparison(f, key, m[key])
			if err != nil {
				return e, err
			}
			parts = append(parts, e.sql)
			args = append(args, e.args...)
		}
		return pipelineExpr{sql: "NOT (" + strings.Join(parts, " AND ") + ")", args: args}, nil
	}
	if op == "$exists" {
		b, ok := v.(bool)
		if !ok {
			return f, pipelineError("$exists requires boolean")
		}
		suffix := " IS NULL"
		if b {
			suffix = " IS NOT NULL"
		}
		return pipelineExpr{sql: f.sql + suffix}, nil
	}
	if op == "$in" || op == "$nin" {
		a, ok := v.([]any)
		if !ok || len(a) < 1 || len(a) > 200 {
			return f, pipelineError("$in/$nin require 1 to 200 scalar values")
		}
		parts := []string{}
		args := []any{}
		for _, x := range a {
			e, err := pipelineComparison(f, "$eq", x)
			if err != nil {
				return e, err
			}
			parts = append(parts, e.sql)
			args = append(args, e.args...)
		}
		s := "(" + strings.Join(parts, " OR ") + ")"
		if op == "$nin" {
			s = "NOT " + s
		}
		return pipelineExpr{sql: s, args: args}, nil
	}
	sqlop, ok := map[string]string{"$eq": "=", "$ne": "<>", "$gt": ">", "$gte": ">=", "$lt": "<", "$lte": "<="}[op]
	if !ok {
		return f, pipelineError("unsupported comparison operator: " + op)
	}
	if v == nil {
		if op != "$eq" && op != "$ne" {
			return f, pipelineError("null supports only $eq/$ne")
		}
		if op == "$eq" {
			return pipelineExpr{sql: "(" + f.sql + " IS NULL OR " + f.sql + " = 'null'::jsonb)"}, nil
		}
		return pipelineExpr{sql: "(" + f.sql + " IS NOT NULL AND " + f.sql + " <> 'null'::jsonb)"}, nil
	}
	if !relationalValue(f.schema.Type, v) {
		return f, pipelineError("predicate value does not match declared field type")
	}
	if f.schema.Type == "boolean" && op != "$eq" && op != "$ne" {
		return f, pipelineError("boolean supports only equality")
	}
	missing := "FALSE"
	if op == "$ne" {
		missing = "TRUE"
	}
	if f.native != "" {
		if f.temporal {
			n := v.(json.Number)
			rational, ok := new(big.Rat).SetString(n.String())
			if !ok || !rational.IsInt() || !rational.Num().IsInt64() {
				return f, pipelineError("time values must be integer Unix milliseconds")
			}
			timestamp := rational.Num().Int64()
			if timestamp < 0 || timestamp > 253402300799999 {
				return f, pipelineError("time values must be integer Unix milliseconds between 0 and 253402300799999")
			}
			v = timestamp
			// Public timestamps are Unix milliseconds. Stored received_at may
			// retain microseconds, so equality addresses the whole millisecond.
			bound := func() string { return "to_timestamp(?::numeric / 1000)" }
			switch op {
			case "$eq":
				return pipelineExpr{sql: "(" + f.native + " >= " + bound() + " AND " + f.native + " < " + bound() + ")", args: []any{timestamp, timestamp + 1}}, nil
			case "$ne":
				return pipelineExpr{sql: "(" + f.native + " < " + bound() + " OR " + f.native + " >= " + bound() + ")", args: []any{timestamp, timestamp + 1}}, nil
			case "$gt":
				return pipelineExpr{sql: f.native + " >= " + bound(), args: []any{timestamp + 1}}, nil
			case "$lte":
				return pipelineExpr{sql: f.native + " < " + bound(), args: []any{timestamp + 1}}, nil
			}
		}
		placeholder := "?"
		if relationalNumeric(f.schema.Type) {
			placeholder = "?::numeric"
		}
		if f.temporal {
			placeholder = "to_timestamp(?::numeric / 1000)"
		}
		comparison := f.native + " " + sqlop + " " + placeholder
		// All mapped physical columns except source_id are NOT NULL. Preserve
		// a plain comparison so PostgreSQL can use the timestamp/identity index.
		if strings.HasSuffix(f.native, ".source_id") {
			comparison = "COALESCE(" + comparison + "," + missing + ")"
		}
		return pipelineExpr{sql: comparison, args: []any{v}}, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return f, err
	}
	// Normalize missing to JSON null before comparison. Besides preserving
	// missing/null truth values under logical negation, this leaves a direct
	// expression-index comparison for PostgreSQL instead of wrapping the
	// predicate itself in COALESCE.
	comparison := "COALESCE(" + f.sql + ",'null'::jsonb) " + sqlop + " ?::jsonb"
	if op != "$eq" && op != "$ne" {
		kind := f.schema.Type
		if kind == "integer" {
			kind = "number"
		}
		comparison = "(" + comparison + " AND COALESCE(jsonb_typeof(" + f.sql + ")=" + pipelineLiteral(kind) + ",FALSE))"
	}
	return pipelineExpr{sql: comparison, args: []any{string(raw)}}, nil
}
func (c *pipelineCompiler) expression(v any, doc string, schema *unsmeta.Schema, depth int) (pipelineExpr, error) {
	c.nodes++
	if depth > 8 || c.nodes > 512 {
		return pipelineExpr{}, pipelineError("expressions exceed complexity limit")
	}
	if s, ok := v.(string); ok && strings.HasPrefix(s, "$") {
		e, err := c.field(doc, strings.TrimPrefix(s, "$"), schema)
		// Expressions see the public millisecond value even when a CRUD
		// predicate resolves a Sample timestamp directly to a physical column.
		if e.temporal {
			e.native = "floor(extract(epoch from " + e.native + ")*1000)::bigint"
			e.sql = "to_jsonb(" + e.native + ")"
			e.temporal = false
		}
		e.directReference = true
		return e, err
	}
	if m, ok := v.(map[string]any); ok {
		if len(m) != 1 {
			return pipelineExpr{}, pipelineError("expression needs exactly one operator")
		}
		for op, value := range m {
			if op == "$literal" {
				return pipelineConstant(value)
			}
			if e, handled, err := c.extendedExpression(op, value, doc, schema, depth); handled {
				return e, err
			}
			if op == "$ifNull" {
				a, ok := value.([]any)
				if !ok || len(a) != 2 {
					return pipelineExpr{}, pipelineError("$ifNull needs two expressions")
				}
				x, err := c.expression(a[0], doc, schema, depth+1)
				if err != nil {
					return x, err
				}
				y, err := c.expression(a[1], doc, schema, depth+1)
				if err != nil {
					return y, err
				}
				numericPair := relationalNumeric(x.schema.Type) && relationalNumeric(y.schema.Type)
				if x.schema.Type != y.schema.Type && x.schema.Type != "null" && y.schema.Type != "null" && !numericPair {
					return x, pipelineError("$ifNull expressions must have matching types")
				}
				typ := x.schema
				if typ.Type == "null" {
					typ = y.schema
				}
				if numericPair && x.schema.Type != y.schema.Type {
					typ = &unsmeta.Schema{Type: "number"}
				}
				return pipelineExpr{sql: "COALESCE(NULLIF(" + x.sql + ",'null'::jsonb)," + y.sql + ")", args: append(x.args, y.args...), schema: typ}, nil
			}
			operators := map[string]string{"$add": "+", "$subtract": "-", "$multiply": "*", "$divide": "/"}
			symbol, ok := operators[op]
			if !ok {
				return pipelineExpr{}, pipelineError("unsupported expression operator: " + op)
			}
			a, ok := value.([]any)
			if !ok || len(a) != 2 {
				return pipelineExpr{}, pipelineError("arithmetic requires two expressions")
			}
			x, err := c.expression(a[0], doc, schema, depth+1)
			if err != nil {
				return x, err
			}
			y, err := c.expression(a[1], doc, schema, depth+1)
			if err != nil {
				return y, err
			}
			if !relationalNumeric(x.schema.Type) || !relationalNumeric(y.schema.Type) {
				return x, pipelineError("arithmetic requires numeric expressions")
			}
			right := pipelineScalar(y)
			if op == "$divide" {
				right = "NULLIF(" + right + ",0)"
			}
			return pipelineExpr{sql: "to_jsonb(" + pipelineScalar(x) + symbol + right + ")", args: append(x.args, y.args...), schema: &unsmeta.Schema{Type: "number"}}, nil
		}
	}
	return pipelineConstant(v)
}
func pipelineConstant(v any) (pipelineExpr, error) {
	kind := ""
	switch v.(type) {
	case nil:
		kind = "null"
	case string:
		kind = "string"
	case bool:
		kind = "boolean"
	case json.Number:
		kind = "number"
	default:
		return pipelineExpr{}, pipelineError("literal must be scalar")
	}
	b, err := json.Marshal(v)
	return pipelineExpr{sql: "?::jsonb", args: []any{string(b)}, schema: &unsmeta.Schema{Type: kind}}, err
}
func pipelineInteger(v any) (int, error) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, pipelineError("expected integer")
	}
	i, err := strconv.Atoi(n.String())
	if err != nil || i < 0 || i > 1000000 {
		return 0, pipelineError("integer must be between 0 and 1000000")
	}
	return i, nil
}
func pipelineDecode(raw json.RawMessage) (any, error) {
	var v any
	err := decodeNumberJSON(raw, &v)
	if err != nil {
		return nil, pipelineError("invalid JSON")
	}
	return v, nil
}
func pipelineTopKey(k string) error {
	p, err := pipelinePath(k)
	if err != nil {
		return err
	}
	if len(p) != 1 {
		return pipelineError("output field names must be top-level; use nested field references for input")
	}
	return nil
}
func pipelineQueryFault(index int, id string, err error) error {
	err = classify(err)
	if f, ok := err.(*Fault); ok {
		v := *f
		v.QueryID = id
		v.Path = fmt.Sprintf("queries[%d].%s", index, strings.TrimPrefix(f.Path, "/"))
		v.Repair = "Correct the identified query using the unified pipeline contract"
		return &v
	}
	return err
}
func pipelineClosedObject() *bool { value := false; return &value }

// CompileDataSort validates the same ordered scalar sort used by pipelines.
// Callers append their stable identity tie-breaker after the returned terms.
func CompileDataSort(ctx context.Context, kind, collectionID, documentSQL string, order []RecordOrder) (string, error) {
	schema, err := pipelineSchema(ctx, kind, collectionID)
	if err != nil {
		return "", err
	}
	c := pipelineCompiler{ctx: ctx}
	terms, err := c.sortTerms(documentSQL, schema, order)
	return strings.Join(terms, ","), err
}
func (c *pipelineCompiler) sortTerms(doc string, schema *unsmeta.Schema, order []RecordOrder) ([]string, error) {
	if len(order) > 16 {
		return nil, pipelineError("sort supports at most 16 fields")
	}
	terms := []string{}
	seen := map[string]bool{}
	for _, o := range order {
		if seen[o.Field] || o.Field == "" {
			return nil, pipelineError("sort fields must be unique")
		}
		seen[o.Field] = true
		if o.Direction != "asc" && o.Direction != "desc" {
			return nil, pipelineError("sort direction must be asc or desc")
		}
		e, err := c.field(doc, o.Field, schema)
		if err != nil {
			return nil, err
		}
		if e.schema.Type == "object" || e.schema.Type == "array" {
			return nil, pipelineError("sort requires scalar fields")
		}
		nulls := " NULLS LAST"
		if e.native != "" && !strings.HasSuffix(e.native, ".source_id") {
			nulls = ""
		}
		terms = append(terms, pipelineScalar(e)+" "+strings.ToUpper(o.Direction)+nulls)
	}
	return terms, nil
}

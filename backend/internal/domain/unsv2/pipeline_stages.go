package unsv2

import (
	"backend/internal/repo"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"backend/internal/domain/unsmeta"
)

type pipelinePlan struct {
	sql       string
	args      []any
	schema    *unsmeta.Schema
	base      bool
	direct    bool
	seek      []pipelineSeekTerm
	from      string
	order     string
	baseWhere string
}

func (c *pipelineCompiler) source(kind, id, recordID string, scopes ...*sampleQueryScope) (pipelinePlan, error) {
	var scope *sampleQueryScope
	if len(scopes) > 0 {
		scope = scopes[0]
	}
	if err := (DataQuery{RecordID: recordID, sampleScope: scope}).validateScope(kind); err != nil {
		return pipelinePlan{}, err
	}
	if err := require(c.ctx, "uns.read"); err != nil {
		return pipelinePlan{}, err
	}
	if err := appCollectionAccess(c.ctx, id, strings.TrimSuffix(kind, "s")+":read"); err != nil {
		return pipelinePlan{}, err
	}
	if err := repo.RequireLocalCollectionRead(c.ctx, id); err != nil {
		return pipelinePlan{}, classify(err)
	}
	schema, err := pipelineSchema(c.ctx, kind, id)
	if err != nil {
		return pipelinePlan{}, err
	}
	table := "public.uns_record"
	order := "b.id"
	active := " AND b.deleted_at IS NULL"
	args := []any{id}
	if kind == "samples" {
		table = "public.uns_sample"
		order = "b.time,b.record_id,b.id"
		active = " AND b.record_id=?"
		if scope == nil {
			args = append(args, recordID)
		} else {
			active = " AND b.time>=? AND b.time<?"
			args = append(args, time.UnixMilli(scope.From), time.UnixMilli(scope.To))
			if len(scope.RecordIDs) > 0 {
				active += " AND b.record_id IN ?"
				args = append(args, scope.RecordIDs)
			}
		}
	}
	return pipelinePlan{sql: "SELECT " + DataDocumentSQL(kind, "b") + " AS doc,row_number() OVER (ORDER BY " + order + ") AS ord FROM " + table + " b WHERE b.collection_id=?" + active, args: args, schema: schema, base: true, baseWhere: "b.collection_id=?" + active}, nil
}
func (c *pipelineCompiler) compile(p pipelinePlan, stages []map[string]json.RawMessage, depth int) (pipelinePlan, error) {
	if depth > 4 {
		return p, pipelineError("lookup nesting exceeds 4")
	}
	for _, stage := range stages {
		c.stages++
		if c.stages > 32 {
			return p, pipelineError("at most 32 pipeline stages including lookup pipelines")
		}
		if len(stage) != 1 {
			return p, pipelineError("each stage must have exactly one operator")
		}
		for op, raw := range stage {
			if op != "$match" {
				p.base = false
			}
			v, err := pipelineDecode(raw)
			if err != nil {
				return p, err
			}
			switch op {
			case "$match":
				m, ok := v.(map[string]any)
				if !ok {
					return p, pipelineError("$match needs an object")
				}
				docSQL := "p.doc"
				if p.base {
					docSQL = "base:b"
				}
				e, err := c.predicate(m, docSQL, p.schema, 0)
				if err != nil {
					return p, err
				}
				if p.base {
					p.sql += " AND (" + e.sql + ")"
				} else {
					p.sql = "SELECT p.doc,p.ord FROM (" + p.sql + ") p WHERE " + e.sql
				}
				p.args = append(p.args, e.args...)
			case "$sort":
				var order []RecordOrder
				if err := Decode(raw, &order); err != nil {
					return p, err
				}
				if len(order) == 0 {
					return p, pipelineError("$sort requires at least one field")
				}
				orders, err := c.sortTerms("p.doc", p.schema, order)
				if err != nil {
					return p, err
				}
				orders = append(orders, "p.ord")
				p.sql = "SELECT p.doc,row_number() OVER (ORDER BY " + strings.Join(orders, ",") + ") AS ord FROM (" + p.sql + ") p"
			case "$limit", "$skip":
				n, err := pipelineInteger(v)
				if err != nil {
					return p, err
				}
				if op == "$limit" && n == 0 {
					return p, pipelineError("$limit must be positive")
				}
				suffix := " LIMIT ?"
				if op == "$skip" {
					suffix = " OFFSET ?"
				}
				p.sql = "SELECT p.doc,p.ord FROM (" + p.sql + ") p ORDER BY p.ord" + suffix
				p.args = append(p.args, n)
			case "$project", "$set":
				m, ok := v.(map[string]any)
				if !ok || len(m) < 1 || len(m) > 64 {
					return p, pipelineError("projection requires 1 to 64 fields")
				}
				p, err = c.project(p, m, op == "$set")
				if err != nil {
					return p, err
				}
			case "$group":
				m, ok := v.(map[string]any)
				if !ok {
					return p, pipelineError("$group needs an object")
				}
				p, err = c.group(p, m)
				if err != nil {
					return p, err
				}
			case "$distinct", "$frequencies":
				p, err = c.summary(p, v, op == "$frequencies")
				if err != nil {
					return p, err
				}
			case "$count", "$exists":
				p, err = pipelineScalarSummary(p, op, v)
				if err != nil {
					return p, err
				}
			case "$lookup":
				var config pipelineLookup
				if err = Decode(raw, &config); err != nil {
					return p, err
				}
				p, err = c.lookup(p, config, depth)
				if err != nil {
					return p, err
				}
			case "$unwind":
				p, err = c.unwind(p, v)
				if err != nil {
					return p, err
				}
			default:
				return p, pipelineError("unsupported stage: " + op)
			}
		}
	}
	return p, nil
}
func pipelineSchemaPut(schema *unsmeta.Schema, path []string, value *unsmeta.Schema) {
	if len(path) == 1 {
		if value == nil {
			delete(schema.Properties, path[0])
		} else {
			schema.Properties[path[0]] = *pipelineClone(value)
		}
		return
	}
	child, ok := schema.Properties[path[0]]
	if !ok || child.Type != "object" {
		child = unsmeta.Schema{Type: "object", AdditionalProperties: pipelineClosedObject(), Properties: map[string]unsmeta.Schema{}}
	}
	pipelineSchemaPut(&child, path[1:], value)
	schema.Properties[path[0]] = child
}

// Build a nested object patch while preserving siblings at every depth.
func pipelineNestedSet(doc string, path []string, value string) string {
	if len(path) == 1 {
		return "(" + doc + " || jsonb_build_object(" + pipelineLiteral(path[0]) + "," + value + "))"
	}
	child := "COALESCE(NULLIF(" + doc + " -> " + pipelineLiteral(path[0]) + ",'null'::jsonb),'{}'::jsonb)"
	return "(" + doc + " || jsonb_build_object(" + pipelineLiteral(path[0]) + "," + pipelineNestedSet(child, path[1:], value) + "))"
}
func (c *pipelineCompiler) project(p pipelinePlan, m map[string]any, set bool) (pipelinePlan, error) {
	schema := pipelineClone(p.schema)
	include, exclude := false, false
	keys := pipelineKeys(m)
	for i, key := range keys {
		path, err := pipelinePath(key)
		if err != nil {
			return p, err
		}
		current := p.schema
		for _, part := range path[:len(path)-1] {
			child, exists := current.Properties[part]
			if !exists {
				break
			}
			if child.Type != "object" {
				return p, pipelineError("projection/set path traverses a non-object field: " + key)
			}
			current = &child
		}
		for _, other := range keys[i+1:] {
			otherPath, parseErr := pipelinePath(other)
			if parseErr != nil {
				return p, parseErr
			}
			prefix := len(otherPath) > len(path)
			if prefix {
				for j := range path {
					if path[j] != otherPath[j] {
						prefix = false
						break
					}
				}
			}
			if prefix {
				return p, pipelineError("projection paths cannot overlap")
			}
		}
	}
	if !set {
		for k, v := range m {
			if n, ok := v.(json.Number); ok && n.String() == "0" {
				if k != "_id" {
					exclude = true
				}
			} else {
				include = true
			}
		}
		if include && exclude {
			return p, pipelineError("cannot mix inclusion and exclusion projection except _id")
		}
		if include {
			schema = &unsmeta.Schema{Type: "object", AdditionalProperties: pipelineClosedObject(), Properties: map[string]unsmeta.Schema{}}
		}
	}
	base := "p.doc"
	if !set && include {
		base = "'{}'::jsonb"
	}
	from := "(SELECT p.doc AS original_doc," + base + " AS doc,p.ord FROM (" + p.sql + ") p) projection_input"
	previous := "projection_input.doc"
	args := append([]any{}, p.args...)
	for index, key := range keys {
		path, _ := pipelinePath(key)
		v := m[key]
		expression := ""
		var fieldSchema *unsmeta.Schema
		if !set {
			if n, ok := v.(json.Number); ok {
				if n.String() == "0" {
					quoted := []string{}
					for _, part := range path {
						quoted = append(quoted, pipelineLiteral(part))
					}
					expression = "(" + previous + " #- ARRAY[" + strings.Join(quoted, ",") + "])"
				} else if n.String() == "1" {
					v = "$" + key
				}
			}
		}
		if expression == "" {
			e, err := c.expression(v, "projection_input.original_doc", p.schema, 0)
			if err != nil {
				return p, err
			}
			fieldSchema = e.schema
			valueAlias := fmt.Sprintf("projection_value_%d", index)
			from += " CROSS JOIN LATERAL (SELECT " + e.sql + " AS value) " + valueAlias
			valueSQL := valueAlias + ".value"
			expression = pipelineNestedSet(previous, path, "COALESCE("+valueSQL+",'null'::jsonb)")
			if e.directReference {
				quoted := []string{}
				for _, part := range path {
					quoted = append(quoted, pipelineLiteral(part))
				}
				expression = "CASE WHEN " + valueSQL + " IS NULL THEN (" + previous + " #- ARRAY[" + strings.Join(quoted, ",") + "]) ELSE " + expression + " END"
			}
			args = append(args, e.args...)
		}
		pipelineSchemaPut(schema, path, fieldSchema)
		alias := fmt.Sprintf("projection_%d", index)
		from += " CROSS JOIN LATERAL (SELECT " + expression + " AS doc) " + alias
		previous = alias + ".doc"
	}
	p.sql = "SELECT " + previous + " AS doc,projection_input.ord FROM " + from
	p.args = args
	p.schema = schema
	return p, nil
}
func (c *pipelineCompiler) groupKey(v any, p pipelinePlan) (pipelineExpr, error) {
	if m, ok := v.(map[string]any); ok {
		operator := false
		for k := range m {
			operator = operator || strings.HasPrefix(k, "$")
		}
		if !operator {
			if len(m) > 16 {
				return pipelineExpr{}, pipelineError("group key has too many fields")
			}
			pieces := []string{}
			args := []any{}
			schema := &unsmeta.Schema{Type: "object", AdditionalProperties: pipelineClosedObject(), Properties: map[string]unsmeta.Schema{}}
			for _, key := range pipelineKeys(m) {
				if err := pipelineTopKey(key); err != nil {
					return pipelineExpr{}, err
				}
				e, err := c.expression(m[key], "p.doc", p.schema, 0)
				if err != nil {
					return e, err
				}
				decodedKey, _ := pipelinePath(key)
				pieces = append(pieces, pipelineLiteral(decodedKey[0]), e.sql)
				args = append(args, e.args...)
				schema.Properties[decodedKey[0]] = *e.schema
			}
			return pipelineExpr{sql: "jsonb_build_object(" + strings.Join(pieces, ",") + ")", args: args, schema: schema}, nil
		}
	}
	return c.expression(v, "p.doc", p.schema, 0)
}
func (c *pipelineCompiler) group(p pipelinePlan, m map[string]any) (pipelinePlan, error) {
	key, ok := m["_id"]
	if !ok || len(m) > 33 {
		return p, pipelineError("$group requires _id and at most 32 accumulators")
	}
	k, err := c.groupKey(key, p)
	if err != nil {
		return p, err
	}
	inputs := []string{"COALESCE(" + k.sql + ",'null'::jsonb) AS group_key"}
	inputArgs := append([]any{}, k.args...)
	outputs := []string{"'_id',g.group_key"}
	schema := &unsmeta.Schema{Type: "object", AdditionalProperties: pipelineClosedObject(), Properties: map[string]unsmeta.Schema{"_id": *k.schema}}
	index := 0
	for _, name := range pipelineKeys(m) {
		if name == "_id" {
			continue
		}
		if err = pipelineTopKey(name); err != nil {
			return p, err
		}
		acc, ok := m[name].(map[string]any)
		decodedName, _ := pipelinePath(name)
		name = decodedName[0]
		if !ok || len(acc) != 1 {
			return p, pipelineError("group output must contain one accumulator")
		}
		for op, value := range acc {
			if op == "$count" {
				empty, ok := value.(map[string]any)
				if !ok || len(empty) != 0 {
					return p, pipelineError("$count accumulator accepts {}")
				}
				outputs = append(outputs, pipelineLiteral(name)+",count(*)")
				schema.Properties[name] = unsmeta.Schema{Type: "integer"}
				continue
			}
			fn, ok := map[string]string{"$sum": "sum", "$avg": "avg", "$min": "min", "$max": "max"}[op]
			if !ok {
				return p, pipelineError("unsupported accumulator: " + op)
			}
			e, err := c.expression(value, "p.doc", p.schema, 0)
			if err != nil {
				return p, err
			}
			if (op == "$sum" || op == "$avg") && !relationalNumeric(e.schema.Type) {
				return p, pipelineError("sum/avg require numeric input")
			}
			if e.schema.Type == "object" || e.schema.Type == "array" || e.schema.Type == "boolean" || e.schema.Type == "null" {
				return p, pipelineError("accumulator requires number or string")
			}
			column := fmt.Sprintf("value%d", index)
			index++
			inputs = append(inputs, pipelineScalar(e)+" AS "+column)
			inputArgs = append(inputArgs, e.args...)
			out := fn + "(g." + column + ")"
			if op == "$sum" {
				out = "COALESCE(" + out + ",0)"
			}
			outputs = append(outputs, pipelineLiteral(name)+","+out)
			kind := e.schema.Type
			if relationalNumeric(kind) {
				kind = "number"
			}
			schema.Properties[name] = unsmeta.Schema{Type: kind}
		}
	}
	p.sql = "SELECT jsonb_build_object(" + strings.Join(outputs, ",") + ") AS doc,row_number() OVER (ORDER BY g.group_key::text) AS ord FROM (SELECT " + strings.Join(inputs, ",") + " FROM (" + p.sql + ") p) g GROUP BY g.group_key"
	p.args = append(inputArgs, p.args...)
	p.schema = schema
	return p, nil
}

type pipelineLookup struct {
	CollectionID string                       `json:"collectionId"`
	RecordID     string                       `json:"recordId,omitempty"`
	Kind         string                       `json:"kind,omitempty"`
	LocalField   string                       `json:"localField"`
	ForeignField string                       `json:"foreignField"`
	As           string                       `json:"as"`
	Pipeline     []map[string]json.RawMessage `json:"pipeline,omitempty"`
}

func (c *pipelineCompiler) lookup(p pipelinePlan, x pipelineLookup, depth int) (pipelinePlan, error) {
	return c.compileLookup(p, x, depth)
}
func (c *pipelineCompiler) unwind(p pipelinePlan, v any) (pipelinePlan, error) {
	path := ""
	preserve := false
	if s, ok := v.(string); ok {
		path = s
	} else {
		m, ok := v.(map[string]any)
		if !ok || len(m) > 2 {
			return p, pipelineError("unwind requires field path or {path,preserveNullAndEmptyArrays}")
		}
		for k := range m {
			if k != "path" && k != "preserveNullAndEmptyArrays" {
				return p, pipelineError("unknown unwind option")
			}
		}
		path, _ = m["path"].(string)
		if x, exists := m["preserveNullAndEmptyArrays"]; exists {
			var ok bool
			preserve, ok = x.(bool)
			if !ok {
				return p, pipelineError("preserveNullAndEmptyArrays must be boolean")
			}
		}
	}
	if !strings.HasPrefix(path, "$") {
		return p, pipelineError("unwind path must start with $")
	}
	path = strings.TrimPrefix(path, "$")
	f, err := c.field("p.doc", path, p.schema)
	if err != nil {
		return p, err
	}
	if f.schema.Type != "array" || f.schema.Items == nil {
		return p, pipelineError("unwind requires a declared array")
	}
	parts, _ := pipelinePath(path)
	for i := 1; i < len(parts); i++ {
		parent, ok := unsmeta.SchemaField(p.schema, parts[:i])
		if !ok || parent.Type != "object" {
			return p, pipelineError("unwind path must traverse objects; unwind outer arrays first")
		}
	}
	quoted := []string{}
	for _, part := range parts {
		quoted = append(quoted, pipelineLiteral(part))
	}
	join := "CROSS JOIN LATERAL "
	tail := ""
	if preserve {
		join = "LEFT JOIN LATERAL "
		tail = " ON TRUE"
	}
	arr := "CASE WHEN jsonb_typeof(" + f.sql + ")='array' THEN " + f.sql + " ELSE '[]'::jsonb END"
	p.sql = "SELECT jsonb_set(p.doc,ARRAY[" + strings.Join(quoted, ",") + "],COALESCE(u.value,'null'::jsonb),true) AS doc,row_number() OVER (ORDER BY p.ord,u.seq) AS ord FROM (" + p.sql + ") p " + join + "jsonb_array_elements(" + arr + ") WITH ORDINALITY AS u(value,seq)" + tail
	p.schema = pipelineClone(p.schema)
	var replace func(*unsmeta.Schema, []string)
	replace = func(s *unsmeta.Schema, ks []string) {
		x := s.Properties[ks[0]]
		if len(ks) == 1 {
			x = *pipelineClone(f.schema.Items)
		} else {
			replace(&x, ks[1:])
		}
		s.Properties[ks[0]] = x
	}
	replace(p.schema, parts)
	return p, nil
}

// Simple reads are compiled directly against physical columns, allowing an
// index scan to stop at the requested page instead of numbering the whole set.
// Filtering and stable sorting commute; the last sort has highest priority.
func (c *pipelineCompiler) simple(p pipelinePlan, kind string, stages []map[string]json.RawMessage) (pipelinePlan, bool, error) {
	for _, stage := range stages {
		if len(stage) != 1 {
			return p, false, nil
		}
		for op := range stage {
			if op != "$match" && op != "$sort" {
				return p, false, nil
			}
		}
	}
	table := "public.uns_record"
	order := []RecordOrder{{Field: "_id", Direction: "asc"}}
	if kind == "samples" {
		table = "public.uns_sample"
		order = []RecordOrder{{Field: "_meta.time", Direction: "asc"}, {Field: "_meta.recordId", Direction: "asc"}, {Field: "_id", Direction: "asc"}}
	}
	where := []string{p.baseWhere}
	args := append([]any{}, p.args...)
	for _, stage := range stages {
		c.stages++
		if c.stages > 32 {
			return p, true, pipelineError("at most 32 stages")
		}
		for op, raw := range stage {
			if op == "$match" {
				v, err := pipelineDecode(raw)
				if err != nil {
					return p, true, err
				}
				m, ok := v.(map[string]any)
				if !ok {
					return p, true, pipelineError("$match requires object")
				}
				e, err := c.predicate(m, "base:b", p.schema, 0)
				if err != nil {
					return p, true, err
				}
				where = append(where, e.sql)
				args = append(args, e.args...)
			} else {
				var sort []RecordOrder
				if err := Decode(raw, &sort); err != nil {
					return p, true, err
				}
				if len(sort) == 0 {
					return p, true, pipelineError("$sort requires at least one field")
				}
				_, err := c.sortTerms("base:b", p.schema, sort)
				if err != nil {
					return p, true, err
				}
				order = append(sort, order...)
			}
		}
	}
	seen := map[string]bool{}
	terms := []string{}
	for _, o := range order {
		if seen[o.Field] {
			continue
		}
		seen[o.Field] = true
		e, err := c.field("base:b", o.Field, p.schema)
		if err != nil {
			return p, true, err
		}
		term := pipelineSeekTerm{sql: pipelineScalar(e), direction: o.Direction, cast: "text", nullable: e.native == "" || strings.HasSuffix(e.native, ".source_id")}
		if e.temporal {
			term.cast = "timestamptz"
		} else if e.schema.Type == "number" || e.schema.Type == "integer" {
			term.cast = "numeric"
		} else if e.schema.Type == "boolean" {
			term.cast = "boolean"
		}
		p.seek = append(p.seek, term)
		nulls := ""
		if term.nullable {
			nulls = " NULLS LAST"
		}
		terms = append(terms, term.sql+" "+strings.ToUpper(term.direction)+nulls)
	}
	if len(p.seek) > 64 {
		return p, true, pipelineError("combined sorts support at most 64 distinct fields")
	}
	p.from = " FROM " + table + " b WHERE " + strings.Join(where, " AND ")
	p.order = " ORDER BY " + strings.Join(terms, ",")
	p.sql = "SELECT " + DataDocumentSQL(kind, "b") + " AS doc" + p.from + p.order
	p.args = args
	p.direct = true
	return p, true, nil
}

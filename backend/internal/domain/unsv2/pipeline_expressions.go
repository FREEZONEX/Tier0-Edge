package unsv2

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"backend/internal/domain/unsmeta"
)

const maxExpressionStringBytes = 1 << 20

func expressionRuntimeError(message string) error {
	if strings.Contains(message, "expression string budget exceeded:") {
		return &Fault{Code: "EXPRESSION_BUDGET_EXCEEDED", Message: "String expression exceeds its 1 MiB intermediate value budget", Repair: "Reduce concatenated inputs or the number of string transformations"}
	}
	return nil
}

// Bind every child exactly once, in SQL text order. Aliases are local to this
// scalar subquery, including when a child contains another expression.
func expressionSQL(children []pipelineExpr, body, kind string) pipelineExpr {
	columns := make([]string, len(children))
	args := []any{}
	for i, child := range children {
		columns[i] = "NULLIF(" + child.sql + ",'null'::jsonb) AS v" + fmt.Sprint(i)
		args = append(args, child.args...)
	}
	return pipelineExpr{sql: "(SELECT " + body + " FROM (SELECT " + strings.Join(columns, ",") + " OFFSET 0) AS ex)", args: args, schema: &unsmeta.Schema{Type: kind}}
}

func (c *pipelineCompiler) extendedExpression(op string, value any, doc string, schema *unsmeta.Schema, depth int) (pipelineExpr, bool, error) {
	fail := func(message string) (pipelineExpr, bool, error) { return pipelineExpr{}, true, pipelineError(message) }
	compile := func(values []any) ([]pipelineExpr, error) {
		out := make([]pipelineExpr, len(values))
		for i, v := range values {
			e, err := c.expression(v, doc, schema, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = e
		}
		return out, nil
	}
	result := func(es []pipelineExpr, sql, kind string) (pipelineExpr, bool, error) {
		return expressionSQL(es, sql, kind), true, nil
	}
	if symbol, ok := map[string]string{"$eq": "=", "$ne": "<>", "$gt": ">", "$gte": ">=", "$lt": "<", "$lte": "<="}[op]; ok {
		a, ok := value.([]any)
		if !ok || len(a) != 2 {
			return fail(op + " requires two expressions")
		}
		es, err := compile(a)
		if err != nil {
			return pipelineExpr{}, true, err
		}
		x, y := es[0].schema.Type, es[1].schema.Type
		if x == "object" || x == "array" || y == "object" || y == "array" || (x != y && x != "null" && y != "null" && !(relationalNumeric(x) && relationalNumeric(y))) {
			return fail("comparison requires compatible scalar expressions")
		}
		if (x == "boolean" || y == "boolean") && op != "$eq" && op != "$ne" {
			return fail("boolean expressions support only equality")
		}
		return result(es, "to_jsonb(v0 "+symbol+" v1)", "boolean")
	}
	if op == "$and" || op == "$or" || op == "$not" {
		a, ok := value.([]any)
		if !ok || len(a) < 1 || len(a) > 64 || op == "$not" && len(a) != 1 {
			return fail(op + " requires an array of boolean expressions ($not: exactly one)")
		}
		es, err := compile(a)
		if err != nil {
			return pipelineExpr{}, true, err
		}
		terms, nulls := []string{}, []string{}
		for i, e := range es {
			if e.schema.Type != "boolean" && e.schema.Type != "null" {
				return fail("logical expressions require booleans")
			}
			v := fmt.Sprintf("v%d", i)
			terms = append(terms, "("+v+" #>> '{}')::boolean")
			nulls = append(nulls, v+" IS NULL")
		}
		join := " AND "
		if op == "$or" {
			join = " OR "
		}
		body := strings.Join(terms, join)
		if op == "$not" {
			body = "NOT (" + body + ")"
		}
		return result(es, "CASE WHEN "+strings.Join(nulls, " OR ")+" THEN NULL ELSE to_jsonb("+body+") END", "boolean")
	}
	if op == "$cond" {
		a, ok := value.([]any)
		if m, isObject := value.(map[string]any); isObject {
			if len(m) != 3 {
				return fail("$cond requires if, then and else")
			}
			x, xok := m["if"]
			y, yok := m["then"]
			z, zok := m["else"]
			if !xok || !yok || !zok {
				return fail("$cond requires if, then and else")
			}
			a = []any{x, y, z}
			ok = true
		}
		if !ok || len(a) != 3 {
			return fail("$cond requires three expressions")
		}
		es, err := compile(a)
		if err != nil {
			return pipelineExpr{}, true, err
		}
		if es[0].schema.Type != "boolean" && es[0].schema.Type != "null" {
			return fail("$cond condition must be boolean")
		}
		x, y := es[1].schema.Type, es[2].schema.Type
		kind := x
		if x != y && x != "null" && y != "null" && !(relationalNumeric(x) && relationalNumeric(y)) {
			return fail("$cond branches require matching types")
		}
		if x == "null" {
			kind = y
		}
		if relationalNumeric(x) && relationalNumeric(y) && x != y {
			kind = "number"
		}
		outputSchema := pipelineClone(es[1].schema)
		if x == "null" {
			outputSchema = pipelineClone(es[2].schema)
		}
		if (kind == "object" || kind == "array") && x != "null" && y != "null" && !reflect.DeepEqual(es[1].schema, es[2].schema) {
			return fail("$cond object/array branches must have identical declared schemas")
		}
		outputSchema.Type = kind
		// Keep branches inside CASE, not its input relation: unselected branches
		// must not execute invalid conversions or expensive nested expressions.
		args := append([]any{}, es[1].args...)
		args = append(args, es[2].args...)
		args = append(args, es[0].args...)
		return pipelineExpr{sql: "(SELECT CASE WHEN cond IS NULL THEN NULL WHEN (cond #>> '{}')::boolean THEN " + es[1].sql + " ELSE " + es[2].sql + " END FROM (SELECT NULLIF(" + es[0].sql + ",'null'::jsonb) AS cond OFFSET 0) AS condition)", args: args, schema: outputSchema}, true, nil
	}
	if op == "$concat" || op == "$toLower" || op == "$toUpper" || op == "$trim" || op == "$strLenCP" {
		a := []any{value}
		if op == "$concat" {
			var ok bool
			a, ok = value.([]any)
			if !ok || len(a) < 1 || len(a) > 64 {
				return fail("$concat requires 1 to 64 expressions")
			}
		}
		if op == "$trim" {
			m, ok := value.(map[string]any)
			if !ok || len(m) < 1 || len(m) > 2 {
				return fail("$trim requires input and optional chars")
			}
			input, ok := m["input"]
			if !ok {
				return fail("$trim requires input")
			}
			a = []any{input}
			if chars, ok := m["chars"]; ok {
				a = append(a, chars)
			} else if len(m) != 1 {
				return fail("unknown $trim option")
			} else {
				a = append(a, " \t\n\r\v\f\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000")
			}
		}
		es, err := compile(a)
		if err != nil {
			return pipelineExpr{}, true, err
		}
		terms := []string{}
		for i, e := range es {
			if e.schema.Type != "string" && e.schema.Type != "null" {
				return fail("string operator requires string expressions")
			}
			terms = append(terms, fmt.Sprintf("(v%d #>> '{}')", i))
		}
		body, kind := terms[0], "string"
		switch op {
		case "$concat":
			body = strings.Join(terms, " || ")
			lengths := make([]string, len(terms))
			nulls := make([]string, len(terms))
			for i, term := range terms {
				lengths[i] = "octet_length(" + term + ")::bigint"
				nulls[i] = fmt.Sprintf("v%d IS NULL", i)
			}
			bytes := "(" + strings.Join(lengths, "+") + ")"
			// Check operands before concatenating, including intermediate values
			// produced by earlier $set stages. The error depends on the row so
			// PostgreSQL cannot pre-evaluate it for otherwise valid queries.
			body = "CASE WHEN " + strings.Join(nulls, " OR ") + " THEN NULL WHEN " + bytes + ">" + fmt.Sprint(maxExpressionStringBytes) + " THEN (('expression string budget exceeded: ' || " + bytes + "::text)::bigint)::text ELSE " + body + " END"
		case "$toLower":
			body = "lower(" + body + ")"
		case "$toUpper":
			body = "upper(" + body + ")"
		case "$strLenCP":
			body = "char_length(" + body + ")"
			kind = "integer"
		case "$trim":
			body = "btrim(" + body + "," + terms[1] + ")"
		}
		return result(es, "to_jsonb("+body+")", kind)
	}
	if op == "$convert" || op == "$toString" || op == "$toDecimal" || op == "$toLong" || op == "$toBool" {
		target := map[string]string{"$toString": "string", "$toDecimal": "decimal", "$toLong": "long", "$toBool": "bool"}[op]
		input := value
		if op == "$convert" {
			m, ok := value.(map[string]any)
			if !ok || len(m) != 2 {
				return fail("$convert requires only input and to")
			}
			var present bool
			input, present = m["input"]
			if !present {
				return fail("$convert requires input")
			}
			target, ok = m["to"].(string)
			if !ok {
				return fail("$convert to must be a literal type name")
			}
		}
		if target != "string" && target != "decimal" && target != "long" && target != "bool" {
			return fail("unsupported conversion target")
		}
		es, err := compile([]any{input})
		if err != nil {
			return pipelineExpr{}, true, err
		}
		source := es[0].schema.Type
		if source != "string" && source != "number" && source != "integer" && source != "boolean" && source != "null" {
			return fail("conversion requires scalar input")
		}
		text := "(v0 #>> '{}')"
		body := text
		kind := target
		switch target {
		case "decimal", "long":
			kind = "number"
			body = "(" + text + ")::numeric"
			if source == "boolean" {
				body = "CASE WHEN " + text + "='true' THEN 1::numeric WHEN " + text + "='false' THEN 0::numeric ELSE NULL::numeric END"
			}
			if source == "string" {
				body = "CASE WHEN " + text + " ~ '^[+-]{0,1}([0-9]+([.][0-9]*){0,1}|[.][0-9]+)([eE][+-]{0,1}[0-9]+){0,1}$' THEN (" + text + ")::numeric ELSE ('invalid numeric: ' || " + text + ")::numeric END"
			}
			if target == "long" {
				body = "trunc(" + body + ")::bigint"
				kind = "integer"
			}
		case "bool":
			kind = "boolean"
			body = "(" + text + ")::boolean"
			if relationalNumeric(source) {
				body = "(" + text + ")::numeric<>0"
			}
			if source == "string" {
				body = "CASE WHEN " + text + "='true' THEN TRUE WHEN " + text + "='false' THEN FALSE ELSE ('invalid boolean: ' || " + text + ")::boolean END"
			}
		}
		return result(es, "CASE WHEN v0 IS NULL THEN NULL ELSE to_jsonb("+body+") END", kind)
	}
	if op == "$dateTrunc" {
		m, ok := value.(map[string]any)
		if !ok {
			return fail("$dateTrunc requires an object")
		}
		for k := range m {
			if k != "date" && k != "unit" && k != "timezone" && k != "startOfWeek" && k != "binSize" {
				return fail("unknown $dateTrunc option")
			}
		}
		date, present := m["date"]
		if !present {
			return fail("$dateTrunc requires date")
		}
		unit, ok := m["unit"].(string)
		if !ok {
			return fail("$dateTrunc unit must be a literal string")
		}
		valid := map[string]bool{"millisecond": true, "second": true, "minute": true, "hour": true, "day": true, "week": true, "month": true, "quarter": true, "year": true}
		if !valid[unit] {
			return fail("unsupported $dateTrunc unit")
		}
		binSize := int64(1)
		if bin, ok := m["binSize"]; ok {
			n, err := pipelineInteger(bin)
			if err != nil || n < 1 || n > 1000000 {
				return fail("$dateTrunc binSize must be an integer from 1 to 1000000")
			}
			binSize = int64(n)
		}
		zone := "UTC"
		if z, ok := m["timezone"]; ok {
			zone, ok = z.(string)
			if !ok {
				return fail("timezone must be a literal IANA zone")
			}
		}
		if _, err := time.LoadLocation(zone); err != nil || zone == "Local" || zone == "" {
			return fail("invalid IANA timezone")
		}
		if binSize > 1 && (zone != "UTC" || unit == "week" || unit == "month" || unit == "quarter" || unit == "year") {
			return fail("$dateTrunc binSize greater than one requires UTC and a fixed unit (millisecond through day)")
		}
		weekday := 0
		if w, ok := m["startOfWeek"]; ok {
			s, ok := w.(string)
			if !ok || unit != "week" {
				return fail("startOfWeek requires week and a literal weekday")
			}
			days := map[string]int{"sunday": 0, "monday": 1, "tuesday": 2, "wednesday": 3, "thursday": 4, "friday": 5, "saturday": 6}
			weekday, ok = days[strings.ToLower(s)]
			if !ok {
				return fail("invalid startOfWeek")
			}
		}
		es, err := compile([]any{date})
		if err != nil {
			return pipelineExpr{}, true, err
		}
		if !relationalNumeric(es[0].schema.Type) && es[0].schema.Type != "null" {
			return fail("$dateTrunc date must be Unix milliseconds")
		}
		// Keep the original instant for truncation: a local timestamp loses
		// which occurrence of an overlapping DST hour the input belongs to.
		for _, v := range []any{unit, zone} {
			e, _ := pipelineConstant(v)
			es = append(es, e)
		}
		instant := "to_timestamp((v0 #>> '{}')::numeric/1000)"
		local := "(" + instant + " AT TIME ZONE (v2 #>> '{}'))"
		body := "date_trunc((v1 #>> '{}')," + instant + ",(v2 #>> '{}'))"
		if binSize > 1 {
			widths := map[string]int64{"millisecond": 1, "second": 1000, "minute": 60000, "hour": 3600000, "day": 86400000}
			width := widths[unit] * binSize
			// 多桶固定时长使用 UTC Unix 纪元对齐，避免本地时间和夏令时歧义。
			return result(es, fmt.Sprintf("to_jsonb((floor((v0 #>> '{}')::numeric/%d)*%d)::bigint)", width, width), "integer")
		}
		if unit == "week" {
			shift := (8 - weekday) % 7
			interval := fmt.Sprintf("interval '%d days'", shift)
			body = "((date_trunc('week'," + local + "+" + interval + ")-" + interval + ") AT TIME ZONE (v2 #>> '{}'))"
		}
		if unit == "day" || unit == "week" || unit == "month" || unit == "quarter" || unit == "year" {
			// Calendar boundaries choose the first occurrence of an overlapping
			// midnight. PostgreSQL otherwise prefers standard time (e.g. Havana)
			// and can return a boundary later than an input in the first hour.
			// Check offsets on both sides and keep only exact wall-time matches;
			// nonexistent boundaries retain PostgreSQL's forward gap resolution.
			candidates := []string{"(boundary)"}
			for _, delta := range []string{"- interval '2 days'", "+ interval '2 days'"} {
				probe := "(boundary " + delta + ")"
				offset := "(" + probe + " AT TIME ZONE (v2 #>> '{}'))-(" + probe + " AT TIME ZONE 'UTC')"
				candidates = append(candidates, "(((boundary AT TIME ZONE (v2 #>> '{}'))-("+offset+")) AT TIME ZONE 'UTC')")
			}
			body = "(SELECT min(candidate) FROM (SELECT " + body + " AS boundary OFFSET 0) AS calendar_boundary CROSS JOIN LATERAL (VALUES " + strings.Join(candidates, ",") + ") AS offsets(candidate) WHERE candidate AT TIME ZONE (v2 #>> '{}') = boundary AT TIME ZONE (v2 #>> '{}'))"
		}
		return result(es, "to_jsonb(floor(extract(epoch from ("+body+"))*1000)::bigint)", "integer")
	}
	return pipelineExpr{}, false, nil
}

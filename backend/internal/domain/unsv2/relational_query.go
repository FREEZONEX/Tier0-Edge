package unsv2

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
)

// User aliases never become SQL identifiers. All SQL aliases are assigned here.
var relationalAlias = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,62}$`)

type relationalSource struct {
	sql     string
	schema  *unsmeta.Schema
	outputs map[string]relationalField
}
type relationalField struct {
	sql  string
	args []any
	kind string
	name string
}
type relationalCompiler struct {
	sources         map[string]relationalSource
	fields          map[string]relationalField
	ordered         []relationalField
	nodes           int
	expressionNodes int
}

func relationalInvalid(message string) error { return invalid("relational", message) }
func relationalNumeric(kind string) bool     { return kind == "integer" || kind == "number" }

func (c *relationalCompiler) field(path string) (relationalField, error) {
	if f, ok := c.fields[path]; ok {
		return f, nil
	}
	parts := strings.Split(path, ".")
	if len(parts) < 2 || len(parts) > 10 {
		return relationalField{}, relationalInvalid("use alias.id/revision/createdAt/updatedAt/data.field")
	}
	source, ok := c.sources[parts[0]]
	if !ok {
		return relationalField{}, relationalInvalid("unknown source alias")
	}
	f := relationalField{kind: "integer", name: fmt.Sprintf("f%d", len(c.ordered))}
	columns := map[string]string{"id": "id", "revision": "revision", "createdAt": "created_at", "updatedAt": "updated_at"}
	if source.outputs != nil {
		output, exists := source.outputs[parts[1]]
		if len(parts) != 2 || !exists {
			return f, relationalInvalid("derived fields use alias.selectedOutputName")
		}
		f.sql, f.kind = source.sql+"."+output.sql, output.kind
	} else if column, ok := columns[parts[1]]; ok && len(parts) == 2 {
		f.sql = source.sql + "." + column
		if parts[1] == "id" {
			f.kind = "string"
		}
	} else {
		if len(parts) < 3 || parts[1] != "data" || source.schema == nil {
			return f, relationalInvalid("expected a declared scalar data field")
		}
		current := *source.schema
		for _, part := range parts[2:] {
			child, exists := current.Properties[part]
			if current.Type != "object" || !exists || !unsmeta.ValidID(part) || strings.Contains(part, ".") {
				return f, relationalInvalid("field is not declared in recordSchema")
			}
			current = child
			f.args = append(f.args, part)
		}
		f.kind = current.Type
		f.sql = "jsonb_extract_path_text(" + source.sql + ".data," + strings.TrimSuffix(strings.Repeat("?,", len(f.args)), ",") + ")"
		if len(f.args) == 1 {
			// A declared top-level object key needs no variadic path array per
			// row. Keep it bound and explicitly text, including numeric-looking keys.
			f.sql = "(" + source.sql + ".data ->> ?::text)"
		}
		switch f.kind {
		case "string":
		case "integer", "number":
			f.sql = "(" + f.sql + ")::numeric"
		case "boolean":
			f.sql = "(" + f.sql + ")::boolean"
		default:
			return f, relationalInvalid("only declared scalar fields are supported")
		}
	}
	c.fields[path] = f
	c.ordered = append(c.ordered, f)
	return f, nil
}

func relationalValue(kind string, value any) bool {
	if n, ok := value.(json.Number); ok {
		if !relationalNumeric(kind) {
			return false
		}
		literal := string(n)
		// Match persisted UNS numeric bounds without expanding exponents into
		// big integers. The JSON decoder has already validated numeric syntax.
		if len(literal) == 0 || len(literal) > 256*1024 {
			return false
		}
		exponent := 0
		if i := strings.IndexAny(literal, "eE"); i >= 0 {
			var err error
			exponent, err = strconv.Atoi(literal[i+1:])
			if err != nil || exponent < -16383 || exponent > 131071 {
				return false
			}
			literal = literal[:i]
		}
		if kind == "number" {
			return true
		}
		fraction := 0
		if dot := strings.IndexByte(literal, '.'); dot >= 0 {
			fraction = len(literal) - dot - 1
			literal = literal[:dot] + literal[dot+1:]
		}
		literal = strings.TrimPrefix(literal, "-")
		if strings.Trim(literal, "0") == "" {
			return true
		}
		return fraction-exponent <= len(literal)-len(strings.TrimRight(literal, "0"))
	}
	if kind == "string" {
		_, ok := value.(string)
		return ok
	}
	if kind == "boolean" {
		_, ok := value.(bool)
		return ok
	}
	return false
}

func relationalFilter(f relationalField, op string, raw json.RawMessage) (string, []any, error) {
	var value any
	if len(raw) == 0 || decodeNumberJSON(raw, &value) != nil {
		return "", nil, relationalInvalid("predicate value is required")
	}
	args := append([]any{}, f.args...)
	placeholder := "?"
	if relationalNumeric(f.kind) {
		// PostgreSQL bigint parameters do not accept equivalent JSON numeric
		// spellings such as 1.0 or 1e0. Numeric preserves these values exactly.
		placeholder = "?::numeric"
	}
	if op == "exists" {
		v, ok := value.(bool)
		if !ok {
			return "", nil, relationalInvalid("exists requires boolean")
		}
		if v {
			return f.sql + " IS NOT NULL", args, nil
		}
		return f.sql + " IS NULL", args, nil
	}
	if value == nil {
		if op == "eq" {
			return f.sql + " IS NULL", args, nil
		}
		if op == "ne" {
			return f.sql + " IS NOT NULL", args, nil
		}
		return "", nil, relationalInvalid("null supports eq/ne only")
	}
	if op == "in" {
		values, ok := value.([]any)
		if !ok || len(values) < 1 || len(values) > 100 {
			return "", nil, relationalInvalid("in requires 1 to 100 scalar values")
		}
		for _, v := range values {
			if !relationalValue(f.kind, v) {
				return "", nil, relationalInvalid("predicate value does not match schema")
			}
			args = append(args, v)
		}
		return f.sql + " IN (" + strings.TrimSuffix(strings.Repeat(placeholder+",", len(values)), ",") + ")", args, nil
	}
	if !relationalValue(f.kind, value) {
		return "", nil, relationalInvalid("predicate value does not match schema")
	}
	if op == "contains" {
		if f.kind != "string" {
			return "", nil, relationalInvalid("contains requires string")
		}
		return "strpos(" + f.sql + ",?)>0", append(args, value), nil
	}
	ops := map[string]string{"eq": "=", "ne": "<>", "gt": ">", "gte": ">=", "lt": "<", "lte": "<="}
	sqlOp, ok := ops[op]
	if !ok || f.kind == "boolean" && op != "eq" && op != "ne" {
		return "", nil, relationalInvalid("unsupported predicate operator")
	}
	return f.sql + " " + sqlOp + " " + placeholder, append(args, value), nil
}

func (c *relationalCompiler) predicate(p QueryPredicate, depth int) (string, []any, error) {
	c.nodes++
	if depth > 6 || c.nodes > 64 {
		return "", nil, relationalInvalid("where permits at most 64 nodes and depth 6")
	}
	forms := 0
	if p.Field != "" || p.Op != "" || len(p.Value) > 0 {
		forms++
	}
	if p.And != nil {
		forms++
	}
	if p.Or != nil {
		forms++
	}
	if p.Not != nil {
		forms++
	}
	if forms != 1 {
		return "", nil, relationalInvalid("predicate must be exactly one leaf, and, or, or not")
	}
	if p.Not != nil {
		sql, args, err := c.predicate(*p.Not, depth+1)
		return "NOT (" + sql + ")", args, err
	}
	if p.And != nil || p.Or != nil {
		children, op := p.And, " AND "
		if p.Or != nil {
			children, op = p.Or, " OR "
		}
		if len(children) == 0 || len(children) > 64 {
			return "", nil, relationalInvalid("boolean group requires 1 to 64 children")
		}
		parts, args := []string{}, []any{}
		for _, child := range children {
			sql, vars, err := c.predicate(child, depth+1)
			if err != nil {
				return "", nil, err
			}
			parts = append(parts, "("+sql+")")
			args = append(args, vars...)
		}
		return strings.Join(parts, op), args, nil
	}
	f, err := c.field(p.Field)
	if err != nil {
		return "", nil, err
	}
	return relationalFilter(f, p.Op, p.Value)
}

// relationalQuery runs only inside Query's read-only repeatable-read transaction.
func relationalQuery(ctx context.Context, q Query) (map[string]any, error) {
	return compileRelationalQuery(ctx, q, false)
}

func compileRelationalQuery(ctx context.Context, q Query, validateOnly bool) (map[string]any, error) {
	return compileRelationalQueryInternal(ctx, q, validateOnly, nil)
}

func compileRelationalQueryInternal(ctx context.Context, q Query, validateOnly bool, capture *relationalPlan) (map[string]any, error) {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ctx = repo.WithUnsTransaction(bounded, repo.GetUnsConn(ctx).WithContext(bounded))
	r := q.Relational
	if r == nil {
		return nil, relationalInvalid("relational plan is required")
	}
	if len(q.Filters) > 0 || len(q.OrderBy) > 0 || q.Search != "" || len(q.RecordIDs) > 0 || q.IncludeLatestSample {
		return nil, relationalInvalid("relational cannot be mixed with legacy filters/orderBy/search/recordIds/includeLatestSample")
	}
	if len(r.Joins) > 4 || len(r.Select) < 1 || len(r.Select) > 32 || len(r.GroupBy) > 8 || len(r.Having) > 16 || len(r.OrderBy) > 8 {
		return nil, relationalInvalid("limits: 4 joins, 1-32 selects, 8 groups, 16 having filters, 8 sort fields")
	}
	page, size, err := bounds(q.Pagination, 200)
	if err != nil {
		return nil, err
	}
	if page > 1000000/size+1 || (page-1)*size > 1000000 {
		return nil, relationalInvalid("offset must not exceed 1000000")
	}
	basePage := boundedComposedPage(r)
	c, from, fromArgs, ids, aliases, queryWarnings, err := prepareRelationalSources(ctx, q, basePage)
	if err != nil {
		return nil, err
	}
	where := "r0.collection_id = ? AND r0.deleted_at IS NULL"
	whereArgs := []any{q.CollectionID}
	if r.Where != nil {
		sql, args, err := c.predicate(*r.Where, 1)
		if err != nil {
			return nil, err
		}
		where += " AND (" + sql + ")"
		whereArgs = append(whereArgs, args...)
	}
	if capture != nil && capture.pageGroup != "" {
		group, err := c.field(capture.pageGroup)
		if err != nil {
			return nil, err
		}
		where += " AND " + group.sql + " IN (SELECT id FROM page_base)"
		whereArgs = append(whereArgs, group.args...)
	}
	groups := []string{}
	groupFields := map[string]bool{}
	for _, path := range r.GroupBy {
		if groupFields[path] {
			return nil, relationalInvalid("duplicate group field")
		}
		f, err := c.field(path)
		if err != nil {
			return nil, err
		}
		groupFields[path] = true
		groups = append(groups, f.name)
	}
	aggregated := len(groups) > 0
	for _, s := range r.Select {
		if s.Aggregate != "" {
			aggregated = true
		}
	}
	selected := map[string]relationalField{}
	projections := []string{}
	outputNames := []string{}
	for i, s := range r.Select {
		if !relationalAlias.MatchString(s.As) {
			return nil, relationalInvalid("select.as must be an ASCII identifier of 1-63 characters")
		}
		if _, ok := selected[s.As]; ok {
			return nil, relationalInvalid("select aliases must be unique")
		}
		f := relationalField{kind: "number"}
		expression := "*"
		if s.Field != "" {
			f, err = c.field(s.Field)
			if err != nil {
				return nil, err
			}
			expression = f.name
		}
		if s.Expression != nil {
			if s.Field != "" || s.Aggregate != "" || s.Distinct || aggregated {
				return nil, relationalInvalid("expression selects cannot also have field/aggregate/distinct or belong to a grouped query")
			}
			f, err = c.expression(*s.Expression, 1)
			if err != nil {
				return nil, err
			}
			f.name = fmt.Sprintf("f%d", len(c.ordered))
			c.ordered = append(c.ordered, f)
			expression = f.name
		}
		if s.Aggregate == "" {
			if s.Field == "" && s.Expression == nil || s.Distinct || aggregated && !groupFields[s.Field] {
				return nil, relationalInvalid("nonaggregate selects require fields in groupBy; select.distinct requires an aggregate")
			}
		} else {
			switch s.Aggregate {
			case "count":
				f.kind = "integer"
			case "sum", "avg":
				if !relationalNumeric(f.kind) || s.Field == "" {
					return nil, relationalInvalid("sum/avg require numeric fields")
				}
				f.kind = "number"
			case "min", "max":
				if s.Field == "" || f.kind == "boolean" {
					return nil, relationalInvalid("min/max require numeric or string fields")
				}
			default:
				return nil, relationalInvalid("aggregate must be count/sum/avg/min/max")
			}
			if s.Distinct {
				if s.Field == "" {
					return nil, relationalInvalid("distinct aggregate requires a field")
				}
				expression = "DISTINCT " + expression
			}
			expression = strings.ToUpper(s.Aggregate) + "(" + expression + ")"
		}
		name := fmt.Sprintf("s%d", i)
		projections = append(projections, expression+" AS "+name)
		selected[s.As] = relationalField{sql: name, kind: f.kind}
		outputNames = append(outputNames, name)
	}
	if len(r.Having) > 0 && !aggregated && len(r.Subqueries) == 0 && c.expressionNodes == 0 {
		return nil, relationalInvalid("having requires grouping or aggregation")
	}
	// Stable ties use all result columns plus group keys or source IDs. DISTINCT
	// uses only projected columns so hidden identity never changes its semantics.
	ties := append([]string{}, outputNames...)
	if !r.Distinct {
		keys := groups
		if !aggregated {
			keys = []string{}
			for _, alias := range aliases {
				f, err := c.field(alias + ".id")
				if err != nil {
					return nil, err
				}
				keys = append(keys, f.name)
			}
		}
		for i, key := range keys {
			name := fmt.Sprintf("k%d", i)
			projections = append(projections, key+" AS "+name)
			ties = append(ties, name)
		}
	}
	inputs, args := []string{}, []any{}
	for _, f := range c.ordered {
		inputs = append(inputs, f.sql+" AS "+f.name)
		args = append(args, f.args...)
	}
	if len(inputs) == 0 {
		inputs = append(inputs, "1 AS dummy")
	}
	args = append(args, fromArgs...)
	args = append(args, whereArgs...)
	distinct := ""
	if r.Distinct {
		distinct = "DISTINCT "
	}
	prefix := "WITH "
	if basePage {
		// All fact joins preserve exactly the filtered base grain. Restrict their
		// input before aggregating, while total remains independent of the page.
		prefix += "base_total AS (SELECT count(*) AS total FROM public.uns_record r0 WHERE " + where + "), page_base AS MATERIALIZED (SELECT r0.* FROM public.uns_record r0 WHERE " + where + " ORDER BY r0.id " + strings.ToUpper(r.OrderBy[0].Direction) + " LIMIT ? OFFSET ?), "
		pageArgs := append(append([]any{}, whereArgs...), whereArgs...)
		pageArgs = append(pageArgs, size, (page-1)*size)
		args = append(pageArgs, args[:len(args)-len(whereArgs)]...)
		from = "page_base r0" + strings.TrimPrefix(from, "public.uns_record r0")
		where = "true"
	}
	statement := prefix + "input AS (SELECT " + strings.Join(inputs, ",") + " FROM " + from + " WHERE " + where + "), result AS (SELECT " + distinct + strings.Join(projections, ",") + " FROM input"
	if len(groups) > 0 {
		statement += " GROUP BY " + strings.Join(groups, ",")
	}
	statement += ")"
	post := []string{}
	postArgs := []any{}
	for _, filter := range r.Having {
		f, ok := selected[filter.Field]
		if !ok {
			return nil, relationalInvalid("having field must name a selected alias")
		}
		sql, vars, err := relationalFilter(f, filter.Op, filter.Value)
		if err != nil {
			return nil, err
		}
		post = append(post, sql)
		postArgs = append(postArgs, vars...)
	}
	resultFrom := " FROM result"
	if len(post) > 0 {
		resultFrom += " WHERE " + strings.Join(post, " AND ")
	}
	orders := []string{}
	ordered := map[string]bool{}
	for _, o := range r.OrderBy {
		f, ok := selected[o.Field]
		if !ok || ordered[f.sql] || o.Direction != "asc" && o.Direction != "desc" {
			return nil, relationalInvalid("orderBy requires unique selected aliases and asc/desc")
		}
		ordered[f.sql] = true
		orders = append(orders, f.sql+" "+strings.ToUpper(o.Direction)+" NULLS LAST")
	}
	for _, tie := range ties {
		if !ordered[tie] {
			orders = append(orders, tie+" ASC NULLS LAST")
		}
	}
	columns := make([]map[string]any, 0, len(r.Select))
	for _, projection := range r.Select {
		column := map[string]any{"name": projection.As, "type": selected[projection.As].kind, "nullable": projection.Aggregate != "count", "sourceField": projection.Field, "aggregate": projection.Aggregate}
		if projection.Expression != nil {
			column["computed"] = true
		}
		parts := strings.Split(projection.Field, ".")
		if len(parts) > 2 && parts[1] == "data" && projection.Aggregate != "count" {
			schema := c.sources[parts[0]].schema
			for _, part := range parts[2:] {
				if schema == nil {
					break
				}
				child, found := schema.Properties[part]
				if !found {
					schema = nil
					break
				}
				schema = &child
			}
			if schema != nil {
				column["description"] = schema.Description
				column["unit"] = schema.Unit
			}
		}
		columns = append(columns, column)
	}
	warnings := []string{}
	for _, warning := range queryWarnings {
		warnings = append(warnings, warning["advice"])
	}
	if capture != nil {
		capture.sql = statement + " SELECT " + strings.Join(outputNames, ",") + resultFrom
		capture.args = append(append([]any{}, args...), postArgs...)
		capture.outputs = selected
		return nil, nil
	}
	if validateOnly {
		return map[string]any{"status": "validated", "executed": false, "collectionIds": ids, "columns": columns, "page": page, "size": size, "warnings": warnings, "queryWarnings": queryWarnings, "limits": map[string]any{"queryTimeoutMs": 5000, "maxPageSize": 200}, "nextAction": "Execute the same query through records/aggregate. Validation neither scans records nor reserves a snapshot; schema and permissions are checked again at execution."}, nil
	}
	db := repo.GetUnsConn(ctx)
	settings := "SELECT set_config('statement_timeout', '5000', true)"
	if aggregated && len(r.Joins) > 0 && len(r.Subqueries) == 0 {
		// The bounded heavy-query slots also bound concurrent sort/hash work.
		// Avoid spilling modest grouped joins under small server defaults; retain
		// larger operator settings and reset automatically when this tx ends.
		settings += ", set_config('work_mem', CASE WHEN pg_size_bytes(current_setting('work_mem')) < pg_size_bytes('8MB') THEN '8MB' ELSE current_setting('work_mem') END, true)"
	}
	if err := db.Exec(settings).Error; err != nil {
		return nil, err
	}
	var total int64
	// Grouping and DISTINCT must build the complete result for an exact total.
	// Reuse that result for the page instead of running the same aggregate twice.
	// Plain projections keep the separate count so it need not evaluate/store
	// potentially wide projected values outside the requested page.
	combinedPage := len(r.Subqueries) > 0 || aggregated || r.Distinct
	if !combinedPage {
		countArgs := append(append([]any{}, args...), postArgs...)
		if err := db.Raw(statement+" SELECT count(*)"+resultFrom, countArgs...).Scan(&total).Error; err != nil {
			return nil, err
		}
	}
	objects := []string{}
	outputArgs := append([]any{}, args...)
	if combinedPage {
		outputArgs = append(outputArgs, postArgs...)
	}
	for i, s := range r.Select {
		objects = append(objects, "?::text,"+outputNames[i])
		outputArgs = append(outputArgs, s.As)
	}
	if !combinedPage {
		outputArgs = append(outputArgs, postArgs...)
	}
	offset := (page - 1) * size
	if basePage {
		offset = 0
	}
	outputArgs = append(outputArgs, size, offset)
	pageSQL := statement + " SELECT jsonb_build_object(" + strings.Join(objects, ",") + ")" + resultFrom + " ORDER BY " + strings.Join(orders, ",") + " LIMIT ? OFFSET ?"
	if combinedPage {
		// Share the filtered result between exact count and page. Keep
		// rows separate so the response budget is checked while scanning, and
		// retain a count row even when the requested page has no records.
		// Build JSON after LIMIT, not on every candidate before top-N sorting.
		// The marker distinguishes an empty page from a real all-NULL row.
		totalSource := "(SELECT count(*) AS total FROM filtered)"
		if basePage {
			totalSource = "base_total"
		}
		pageSQL = statement + ", filtered AS MATERIALIZED (SELECT *" + resultFrom + ") SELECT totals.total, CASE WHEN page.row_present IS NULL THEN NULL ELSE jsonb_build_object(" + strings.Join(objects, ",") + ") END AS row_data FROM " + totalSource + " totals LEFT JOIN LATERAL (SELECT filtered.*, true AS row_present FROM filtered ORDER BY " + strings.Join(orders, ",") + " LIMIT ? OFFSET ?) page ON true ORDER BY page." + strings.Join(orders, ",page.")
	}
	rows, err := db.Raw(pageSQL, outputArgs...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []map[string]any{}
	responseBytes := 0
	for rows.Next() {
		var raw []byte
		var scanErr error
		if combinedPage {
			scanErr = rows.Scan(&total, &raw)
		} else {
			scanErr = rows.Scan(&raw)
		}
		if scanErr != nil {
			return nil, scanErr
		}
		if combinedPage && raw == nil {
			continue
		}
		responseBytes += len(raw)
		if responseBytes > MaxQueryResponseBytes {
			return nil, problem("RESPONSE_TOO_LARGE", "query exceeds 8 MiB; reduce page size or projected fields")
		}
		var item map[string]any
		if err := decodeNumberJSON(raw, &item); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := pageResult(list, total, page, size)
	out["columns"] = columns
	out["warnings"] = warnings
	out["queryWarnings"] = queryWarnings
	out["rowShape"] = "projection"
	out["nullMeaning"] = "Missing or unmatched values are null. Empty count is 0; empty sum/avg/min/max is null."
	return out, nil
}

package unsv2

import (
	"backend/internal/repo"
	"context"
	"fmt"
	"strings"

	"backend/internal/domain/unsmeta"
)

// A captured plan is SQL and typed output metadata only. It is never executed
// independently: the outer query embeds it in the same read-only snapshot.
type relationalPlan struct {
	sql          string
	args         []any
	outputs      map[string]relationalField
	groupOutputs map[string]bool
	pageGroup    string
}

// Shared prerequisites for paging by a unique base ID. Join-specific callers
// must separately prove membership and cardinality; the full compiler validates
// the complete graph even when the resulting page is empty.
func baseIDPageOrder(r *RelationalQuery) bool {
	if len(r.Having) != 0 || r.Distinct || len(r.OrderBy) != 1 {
		return false
	}
	order := r.OrderBy[0]
	if order.Direction != "asc" && order.Direction != "desc" {
		return false
	}
	baseIDOrder := false
	for _, selectField := range r.Select {
		if selectField.As == order.Field && selectField.Field == "base.id" && selectField.Expression == nil && selectField.Aggregate == "" {
			baseIDOrder = true
		}
	}
	nodes := 0
	var baseOnly func(*QueryPredicate, int) bool
	baseOnly = func(p *QueryPredicate, depth int) bool {
		if p == nil {
			return true
		}
		nodes++
		if nodes > 64 || depth > 6 {
			return false
		}
		if p.Field != "" && !strings.HasPrefix(p.Field, "base.") {
			return false
		}
		for _, children := range [][]QueryPredicate{p.And, p.Or} {
			for i := range children {
				if !baseOnly(&children[i], depth+1) {
					return false
				}
			}
		}
		return baseOnly(p.Not, depth+1)
	}
	return baseIDOrder && baseOnly(r.Where, 1)
}

// Group-key LEFT joins preserve the base grain without a fact-existence test.
func boundedComposedPage(r *RelationalQuery) bool {
	if len(r.Subqueries) == 0 || len(r.Subqueries) > 4 || len(r.GroupBy) != 0 || !baseIDPageOrder(r) {
		return false
	}
	groups := map[string]string{}
	for _, sub := range r.Subqueries {
		if len(sub.GroupBy) != 1 || len(sub.Joins) != 0 {
			return false
		}
		for _, selected := range sub.Select {
			if selected.Field == sub.GroupBy[0] && selected.Aggregate == "" {
				groups[sub.Name] = selected.As
				break
			}
		}
	}
	for _, join := range r.Joins {
		group := groups[join.Subquery]
		if join.Type != "left" || group == "" || len(join.On) != 1 {
			return false
		}
		on, key := join.On[0], join.Alias+"."+group
		if !(on.Left == "base.id" && on.Right == key || on.Right == "base.id" && on.Left == key) {
			return false
		}
	}
	return true
}

func prepareRelationalSources(ctx context.Context, q Query, basePage bool) (*relationalCompiler, string, []any, []string, []string, []map[string]string, error) {
	c := &relationalCompiler{sources: map[string]relationalSource{}, fields: map[string]relationalField{}}
	ids, aliases := []string{q.CollectionID}, []string{"base"}
	warnings := []map[string]string{}
	fail := func(err error) (*relationalCompiler, string, []any, []string, []string, []map[string]string, error) {
		return nil, "", nil, nil, nil, nil, err
	}
	r := q.Relational
	if len(r.Subqueries) > 4 {
		return fail(relationalInvalid("at most 4 named subqueries are allowed"))
	}
	composed := len(r.Subqueries) > 0
	if composed && (len(r.GroupBy) > 0 || r.Distinct) {
		return fail(relationalInvalid("composed queries preserve base-record grain; final groupBy and distinct are not supported"))
	}
	if composed {
		for _, s := range r.Select {
			if s.Aggregate != "" {
				return fail(relationalInvalid("aggregate inside named subqueries before joining; final aggregate is not supported"))
			}
		}
	}
	definitions := map[string]QuerySubquery{}
	rawJoins := 0
	for _, sub := range r.Subqueries {
		if !relationalAlias.MatchString(sub.Name) || definitions[sub.Name].Name != "" || sub.CollectionID == "" || len(sub.GroupBy) == 0 {
			return fail(relationalInvalid("subqueries require unique names, collectionId, and nonempty groupBy"))
		}
		definitions[sub.Name] = sub
		ids = append(ids, sub.CollectionID)
		aggregated := false
		for _, s := range sub.Select {
			if s.Expression != nil {
				return fail(relationalInvalid("subquery selects support grouped fields and aggregates, not expressions"))
			}
			aggregated = aggregated || s.Aggregate != ""
		}
		if !aggregated {
			return fail(relationalInvalid("each named subquery requires at least one aggregate"))
		}
		for _, j := range sub.Joins {
			if j.Subquery != "" || j.CollectionID == "" {
				return fail(relationalInvalid("subqueries can join physical collections only; nested subqueries are not supported"))
			}
			ids = append(ids, j.CollectionID)
			rawJoins++
		}
		if len(sub.Joins) > 0 {
			warnings = append(warnings, fanoutWarning("subqueries."+sub.Name+".joins"))
		}
	}
	seen := map[string]bool{"base": true}
	used := map[string]bool{}
	for _, j := range r.Joins {
		if !relationalAlias.MatchString(j.Alias) || seen[j.Alias] || j.Type != "inner" && j.Type != "left" || len(j.On) < 1 || len(j.On) > 8 {
			return fail(relationalInvalid("joins require a unique alias, inner/left type, and 1-8 equality pairs"))
		}
		seen[j.Alias] = true
		if (j.CollectionID == "") == (j.Subquery == "") {
			return fail(relationalInvalid("join requires exactly one collectionId or subquery"))
		}
		if composed {
			if j.CollectionID != "" || definitions[j.Subquery].Name == "" {
				return fail(relationalInvalid("composed final joins must reference declared subqueries"))
			}
			used[j.Subquery] = true
		} else {
			if j.Subquery != "" {
				return fail(relationalInvalid("unknown subquery name"))
			}
			ids = append(ids, j.CollectionID)
			aliases = append(aliases, j.Alias)
			rawJoins++
		}
	}
	if rawJoins > 4 {
		return fail(relationalInvalid("at most 4 physical joins are allowed across the whole query"))
	}
	for name := range definitions {
		if !used[name] {
			return fail(relationalInvalid("every named subquery must be referenced by a final join"))
		}
	}
	if !composed && len(r.Joins) > 0 {
		warnings = append(warnings, fanoutWarning("joins"))
	}
	// Check the entire collection graph before reading any schema. This includes
	// empty/unmatched facts and sources whose output is not projected.
	uniqueIDs := []string{}
	seenIDs := map[string]bool{}
	for _, id := range ids {
		if id == "" {
			return fail(relationalInvalid("collectionId is required"))
		}
		if seenIDs[id] {
			continue
		}
		if err := repo.RequireLocalCollectionRead(ctx, id); err != nil {
			return fail(classify(err))
		}
		seenIDs[id] = true
		uniqueIDs = append(uniqueIDs, id)
		if err := appCollectionAccess(ctx, id, "record:read"); err != nil {
			return fail(err)
		}
	}
	load := func(id, sql string) (relationalSource, error) {
		row, err := meta(ctx, "collection", id, false)
		if err != nil {
			return relationalSource{}, err
		}
		var doc unsmeta.Document
		if err := decodeNumberJSON(row.Data, &doc); err != nil {
			return relationalSource{}, err
		}
		return relationalSource{sql: sql, schema: doc.RecordSchema}, nil
	}
	base, err := load(q.CollectionID, "r0")
	if err != nil {
		return fail(err)
	}
	c.sources["base"] = base
	plans := map[string]relationalPlan{}
	for _, sub := range r.Subqueries {
		plan := relationalPlan{groupOutputs: map[string]bool{}}
		if basePage {
			plan.pageGroup = sub.GroupBy[0]
		}
		for _, group := range sub.GroupBy {
			output := ""
			for _, s := range sub.Select {
				if s.Aggregate == "" && s.Field == group {
					output = s.As
					break
				}
			}
			if output == "" {
				return fail(relationalInvalid("each subquery groupBy field must be projected as a named output"))
			}
			plan.groupOutputs[output] = true
		}
		inner := Query{CollectionID: sub.CollectionID, Relational: &RelationalQuery{Joins: sub.Joins, Select: sub.Select, Where: sub.Where, GroupBy: sub.GroupBy, Having: sub.Having}}
		if _, err := compileRelationalQueryInternal(ctx, inner, true, &plan); err != nil {
			return fail(err)
		}
		plans[sub.Name] = plan
	}
	from, args := "public.uns_record r0", []any{}
	known := map[string]bool{"base": true}
	for i, j := range r.Joins {
		alias := fmt.Sprintf("r%d", i+1)
		joinSource := "public.uns_record " + alias
		pairs := []string{}
		if composed {
			plan := plans[j.Subquery]
			c.sources[j.Alias] = relationalSource{sql: alias, outputs: plan.outputs}
			joinSource = "(" + plan.sql + ") " + alias
			args = append(args, plan.args...)
		} else {
			source, err := load(j.CollectionID, alias)
			if err != nil {
				return fail(err)
			}
			c.sources[j.Alias] = source
			pairs = append(pairs, alias+".collection_id = ?", alias+".deleted_at IS NULL")
			args = append(args, j.CollectionID)
		}
		covered := map[string]bool{}
		for _, on := range j.On {
			lp, rp := strings.Split(on.Left, "."), strings.Split(on.Right, ".")
			la, ra := lp[0], rp[0]
			if !(la == j.Alias && known[ra] || ra == j.Alias && known[la]) {
				return fail(relationalInvalid("each join equality must connect its new alias to an earlier source"))
			}
			left, err := c.field(on.Left)
			if err != nil {
				return fail(err)
			}
			right, err := c.field(on.Right)
			if err != nil {
				return fail(err)
			}
			if left.kind != right.kind && !(relationalNumeric(left.kind) && relationalNumeric(right.kind)) {
				return fail(relationalInvalid("join field types must match"))
			}
			if la == j.Alias && len(lp) == 2 {
				covered[lp[1]] = true
			}
			if ra == j.Alias && len(rp) == 2 {
				covered[rp[1]] = true
			}
			pairs = append(pairs, left.sql+" = "+right.sql)
			args = append(args, left.args...)
			args = append(args, right.args...)
		}
		if composed {
			for key := range plans[j.Subquery].groupOutputs {
				if !covered[key] {
					return fail(relationalInvalid("join ON must cover every subquery group output to prevent fanout"))
				}
			}
		}
		from += " " + strings.ToUpper(j.Type) + " JOIN " + joinSource + " ON " + strings.Join(pairs, " AND ")
		known[j.Alias] = true
	}
	return c, from, args, uniqueIDs, aliases, warnings, nil
}

func fanoutWarning(path string) map[string]string {
	return map[string]string{"code": "JOIN_FANOUT_RISK", "severity": "warning", "path": path, "advice": "Raw one-to-many joins can multiply rows and aggregate totals. Aggregate independent fact collections in separate named subqueries; declared relationships are not uniqueness guarantees."}
}

func (c *relationalCompiler) expression(e QueryExpression, depth int) (relationalField, error) {
	c.expressionNodes++
	if depth > 6 || c.expressionNodes > 64 {
		return relationalField{}, relationalInvalid("expressions permit at most 64 nodes and depth 6")
	}
	forms := 0
	if e.Field != "" {
		forms++
	}
	if len(e.Value) > 0 {
		forms++
	}
	if e.Op != "" || e.Args != nil {
		forms++
	}
	if forms != 1 {
		return relationalField{}, relationalInvalid("expression must contain exactly field, numeric value, or op with args")
	}
	if e.Field != "" {
		return c.field(e.Field)
	}
	if len(e.Value) > 0 {
		var value any
		if decodeNumberJSON(e.Value, &value) != nil || !relationalValue("number", value) {
			return relationalField{}, relationalInvalid("expression value must be a bounded JSON number")
		}
		return relationalField{sql: "?::numeric", args: []any{value}, kind: "number"}, nil
	}
	if e.Op == "coalesce" {
		if len(e.Args) < 2 || len(e.Args) > 8 {
			return relationalField{}, relationalInvalid("coalesce requires 2-8 arguments")
		}
	} else if len(e.Args) != 2 {
		return relationalField{}, relationalInvalid("arithmetic expressions require exactly 2 arguments")
	}
	parts, args, kind := []string{}, []any{}, ""
	for _, child := range e.Args {
		f, err := c.expression(child, depth+1)
		if err != nil {
			return relationalField{}, err
		}
		if kind == "" {
			kind = f.kind
		}
		if relationalNumeric(kind) && relationalNumeric(f.kind) {
			kind = "number"
		} else if kind != f.kind {
			return relationalField{}, relationalInvalid("expression arguments must have compatible scalar types")
		}
		parts = append(parts, f.sql)
		args = append(args, f.args...)
	}
	sql := ""
	if e.Op == "coalesce" {
		sql = "COALESCE(" + strings.Join(parts, ",") + ")"
	} else {
		if !relationalNumeric(kind) {
			return relationalField{}, relationalInvalid("arithmetic requires numeric arguments")
		}
		// System fields and COUNT may be bigint. Promote before arithmetic so
		// calculations use the same exact numeric range as schema numeric fields.
		left, right := "("+parts[0]+")::numeric", "("+parts[1]+")::numeric"
		switch e.Op {
		case "add":
			sql = "(" + left + " + " + right + ")"
		case "subtract":
			sql = "(" + left + " - " + right + ")"
		case "multiply":
			sql = "(" + left + " * " + right + ")"
		case "safeDivide":
			sql = "(" + left + " / NULLIF(" + right + ",0))"
		default:
			return relationalField{}, relationalInvalid("expression op must be coalesce/add/subtract/multiply/safeDivide")
		}
		kind = "number"
	}
	return relationalField{sql: sql, args: args, kind: kind}, nil
}

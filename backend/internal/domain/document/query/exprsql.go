package query

import (
	"fmt"
	"strings"
)

// 表达式编译成 SQL 的约定（docs/collection-crud/design.md 第 5.5 节）：
//   - 每个表达式是一个 jsonb 值；SQL NULL 表示“缺失 / 无效”，'null'::jsonb 是 JSON null，运算符把两者都当成 null。
//   - 类型不对、除以 0、日期非法等一律得到 SQL NULL，不抛错：每个转换前都先检查 jsonb_typeof，
//     日期字符串先用 ISO 8601 正则和 pg_input_is_valid 检查。
//   - 字段引用：表上（$expr）是 data #> ARRAY[...] / 系统列；聚合管线里是 doc #> ARRAY[...]（DocMode）。

// isoDateRe 是接受的 ISO 8601 日期时间写法（SQL 与 Go 共用）。
const isoDateRe = `^[0-9]{4}-[0-9]{2}-[0-9]{2}([T ][0-9]{2}:[0-9]{2}(:[0-9]{2}(\.[0-9]{1,9})?)?)?(Z|[+-][0-9]{2}(:?[0-9]{2})?)?$`

// toNumberRe 是 $toNumber 接受的字符串（JSON 数字语法，指数最多 3 位）。
const toNumberRe = `^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]{1,3})?$`

// maxMs 是时间毫秒数的范围（约 ±31700 年），超出视为无效。
const maxMs = "1000000000000000"

func nn(x string) string { return "NULLIF(" + x + ", 'null'::jsonb)" }

func isType(x, ty string) string { return "jsonb_typeof(" + x + ") = '" + ty + "'" }

func numv(x string) string { return "(" + x + ")::numeric" }

func strv(x string) string { return "(" + x + " #>> '{}')" }

func isInt(x string) string {
	return "(" + isType(x, "number") + " AND " + numv(x) + " = trunc(" + numv(x) + "))"
}

// truthySQL 是 Mongo 真值规则的 SQL（结果是 boolean，不为 NULL）。
func truthySQL(x string) string {
	return "(CASE WHEN " + x + " IS NULL OR " + x + " = 'null'::jsonb OR " + x + " = 'false'::jsonb THEN FALSE WHEN " +
		isType(x, "number") + " THEN " + numv(x) + " <> 0 ELSE TRUE END)"
}

// fieldSQL 返回字段引用的 jsonb 表达式。
func (b *Builder) fieldSQL(p Path) string {
	if b.DocMode {
		return "(" + docPath(p).sqlOn("doc") + ")"
	}
	if !p.IsSys() {
		return "(" + p.SQL() + ")"
	}
	switch p.Sys {
	case "id":
		return "to_jsonb(id::text)"
	case "revision":
		return "to_jsonb(revision)"
	case "createdAt":
		return "to_jsonb(" + MsSQL("created_at") + ")"
	case "updatedAt":
		return "to_jsonb(" + MsSQL("updated_at") + ")"
	case "sourceId":
		return "to_jsonb(source_id::text)"
	case "sourceTime":
		return "to_jsonb(" + MsSQL("source_time") + ")"
	case "locked":
		return "to_jsonb(" + LockedSQL + ")"
	}
	return "NULL::jsonb"
}

// MsSQL 把 timestamptz 列换成截断到毫秒的毫秒数（bigint）。
func MsSQL(col string) string {
	return "floor(extract(epoch from " + col + ") * 1000)::bigint"
}

// docPath 把系统字段写成管线文档里的路径（_id、_meta.xxx）。
func docPath(p Path) Path {
	if !p.IsSys() {
		return p
	}
	if p.Sys == "id" {
		return Path{Segs: []string{"_id"}, Raw: p.Raw}
	}
	return Path{Segs: []string{"_meta", p.Sys}, Raw: p.Raw}
}

// ExprSQL 编译表达式，返回 jsonb 表达式。
func (b *Builder) ExprSQL(e Expr) (string, error) {
	switch t := e.(type) {
	case FieldRef:
		return b.fieldSQL(t.Path), nil
	case Literal:
		if t.Val == nil {
			return "'null'::jsonb", nil
		}
		return "(" + b.JSONArg(t.Val) + ")", nil
	case ArrayExpr:
		parts := make([]string, len(t.Items))
		for i, it := range t.Items {
			s, err := b.ExprSQL(it)
			if err != nil {
				return "", err
			}
			parts[i] = "COALESCE(" + s + ", 'null'::jsonb)"
		}
		if len(parts) == 0 {
			return "'[]'::jsonb", nil
		}
		return "jsonb_build_array(" + strings.Join(parts, ", ") + ")", nil
	case OpExpr:
		return b.opSQL(t)
	}
	return "", errf("unsupported expression %T", e)
}

func (b *Builder) args(es []Expr) ([]string, error) {
	out := make([]string, len(es))
	for i, e := range es {
		s, err := b.ExprSQL(e)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

func allNum(xs []string) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = isType(x, "number")
	}
	return strings.Join(parts, " AND ")
}

func allStr(xs []string) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = isType(x, "string")
	}
	return strings.Join(parts, " AND ")
}

func caseWhen(cond, val string) string { return "(CASE WHEN " + cond + " THEN " + val + " END)" }

// tsSQL 把表达式值（毫秒数或 ISO 8601 字符串）换成 timestamptz（无效时 NULL）。
func (b *Builder) tsSQL(x string) string {
	return "(CASE WHEN " + isType(x, "number") + " AND abs(" + numv(x) + ") < " + maxMs +
		" THEN 'epoch'::timestamptz + trunc(" + numv(x) + ")::bigint * interval '1 millisecond'" +
		" WHEN " + isType(x, "string") + " AND " + strv(x) + " ~ " + b.Arg(isoDateRe) +
		" AND pg_input_is_valid(" + strv(x) + ", 'timestamptz') THEN " + strv(x) + "::timestamptz END)"
}

// localTS 把 timestamptz 换成时区里的本地时间（timestamp）。
func (b *Builder) localTS(ts string, tz Timezone) string {
	if tz.Name != "" {
		return "(" + ts + " AT TIME ZONE " + b.Arg(tz.Name) + ")"
	}
	return "((" + ts + " AT TIME ZONE 'UTC') + " + b.Arg(fmt.Sprintf("%d minutes", tz.Offset)) + "::interval)"
}

// fromLocal 把时区里的本地时间（timestamp）换回 timestamptz。
func (b *Builder) fromLocal(local string, tz Timezone) string {
	if tz.Name != "" {
		return "(" + local + " AT TIME ZONE " + b.Arg(tz.Name) + ")"
	}
	return "((" + local + " - " + b.Arg(fmt.Sprintf("%d minutes", tz.Offset)) + "::interval) AT TIME ZONE 'UTC')"
}

func exprTZ(e OpExpr) Timezone {
	if l, ok := e.Named["timezone"].(Literal); ok {
		if s, ok := l.Val.(string); ok {
			tz, _ := ParseTimezone(s)
			return tz
		}
	}
	tz, _ := ParseTimezone("")
	return tz
}

func (b *Builder) opSQL(e OpExpr) (string, error) {
	switch e.Op {
	case "$cond":
		a, err := b.args(e.Args)
		if err != nil {
			return "", err
		}
		return "(CASE WHEN " + truthySQL(a[0]) + " THEN " + a[1] + " ELSE " + a[2] + " END)", nil
	case "$switch":
		var sb strings.Builder
		sb.WriteString("(CASE")
		for _, br := range e.Branches {
			c, err := b.ExprSQL(br[0])
			if err != nil {
				return "", err
			}
			t, err := b.ExprSQL(br[1])
			if err != nil {
				return "", err
			}
			sb.WriteString(" WHEN " + truthySQL(c) + " THEN " + t)
		}
		if len(e.Args) == 1 {
			d, err := b.ExprSQL(e.Args[0])
			if err != nil {
				return "", err
			}
			sb.WriteString(" ELSE " + d)
		}
		sb.WriteString(" END)")
		return sb.String(), nil
	case "$dateTrunc":
		d, err := b.ExprSQL(e.Named["date"])
		if err != nil {
			return "", err
		}
		unit := e.Named["unit"].(Literal).Val.(string)
		tz := exprTZ(e)
		ts := b.tsSQL(d)
		var out string
		if tz.Name != "" {
			out = "date_trunc(" + Lit(unit) + ", " + ts + ", " + b.Arg(tz.Name) + ")"
		} else {
			out = b.fromLocal("date_trunc("+Lit(unit)+", "+b.localTS(ts, tz)+")", tz)
		}
		return "to_jsonb(" + MsSQL(out) + ")", nil
	case "$dateToParts":
		d, err := b.ExprSQL(e.Named["date"])
		if err != nil {
			return "", err
		}
		l := b.localTS(b.tsSQL(d), exprTZ(e))
		return "(CASE WHEN " + l + " IS NOT NULL THEN jsonb_build_object('year', extract(year from " + l + ")::int, 'month', extract(month from " + l +
			")::int, 'day', extract(day from " + l + ")::int, 'hour', extract(hour from " + l + ")::int, 'minute', extract(minute from " + l +
			")::int, 'second', floor(extract(second from " + l + "))::int, 'millisecond', (floor(extract(milliseconds from " + l + "))::int % 1000)) END)", nil
	case "$dateFromParts":
		keys := []string{"year", "month", "day", "hour", "minute", "second", "millisecond"}
		defaults := map[string]int{"month": 1, "day": 1}
		lo := map[string]int{"year": 1, "month": 1, "day": 1}
		hi := map[string]int{"year": 9999, "month": 12, "day": 31, "hour": 23, "minute": 59, "second": 59, "millisecond": 999}
		var conds, vals []string
		for _, k := range keys {
			ex, ok := e.Named[k]
			if !ok {
				vals = append(vals, fmt.Sprintf("%d", defaults[k]))
				continue
			}
			x, err := b.ExprSQL(ex)
			if err != nil {
				return "", err
			}
			conds = append(conds, isInt(x), fmt.Sprintf("%s BETWEEN %d AND %d", numv(x), lo[k], hi[k]))
			vals = append(vals, "("+numv(x)+")::int")
		}
		txt := fmt.Sprintf("format('%%s-%%s-%%s %%s:%%s:%%s.%%s', lpad((%s)::text, 4, '0'), lpad((%s)::text, 2, '0'), lpad((%s)::text, 2, '0'), lpad((%s)::text, 2, '0'), lpad((%s)::text, 2, '0'), lpad((%s)::text, 2, '0'), lpad((%s)::text, 3, '0'))",
			vals[0], vals[1], vals[2], vals[3], vals[4], vals[5], vals[6])
		conds = append(conds, "pg_input_is_valid("+txt+", 'timestamp')")
		local := "(" + txt + ")::timestamp"
		return caseWhen(strings.Join(conds, " AND "), "to_jsonb("+MsSQL(b.fromLocal(local, exprTZ(e)))+")"), nil
	case "$trim":
		x, err := b.ExprSQL(e.Named["input"])
		if err != nil {
			return "", err
		}
		chars := defaultTrimChars
		if c, ok := e.Named["chars"].(Literal); ok {
			chars, _ = c.Val.(string)
		}
		return caseWhen(isType(x, "string"), "to_jsonb(btrim("+strv(x)+", "+b.Arg(chars)+"))"), nil
	case "$round":
		a, err := b.args(e.Args)
		if err != nil {
			return "", err
		}
		if len(a) == 1 {
			return caseWhen(isType(a[0], "number"), "to_jsonb(round("+numv(a[0])+"))"), nil
		}
		return caseWhen(isType(a[0], "number")+" AND "+isInt(a[1])+" AND "+numv(a[1])+" BETWEEN -20 AND 100",
			"to_jsonb(round("+numv(a[0])+", ("+numv(a[1])+")::int))"), nil
	}
	a, err := b.args(e.Args)
	if err != nil {
		return "", err
	}
	switch e.Op {
	case "$add", "$multiply":
		op := " + "
		if e.Op == "$multiply" {
			op = " * "
		}
		nums := make([]string, len(a))
		for i, x := range a {
			nums[i] = numv(x)
		}
		return caseWhen(allNum(a), "to_jsonb("+strings.Join(nums, op)+")"), nil
	case "$subtract":
		return caseWhen(allNum(a), "to_jsonb("+numv(a[0])+" - "+numv(a[1])+")"), nil
	case "$divide":
		return caseWhen(allNum(a)+" AND "+numv(a[1])+" <> 0", "to_jsonb("+numv(a[0])+" / "+numv(a[1])+")"), nil
	case "$mod":
		return caseWhen(allNum(a)+" AND "+numv(a[1])+" <> 0", "to_jsonb(mod("+numv(a[0])+", "+numv(a[1])+"))"), nil
	case "$abs":
		return caseWhen(isType(a[0], "number"), "to_jsonb(abs("+numv(a[0])+"))"), nil
	case "$floor":
		return caseWhen(isType(a[0], "number"), "to_jsonb(floor("+numv(a[0])+"))"), nil
	case "$ceil":
		return caseWhen(isType(a[0], "number"), "to_jsonb(ceil("+numv(a[0])+"))"), nil
	case "$eq":
		return "to_jsonb(" + nn(a[0]) + " IS NOT DISTINCT FROM " + nn(a[1]) + ")", nil
	case "$ne":
		return "to_jsonb(" + nn(a[0]) + " IS DISTINCT FROM " + nn(a[1]) + ")", nil
	case "$gt", "$gte", "$lt", "$lte":
		cond := "jsonb_typeof(" + a[0] + ") = jsonb_typeof(" + a[1] + ") AND jsonb_typeof(" + a[0] + ") IN ('number', 'string', 'boolean')"
		return caseWhen(cond, "to_jsonb("+a[0]+" "+sqlOp(e.Op[1:])+" "+a[1]+")"), nil
	case "$and", "$or":
		op := " AND "
		if e.Op == "$or" {
			op = " OR "
		}
		parts := make([]string, len(a))
		for i, x := range a {
			parts[i] = truthySQL(x)
		}
		return "to_jsonb(" + strings.Join(parts, op) + ")", nil
	case "$not":
		return "to_jsonb(NOT " + truthySQL(a[0]) + ")", nil
	case "$in":
		return caseWhen(isType(a[1], "array"), "to_jsonb(EXISTS(SELECT 1 FROM jsonb_array_elements("+a[1]+") AS e(v) WHERE e.v = COALESCE("+a[0]+", 'null'::jsonb)))"), nil
	case "$ifNull":
		parts := make([]string, len(a))
		for i, x := range a {
			parts[i] = nn(x)
		}
		return "COALESCE(" + strings.Join(parts, ", ") + ")", nil
	case "$concat":
		strs := make([]string, len(a))
		for i, x := range a {
			strs[i] = strv(x)
		}
		cat := "(" + strings.Join(strs, " || ") + ")"
		return caseWhen(allStr(a)+fmt.Sprintf(" AND octet_length%s <= %d", cat, MaxConcatBytes), "to_jsonb("+cat+")"), nil
	case "$toLower":
		return caseWhen(isType(a[0], "string"), "to_jsonb(lower("+strv(a[0])+"))"), nil
	case "$toUpper":
		return caseWhen(isType(a[0], "string"), "to_jsonb(upper("+strv(a[0])+"))"), nil
	case "$substrCP":
		cond := isType(a[0], "string") + " AND " + isInt(a[1]) + " AND " + isInt(a[2]) + " AND " + numv(a[1]) + " >= 0 AND " + numv(a[2]) + " >= 0 AND " +
			numv(a[1]) + " < 1000000000 AND " + numv(a[2]) + " < 1000000000"
		return caseWhen(cond, "to_jsonb(substr("+strv(a[0])+", ("+numv(a[1])+")::int + 1, ("+numv(a[2])+")::int))"), nil
	case "$strLenCP":
		return caseWhen(isType(a[0], "string"), "to_jsonb(char_length("+strv(a[0])+"))"), nil
	case "$toNumber":
		x := a[0]
		return "(CASE WHEN " + isType(x, "number") + " THEN " + x +
			" WHEN " + isType(x, "string") + " AND " + strv(x) + " ~ " + b.Arg(toNumberRe) + " THEN to_jsonb(" + strv(x) + "::numeric)" +
			" WHEN " + x + " = 'true'::jsonb THEN '1'::jsonb WHEN " + x + " = 'false'::jsonb THEN '0'::jsonb END)", nil
	case "$toString":
		x := a[0]
		// 数字输出规范写法（去掉尾随零，1.50 → "1.5"），与 Go 侧一致
		return "(CASE WHEN " + isType(x, "string") + " THEN " + x + " WHEN " + isType(x, "number") +
			" THEN to_jsonb(trim_scale(" + numv(x) + ")::text) WHEN " + isType(x, "boolean") + " THEN to_jsonb(" + strv(x) + ") END)", nil
	case "$type":
		return "to_jsonb(COALESCE(jsonb_typeof(" + a[0] + "), 'missing'))", nil
	case "$size":
		return caseWhen(isType(a[0], "array"), "to_jsonb(jsonb_array_length("+a[0]+"))"), nil
	case "$arrayElemAt":
		cond := isType(a[0], "array") + " AND " + isInt(a[1]) + " AND abs(" + numv(a[1]) + ") < 1000000"
		return caseWhen(cond, a[0]+" -> ("+numv(a[1])+")::int"), nil
	case "$first":
		return caseWhen(isType(a[0], "array"), a[0]+" -> 0"), nil
	case "$last":
		return caseWhen(isType(a[0], "array"), a[0]+" -> -1"), nil
	}
	return "", errf("unsupported expression operator %s", e.Op)
}

// PathSQLOn 返回指定 jsonb 列上的路径值表达式（例如 r.data #> ARRAY[...]）。
func PathSQLOn(p Path, col string) string { return p.sqlOn(col) }

// FieldSQLFor 返回字段的 jsonb 表达式，系统列加上表别名前缀（例如 "r."）。
func (b *Builder) FieldSQLFor(p Path, prefix string) string {
	if !p.IsSys() {
		return "(" + p.sqlOn(prefix+"data") + ")"
	}
	switch p.Sys {
	case "id":
		return "to_jsonb(" + prefix + "id::text)"
	case "revision":
		return "to_jsonb(" + prefix + "revision)"
	case "createdAt":
		return "to_jsonb(" + MsSQL(prefix+"created_at") + ")"
	case "updatedAt":
		return "to_jsonb(" + MsSQL(prefix+"updated_at") + ")"
	case "sourceId":
		return "to_jsonb(" + prefix + "source_id::text)"
	case "sourceTime":
		return "to_jsonb(" + MsSQL(prefix+"source_time") + ")"
	}
	return "NULL::jsonb"
}

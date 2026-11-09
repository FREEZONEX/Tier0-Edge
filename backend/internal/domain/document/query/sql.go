package query

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"backend/internal/domain/document/num"
)

// Builder 把 AST 编译成参数化的 SQL 条件（docs/collection-crud/design.md 第 4.2 节）。
// 值一律是参数；只有路径段按 Lit 内联（表达式索引按字面匹配）。$elemMatch 生成的 jsonpath 文本也作为参数传入。
type Builder struct {
	Args []any
	// ArrayPath 判断路径在集合的 recordSchema 里是否声明为 array：是时 {f: v} / $in 按包含（@>）编译（第 2.2 节）
	ArrayPath func(Path) bool
	// DocMode 为真时在聚合管线里编译：文档是 doc 列（{_id, _meta, ...data}），系统字段也是文档里的路径
	DocMode bool
}

// Arg 追加一个参数，返回占位符 $n。
func (b *Builder) Arg(v any) string {
	b.Args = append(b.Args, v)
	return fmt.Sprintf("$%d", len(b.Args))
}

// JSONArg 追加一个 jsonb 参数（值序列化成 JSON，数字保留原文）。
func (b *Builder) JSONArg(v any) string { return b.Arg(jsonValue(v)) + "::jsonb" }

func (b *Builder) isArray(p Path) bool { return !p.IsSys() && b.ArrayPath != nil && b.ArrayPath(p) }

// ps / pt 返回路径值（jsonb）/ 路径文本的 SQL：表上是 data 列，管线里是 doc 列。
func (b *Builder) ps(p Path) string {
	if b.DocMode {
		return docPath(p).sqlOn("doc")
	}
	return p.SQL()
}

func (b *Builder) pt(p Path) string {
	if b.DocMode {
		return strings.Replace(docPath(p).sqlOn("doc"), " #> ", " #>> ", 1)
	}
	return p.TextSQL()
}

// Where 编译过滤条件。结果不会是 SQL NULL（所有叶子条件都按 null 安全写法生成，NOT 外面再包 COALESCE）。
func (b *Builder) Where(n Node) (string, error) {
	if b.DocMode {
		n = toDocPaths(n)
	}
	return b.where(n)
}

// toDocPaths 把系统字段换成管线文档里的普通路径（_id、_meta.xxx）。
func toDocPaths(n Node) Node {
	switch t := n.(type) {
	case And:
		k := make([]Node, len(t.Kids))
		for i, x := range t.Kids {
			k[i] = toDocPaths(x)
		}
		return And{Kids: k}
	case Or:
		k := make([]Node, len(t.Kids))
		for i, x := range t.Kids {
			k[i] = toDocPaths(x)
		}
		return Or{Kids: k}
	case Nor:
		k := make([]Node, len(t.Kids))
		for i, x := range t.Kids {
			k[i] = toDocPaths(x)
		}
		return Nor{Kids: k}
	case Not:
		return Not{Kid: toDocPaths(t.Kid)}
	case Cmp:
		t.Path = docPath(t.Path)
		return t
	case In:
		t.Path = docPath(t.Path)
		return t
	case Exists:
		t.Path = docPath(t.Path)
		return t
	case TypeIs:
		t.Path = docPath(t.Path)
		return t
	case All:
		t.Path = docPath(t.Path)
		return t
	case Size:
		t.Path = docPath(t.Path)
		return t
	case ElemMatch:
		t.Path = docPath(t.Path)
		return t
	case Regex:
		t.Path = docPath(t.Path)
		return t
	case Prefix:
		t.Path = docPath(t.Path)
		return t
	case Mod:
		t.Path = docPath(t.Path)
		return t
	case Search:
		f := make([]Path, len(t.Fields))
		for i, p := range t.Fields {
			f[i] = docPath(p)
		}
		t.Fields = f
		return t
	}
	return n
}

func (b *Builder) where(n Node) (string, error) {
	switch t := n.(type) {
	case And:
		if len(t.Kids) == 0 {
			return "TRUE", nil
		}
		return b.join(t.Kids, " AND ")
	case Or:
		return b.join(t.Kids, " OR ")
	case Nor:
		s, err := b.join(t.Kids, " OR ")
		if err != nil {
			return "", err
		}
		return "NOT COALESCE(" + s + ", FALSE)", nil
	case Not:
		s, err := b.where(t.Kid)
		if err != nil {
			return "", err
		}
		return "NOT COALESCE(" + s + ", FALSE)", nil
	case Cmp:
		if t.Path.IsSys() {
			return b.sysCmp(t.Path, t.Op, t.Val)
		}
		return b.cmp(t)
	case In:
		// 非系统字段、全是非 null 标量、不按包含编译时写成 = ANY(...)，能走索引
		if !t.Path.IsSys() && !b.isArray(t.Path) && allScalarNonNull(t.Vals) && len(t.Vals) > 0 {
			vals := make([]string, len(t.Vals))
			for i, v := range t.Vals {
				vals[i] = jsonValue(v)
			}
			s := b.ps(t.Path) + " = ANY(" + b.Arg(vals) + "::jsonb[])"
			if t.Neg {
				return "NOT COALESCE(" + s + ", FALSE)", nil
			}
			return "COALESCE(" + s + ", FALSE)", nil
		}
		var parts []string
		for _, v := range t.Vals {
			var s string
			var err error
			if t.Path.IsSys() {
				s, err = b.sysCmp(t.Path, "eq", v)
			} else {
				s, err = b.cmp(Cmp{Path: t.Path, Op: "eq", Val: v})
			}
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		s := "FALSE"
		if len(parts) > 0 {
			s = "(" + strings.Join(parts, " OR ") + ")"
		}
		if t.Neg {
			return "NOT COALESCE(" + s + ", FALSE)", nil
		}
		return s, nil
	case Exists:
		if t.Path.IsSys() {
			col, nullable := sysColumn(t.Path.Sys)
			if !nullable {
				return boolSQL(t.Want), nil
			}
			if t.Want {
				return col + " IS NOT NULL", nil
			}
			return col + " IS NULL", nil
		}
		if t.Want {
			return "(" + b.ps(t.Path) + ") IS NOT NULL", nil
		}
		return "(" + b.ps(t.Path) + ") IS NULL", nil
	case TypeIs:
		if t.Path.IsSys() {
			return b.sysType(t.Path, t.Types)
		}
		var parts []string
		for _, ty := range t.Types {
			p := b.ps(t.Path)
			if ty == "integer" {
				parts = append(parts, fmt.Sprintf("(jsonb_typeof(%s) = 'number' AND (%s)::numeric = trunc((%s)::numeric))", p, p, p))
				continue
			}
			parts = append(parts, fmt.Sprintf("jsonb_typeof(%s) = %s", p, Lit(ty)))
		}
		return "COALESCE(" + strings.Join(parts, " OR ") + ", FALSE)", nil
	case All:
		if t.Path.IsSys() {
			return "", errf("$all is not supported on %s", t.Path)
		}
		p := b.ps(t.Path)
		return fmt.Sprintf("COALESCE(jsonb_typeof(%s) = 'array' AND %s @> %s, FALSE)", p, p, b.JSONArg(t.Vals)), nil
	case Size:
		if t.Path.IsSys() {
			return "", errf("$size is not supported on %s", t.Path)
		}
		p := b.ps(t.Path)
		return fmt.Sprintf("COALESCE(jsonb_typeof(%s) = 'array' AND jsonb_array_length(%s) = %s, FALSE)", p, p, b.Arg(t.N)), nil
	case ElemMatch:
		jp := &jpBuilder{vars: map[string]any{}}
		pred, err := jp.pred(t.Sub, "@")
		if err != nil {
			return "", err
		}
		p := b.ps(t.Path)
		path := "lax $[*] ? (" + pred + ")"
		vars, _ := json.Marshal(jp.vars)
		return fmt.Sprintf("COALESCE(jsonb_typeof(%s) = 'array' AND jsonb_path_exists(%s, %s::jsonpath, %s::jsonb), FALSE)",
			p, p, b.Arg(path), b.Arg(string(vars))), nil
	case Regex:
		op := "~"
		if t.CI {
			op = "~*"
		}
		if t.Path.IsSys() {
			col, err := sysTextColumn(t.Path)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("COALESCE(%s %s %s, FALSE)", col, op, b.Arg(t.Pattern)), nil
		}
		return fmt.Sprintf("COALESCE(jsonb_typeof(%s) = 'string' AND (%s) %s %s, FALSE)", b.ps(t.Path), b.pt(t.Path), op, b.Arg(t.Pattern)), nil
	case Prefix:
		like := likePrefix(t.Prefix)
		if t.Path.IsSys() {
			col, err := sysTextColumn(t.Path)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("COALESCE(%s LIKE %s, FALSE)", col, b.Arg(like)), nil
		}
		// 与 prefix 类索引同样的表达式（data #>> ARRAY[...]），jsonb 索引用不上 LIKE
		return fmt.Sprintf("COALESCE(jsonb_typeof(%s) = 'string' AND (%s) LIKE %s, FALSE)", b.ps(t.Path), b.pt(t.Path), b.Arg(like)), nil
	case Search:
		pat := "%" + likeEscape(t.Text) + "%"
		ph := b.Arg(pat)
		idCol := "id"
		if b.DocMode {
			idCol = "(doc ->> '_id')"
		}
		parts := []string{idCol + " ILIKE " + ph}
		for _, f := range t.Fields {
			if f.IsSys() {
				col, err := sysTextColumn(f)
				if err != nil {
					return "", err
				}
				parts = append(parts, fmt.Sprintf("COALESCE(%s ILIKE %s, FALSE)", col, ph))
				continue
			}
			parts = append(parts, fmt.Sprintf("COALESCE(jsonb_typeof(%s) = 'string' AND (%s) ILIKE %s, FALSE)", b.ps(f), b.pt(f), ph))
		}
		return "(" + strings.Join(parts, " OR ") + ")", nil
	case Mod:
		if t.Path.IsSys() {
			if t.Path.Sys != "revision" {
				return "", errf("$mod is not supported on %s", t.Path)
			}
			return fmt.Sprintf("mod(revision, %s) = %s", b.Arg(t.Div), b.Arg(t.Rem)), nil
		}
		p := b.ps(t.Path)
		return fmt.Sprintf("COALESCE(jsonb_typeof(%s) = 'number' AND mod(trunc((%s)::numeric), %s::numeric) = %s::numeric, FALSE)",
			p, p, b.Arg(t.Div), b.Arg(t.Rem)), nil
	case ExprCond:
		s, err := b.ExprSQL(t.E)
		if err != nil {
			return "", err
		}
		return "COALESCE(" + truthySQL(s) + ", FALSE)", nil
	}
	return "", errf("unsupported filter node %T", n)
}

func (b *Builder) join(kids []Node, sep string) (string, error) {
	parts := make([]string, 0, len(kids))
	for _, k := range kids {
		s, err := b.where(k)
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	return "(" + strings.Join(parts, sep) + ")", nil
}

func boolSQL(v bool) string {
	if v {
		return "TRUE"
	}
	return "FALSE"
}

func allScalarNonNull(vs []any) bool {
	for _, v := range vs {
		switch v.(type) {
		case nil, map[string]any, []any:
			return false
		}
	}
	return true
}

// jsonType 返回 Go 值对应的 jsonb_typeof 名字。
func jsonType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	}
	if num.IsNumber(v) {
		return "number"
	}
	return "unknown"
}

// cmp 编译文档路径上的比较。
func (b *Builder) cmp(t Cmp) (string, error) {
	p := b.ps(t.Path)
	switch t.Op {
	case "eq":
		if t.Val == nil {
			return fmt.Sprintf("((%s) IS NULL OR %s = 'null'::jsonb)", p, p), nil
		}
		if b.isArray(t.Path) && jsonType(t.Val) != "array" && jsonType(t.Val) != "object" {
			return fmt.Sprintf("COALESCE(%s @> %s, FALSE)", p, b.JSONArg([]any{t.Val})), nil
		}
		return fmt.Sprintf("COALESCE(%s = %s, FALSE)", p, b.JSONArg(t.Val)), nil
	case "ne":
		eq, err := b.cmp(Cmp{Path: t.Path, Op: "eq", Val: t.Val})
		if err != nil {
			return "", err
		}
		return "NOT " + eq, nil
	case "gt", "gte", "lt", "lte":
		if b.isArray(t.Path) {
			return "", errf("%s is declared as an array; use $elemMatch to compare its elements", t.Path)
		}
		ty := jsonType(t.Val)
		if ty != "number" && ty != "string" && ty != "boolean" {
			return "", errf("$%s on %s needs a number, string or boolean", t.Op, t.Path)
		}
		return fmt.Sprintf("COALESCE(jsonb_typeof(%s) = %s AND %s %s %s, FALSE)", p, Lit(ty), p, sqlOp(t.Op), b.JSONArg(t.Val)), nil
	}
	return "", errf("unsupported comparison %s", t.Op)
}

func sqlOp(op string) string {
	switch op {
	case "gt":
		return ">"
	case "gte":
		return ">="
	case "lt":
		return "<"
	case "lte":
		return "<="
	case "ne":
		return "<>"
	}
	return "="
}

// sysColumn 返回系统字段的列和是否可空。
func sysColumn(sys string) (string, bool) {
	switch sys {
	case "id":
		return "id", false
	case "revision":
		return "revision", false
	case "createdAt":
		return "created_at", false
	case "updatedAt":
		return "updated_at", false
	case "sourceId":
		return "source_id", true
	case "sourceTime":
		return "source_time", true
	case "locked":
		return LockedSQL, false
	}
	return "", false
}

// LockedSQL 是“记录当前被租约锁住”的条件（第 8.4 节；到期判断用 clock_timestamp()）。
const LockedSQL = "(lock_expires_at IS NOT NULL AND lock_expires_at > clock_timestamp())"

func sysTextColumn(p Path) (string, error) {
	switch p.Sys {
	case "id":
		return "id", nil
	case "sourceId":
		return "source_id", nil
	}
	return "", errf("text operators are only supported on _id and _meta.sourceId, not %s", p)
}

func isTimeSys(sys string) bool {
	return sys == "createdAt" || sys == "updatedAt" || sys == "sourceTime"
}

// msTS 返回毫秒数对应的 timestamptz 表达式（精确：epoch + n × 1 毫秒，不经过浮点）。
func (b *Builder) msTS(ms *big.Int) string {
	return "('epoch'::timestamptz + " + b.Arg(ms.String()) + "::bigint * interval '1 millisecond')"
}

// sysCmp 编译系统字段上的比较。值类型不对时不匹配（与 Go 求值一致）。时间字段按毫秒区间比较：
// 记录视图里的时间是截断到毫秒的值，T > v ⇔ ts ≥ (⌊v⌋+1) ms，这样既与 Go 求值一致，又能用上 updated_at 索引。
func (b *Builder) sysCmp(p Path, op string, v any) (string, error) {
	col, nullable := sysColumn(p.Sys)
	if v == nil {
		switch op {
		case "eq":
			if nullable {
				return col + " IS NULL", nil
			}
			return "FALSE", nil
		case "ne":
			if nullable {
				return col + " IS NOT NULL", nil
			}
			return "TRUE", nil
		}
		return "", errf("$%s null is not supported", op)
	}
	neg := func(eq string) string {
		if nullable {
			return "(" + col + " IS NULL OR NOT COALESCE(" + eq + ", FALSE))"
		}
		return "NOT COALESCE(" + eq + ", FALSE)"
	}
	switch p.Sys {
	case "id", "sourceId":
		s, ok := v.(string)
		if !ok {
			if op == "ne" {
				return "TRUE", nil
			}
			return "FALSE", nil
		}
		if op == "ne" {
			return neg(col + " = " + b.Arg(s)), nil
		}
		return fmt.Sprintf("COALESCE(%s %s %s, FALSE)", col, sqlOp(op), b.Arg(s)), nil
	case "locked":
		bv, ok := v.(bool)
		if !ok || (op != "eq" && op != "ne") {
			return "", errf("_meta.locked only supports equality with true or false")
		}
		if (op == "eq") == bv {
			return LockedSQL, nil
		}
		return "NOT " + LockedSQL, nil
	case "revision":
		r, ok := num.Rat(v)
		if !ok {
			if op == "ne" {
				return "TRUE", nil
			}
			return "FALSE", nil
		}
		ph := b.Arg(r.FloatString(20)) + "::numeric"
		if op == "ne" {
			return neg("revision = " + ph), nil
		}
		return fmt.Sprintf("revision %s %s", sqlOp(op), ph), nil
	}
	if isTimeSys(p.Sys) {
		r, ok := num.Rat(v)
		if !ok {
			if op == "ne" {
				return "TRUE", nil
			}
			return "FALSE", nil
		}
		floor, ceil := ratFloor(r), ratCeil(r)
		one := big.NewInt(1)
		switch op {
		case "eq", "ne":
			var eq string
			if !r.IsInt() {
				eq = "FALSE"
			} else {
				eq = fmt.Sprintf("(%s >= %s AND %s < %s)", col, b.msTS(floor), col, b.msTS(new(big.Int).Add(floor, one)))
			}
			if op == "ne" {
				return neg(eq), nil
			}
			return "COALESCE(" + eq + ", FALSE)", nil
		case "gt":
			return fmt.Sprintf("COALESCE(%s >= %s, FALSE)", col, b.msTS(new(big.Int).Add(floor, one))), nil
		case "gte":
			return fmt.Sprintf("COALESCE(%s >= %s, FALSE)", col, b.msTS(ceil)), nil
		case "lt":
			return fmt.Sprintf("COALESCE(%s < %s, FALSE)", col, b.msTS(ceil)), nil
		case "lte":
			return fmt.Sprintf("COALESCE(%s < %s, FALSE)", col, b.msTS(new(big.Int).Add(floor, one))), nil
		}
	}
	return "", errf("unsupported comparison on %s", p)
}

func (b *Builder) sysType(p Path, types []string) (string, error) {
	_, nullable := sysColumn(p.Sys)
	col, _ := sysColumn(p.Sys)
	var parts []string
	for _, ty := range types {
		switch {
		case ty == "null" && nullable:
			parts = append(parts, col+" IS NULL")
		case ty == "string" && (p.Sys == "id" || p.Sys == "sourceId"):
			parts = append(parts, col+" IS NOT NULL")
		case (ty == "number" || ty == "integer") && (p.Sys == "revision" || isTimeSys(p.Sys)):
			parts = append(parts, col+" IS NOT NULL")
		case ty == "boolean" && p.Sys == "locked":
			parts = append(parts, "TRUE")
		}
	}
	if len(parts) == 0 {
		return "FALSE", nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", nil
}

func ratFloor(r *big.Rat) *big.Int {
	q := new(big.Int).Quo(r.Num(), r.Denom()) // 向零取整
	if r.Sign() < 0 && !r.IsInt() {
		q.Sub(q, big.NewInt(1))
	}
	return q
}

func ratCeil(r *big.Rat) *big.Int {
	q := new(big.Int).Quo(r.Num(), r.Denom())
	if r.Sign() > 0 && !r.IsInt() {
		q.Add(q, big.NewInt(1))
	}
	return q
}

// likeEscape 转义 LIKE 的通配符（默认转义字符是反斜杠）。
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func likePrefix(s string) string { return likeEscape(s) + "%" }

// ---------------- $elemMatch → jsonpath ----------------

// jpBuilder 把 $elemMatch 的子条件编译成 jsonpath 谓词（lax 模式，作用在数组值上：lax $[*] ? (...)）。
// 比较值放在 vars 里（$v1、$v2…），jsonpath 文本整体作为 SQL 参数传入。语义见 eval.go 的 jpEval。
type jpBuilder struct {
	vars map[string]any
	n    int
}

func (j *jpBuilder) v(val any) string {
	j.n++
	name := fmt.Sprintf("v%d", j.n)
	j.vars[name] = val
	return "$" + name
}

// jpKey 把一个键写成 jsonpath 的成员访问（."key"，反斜杠和双引号转义）。
func jpKey(k string) string {
	return `."` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(k) + `"`
}

func jpPath(base string, p Path) string {
	var sb strings.Builder
	sb.WriteString(base)
	for _, s := range p.Segs {
		sb.WriteString(jpKey(s))
	}
	return sb.String()
}

// jsonpath 的谓词位置不接受字面量 true / false，用恒等比较代替。
const (
	jpTrue  = "(1 == 1)"
	jpFalse = "(1 == 2)"
)

func jpScalar(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return false
	}
	return true
}

func (j *jpBuilder) pred(n Node, base string) (string, error) {
	switch t := n.(type) {
	case And:
		if len(t.Kids) == 0 {
			return jpTrue, nil
		}
		return j.join(t.Kids, base, " && ")
	case Or:
		return j.join(t.Kids, base, " || ")
	case Nor:
		s, err := j.join(t.Kids, base, " || ")
		if err != nil {
			return "", err
		}
		return "!(" + s + ")", nil
	case Not:
		s, err := j.pred(t.Kid, base)
		if err != nil {
			return "", err
		}
		return "!(" + s + ")", nil
	case Cmp:
		x := jpPath(base, t.Path)
		if !jpScalar(t.Val) {
			return "", errf("$elemMatch cannot compare %s with an object or array", t.Path)
		}
		switch t.Op {
		case "eq":
			if t.Val == nil {
				return fmt.Sprintf("(!exists(%s) || %s == null)", x, x), nil
			}
			return fmt.Sprintf("%s == %s", x, j.v(t.Val)), nil
		case "ne":
			if t.Val == nil {
				return fmt.Sprintf("(exists(%s) && !exists(%s ? (@ == null)))", x, x), nil
			}
			return fmt.Sprintf("!exists(%s ? (@ == %s))", x, j.v(t.Val)), nil
		case "gt", "gte", "lt", "lte":
			ty := jsonType(t.Val)
			if ty != "number" && ty != "string" && ty != "boolean" {
				return "", errf("$%s needs a number, string or boolean", t.Op)
			}
			return fmt.Sprintf("%s %s %s", x, sqlOp(t.Op), j.v(t.Val)), nil
		}
	case In:
		x := jpPath(base, t.Path)
		var alts []string
		hasNull := false
		for _, v := range t.Vals {
			if !jpScalar(v) {
				return "", errf("$elemMatch cannot compare %s with an object or array", t.Path)
			}
			if v == nil {
				hasNull = true
				continue
			}
			alts = append(alts, "@ == "+j.v(v))
		}
		if !t.Neg {
			var parts []string
			if len(alts) > 0 {
				parts = append(parts, fmt.Sprintf("exists(%s ? (%s))", x, strings.Join(alts, " || ")))
			}
			if hasNull {
				parts = append(parts, fmt.Sprintf("(!exists(%s) || %s == null)", x, x))
			}
			if len(parts) == 0 {
				return jpFalse, nil
			}
			return "(" + strings.Join(parts, " || ") + ")", nil
		}
		if hasNull {
			alts = append(alts, "@ == null")
			return fmt.Sprintf("(exists(%s) && !exists(%s ? (%s)))", x, x, strings.Join(alts, " || ")), nil
		}
		if len(alts) == 0 {
			return jpTrue, nil
		}
		return fmt.Sprintf("!exists(%s ? (%s))", x, strings.Join(alts, " || ")), nil
	case Exists:
		x := jpPath(base, t.Path)
		if t.Want {
			return "exists(" + x + ")", nil
		}
		return "!exists(" + x + ")", nil
	case TypeIs:
		x := jpPath(base, t.Path)
		var parts []string
		for _, ty := range t.Types {
			if ty == "integer" {
				parts = append(parts, fmt.Sprintf(`(%s.type() == "number" && %s == %s.floor())`, x, x, x))
				continue
			}
			parts = append(parts, fmt.Sprintf(`%s.type() == "%s"`, x, ty))
		}
		return "(" + strings.Join(parts, " || ") + ")", nil
	case Size:
		x := jpPath(base, t.Path)
		return fmt.Sprintf(`(%s.type() == "array" && %s.size() == %s)`, x, x, j.v(t.N)), nil
	case ElemMatch:
		x := jpPath(base, t.Path)
		sub, err := j.pred(t.Sub, "@")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(`(%s.type() == "array" && exists(%s[*] ? (%s)))`, x, x, sub), nil
	case Prefix:
		x := jpPath(base, t.Path)
		return fmt.Sprintf("%s starts with %s", x, j.v(t.Prefix)), nil
	case Mod:
		x := jpPath(base, t.Path)
		return fmt.Sprintf(`(%s.type() == "number" && (%s - (%s %% 1)) %% %s == %s)`, x, x, x, j.v(t.Div), j.v(t.Rem)), nil
	case All:
		return "", errf("$all is not supported inside $elemMatch")
	case Regex:
		return "", errf("$regex is not supported inside $elemMatch")
	}
	return "", errf("unsupported condition inside $elemMatch: %T", n)
}

func (j *jpBuilder) join(kids []Node, base, sep string) (string, error) {
	parts := make([]string, 0, len(kids))
	for _, k := range kids {
		s, err := j.pred(k, base)
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	return "(" + strings.Join(parts, sep) + ")", nil
}

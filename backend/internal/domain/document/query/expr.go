package query

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"backend/internal/domain/document/num"
)

// 表达式的复杂度上限（docs/collection-crud/design.md 第 5.5 节）。
const (
	MaxExprNodes     = 64
	MaxExprDepth     = 8
	MaxConcatBytes   = 65536
	MaxSwitchBranch  = 16
	defaultTrimChars = " \t\n\r\v\f"
)

// Expr 是白名单表达式的 AST。
type Expr interface{ isExpr() }

type (
	// FieldRef 是 "$path"。
	FieldRef struct{ Path Path }
	// Literal 是常量。
	Literal struct{ Val any }
	// ArrayExpr 是数组字面量（元素可以是表达式）。
	ArrayExpr struct{ Items []Expr }
	// OpExpr 是运算符调用；Named 存放对象形式的参数（$dateTrunc、$cond、$switch 等）。
	OpExpr struct {
		Op    string
		Args  []Expr
		Named map[string]Expr
		// Branches 是 $switch 的分支
		Branches [][2]Expr
	}
)

func (FieldRef) isExpr()  {}
func (Literal) isExpr()   {}
func (ArrayExpr) isExpr() {}
func (OpExpr) isExpr()    {}

// 运算符的参数个数：-1 表示 n 元（≥1），其余是固定个数。对象形式的运算符单独处理。
var exprArity = map[string]int{
	"$add": -1, "$subtract": 2, "$multiply": -1, "$divide": 2, "$mod": 2, "$abs": 1, "$floor": 1, "$ceil": 1,
	"$eq": 2, "$ne": 2, "$gt": 2, "$gte": 2, "$lt": 2, "$lte": 2,
	"$and": -1, "$or": -1, "$not": 1, "$in": 2,
	"$ifNull": -1, "$concat": -1, "$toLower": 1, "$toUpper": 1, "$substrCP": 3, "$strLenCP": 1,
	"$toNumber": 1, "$toString": 1, "$type": 1,
	"$size": 1, "$arrayElemAt": 2, "$first": 1, "$last": 1,
}

// 对象形式参数的运算符及其允许的键（必填键在前面的集合里）。
var exprNamed = map[string]struct{ required, optional []string }{
	"$dateTrunc":     {[]string{"date", "unit"}, []string{"timezone"}},
	"$dateToParts":   {[]string{"date"}, []string{"timezone"}},
	"$dateFromParts": {[]string{"year"}, []string{"month", "day", "hour", "minute", "second", "millisecond", "timezone"}},
	"$trim":          {[]string{"input"}, []string{"chars"}},
	"$round":         {nil, nil}, // 数组形式：[x] 或 [x, place]
}

var dateUnits = map[string]bool{"minute": true, "hour": true, "day": true, "week": true, "month": true, "year": true}

type exprParser struct{ nodes int }

// ParseExpr 解析表达式。
func ParseExpr(v any) (Expr, error) {
	p := &exprParser{}
	return p.parse(v, 1)
}

func (p *exprParser) parse(v any, depth int) (Expr, error) {
	if depth > MaxExprDepth {
		return nil, errf("expression is nested deeper than %d levels", MaxExprDepth)
	}
	p.nodes++
	if p.nodes > MaxExprNodes {
		return nil, errf("expression has more than %d nodes", MaxExprNodes)
	}
	switch t := v.(type) {
	case string:
		if strings.HasPrefix(t, "$$") {
			return nil, errf("expression variables (%s) are not supported", t)
		}
		if strings.HasPrefix(t, "$") {
			path, err := ParsePath(t[1:])
			if err != nil {
				return nil, &Error{Msg: err.Error()}
			}
			return FieldRef{Path: path}, nil
		}
		return Literal{Val: t}, nil
	case []any:
		items := make([]Expr, 0, len(t))
		for _, x := range t {
			e, err := p.parse(x, depth+1)
			if err != nil {
				return nil, err
			}
			items = append(items, e)
		}
		return ArrayExpr{Items: items}, nil
	case map[string]any:
		if len(t) != 1 {
			return nil, errf("an expression object must have exactly one operator, got %d keys", len(t))
		}
		var op string
		var arg any
		for k, x := range t {
			op, arg = k, x
		}
		if !strings.HasPrefix(op, "$") {
			return nil, errf("object literals are not supported in expressions (wrap them in $literal)")
		}
		return p.op(op, arg, depth)
	}
	return Literal{Val: v}, nil
}

func (p *exprParser) op(op string, arg any, depth int) (Expr, error) {
	switch op {
	case "$literal":
		return Literal{Val: arg}, nil
	case "$cond":
		var parts []any
		switch t := arg.(type) {
		case []any:
			parts = t
		case map[string]any:
			parts = []any{t["if"], t["then"], t["else"]}
			for k := range t {
				if k != "if" && k != "then" && k != "else" {
					return nil, errf("unknown $cond key %q", k)
				}
			}
		}
		if len(parts) != 3 {
			return nil, errf("$cond needs if, then and else")
		}
		args, err := p.list(parts, depth)
		if err != nil {
			return nil, err
		}
		return OpExpr{Op: op, Args: args}, nil
	case "$switch":
		m, ok := arg.(map[string]any)
		if !ok {
			return nil, errf("$switch must be {branches, default?}")
		}
		brs, ok := m["branches"].([]any)
		if !ok || len(brs) == 0 || len(brs) > MaxSwitchBranch {
			return nil, errf("$switch.branches must be an array of 1–%d {case, then}", MaxSwitchBranch)
		}
		out := OpExpr{Op: op}
		for _, b := range brs {
			bm, ok := b.(map[string]any)
			if !ok || len(bm) != 2 {
				return nil, errf("$switch branches must be {case, then}")
			}
			c, err := p.parse(bm["case"], depth+1)
			if err != nil {
				return nil, err
			}
			th, err := p.parse(bm["then"], depth+1)
			if err != nil {
				return nil, err
			}
			out.Branches = append(out.Branches, [2]Expr{c, th})
		}
		if d, ok := m["default"]; ok {
			e, err := p.parse(d, depth+1)
			if err != nil {
				return nil, err
			}
			out.Args = []Expr{e}
		}
		return out, nil
	case "$round":
		arr, ok := arg.([]any)
		if !ok {
			arr = []any{arg}
		}
		if len(arr) < 1 || len(arr) > 2 {
			return nil, errf("$round needs [number] or [number, place]")
		}
		args, err := p.list(arr, depth)
		if err != nil {
			return nil, err
		}
		return OpExpr{Op: op, Args: args}, nil
	}
	if spec, ok := exprNamed[op]; ok {
		m, ok := arg.(map[string]any)
		if !ok {
			return nil, errf("%s needs an object argument", op)
		}
		allowed := map[string]bool{}
		for _, k := range append(append([]string{}, spec.required...), spec.optional...) {
			allowed[k] = true
		}
		out := OpExpr{Op: op, Named: map[string]Expr{}}
		for _, k := range sortedKeys(m) {
			if !allowed[k] {
				return nil, errf("unknown %s key %q", op, k)
			}
			e, err := p.parse(m[k], depth+1)
			if err != nil {
				return nil, err
			}
			out.Named[k] = e
		}
		for _, k := range spec.required {
			if _, ok := out.Named[k]; !ok {
				return nil, errf("%s needs %q", op, k)
			}
		}
		if op == "$dateTrunc" {
			u, ok := out.Named["unit"].(Literal)
			us, _ := u.Val.(string)
			if !ok || !dateUnits[us] {
				return nil, errf("$dateTrunc.unit must be one of minute, hour, day, week, month, year")
			}
		}
		if tz, ok := out.Named["timezone"]; ok {
			l, ok := tz.(Literal)
			s, _ := l.Val.(string)
			if !ok {
				return nil, errf("timezone must be a string constant")
			}
			if _, err := ParseTimezone(s); err != nil {
				return nil, err
			}
		}
		if op == "$trim" {
			if c, ok := out.Named["chars"]; ok {
				if l, ok := c.(Literal); !ok || l.Val == nil {
					return nil, errf("$trim.chars must be a string constant")
				} else if _, ok := l.Val.(string); !ok {
					return nil, errf("$trim.chars must be a string constant")
				}
			}
		}
		return out, nil
	}
	arity, ok := exprArity[op]
	if !ok {
		return nil, errf("unsupported expression operator %s", op)
	}
	arr, isArr := arg.([]any)
	if !isArr {
		arr = []any{arg}
	}
	if arity == -1 && len(arr) == 0 {
		return nil, errf("%s needs at least one argument", op)
	}
	if arity >= 0 && len(arr) != arity {
		return nil, errf("%s needs %d argument(s)", op, arity)
	}
	args, err := p.list(arr, depth)
	if err != nil {
		return nil, err
	}
	return OpExpr{Op: op, Args: args}, nil
}

func (p *exprParser) list(xs []any, depth int) ([]Expr, error) {
	out := make([]Expr, 0, len(xs))
	for _, x := range xs {
		e, err := p.parse(x, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// ---------------- 时区 ----------------

// Timezone 是 $dateTrunc 等的时区：IANA 名字或固定偏移（+08:00）。
type Timezone struct {
	Name   string // IANA 名字（为空表示固定偏移）
	Offset int    // 固定偏移（分钟，东正西负）
	loc    *time.Location
}

var (
	offsetRe = regexp.MustCompile(`^([+-])(\d{2}):?(\d{2})$`)
	tzCache  sync.Map
)

// ParseTimezone 解析时区：IANA 名字（Go 能加载的）或 ±HH:MM。默认 UTC。
func ParseTimezone(s string) (Timezone, error) {
	if s == "" || s == "UTC" || s == "Z" {
		return Timezone{Name: "UTC", loc: time.UTC}, nil
	}
	if m := offsetRe.FindStringSubmatch(s); m != nil {
		h, mi := int(m[2][0]-'0')*10+int(m[2][1]-'0'), int(m[3][0]-'0')*10+int(m[3][1]-'0')
		if h > 14 || mi > 59 {
			return Timezone{}, errf("bad timezone offset %q", s)
		}
		off := h*60 + mi
		if m[1] == "-" {
			off = -off
		}
		return Timezone{Offset: off, loc: time.FixedZone(s, off*60)}, nil
	}
	if v, ok := tzCache.Load(s); ok {
		return v.(Timezone), nil
	}
	if len(s) > 64 || strings.ContainsAny(s, " '\"\\") {
		return Timezone{}, errf("unknown timezone %q", s)
	}
	loc, err := time.LoadLocation(s)
	if err != nil {
		return Timezone{}, errf("unknown timezone %q (use an IANA name such as Asia/Shanghai, or an offset such as +08:00)", s)
	}
	tz := Timezone{Name: s, loc: loc}
	tzCache.Store(s, tz)
	return tz, nil
}

// ---------------- 求值的公共规则 ----------------

// truthy 是 Mongo 的真值规则：false、null、缺失、0 为假，其他（包括空字符串、空数组）为真。
func truthy(v any, present bool) bool {
	if !present || v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	if r, ok := num.Rat(v); ok {
		return r.Sign() != 0
	}
	return true
}

// sortedNamed 返回 Named 的键（稳定顺序）。
func sortedNamed(m map[string]Expr) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func (e OpExpr) String() string { return fmt.Sprintf("%s(%d args)", e.Op, len(e.Args)) }

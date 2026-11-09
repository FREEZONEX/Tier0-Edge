package query

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"backend/internal/domain/document/num"
)

// 过滤器的复杂度上限（docs/collection-crud/design.md 第 4.3 节）。
const (
	MaxFilterNodes   = 128
	MaxFilterDepth   = 8
	MaxInValues      = 1000
	MaxRegexLen      = 256
	MaxElemMatchNest = 2
	MaxSearchFields  = 8
)

// Node 是过滤条件的 AST 节点。
type Node interface{ isNode() }

type (
	// And / Or / Nor 是逻辑组合；空的 And 匹配全部。
	And struct{ Kids []Node }
	Or  struct{ Kids []Node }
	Nor struct{ Kids []Node }
	// Not 是字段级的 $not：子条件不成立（含字段缺失）时匹配。
	Not struct{ Kid Node }
	// Cmp 是比较：eq ne gt gte lt lte。
	Cmp struct {
		Path Path
		Op   string
		Val  any
	}
	// In 是 $in（Neg 为 $nin）。
	In struct {
		Path Path
		Vals []any
		Neg  bool
	}
	// Exists 是 $exists（字段存在，值为 null 也算存在）。
	Exists struct {
		Path Path
		Want bool
	}
	// TypeIs 是 $type：null boolean number integer string array object。
	TypeIs struct {
		Path  Path
		Types []string
	}
	// All 是 $all（数组包含全部这些元素）。
	All struct {
		Path Path
		Vals []any
	}
	// Size 是 $size（数组长度）。
	Size struct {
		Path Path
		N    int64
	}
	// ElemMatch 是 $elemMatch：数组里至少有一个元素满足 Sub；Sub 里的路径相对于元素（空路径表示元素本身）。
	ElemMatch struct {
		Path Path
		Sub  Node
	}
	// Regex 是 $regex（PG POSIX 正则；CI 为 $options:"i"）。
	Regex struct {
		Path    Path
		Pattern string
		CI      bool
	}
	// Prefix 是 $prefix（扩展：字符串前缀）。
	Prefix struct {
		Path   Path
		Prefix string
	}
	// Search 是 $search（扩展：_id 和指定字段的不区分大小写子串匹配）。
	Search struct {
		Text   string
		Fields []Path
	}
	// Mod 是 $mod：[除数, 余数]，值和除数都先向零取整（与 Mongo 一致）。
	Mod struct {
		Path     Path
		Div, Rem int64
	}
	// ExprCond 是 $expr：白名单表达式（第 5.5 节）为真时匹配。
	ExprCond struct{ E Expr }
)

func (And) isNode()       {}
func (Or) isNode()        {}
func (Nor) isNode()       {}
func (Not) isNode()       {}
func (Cmp) isNode()       {}
func (In) isNode()        {}
func (Exists) isNode()    {}
func (TypeIs) isNode()    {}
func (All) isNode()       {}
func (Size) isNode()      {}
func (ElemMatch) isNode() {}
func (Regex) isNode()     {}
func (Prefix) isNode()    {}
func (Search) isNode()    {}
func (Mod) isNode()       {}
func (ExprCond) isNode()  {}

// Error 是过滤器 / 表达式 / 更新写法错误（API 返回 400）。
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func errf(format string, a ...any) error { return &Error{Msg: fmt.Sprintf(format, a...)} }

var jsonTypes = map[string]bool{"null": true, "boolean": true, "number": true, "integer": true, "string": true, "array": true, "object": true}

type parser struct {
	nodes int
	// elem 为真时在解析 $elemMatch 的子条件：路径相对于元素
	elem int
}

func (p *parser) count() error {
	p.nodes++
	if p.nodes > MaxFilterNodes {
		return errf("filter has more than %d conditions", MaxFilterNodes)
	}
	return nil
}

// ParseFilter 解析过滤条件（JSON 对象，数字为 json.Number 或 float64）。nil / 空对象匹配全部。
func ParseFilter(v any) (Node, error) {
	if v == nil {
		return And{}, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errf("filter must be an object")
	}
	p := &parser{}
	return p.object(m, 1)
}

// ParseFilterJSON 解析 JSON 文本形式的过滤条件。
func ParseFilterJSON(raw []byte) (Node, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return And{}, nil
	}
	var v any
	if err := num.Decode(raw, &v); err != nil {
		return nil, errf("filter is not valid JSON: %v", err)
	}
	return ParseFilter(v)
}

func sortedKeys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func (p *parser) object(m map[string]any, depth int) (Node, error) {
	if depth > MaxFilterDepth {
		return nil, errf("filter is nested deeper than %d levels", MaxFilterDepth)
	}
	var kids []Node
	for _, k := range sortedKeys(m) {
		v := m[k]
		switch k {
		case "$and", "$or", "$nor":
			arr, ok := v.([]any)
			if !ok || len(arr) == 0 {
				return nil, errf("%s must be a non-empty array of filters", k)
			}
			var sub []Node
			for _, x := range arr {
				xm, ok := x.(map[string]any)
				if !ok {
					return nil, errf("%s must be an array of objects", k)
				}
				n, err := p.object(xm, depth+1)
				if err != nil {
					return nil, err
				}
				sub = append(sub, n)
			}
			if err := p.count(); err != nil {
				return nil, err
			}
			switch k {
			case "$and":
				kids = append(kids, And{Kids: sub})
			case "$or":
				kids = append(kids, Or{Kids: sub})
			default:
				kids = append(kids, Nor{Kids: sub})
			}
		case "$expr":
			if p.elem > 0 {
				return nil, errf("$expr is not allowed inside $elemMatch")
			}
			e, err := ParseExpr(v)
			if err != nil {
				return nil, err
			}
			if err := p.count(); err != nil {
				return nil, err
			}
			kids = append(kids, ExprCond{E: e})
		case "$search":
			if p.elem > 0 {
				return nil, errf("$search is not allowed inside $elemMatch")
			}
			n, err := parseSearch(v)
			if err != nil {
				return nil, err
			}
			if err := p.count(); err != nil {
				return nil, err
			}
			kids = append(kids, n)
		case "$comment":
		default:
			if strings.HasPrefix(k, "$") {
				return nil, errf("unsupported top-level operator %s", k)
			}
			path, err := p.path(k)
			if err != nil {
				return nil, err
			}
			n, err := p.field(path, v, depth)
			if err != nil {
				return nil, err
			}
			kids = append(kids, n)
		}
	}
	if len(kids) == 1 {
		return kids[0], nil
	}
	return And{Kids: kids}, nil
}

func (p *parser) path(k string) (Path, error) {
	path, err := ParsePath(k)
	if err != nil {
		return Path{}, &Error{Msg: err.Error()}
	}
	if p.elem > 0 && path.IsSys() {
		return Path{}, errf("system field %s is not allowed inside $elemMatch", k)
	}
	return path, nil
}

// isOperatorObject 判断值是否是运算符对象（所有键都以 $ 开头）；键混用时报错。
func isOperatorObject(v any) (map[string]any, bool, error) {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil, false, nil
	}
	ops, plain := 0, 0
	for k := range m {
		if strings.HasPrefix(k, "$") {
			ops++
		} else {
			plain++
		}
	}
	if ops > 0 && plain > 0 {
		return nil, false, errf("cannot mix operators and field names in one object")
	}
	return m, ops > 0, nil
}

// field 解析一个字段上的条件：值是运算符对象时逐个运算符 AND，否则是等值。
func (p *parser) field(path Path, v any, depth int) (Node, error) {
	m, isOps, err := isOperatorObject(v)
	if err != nil {
		return nil, err
	}
	if !isOps {
		if err := p.count(); err != nil {
			return nil, err
		}
		return Cmp{Path: path, Op: "eq", Val: v}, nil
	}
	var kids []Node
	for _, op := range sortedKeys(m) {
		arg := m[op]
		if op == "$options" {
			if _, ok := m["$regex"]; !ok {
				return nil, errf("$options requires $regex")
			}
			continue
		}
		if err := p.count(); err != nil {
			return nil, err
		}
		n, err := p.operator(path, op, arg, m, depth)
		if err != nil {
			return nil, err
		}
		kids = append(kids, n)
	}
	if len(kids) == 1 {
		return kids[0], nil
	}
	return And{Kids: kids}, nil
}

func (p *parser) operator(path Path, op string, arg any, all map[string]any, depth int) (Node, error) {
	switch op {
	case "$eq", "$ne":
		return Cmp{Path: path, Op: op[1:], Val: arg}, nil
	case "$gt", "$gte", "$lt", "$lte":
		switch arg.(type) {
		case string, bool:
		default:
			if !num.IsNumber(arg) {
				return nil, errf("%s on %s needs a number, string or boolean", op, path)
			}
		}
		return Cmp{Path: path, Op: op[1:], Val: arg}, nil
	case "$in", "$nin":
		arr, ok := arg.([]any)
		if !ok {
			return nil, errf("%s on %s must be an array", op, path)
		}
		if len(arr) > MaxInValues {
			return nil, errf("%s on %s has more than %d values", op, path, MaxInValues)
		}
		return In{Path: path, Vals: arr, Neg: op == "$nin"}, nil
	case "$exists":
		b, ok := arg.(bool)
		if !ok {
			return nil, errf("$exists on %s must be true or false", path)
		}
		return Exists{Path: path, Want: b}, nil
	case "$type":
		var ts []string
		switch t := arg.(type) {
		case string:
			ts = []string{t}
		case []any:
			for _, x := range t {
				s, ok := x.(string)
				if !ok {
					return nil, errf("$type on %s must be a type name or an array of type names", path)
				}
				ts = append(ts, s)
			}
		default:
			return nil, errf("$type on %s must be a type name or an array of type names", path)
		}
		for _, t := range ts {
			if !jsonTypes[t] {
				return nil, errf("$type %q is not one of null, boolean, number, integer, string, array, object", t)
			}
		}
		return TypeIs{Path: path, Types: ts}, nil
	case "$all":
		arr, ok := arg.([]any)
		if !ok || len(arr) == 0 {
			return nil, errf("$all on %s must be a non-empty array", path)
		}
		if len(arr) > MaxInValues {
			return nil, errf("$all on %s has more than %d values", path, MaxInValues)
		}
		return All{Path: path, Vals: arr}, nil
	case "$size":
		n, ok := intArg(arg)
		if !ok || n < 0 {
			return nil, errf("$size on %s must be a non-negative integer", path)
		}
		return Size{Path: path, N: n}, nil
	case "$elemMatch":
		if path.IsSys() {
			return nil, errf("$elemMatch is not allowed on system field %s", path)
		}
		if p.elem >= MaxElemMatchNest {
			return nil, errf("$elemMatch is nested deeper than %d levels", MaxElemMatchNest)
		}
		m, ok := arg.(map[string]any)
		if !ok || len(m) == 0 {
			return nil, errf("$elemMatch on %s must be a non-empty object", path)
		}
		p.elem++
		defer func() { p.elem-- }()
		_, isOps, err := isOperatorObject(m)
		if err != nil {
			return nil, err
		}
		var sub Node
		if isOps {
			// 元素本身的条件：{$gte: 80, $lt: 85}
			sub, err = p.field(Path{Raw: "@"}, m, depth+1)
		} else {
			sub, err = p.object(m, depth+1)
		}
		if err != nil {
			return nil, err
		}
		return ElemMatch{Path: path, Sub: sub}, nil
	case "$regex":
		if p.elem > 0 {
			return nil, errf("$regex is not allowed inside $elemMatch (use $prefix, or filter the array elements in your code)")
		}
		pat, ok := arg.(string)
		if !ok || pat == "" {
			return nil, errf("$regex on %s must be a non-empty string", path)
		}
		if len(pat) > MaxRegexLen {
			return nil, errf("$regex on %s is longer than %d characters", path, MaxRegexLen)
		}
		ci := false
		if o, ok := all["$options"]; ok {
			s, _ := o.(string)
			switch s {
			case "":
			case "i":
				ci = true
			default:
				return nil, errf("$options only supports \"i\"")
			}
		}
		return Regex{Path: path, Pattern: pat, CI: ci}, nil
	case "$prefix":
		s, ok := arg.(string)
		if !ok || s == "" {
			return nil, errf("$prefix on %s must be a non-empty string", path)
		}
		return Prefix{Path: path, Prefix: s}, nil
	case "$mod":
		arr, ok := arg.([]any)
		if !ok || len(arr) != 2 {
			return nil, errf("$mod on %s must be [divisor, remainder]", path)
		}
		d, ok1 := intArg(arr[0])
		r, ok2 := intArg(arr[1])
		if !ok1 || !ok2 || d == 0 {
			return nil, errf("$mod on %s needs a non-zero integer divisor and an integer remainder", path)
		}
		return Mod{Path: path, Div: d, Rem: r}, nil
	case "$not":
		m, isOps, err := isOperatorObject(arg)
		if err != nil {
			return nil, err
		}
		if !isOps {
			return nil, errf("$not on %s must be an operator object such as {\"$gt\": 5}", path)
		}
		kid, err := p.field(path, m, depth+1)
		if err != nil {
			return nil, err
		}
		return Not{Kid: kid}, nil
	}
	return nil, errf("unsupported operator %s on %s", op, path)
}

func intArg(v any) (int64, bool) {
	r, ok := num.Rat(v)
	if !ok || !r.IsInt() || !r.Num().IsInt64() {
		return 0, false
	}
	return r.Num().Int64(), true
}

func parseSearch(v any) (Node, error) {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil, errf("$search must not be empty")
		}
		return Search{Text: t}, nil
	case map[string]any:
		text, _ := t["text"].(string)
		if text == "" {
			return nil, errf("$search.text must be a non-empty string")
		}
		s := Search{Text: text}
		if fs, ok := t["fields"]; ok {
			arr, ok := fs.([]any)
			if !ok || len(arr) > MaxSearchFields {
				return nil, errf("$search.fields must be an array of at most %d paths", MaxSearchFields)
			}
			for _, f := range arr {
				fsx, ok := f.(string)
				if !ok {
					return nil, errf("$search.fields must be paths")
				}
				path, err := ParsePath(fsx)
				if err != nil {
					return nil, &Error{Msg: err.Error()}
				}
				s.Fields = append(s.Fields, path)
			}
		}
		for k := range t {
			if k != "text" && k != "fields" {
				return nil, errf("unknown $search option %q", k)
			}
		}
		return s, nil
	}
	return nil, errf("$search must be a string or {text, fields}")
}

// jsonValue 把过滤值写成 JSON（json.Number 原样保留）。
func jsonValue(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// Paths 收集过滤器里用到的文档路径（不含系统字段），用于判断能否走索引。
func Paths(n Node) []Path {
	var out []Path
	var walk func(Node)
	walk = func(n Node) {
		switch t := n.(type) {
		case And:
			for _, k := range t.Kids {
				walk(k)
			}
		case Or:
			for _, k := range t.Kids {
				walk(k)
			}
		case Nor:
			for _, k := range t.Kids {
				walk(k)
			}
		case Not:
			walk(t.Kid)
		case Cmp:
			out = append(out, t.Path)
		case In:
			out = append(out, t.Path)
		case Exists:
			out = append(out, t.Path)
		case TypeIs:
			out = append(out, t.Path)
		case All:
			out = append(out, t.Path)
		case Size:
			out = append(out, t.Path)
		case ElemMatch:
			out = append(out, t.Path)
		case Regex:
			out = append(out, t.Path)
		case Prefix:
			out = append(out, t.Path)
		case Mod:
			out = append(out, t.Path)
		}
	}
	walk(n)
	return out
}

// CountNodes 返回过滤条件的节点数。
func CountNodes(n Node) int {
	switch t := n.(type) {
	case And:
		c := 0
		for _, k := range t.Kids {
			c += CountNodes(k)
		}
		return c
	case Or:
		c := 1
		for _, k := range t.Kids {
			c += CountNodes(k)
		}
		return c
	case Nor:
		c := 1
		for _, k := range t.Kids {
			c += CountNodes(k)
		}
		return c
	case Not:
		return 1 + CountNodes(t.Kid)
	case ElemMatch:
		return 1 + CountNodes(t.Sub)
	}
	return 1
}

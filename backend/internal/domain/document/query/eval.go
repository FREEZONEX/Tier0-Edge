package query

import (
	"math/big"
	"regexp"
	"strings"
	"sync"

	"backend/internal/domain/document/num"
)

// Doc 是 Go 求值时的文档：data 的顶层字段，加上 _id（字符串）和 _meta（revision、createdAt、updatedAt 为毫秒数，
// sourceId / sourceTime 为空时不出现，locked 为布尔）。与聚合管线里的文档形态相同。
type Doc = map[string]any

// Matcher 在 Go 里对单个文档求值过滤条件，语义与 Builder 编译的 SQL 相同（由差分测试保证）。
type Matcher struct {
	ArrayPath func(Path) bool
}

func (m Matcher) isArray(p Path) bool { return !p.IsSys() && m.ArrayPath != nil && m.ArrayPath(p) }

func get(doc Doc, p Path) (any, bool) { return docPath(p).Get(doc) }

// Match 判断文档是否满足过滤条件。
func (m Matcher) Match(n Node, doc Doc) bool {
	switch t := n.(type) {
	case And:
		for _, k := range t.Kids {
			if !m.Match(k, doc) {
				return false
			}
		}
		return true
	case Or:
		for _, k := range t.Kids {
			if m.Match(k, doc) {
				return true
			}
		}
		return false
	case Nor:
		for _, k := range t.Kids {
			if m.Match(k, doc) {
				return false
			}
		}
		return true
	case Not:
		return !m.Match(t.Kid, doc)
	case Cmp:
		v, ok := get(doc, t.Path)
		return m.cmp(t.Path, t.Op, t.Val, v, ok)
	case In:
		v, ok := get(doc, t.Path)
		hit := false
		for _, x := range t.Vals {
			if m.cmp(t.Path, "eq", x, v, ok) {
				hit = true
				break
			}
		}
		return hit != t.Neg
	case Exists:
		_, ok := get(doc, t.Path)
		return ok == t.Want
	case TypeIs:
		v, ok := get(doc, t.Path)
		if !ok {
			return false
		}
		for _, ty := range t.Types {
			if ty == "integer" {
				if num.IsInteger(v) {
					return true
				}
				continue
			}
			if jsonType(v) == ty {
				return true
			}
		}
		return false
	case All:
		v, ok := get(doc, t.Path)
		arr, isArr := v.([]any)
		return ok && isArr && JSONBContains(arr, t.Vals, true)
	case Size:
		v, _ := get(doc, t.Path)
		arr, isArr := v.([]any)
		return isArr && int64(len(arr)) == t.N
	case ElemMatch:
		v, _ := get(doc, t.Path)
		arr, isArr := v.([]any)
		if !isArr {
			return false
		}
		// lax $[*] ? (...)：本身是数组的元素在过滤前再展开一层（与 PG jsonpath 一致）
		for _, el := range unwrap(arr) {
			if jpEval(t.Sub, el) == triTrue {
				return true
			}
		}
		return false
	case Regex:
		v, ok := get(doc, t.Path)
		s, isStr := v.(string)
		if !ok || !isStr {
			return false
		}
		re := compileRegex(t.Pattern, t.CI)
		return re != nil && re.MatchString(s)
	case Prefix:
		v, _ := get(doc, t.Path)
		s, isStr := v.(string)
		return isStr && strings.HasPrefix(s, t.Prefix)
	case Search:
		needle := strings.ToLower(t.Text)
		if id, _ := doc["_id"].(string); strings.Contains(strings.ToLower(id), needle) {
			return true
		}
		for _, f := range t.Fields {
			v, _ := get(doc, f)
			if s, ok := v.(string); ok && strings.Contains(strings.ToLower(s), needle) {
				return true
			}
		}
		return false
	case Mod:
		v, _ := get(doc, t.Path)
		r, ok := num.Rat(v)
		if !ok {
			return false
		}
		tr := new(big.Int).Quo(r.Num(), r.Denom())
		rem := new(big.Int).Rem(tr, big.NewInt(t.Div))
		return rem.Cmp(big.NewInt(t.Rem)) == 0
	case ExprCond:
		v, ok := EvalExpr(t.E, doc)
		return truthy(v, ok)
	}
	return false
}

func (m Matcher) cmp(p Path, op string, want, v any, ok bool) bool {
	switch op {
	case "eq":
		if want == nil {
			return !ok || v == nil
		}
		if !ok {
			return false
		}
		if m.isArray(p) && jsonType(want) != "array" && jsonType(want) != "object" {
			arr, isArr := v.([]any)
			return isArr && JSONBContains(arr, []any{want}, true)
		}
		return num.Equal(v, want)
	case "ne":
		return !m.cmp(p, "eq", want, v, ok)
	}
	if !ok {
		return false
	}
	c, comparable := compareSameType(v, want)
	if !comparable {
		return false
	}
	switch op {
	case "gt":
		return c > 0
	case "gte":
		return c >= 0
	case "lt":
		return c < 0
	case "lte":
		return c <= 0
	}
	return false
}

// compareSameType 比较两个同类型的标量（数字按十进制、字符串按字节 = 码点、布尔 false < true）。
func compareSameType(a, b any) (int, bool) {
	if c, ok := num.Compare(a, b); ok {
		return c, true
	}
	switch x := a.(type) {
	case string:
		y, ok := b.(string)
		if !ok {
			return 0, false
		}
		return strings.Compare(x, y), true
	case bool:
		y, ok := b.(bool)
		if !ok {
			return 0, false
		}
		switch {
		case x == y:
			return 0, true
		case !x:
			return -1, true
		}
		return 1, true
	}
	return 0, false
}

var regexCache sync.Map

// compileRegex 编译 $regex（Go RE2；PG 的 POSIX 正则支持的少数写法 RE2 不支持，这时 Go 侧视为不匹配）。
func compileRegex(pat string, ci bool) *regexp.Regexp {
	key := pat
	if ci {
		key = "(?i)" + pat
	}
	if v, ok := regexCache.Load(key); ok {
		re, _ := v.(*regexp.Regexp)
		return re
	}
	re, err := regexp.Compile(key)
	if err != nil {
		re = nil
	}
	regexCache.Store(key, re)
	return re
}

// JSONBContains 实现 PG jsonb 的 @> 语义：对象按键递归包含，数组要求右边每个元素被左边某个元素包含
// （标量元素只和标量元素比较相等，容器元素递归比较）；top 为真时左边是数组、右边是标量也算包含（PG 的顶层特例）。
func JSONBContains(a, b any, top bool) bool {
	switch y := b.(type) {
	case map[string]any:
		x, ok := a.(map[string]any)
		if !ok {
			return false
		}
		for k, yv := range y {
			xv, ok := x[k]
			if !ok || !JSONBContains(xv, yv, false) {
				return false
			}
		}
		return true
	case []any:
		x, ok := a.([]any)
		if !ok {
			return false
		}
		for _, e := range y {
			found := false
			for _, f := range x {
				if isContainer(e) != isContainer(f) {
					continue
				}
				if isContainer(e) {
					if JSONBContains(f, e, false) {
						found = true
						break
					}
				} else if num.Equal(f, e) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	if x, ok := a.([]any); ok && top {
		for _, f := range x {
			if !isContainer(f) && num.Equal(f, b) {
				return true
			}
		}
		return false
	}
	if isContainer(a) {
		return false
	}
	return num.Equal(a, b)
}

func isContainer(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}

// ---------------- $elemMatch：PG jsonpath（lax）的三值语义 ----------------

type tri int8

const (
	triFalse tri = iota
	triTrue
	triUnknown
)

func triNot(t tri) tri {
	switch t {
	case triTrue:
		return triFalse
	case triFalse:
		return triTrue
	}
	return triUnknown
}

func triAnd(a, b tri) tri {
	if a == triFalse || b == triFalse {
		return triFalse
	}
	if a == triTrue && b == triTrue {
		return triTrue
	}
	return triUnknown
}

func triOr(a, b tri) tri {
	if a == triTrue || b == triTrue {
		return triTrue
	}
	if a == triFalse && b == triFalse {
		return triFalse
	}
	return triUnknown
}

func triOf(b bool) tri {
	if b {
		return triTrue
	}
	return triFalse
}

// jpSeq 是 lax 模式下的路径求值：成员访问作用在对象上；遇到数组时对元素展开一层再访问；标量上的访问得到空序列。
func jpSeq(item any, p Path) []any {
	cur := []any{item}
	for _, seg := range p.Segs {
		var next []any
		for _, v := range cur {
			switch t := v.(type) {
			case map[string]any:
				if x, ok := t[seg]; ok {
					next = append(next, x)
				}
			case []any:
				for _, e := range t {
					if m, ok := e.(map[string]any); ok {
						if x, ok := m[seg]; ok {
							next = append(next, x)
						}
					}
				}
			}
		}
		cur = next
	}
	return cur
}

// unwrap 是比较 / 过滤时 lax 模式对数组展开一层。
func unwrap(seq []any) []any {
	var out []any
	for _, v := range seq {
		if arr, ok := v.([]any); ok {
			out = append(out, arr...)
		} else {
			out = append(out, v)
		}
	}
	return out
}

// jpCompare 比较一个 jsonpath 项和比较值：同类型标量可比；null 只有 == / != 有结果，排序比较为 false；其他组合是错误（unknown）。
func jpCompare(y any, op string, v any) (bool, bool) {
	if y == nil || v == nil {
		switch op {
		case "eq":
			return y == nil && v == nil, true
		case "ne":
			return !(y == nil && v == nil), true
		}
		return false, true
	}
	if isContainer(y) || isContainer(v) {
		return false, false
	}
	c, ok := compareSameType(y, v)
	if !ok {
		return false, false
	}
	switch op {
	case "eq":
		return c == 0, true
	case "ne":
		return c != 0, true
	case "gt":
		return c > 0, true
	case "gte":
		return c >= 0, true
	case "lt":
		return c < 0, true
	case "lte":
		return c <= 0, true
	}
	return false, false
}

// jpCmpSeq 是序列与常量的比较：有一项为真 → 真；否则有错误 → unknown；否则假。
func jpCmpSeq(seq []any, op string, v any) tri {
	errored := false
	for _, y := range unwrap(seq) {
		r, ok := jpCompare(y, op, v)
		if !ok {
			errored = true
			continue
		}
		if r {
			return triTrue
		}
	}
	if errored {
		return triUnknown
	}
	return triFalse
}

// jpFilterExists 是 exists(X ? (pred))：X 展开一层后，有一项使 pred 为真。
func jpFilterExists(seq []any, pred func(any) tri) bool {
	for _, y := range unwrap(seq) {
		if pred(y) == triTrue {
			return true
		}
	}
	return false
}

func jpTypeIs(seq []any, ty string) bool {
	for _, y := range seq {
		if jsonType(y) == ty {
			return true
		}
	}
	return false
}

// jpEval 求值 $elemMatch 的子条件（item 是数组的一个元素），与 sql.go 的 jpBuilder 生成的 jsonpath 一一对应。
func jpEval(n Node, item any) tri {
	switch t := n.(type) {
	case And:
		r := triTrue
		for _, k := range t.Kids {
			r = triAnd(r, jpEval(k, item))
		}
		return r
	case Or:
		r := triFalse
		for _, k := range t.Kids {
			r = triOr(r, jpEval(k, item))
		}
		return r
	case Nor:
		r := triFalse
		for _, k := range t.Kids {
			r = triOr(r, jpEval(k, item))
		}
		return triNot(r)
	case Not:
		return triNot(jpEval(t.Kid, item))
	case Cmp:
		seq := jpSeq(item, t.Path)
		switch t.Op {
		case "eq":
			if t.Val == nil {
				return triOr(triOf(len(seq) == 0), jpCmpSeq(seq, "eq", nil))
			}
			return jpCmpSeq(seq, "eq", t.Val)
		case "ne":
			if t.Val == nil {
				return triOf(len(seq) > 0 && !jpFilterExists(seq, func(y any) tri { return triOf(y == nil) }))
			}
			return triOf(!jpFilterExists(seq, func(y any) tri { r, ok := jpCompare(y, "eq", t.Val); return okTri(r, ok) }))
		}
		return jpCmpSeq(seq, t.Op, t.Val)
	case In:
		seq := jpSeq(item, t.Path)
		hasNull := false
		var vals []any
		for _, v := range t.Vals {
			if v == nil {
				hasNull = true
			} else {
				vals = append(vals, v)
			}
		}
		anyEq := func(y any) tri {
			r := triFalse
			for _, v := range vals {
				x, ok := jpCompare(y, "eq", v)
				r = triOr(r, okTri(x, ok))
			}
			return r
		}
		if !t.Neg {
			res := triFalse
			if len(vals) > 0 {
				res = triOf(jpFilterExists(seq, anyEq))
			}
			if hasNull {
				res = triOr(res, triOr(triOf(len(seq) == 0), jpCmpSeq(seq, "eq", nil)))
			}
			return res
		}
		if hasNull {
			withNull := func(y any) tri { return triOr(anyEq(y), triOf(y == nil)) }
			return triOf(len(seq) > 0 && !jpFilterExists(seq, withNull))
		}
		if len(vals) == 0 {
			return triTrue
		}
		return triOf(!jpFilterExists(seq, anyEq))
	case Exists:
		return triOf((len(jpSeq(item, t.Path)) > 0) == t.Want)
	case TypeIs:
		seq := jpSeq(item, t.Path)
		r := triFalse
		for _, ty := range t.Types {
			if ty == "integer" {
				isNum := jpTypeIs(seq, "number")
				intEq := triFalse
				if isNum {
					for _, y := range unwrap(seq) {
						if num.IsInteger(y) {
							intEq = triTrue
						}
					}
				}
				r = triOr(r, triAnd(triOf(isNum), intEq))
				continue
			}
			r = triOr(r, triOf(jpTypeIs(seq, ty)))
		}
		return r
	case Size:
		seq := jpSeq(item, t.Path)
		if !jpTypeIs(seq, "array") {
			return triFalse
		}
		for _, y := range seq {
			n := int64(1)
			if arr, ok := y.([]any); ok {
				n = int64(len(arr))
			}
			if n == t.N {
				return triTrue
			}
		}
		return triFalse
	case ElemMatch:
		seq := jpSeq(item, t.Path)
		if !jpTypeIs(seq, "array") {
			return triFalse
		}
		for _, y := range seq {
			elems := []any{y}
			if arr, ok := y.([]any); ok {
				elems = arr
			}
			// X[*] ? (...)：过滤前本身是数组的元素再展开一层
			for _, e := range unwrap(elems) {
				if jpEval(t.Sub, e) == triTrue {
					return triTrue
				}
			}
		}
		return triFalse
	case Prefix:
		seq := unwrap(jpSeq(item, t.Path))
		errored := false
		for _, y := range seq {
			s, ok := y.(string)
			if !ok {
				errored = true
				continue
			}
			if strings.HasPrefix(s, t.Prefix) {
				return triTrue
			}
		}
		if errored {
			return triUnknown
		}
		return triFalse
	case Mod:
		seq := jpSeq(item, t.Path)
		if !jpTypeIs(seq, "number") {
			return triFalse
		}
		if len(seq) != 1 {
			return triUnknown
		}
		r, ok := num.Rat(seq[0])
		if !ok {
			return triUnknown
		}
		tr := new(big.Int).Quo(r.Num(), r.Denom())
		rem := new(big.Int).Rem(tr, big.NewInt(t.Div))
		return triOf(rem.Cmp(big.NewInt(t.Rem)) == 0)
	}
	return triUnknown
}

func okTri(r, ok bool) tri {
	if !ok {
		return triUnknown
	}
	return triOf(r)
}

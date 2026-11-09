package query

import (
	"encoding/json"
	"math/big"
	"regexp"
	"sort"
	"strings"

	"backend/internal/domain/document/num"
)

// 更新运算符（docs/collection-crud/design.md 第 6.1 节）。

// MaxArrayFilters 是 arrayFilters 的个数上限；MaxPositional 是一个路径里位置运算符的个数上限。
const (
	MaxArrayFilters = 8
	MaxPositional   = 2
	MaxUpdatePaths  = 128
)

var arrayFilterIDRe = regexp.MustCompile(`^[a-z][A-Za-z0-9]{0,31}$`)

// upath 是更新路径：普通段，或位置运算符 $（Pos=1）、$[]（Pos=2）、$[id]（Pos=3，ID）。
type useg struct {
	Key string
	Pos int
	ID  string
}

type upath struct {
	Segs []useg
	Raw  string
}

type updateOp struct {
	Op   string
	Path upath
	Arg  any
	// To 是 $rename 的目标
	To upath
}

// Update 是解析后的 update 文档。
type Update struct {
	ops           []updateOp
	arrayFilters  map[string]Node // 标识符 → 元素条件（元素包在 {"@e": 元素} 里求值）
	hasPositional bool
}

var updateOps = map[string]bool{"$set": true, "$unset": true, "$inc": true, "$mul": true, "$min": true, "$max": true, "$rename": true,
	"$setOnInsert": true, "$currentDate": true, "$push": true, "$addToSet": true, "$pop": true, "$pull": true, "$pullAll": true}

// parseUpath 解析更新路径：与 ParsePath 相同的转义规则，另外允许 $、$[]、$[id] 段；不允许系统字段。
func parseUpath(s string) (upath, error) {
	if s == "_id" || s == "_meta" || strings.HasPrefix(s, "_id.") || strings.HasPrefix(s, "_meta.") {
		return upath{}, errf("cannot modify %s", s)
	}
	// 先把位置段换成占位，再按普通路径解析
	var raw []string
	var cur strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune('\\')
			cur.WriteRune(r)
			esc = false
		case r == '\\':
			esc = true
		case r == '.':
			raw = append(raw, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if esc {
		return upath{}, errf("path %q ends with a backslash", s)
	}
	raw = append(raw, cur.String())
	if len(raw) > MaxPathSegments {
		return upath{}, errf("path %q has more than %d segments", s, MaxPathSegments)
	}
	out := upath{Raw: s}
	npos := 0
	for i, seg := range raw {
		switch {
		case seg == "$":
			out.Segs = append(out.Segs, useg{Pos: 1})
			npos++
		case seg == "$[]":
			out.Segs = append(out.Segs, useg{Pos: 2})
			npos++
		case strings.HasPrefix(seg, "$[") && strings.HasSuffix(seg, "]"):
			id := seg[2 : len(seg)-1]
			if !arrayFilterIDRe.MatchString(id) {
				return upath{}, errf("bad array filter identifier %q in %s", id, s)
			}
			out.Segs = append(out.Segs, useg{Pos: 3, ID: id})
			npos++
		default:
			p, err := ParsePath(seg)
			if err != nil || p.IsSys() || len(p.Segs) != 1 {
				if err == nil {
					return upath{}, errf("bad path %q", s)
				}
				return upath{}, &Error{Msg: err.Error()}
			}
			out.Segs = append(out.Segs, useg{Key: p.Segs[0]})
		}
		if i == 0 && out.Segs[0].Pos != 0 {
			return upath{}, errf("path %q cannot start with a positional operator", s)
		}
	}
	if npos > MaxPositional {
		return upath{}, errf("path %q has more than %d positional operators", s, MaxPositional)
	}
	return out, nil
}

func (p upath) positional() bool {
	for _, s := range p.Segs {
		if s.Pos != 0 {
			return true
		}
	}
	return false
}

// conflicts 判断两个路径是否冲突（相同，或一个是另一个的前缀）。位置段按“同一位置”处理。
func conflicts(a, b upath) bool {
	n := len(a.Segs)
	if len(b.Segs) < n {
		n = len(b.Segs)
	}
	for i := 0; i < n; i++ {
		x, y := a.Segs[i], b.Segs[i]
		if x.Pos != 0 || y.Pos != 0 {
			continue
		}
		if x.Key != y.Key {
			return false
		}
	}
	return true
}

// ParseUpdate 解析 update 文档（只能包含更新运算符）和 arrayFilters。
func ParseUpdate(v any, arrayFilters []any) (*Update, error) {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil, errf("update must be a non-empty object of update operators such as {\"$set\": {...}}")
	}
	u := &Update{arrayFilters: map[string]Node{}}
	var paths []upath
	for _, op := range sortedKeys(m) {
		if !strings.HasPrefix(op, "$") {
			return nil, errf("update must only contain update operators; %q is a field (use replaceOne to replace the whole document)", op)
		}
		if !updateOps[op] {
			return nil, errf("unsupported update operator %s", op)
		}
		fields, ok := m[op].(map[string]any)
		if !ok || len(fields) == 0 {
			return nil, errf("%s must be a non-empty object", op)
		}
		for _, k := range sortedKeys(fields) {
			p, err := parseUpath(k)
			if err != nil {
				return nil, err
			}
			o := updateOp{Op: op, Path: p, Arg: fields[k]}
			if op == "$rename" {
				to, ok := fields[k].(string)
				if !ok {
					return nil, errf("$rename target for %s must be a string", k)
				}
				tp, err := parseUpath(to)
				if err != nil {
					return nil, err
				}
				if p.positional() || tp.positional() {
					return nil, errf("$rename does not support positional operators")
				}
				if conflicts(p, tp) {
					return nil, errf("$rename source and target must not be the same or nested: %s → %s", k, to)
				}
				o.To = tp
				paths = append(paths, tp)
			}
			if err := checkUpdateArg(o); err != nil {
				return nil, err
			}
			if p.positional() {
				u.hasPositional = true
			}
			paths = append(paths, p)
			u.ops = append(u.ops, o)
		}
	}
	if len(paths) > MaxUpdatePaths {
		return nil, errf("update touches more than %d paths", MaxUpdatePaths)
	}
	for i := range paths {
		for j := i + 1; j < len(paths); j++ {
			if conflicts(paths[i], paths[j]) {
				return nil, errf("update paths %s and %s conflict", paths[i].Raw, paths[j].Raw)
			}
		}
	}
	// arrayFilters：每个元素是一个以标识符开头的条件，例如 {"elem.qty": {"$gt": 1}} 或 {"elem": {"$gte": 80}}
	if len(arrayFilters) > MaxArrayFilters {
		return nil, errf("at most %d arrayFilters", MaxArrayFilters)
	}
	for _, af := range arrayFilters {
		am, ok := af.(map[string]any)
		if !ok || len(am) == 0 {
			return nil, errf("each arrayFilter must be a non-empty object")
		}
		id := ""
		rewritten := map[string]any{}
		for k, cond := range am {
			first, rest, _ := strings.Cut(k, ".")
			if !arrayFilterIDRe.MatchString(first) {
				return nil, errf("arrayFilter key %q must start with an identifier", k)
			}
			if id != "" && id != first {
				return nil, errf("one arrayFilter must use a single identifier, got %s and %s", id, first)
			}
			id = first
			nk := "@e"
			if rest != "" {
				nk += "." + rest
			}
			rewritten[nk] = cond
		}
		if _, dup := u.arrayFilters[id]; dup {
			return nil, errf("duplicate arrayFilter identifier %s", id)
		}
		n, err := ParseFilter(rewritten)
		if err != nil {
			return nil, err
		}
		u.arrayFilters[id] = n
	}
	used := map[string]bool{}
	for _, o := range u.ops {
		for _, s := range o.Path.Segs {
			if s.Pos == 3 {
				if _, ok := u.arrayFilters[s.ID]; !ok {
					return nil, errf("no arrayFilter for identifier %s", s.ID)
				}
				used[s.ID] = true
			}
		}
	}
	for id := range u.arrayFilters {
		if !used[id] {
			return nil, errf("arrayFilter %s is not used by the update", id)
		}
	}
	return u, nil
}

func checkUpdateArg(o updateOp) error {
	switch o.Op {
	case "$inc", "$mul":
		if !num.IsNumber(o.Arg) {
			return errf("%s %s needs a number", o.Op, o.Path.Raw)
		}
	case "$pop":
		n, ok := intArg(o.Arg)
		if !ok || (n != 1 && n != -1) {
			return errf("$pop %s needs 1 or -1", o.Path.Raw)
		}
	case "$pullAll":
		if _, ok := o.Arg.([]any); !ok {
			return errf("$pullAll %s needs an array", o.Path.Raw)
		}
	case "$currentDate":
		switch t := o.Arg.(type) {
		case bool:
			if !t {
				return errf("$currentDate %s must be true", o.Path.Raw)
			}
		case map[string]any:
			ty, _ := t["$type"].(string)
			if len(t) != 1 || (ty != "date" && ty != "timestamp") {
				return errf("$currentDate %s must be true or {\"$type\": \"date\"}", o.Path.Raw)
			}
		default:
			return errf("$currentDate %s must be true", o.Path.Raw)
		}
	case "$push":
		if m, ok := o.Arg.(map[string]any); ok {
			if _, has := m["$each"]; has {
				if _, ok := m["$each"].([]any); !ok {
					return errf("$push.$each for %s must be an array", o.Path.Raw)
				}
				for k := range m {
					switch k {
					case "$each", "$slice", "$position", "$sort":
					default:
						return errf("unknown $push modifier %s", k)
					}
				}
			}
		}
	case "$addToSet":
		if m, ok := o.Arg.(map[string]any); ok {
			if _, has := m["$each"]; has {
				if _, ok := m["$each"].([]any); !ok || len(m) != 1 {
					return errf("$addToSet.$each for %s must be an array", o.Path.Raw)
				}
			}
		}
	}
	return nil
}

// HasPositional 表示 update 用了 $ 位置运算符（需要过滤条件来定位元素）。
func (u *Update) HasPositional() bool { return u.hasPositional }

// ApplyContext 是一次应用的环境。
type ApplyContext struct {
	Insert bool  // upsert 新建文档时为真（$setOnInsert 生效）
	NowMs  int64 // $currentDate 的时间
	Filter Node  // 定位 $ 位置运算符的过滤条件
	Arrays func(Path) bool
}

// Apply 把更新应用到文档（data 的深拷贝上），返回新文档。
func (u *Update) Apply(data map[string]any, ac ApplyContext) (map[string]any, error) {
	doc := deepCopy(data).(map[string]any)
	if doc == nil {
		doc = map[string]any{}
	}
	for _, o := range u.ops {
		if o.Op == "$setOnInsert" && !ac.Insert {
			continue
		}
		targets, err := u.expand(doc, o.Path, ac, data)
		if err != nil {
			return nil, err
		}
		for _, t := range targets {
			if err := applyOne(doc, o, t, ac); err != nil {
				return nil, err
			}
		}
	}
	return doc, nil
}

// expand 把带位置运算符的路径展开成具体路径（每段是键或下标）。
func (u *Update) expand(doc map[string]any, p upath, ac ApplyContext, orig map[string]any) ([][]string, error) {
	paths := [][]string{{}}
	for i, s := range p.Segs {
		if s.Pos == 0 {
			for j := range paths {
				paths[j] = append(paths[j], s.Key)
			}
			continue
		}
		var next [][]string
		for _, prefix := range paths {
			arr, ok := getRaw(doc, prefix).([]any)
			if !ok {
				if s.Pos == 1 {
					return nil, errf("the positional operator in %s did not find an array", p.Raw)
				}
				continue // $[] / $[id] 在不存在或不是数组时不做事（与 Mongo 一致：路径必须存在才报错，这里宽松处理）
			}
			switch s.Pos {
			case 1:
				idx, err := positionalIndex(ac.Filter, prefix, arr, ac.Arrays)
				if err != nil {
					return nil, errf("%s: %v", p.Raw, err)
				}
				next = append(next, appendCopy(prefix, itoa(idx)))
			case 2:
				for k := range arr {
					next = append(next, appendCopy(prefix, itoa(k)))
				}
			case 3:
				cond := u.arrayFilters[s.ID]
				for k, el := range arr {
					if (Matcher{}).Match(cond, Doc{"@e": el}) {
						next = append(next, appendCopy(prefix, itoa(k)))
					}
				}
			}
		}
		paths = next
		_ = i
	}
	return paths, nil
}

func appendCopy(p []string, s string) []string {
	out := make([]string, len(p), len(p)+1)
	copy(out, p)
	return append(out, s)
}

func itoa(i int) string { return big.NewInt(int64(i)).String() }

// positionalIndex 找出过滤条件在数组上匹配到的第一个元素下标：$elemMatch，或数组字段（schema 声明为数组）上的等值。
func positionalIndex(filter Node, prefix []string, arr []any, arrays func(Path) bool) (int, error) {
	var conds []Node
	switch t := filter.(type) {
	case And:
		conds = t.Kids
	case nil:
	default:
		conds = []Node{t}
	}
	samePath := func(p Path) bool {
		if p.IsSys() || len(p.Segs) != len(prefix) {
			return false
		}
		for i := range prefix {
			if p.Segs[i] != prefix[i] {
				return false
			}
		}
		return true
	}
	for _, c := range conds {
		switch t := c.(type) {
		case ElemMatch:
			if samePath(t.Path) {
				for i, el := range arr {
					if jpEval(t.Sub, el) == triTrue {
						return i, nil
					}
				}
			}
		case Cmp:
			if samePath(t.Path) && t.Op == "eq" {
				for i, el := range arr {
					if num.Equal(el, t.Val) || JSONBContains(el, t.Val, false) && !isContainer(t.Val) {
						return i, nil
					}
				}
			}
		case In:
			if samePath(t.Path) && !t.Neg {
				for i, el := range arr {
					for _, v := range t.Vals {
						if num.Equal(el, v) {
							return i, nil
						}
					}
				}
			}
		}
	}
	return 0, errf("the positional operator $ did not find the match needed from the query (filter the array with $elemMatch or an equality in the top-level AND)")
}

func getRaw(doc map[string]any, segs []string) any {
	var cur any = doc
	for _, s := range segs {
		switch t := cur.(type) {
		case map[string]any:
			cur = t[s]
		case []any:
			i, ok := arrayIndex(s)
			if !ok || i >= len(t) {
				return nil
			}
			cur = t[i]
		default:
			return nil
		}
	}
	return cur
}

// container 返回路径父节点和最后一段，create 为真时沿途建对象（数组下标超出长度时补 null）。
func container(doc map[string]any, segs []string, create bool) (any, string, error) {
	var cur any = doc
	for i, s := range segs[:len(segs)-1] {
		var next any
		switch t := cur.(type) {
		case map[string]any:
			v, ok := t[s]
			if !ok || v == nil {
				if !create {
					return nil, "", nil
				}
				v = map[string]any{}
				t[s] = v
			}
			next = v
		case []any:
			idx, ok := arrayIndex(s)
			if !ok {
				return nil, "", errf("cannot use the part %q of %s to traverse an array", s, strings.Join(segs, "."))
			}
			if idx >= len(t) {
				if !create {
					return nil, "", nil
				}
				return nil, "", errf("array index %d in %s is out of range", idx, strings.Join(segs, "."))
			}
			if t[idx] == nil && create {
				t[idx] = map[string]any{}
			}
			next = t[idx]
		default:
			if !create {
				return nil, "", nil
			}
			return nil, "", errf("cannot create field %q in a %s (path %s)", segs[i+1], jsonType(cur), strings.Join(segs, "."))
		}
		cur = next
	}
	return cur, segs[len(segs)-1], nil
}

func getAt(parent any, key string) (any, bool) {
	switch t := parent.(type) {
	case map[string]any:
		v, ok := t[key]
		return v, ok
	case []any:
		i, ok := arrayIndex(key)
		if !ok || i >= len(t) {
			return nil, false
		}
		return t[i], true
	}
	return nil, false
}

// setAt 设置父节点里的值；数组下标超出长度时补 null（与 Mongo 一致）。
func setAt(doc map[string]any, segs []string, parent any, key string, v any) error {
	switch t := parent.(type) {
	case map[string]any:
		t[key] = v
		return nil
	case []any:
		i, ok := arrayIndex(key)
		if !ok {
			return errf("cannot use the part %q to traverse an array", key)
		}
		if i >= len(t) {
			if i > 100000 {
				return errf("array index %d is too large", i)
			}
			grown := make([]any, i+1)
			copy(grown, t)
			grown[i] = v
			// 替换父数组：写回祖父节点
			gp, gk, err := container(doc, segs[:len(segs)-1], false)
			if err != nil {
				return err
			}
			if gp == nil {
				doc[segs[0]] = grown
				return nil
			}
			return setAt(doc, segs[:len(segs)-1], gp, gk, grown)
		}
		t[i] = v
		return nil
	}
	return errf("cannot create field %q in a %s", key, jsonType(parent))
}

func applyOne(doc map[string]any, o updateOp, segs []string, ac ApplyContext) error {
	if len(segs) == 0 {
		return errf("empty update path")
	}
	switch o.Op {
	case "$unset":
		parent, key, err := container(doc, segs, false)
		if err != nil || parent == nil {
			return err
		}
		switch t := parent.(type) {
		case map[string]any:
			delete(t, key)
		case []any:
			if i, ok := arrayIndex(key); ok && i < len(t) {
				t[i] = nil // 与 Mongo 一致：数组元素置为 null，不改变长度
			}
		}
		return nil
	case "$rename":
		parent, key, err := container(doc, segs, false)
		if err != nil || parent == nil {
			return err
		}
		pm, ok := parent.(map[string]any)
		if !ok {
			return errf("$rename source %s must not be inside an array", o.Path.Raw)
		}
		v, ok := pm[key]
		if !ok {
			return nil
		}
		delete(pm, key)
		to := make([]string, len(o.To.Segs))
		for i, s := range o.To.Segs {
			to[i] = s.Key
		}
		tp, tk, err := container(doc, to, true)
		if err != nil {
			return err
		}
		tm, ok := tp.(map[string]any)
		if !ok {
			return errf("$rename target %s must not be inside an array", o.To.Raw)
		}
		tm[tk] = v
		return nil
	}
	parent, key, err := container(doc, segs, true)
	if err != nil {
		return err
	}
	cur, exists := getAt(parent, key)
	set := func(v any) error { return setAt(doc, segs, parent, key, v) }
	switch o.Op {
	case "$set", "$setOnInsert":
		return set(deepCopy(o.Arg))
	case "$currentDate":
		return set(json.Number(big.NewInt(ac.NowMs).String()))
	case "$inc":
		if !exists || cur == nil && !exists {
			return set(o.Arg)
		}
		r, err := num.Add(cur, o.Arg)
		if err != nil {
			return errf("$inc %s: the current value is a %s, not a number", o.Path.Raw, jsonType(cur))
		}
		return set(r)
	case "$mul":
		if !exists {
			return set(json.Number("0"))
		}
		r, err := num.Mul(cur, o.Arg)
		if err != nil {
			return errf("$mul %s: the current value is a %s, not a number", o.Path.Raw, jsonType(cur))
		}
		return set(r)
	case "$min", "$max":
		if !exists {
			return set(deepCopy(o.Arg))
		}
		c, ok := compareSameType(o.Arg, cur)
		if !ok {
			return errf("%s %s: cannot compare a %s with a %s", o.Op, o.Path.Raw, jsonType(o.Arg), jsonType(cur))
		}
		if (o.Op == "$min" && c < 0) || (o.Op == "$max" && c > 0) {
			return set(deepCopy(o.Arg))
		}
		return nil
	case "$push", "$addToSet", "$pop", "$pull", "$pullAll":
		var arr []any
		if exists && cur != nil {
			a, ok := cur.([]any)
			if !ok {
				return errf("%s %s: the current value is a %s, not an array", o.Op, o.Path.Raw, jsonType(cur))
			}
			arr = a
		} else if o.Op == "$pop" || o.Op == "$pull" || o.Op == "$pullAll" {
			return nil
		}
		out, err := arrayOp(o, arr)
		if err != nil {
			return err
		}
		return set(out)
	}
	return errf("unsupported update operator %s", o.Op)
}

func arrayOp(o updateOp, arr []any) ([]any, error) {
	switch o.Op {
	case "$push":
		items := []any{o.Arg}
		pos := len(arr)
		var slice *int64
		var sortSpec any
		if m, ok := o.Arg.(map[string]any); ok {
			if each, has := m["$each"]; has {
				items = each.([]any)
				if p, ok := m["$position"]; ok {
					n, ok := intArg(p)
					if !ok {
						return nil, errf("$push.$position must be an integer")
					}
					if n < 0 {
						n += int64(len(arr))
						if n < 0 {
							n = 0
						}
					}
					if n < int64(len(arr)) {
						pos = int(n)
					}
				}
				if s, ok := m["$slice"]; ok {
					n, ok := intArg(s)
					if !ok {
						return nil, errf("$push.$slice must be an integer")
					}
					slice = &n
				}
				sortSpec = m["$sort"]
			}
		}
		out := make([]any, 0, len(arr)+len(items))
		out = append(out, arr[:pos]...)
		for _, it := range items {
			out = append(out, deepCopy(it))
		}
		out = append(out, arr[pos:]...)
		if sortSpec != nil {
			if err := sortArray(out, sortSpec); err != nil {
				return nil, err
			}
		}
		if slice != nil {
			n := *slice
			switch {
			case n >= 0 && n < int64(len(out)):
				out = out[:n]
			case n < 0 && -n < int64(len(out)):
				out = out[int64(len(out))+n:]
			}
		}
		return out, nil
	case "$addToSet":
		items := []any{o.Arg}
		if m, ok := o.Arg.(map[string]any); ok {
			if each, has := m["$each"]; has {
				items = each.([]any)
			}
		}
		out := append([]any{}, arr...)
		for _, it := range items {
			found := false
			for _, e := range out {
				if num.Equal(e, it) {
					found = true
					break
				}
			}
			if !found {
				out = append(out, deepCopy(it))
			}
		}
		return out, nil
	case "$pop":
		n, _ := intArg(o.Arg)
		if len(arr) == 0 {
			return arr, nil
		}
		if n == 1 {
			return append([]any{}, arr[:len(arr)-1]...), nil
		}
		return append([]any{}, arr[1:]...), nil
	case "$pullAll":
		vals := o.Arg.([]any)
		out := []any{}
		for _, e := range arr {
			drop := false
			for _, v := range vals {
				if num.Equal(e, v) {
					drop = true
					break
				}
			}
			if !drop {
				out = append(out, e)
			}
		}
		return out, nil
	case "$pull":
		match, err := pullMatcher(o.Arg)
		if err != nil {
			return nil, err
		}
		out := []any{}
		for _, e := range arr {
			if !match(e) {
				out = append(out, e)
			}
		}
		return out, nil
	}
	return arr, nil
}

// pullMatcher：值是运算符对象时是元素本身的条件（{$gte: 6}）；是普通对象时是作用在对象元素上的查询（{sku: "x"}）；
// 其他是相等。
func pullMatcher(arg any) (func(any) bool, error) {
	m, isOps, err := isOperatorObject(arg)
	if err != nil {
		return nil, err
	}
	if isOps {
		n, err := ParseFilter(map[string]any{"@e": m})
		if err != nil {
			return nil, err
		}
		return func(e any) bool { return (Matcher{}).Match(n, Doc{"@e": e}) }, nil
	}
	if m != nil {
		n, err := ParseFilter(m)
		if err != nil {
			return nil, err
		}
		return func(e any) bool {
			em, ok := e.(map[string]any)
			return ok && (Matcher{}).Match(n, em)
		}, nil
	}
	return func(e any) bool { return num.Equal(e, arg) }, nil
}

// sortArray 实现 $push.$sort：1 / -1 按元素本身排序，{field: 1} 按对象字段排序。跨类型按 jsonb 的类型顺序。
func sortArray(arr []any, spec any) error {
	type key struct {
		path Path
		dir  int
	}
	var keys []key
	switch t := spec.(type) {
	case map[string]any:
		for _, k := range sortedKeys(t) {
			d, ok := intArg(t[k])
			if !ok || (d != 1 && d != -1) {
				return errf("$push.$sort values must be 1 or -1")
			}
			p, err := ParsePath(k)
			if err != nil {
				return &Error{Msg: err.Error()}
			}
			keys = append(keys, key{p, int(d)})
		}
	default:
		d, ok := intArg(spec)
		if !ok || (d != 1 && d != -1) {
			return errf("$push.$sort must be 1, -1 or {field: 1|-1}")
		}
		keys = append(keys, key{Path{}, int(d)})
	}
	sort.SliceStable(arr, func(i, j int) bool {
		for _, k := range keys {
			a, b := arr[i], arr[j]
			if len(k.path.Segs) > 0 {
				am, _ := a.(map[string]any)
				bm, _ := b.(map[string]any)
				a, _ = k.path.Get(am)
				b, _ = k.path.Get(bm)
			}
			c := CompareJSONB(a, b)
			if c != 0 {
				return c*k.dir < 0
			}
		}
		return false
	})
	return nil
}

// CompareJSONB 按 PG jsonb 的 btree 顺序比较两个值（第 2.2 节）：null < string < number < boolean < array < object，
// 数组和对象先比长度。缺失（nil）与 null 同序。
func CompareJSONB(a, b any) int {
	rank := func(v any) int {
		switch v.(type) {
		case nil:
			return 0
		case string:
			return 1
		case bool:
			return 3
		case []any:
			return 4
		case map[string]any:
			return 5
		}
		if num.IsNumber(v) {
			return 2
		}
		return 6
	}
	ra, rb := rank(a), rank(b)
	if ra != rb {
		if ra < rb {
			return -1
		}
		return 1
	}
	switch x := a.(type) {
	case []any:
		y := b.([]any)
		if len(x) != len(y) {
			if len(x) < len(y) {
				return -1
			}
			return 1
		}
		for i := range x {
			if c := CompareJSONB(x[i], y[i]); c != 0 {
				return c
			}
		}
		return 0
	case map[string]any:
		y := b.(map[string]any)
		if len(x) != len(y) {
			if len(x) < len(y) {
				return -1
			}
			return 1
		}
		xk, yk := sortedKeys(x), sortedKeys(y)
		for i := range xk {
			if c := strings.Compare(xk[i], yk[i]); c != 0 {
				return c
			}
			if c := CompareJSONB(x[xk[i]], y[yk[i]]); c != 0 {
				return c
			}
		}
		return 0
	}
	c, _ := compareSameType(a, b)
	return c
}

func deepCopy(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = deepCopy(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = deepCopy(x)
		}
		return out
	}
	return v
}

// DeepCopy 深拷贝一个解码后的 JSON 值。
func DeepCopy(v any) any { return deepCopy(v) }

// UpsertBase 从过滤条件里取出等值字段，作为 upsert 新文档的初始内容（顶层 AND 里的 {path: 标量 / 对象} 等值）。
// 返回初始 data 和 _id（过滤里有 _id 等值时）。
func UpsertBase(filter Node) (map[string]any, string) {
	data := map[string]any{}
	id := ""
	var conds []Node
	switch t := filter.(type) {
	case And:
		conds = t.Kids
	default:
		conds = []Node{t}
	}
	for _, c := range conds {
		cmp, ok := c.(Cmp)
		if !ok || cmp.Op != "eq" {
			continue
		}
		if cmp.Path.IsSys() {
			if cmp.Path.Sys == "id" {
				id, _ = cmp.Val.(string)
			}
			continue
		}
		segs := cmp.Path.Segs
		parent, key, err := container(data, segs, true)
		if err == nil {
			if pm, ok := parent.(map[string]any); ok {
				pm[key] = deepCopy(cmp.Val)
			}
		}
	}
	return data, id
}

// EqualityPaths 返回过滤条件顶层 AND 里做等值的文档路径（upsert 要求它们被唯一索引覆盖）。
func EqualityPaths(filter Node) ([]Path, bool) {
	var conds []Node
	switch t := filter.(type) {
	case And:
		conds = t.Kids
	default:
		conds = []Node{t}
	}
	var out []Path
	hasID := false
	for _, c := range conds {
		cmp, ok := c.(Cmp)
		if !ok || cmp.Op != "eq" {
			continue
		}
		if cmp.Path.IsSys() {
			hasID = hasID || cmp.Path.Sys == "id"
			continue
		}
		out = append(out, cmp.Path)
	}
	return out, hasID
}

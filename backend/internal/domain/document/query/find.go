package query

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// MaxSortKeys / MaxProjectionKeys 是排序键与投影键的上限（第 4.3、5.1 节）。
const (
	MaxSortKeys       = 4
	MaxProjectionKeys = 50
)

// SortKey 是一个排序键。Dir 为 1（升序）或 -1（降序）。
type SortKey struct {
	Path Path
	Dir  int
}

// ParseSort 解析排序：[[path, 1|-1], ...]（对象形式只接受一个键，JSON 对象的键序不可靠）。末尾自动补 _id 升序保证全序。
// def 是没有给排序时的默认值。
func ParseSort(v any, def []SortKey) ([]SortKey, error) {
	var keys []SortKey
	switch t := v.(type) {
	case nil:
		keys = append(keys, def...)
	case []any:
		for _, x := range t {
			pair, ok := x.([]any)
			if !ok || len(pair) != 2 {
				return nil, errf("sort must be [[path, 1 | -1], ...]")
			}
			ps, ok := pair[0].(string)
			if !ok {
				return nil, errf("sort path must be a string")
			}
			k, err := sortKey(ps, pair[1])
			if err != nil {
				return nil, err
			}
			keys = append(keys, k)
		}
	case map[string]any:
		if len(t) > 1 {
			return nil, errf("sort objects with more than one key have no reliable order; use [[path, 1 | -1], ...]")
		}
		for ps, d := range t {
			k, err := sortKey(ps, d)
			if err != nil {
				return nil, err
			}
			keys = append(keys, k)
		}
	default:
		return nil, errf("sort must be [[path, 1 | -1], ...]")
	}
	if len(keys) > MaxSortKeys {
		return nil, errf("at most %d sort keys", MaxSortKeys)
	}
	hasID := false
	for _, k := range keys {
		if k.Path.Sys == "id" {
			hasID = true
		}
	}
	if !hasID {
		keys = append(keys, SortKey{Path: Path{Sys: "id", Raw: "_id"}, Dir: 1})
	}
	return keys, nil
}

func sortKey(ps string, d any) (SortKey, error) {
	p, err := ParsePath(ps)
	if err != nil {
		return SortKey{}, &Error{Msg: err.Error()}
	}
	dir, ok := intArg(d)
	if !ok || (dir != 1 && dir != -1) {
		return SortKey{}, errf("sort direction for %s must be 1 or -1", ps)
	}
	return SortKey{Path: p, Dir: int(dir)}, nil
}

// sortExpr 返回排序键的 SQL 表达式和游标值的类型。
func (b *Builder) sortExpr(k SortKey) (string, string) {
	if b.DocMode {
		return "(" + docPath(k.Path).sqlOn("doc") + ")", "jsonb"
	}
	if !k.Path.IsSys() {
		return "(" + k.Path.SQL() + ")", "jsonb"
	}
	col, _ := sysColumn(k.Path.Sys)
	switch k.Path.Sys {
	case "id", "sourceId":
		return col, "text"
	case "revision":
		return col, "bigint"
	case "locked":
		return col, "boolean"
	}
	return col, "timestamptz"
}

// OrderBy 返回 ORDER BY 子句（不含关键字）和每个键用于游标的取值表达式（::text）。
// 升序 NULLS LAST、降序 NULLS FIRST（PG 默认，能用上索引）：字段缺失的文档升序排在最后、降序排在最前。
func (b *Builder) OrderBy(keys []SortKey) (string, []string) {
	parts := make([]string, len(keys))
	vals := make([]string, len(keys))
	for i, k := range keys {
		e, _ := b.sortExpr(k)
		if k.Dir < 0 {
			parts[i] = e + " DESC"
		} else {
			parts[i] = e
		}
		vals[i] = "(" + e + ")::text"
	}
	return strings.Join(parts, ", "), vals
}

// After 返回“排在游标值之后”的 keyset 条件：按键展开成 OR，每个分支前面的键相等、本键在之后；
// NULL（字段缺失）单独处理（升序时 NULL 在最后，降序时在最前）。vals[i] 为 nil 表示该键的值是 NULL。
func (b *Builder) After(keys []SortKey, vals []*string) (string, error) {
	if len(vals) != len(keys) {
		return "", errf("cursor does not match the sort")
	}
	var branches []string
	for i := range keys {
		// 升序且游标值为 NULL 时本键之后没有值，整个分支恒为假：跳过，且不能先生成参数（未使用的参数 PG 推断不出类型）
		if keys[i].Dir >= 0 && vals[i] == nil {
			continue
		}
		var conds []string
		for j := 0; j < i; j++ {
			conds = append(conds, b.keyEq(keys[j], vals[j]))
		}
		conds = append(conds, b.keyAfter(keys[i], vals[i]))
		branches = append(branches, "("+strings.Join(conds, " AND ")+")")
	}
	if len(branches) == 0 {
		return "FALSE", nil
	}
	return "(" + strings.Join(branches, " OR ") + ")", nil
}

func (b *Builder) keyEq(k SortKey, v *string) string {
	e, ty := b.sortExpr(k)
	if v == nil {
		return e + " IS NULL"
	}
	return e + " = " + b.Arg(*v) + "::" + ty
}

func (b *Builder) keyAfter(k SortKey, v *string) string {
	e, ty := b.sortExpr(k)
	if k.Dir >= 0 {
		if v == nil {
			return "FALSE"
		}
		return "(" + e + " > " + b.Arg(*v) + "::" + ty + " OR " + e + " IS NULL)"
	}
	if v == nil {
		return e + " IS NOT NULL"
	}
	return e + " < " + b.Arg(*v) + "::" + ty
}

// ---------------- 游标 ----------------

// Cursor 是 keyset 游标：排序键的原始值（::text，NULL 为 nil）和条件哈希。
type Cursor struct {
	Vals []*string `json:"v"`
	Hash string    `json:"h"`
}

// CursorHash 是绑定游标的条件哈希：workspace、集合、集合 revision（schema 变了游标作废）、过滤、排序、投影之外的查询条件。
func CursorHash(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return base64.RawURLEncoding.EncodeToString(h[:12])
}

// EncodeCursor 把游标写成不透明字符串：base64url(JSON) + "." + base64url(HMAC-SHA256)。
func EncodeCursor(key []byte, c Cursor) string {
	payload, _ := json.Marshal(c)
	p := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(p))
	return p + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:16])
}

// DecodeCursor 校验签名和条件哈希后取出游标。
func DecodeCursor(key []byte, s, wantHash string) (*Cursor, error) {
	p, sig, ok := strings.Cut(s, ".")
	if !ok {
		return nil, errf("bad cursor")
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(p))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:16])
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return nil, errf("bad cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(p)
	if err != nil {
		return nil, errf("bad cursor")
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, errf("bad cursor")
	}
	if c.Hash != wantHash {
		return nil, errf("the cursor belongs to a different query (filter, sort or collection schema changed); start again without a cursor")
	}
	return &c, nil
}

// ---------------- 投影 ----------------

// Projection 是包含式或排除式投影（不能混用）；_id 等记录列总是返回，投影里写了也忽略。
type Projection struct {
	Include bool
	Paths   []Path
}

// ParseProjection 解析投影：{path: 1 | 0 | true | false}。
func ParseProjection(v any) (*Projection, error) {
	if v == nil {
		return nil, nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errf("projection must be an object such as {\"a\": 1}")
	}
	if len(m) > MaxProjectionKeys {
		return nil, errf("projection has more than %d keys", MaxProjectionKeys)
	}
	p := &Projection{}
	mode := 0
	for _, k := range sortedKeys(m) {
		path, err := ParsePath(k)
		if err != nil {
			return nil, &Error{Msg: err.Error()}
		}
		var inc bool
		switch t := m[k].(type) {
		case bool:
			inc = t
		default:
			n, ok := intArg(t)
			if !ok || (n != 0 && n != 1) {
				return nil, errf("projection values must be 1 or 0")
			}
			inc = n == 1
		}
		if path.IsSys() {
			continue
		}
		m := 1
		if !inc {
			m = 2
		}
		if mode != 0 && mode != m {
			return nil, errf("projection cannot mix inclusion and exclusion")
		}
		mode = m
		p.Paths = append(p.Paths, path)
	}
	if mode == 0 {
		return nil, nil
	}
	p.Include = mode == 1
	return p, nil
}

// Apply 对 data 做投影（返回新对象）。
func (p *Projection) Apply(data map[string]any) map[string]any {
	if p == nil {
		return data
	}
	if p.Include {
		out := map[string]any{}
		for _, path := range p.Paths {
			v, ok := path.Get(data)
			if !ok {
				continue
			}
			cur := out
			for i, s := range path.Segs {
				if i == len(path.Segs)-1 {
					cur[s] = deepCopy(v)
					break
				}
				next, ok := cur[s].(map[string]any)
				if !ok {
					next = map[string]any{}
					cur[s] = next
				}
				cur = next
			}
		}
		return out
	}
	out := deepCopy(data).(map[string]any)
	for _, path := range p.Paths {
		parent := any(out)
		for i, s := range path.Segs {
			pm, ok := parent.(map[string]any)
			if !ok {
				break
			}
			if i == len(path.Segs)-1 {
				delete(pm, s)
				break
			}
			parent = pm[s]
		}
	}
	return out
}

// String 用于游标哈希。
func (p *Projection) String() string {
	if p == nil {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%v:", p.Include)
	for _, x := range p.Paths {
		sb.WriteString(x.Raw + ",")
	}
	return sb.String()
}

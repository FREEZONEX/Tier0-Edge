// Package query 实现集合文档接口（docs/collection-crud/design.md）的查询内核：字段路径、过滤语言、表达式、
// 更新运算符、排序 / 投影 / 游标与聚合管线。过滤器和表达式有两个实现：编译成参数化 SQL（查库）和在 Go 里对单个
// 文档求值（$pull、位置运算符、SSE 过滤）；两者的语义由差分测试保证一致。
package query

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// 路径与过滤器的复杂度上限（第 2.1、4.3 节）。
const (
	MaxPathSegments = 8
	MaxSegmentBytes = 128
)

// Path 是一个字段路径：文档里的路径（Segs），或系统字段（Sys 非空：_id、_meta.revision 等）。
type Path struct {
	Segs []string
	Sys  string // id | revision | createdAt | updatedAt | sourceId | sourceTime | locked
	Raw  string // 原始写法（错误信息用）
}

// 系统字段：查询里写 _id / _meta.xxx，映射到记录表的列。
var sysFields = map[string]string{
	"_id":              "id",
	"_meta.revision":   "revision",
	"_meta.createdAt":  "createdAt",
	"_meta.updatedAt":  "updatedAt",
	"_meta.sourceId":   "sourceId",
	"_meta.sourceTime": "sourceTime",
	"_meta.locked":     "locked",
}

// ReservedKey 判断 data 顶层的键是否是保留名（新写入的 data 不能用）。
func ReservedKey(k string) bool { return k == "_id" || k == "_meta" }

// ParsePath 解析点分路径：`\.` 是字面的点，`\\` 是反斜杠。段为 1–128 字节 UTF-8，不含控制字符，不以 $ 开头，最多 8 段。
func ParsePath(s string) (Path, error) {
	if sys, ok := sysFields[s]; ok {
		return Path{Sys: sys, Raw: s}, nil
	}
	if s == "_meta" || strings.HasPrefix(s, "_meta.") {
		return Path{}, fmt.Errorf("unknown system field %q (use _id, _meta.revision, _meta.createdAt, _meta.updatedAt, _meta.sourceId, _meta.sourceTime, _meta.locked)", s)
	}
	var segs []string
	var cur strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case esc:
			if r != '.' && r != '\\' {
				return Path{}, fmt.Errorf("bad escape \\%c in path %q (only \\. and \\\\ are allowed)", r, s)
			}
			cur.WriteRune(r)
			esc = false
		case r == '\\':
			esc = true
		case r == '.':
			segs = append(segs, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if esc {
		return Path{}, fmt.Errorf("path %q ends with a backslash", s)
	}
	segs = append(segs, cur.String())
	if len(segs) > MaxPathSegments {
		return Path{}, fmt.Errorf("path %q has more than %d segments", s, MaxPathSegments)
	}
	for _, seg := range segs {
		if err := checkSegment(seg); err != nil {
			return Path{}, fmt.Errorf("path %q: %w", s, err)
		}
	}
	return Path{Segs: segs, Raw: s}, nil
}

func checkSegment(seg string) error {
	if seg == "" {
		return fmt.Errorf("empty segment")
	}
	if len(seg) > MaxSegmentBytes {
		return fmt.Errorf("segment longer than %d bytes", MaxSegmentBytes)
	}
	if !utf8.ValidString(seg) {
		return fmt.Errorf("segment is not valid UTF-8")
	}
	if strings.HasPrefix(seg, "$") {
		return fmt.Errorf("segment %q must not start with $", seg)
	}
	for _, r := range seg {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("segment contains a control character")
		}
	}
	return nil
}

// Key 返回路径在投影 / 分组输出里的显示名（原始写法）。
func (p Path) Key() string { return p.Raw }

// IsSys 判断是否系统字段。
func (p Path) IsSys() bool { return p.Sys != "" }

// String 返回原始写法。
func (p Path) String() string { return p.Raw }

// Lit 把字符串写成 SQL 字面量（单引号，' 写成 ”）。库上 standard_conforming_strings=on，反斜杠不是转义字符。
// 调用方保证 s 不含 NUL（路径段已经检查过控制字符）。这是唯一允许把用户字符串内联进 SQL 的地方：
// 表达式索引按字面表达式匹配，参数化的键用不上索引（第 2.1 节）。
func Lit(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// SQL 返回取路径值（jsonb）的 SQL 表达式：data #> ARRAY['a','b']。表达式索引必须用完全相同的写法。
func (p Path) SQL() string {
	return p.sqlOn("data")
}

func (p Path) sqlOn(col string) string {
	parts := make([]string, len(p.Segs))
	for i, s := range p.Segs {
		parts[i] = Lit(s)
	}
	return col + " #> ARRAY[" + strings.Join(parts, ",") + "]"
}

// TextSQL 返回取路径值文本的 SQL 表达式：data #>> ARRAY[...]（prefix 类索引用同样的写法）。
func (p Path) TextSQL() string {
	parts := make([]string, len(p.Segs))
	for i, s := range p.Segs {
		parts[i] = Lit(s)
	}
	return "data #>> ARRAY[" + strings.Join(parts, ",") + "]"
}

// Get 在解码后的文档里取路径的值（数字段在数组上是下标）。
func (p Path) Get(doc map[string]any) (any, bool) {
	var cur any = doc
	for _, seg := range p.Segs {
		switch t := cur.(type) {
		case map[string]any:
			v, ok := t[seg]
			if !ok {
				return nil, false
			}
			cur = v
		case []any:
			i, ok := arrayIndex(seg)
			if !ok || i >= len(t) {
				return nil, false
			}
			cur = t[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

// arrayIndex 把纯数字段解析成下标（不接受前导 0，除了 "0" 本身）。
func arrayIndex(seg string) (int, bool) {
	if seg == "" || len(seg) > 9 || (len(seg) > 1 && seg[0] == '0') {
		return 0, false
	}
	n := 0
	for _, c := range seg {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// EscapeSegment 把一个键写成路径段（点和反斜杠加转义），用于把字面带点的键拼进路径。
func EscapeSegment(k string) string {
	return strings.NewReplacer(`\`, `\\`, `.`, `\.`).Replace(k)
}

// Package num 处理 JSON 数字的精确运算：解析时保留原文（json.Number），比较、加法、乘法用有理数（math/big.Rat）
// 精确计算，结果写回时是十进制字符串。避免经过 float64 时 2^53 以上的整数被改写、0.1 + 0.2 得到 0.30000000000000004
// （docs/collection-crud/design.md 第 2.2、6.1、13 节）。
package num

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strings"
)

// jsonNumberRe 是 JSON 数字的语法（RFC 8259）。
var jsonNumberRe = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

// maxExponent 限制科学计数法的指数，防止 1e1000000000 这类输入在转成有理数时耗尽内存。
const maxExponent = 4096

// IsJSONNumber 判断 s 是否符合 JSON 数字语法（不接受 0x1F、1_000、NaN、Infinity、前后空格）。
func IsJSONNumber(s string) bool {
	if !jsonNumberRe.MatchString(s) {
		return false
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		var e int
		if _, err := fmt.Sscanf(strings.TrimLeft(s[i+1:], "+"), "%d", &e); err != nil || e > maxExponent || e < -maxExponent {
			return false
		}
	}
	return true
}

// Decode 解析 JSON，数字保留为 json.Number。
func Decode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("unexpected data after JSON value")
	}
	return nil
}

// Rat 把 Go 里的数字值（json.Number、float64、int、int64）转成有理数；不是数字返回 false。
func Rat(v any) (*big.Rat, bool) {
	switch t := v.(type) {
	case json.Number:
		if !IsJSONNumber(string(t)) {
			return nil, false
		}
		r, ok := new(big.Rat).SetString(string(t))
		return r, ok
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return nil, false
		}
		return new(big.Rat).SetFloat64(t), true
	case float32:
		return Rat(float64(t))
	case int:
		return new(big.Rat).SetInt64(int64(t)), true
	case int64:
		return new(big.Rat).SetInt64(t), true
	case int32:
		return new(big.Rat).SetInt64(int64(t)), true
	case uint64:
		return new(big.Rat).SetInt(new(big.Int).SetUint64(t)), true
	}
	return nil, false
}

// IsNumber 判断 v 是否是数字值。
func IsNumber(v any) bool {
	_, ok := Rat(v)
	return ok
}

// Compare 比较两个数字值：-1 / 0 / 1；有一个不是数字时 ok 为 false。
func Compare(a, b any) (int, bool) {
	ra, ok1 := Rat(a)
	rb, ok2 := Rat(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	return ra.Cmp(rb), true
}

// Format 把有理数写成 JSON 数字（json.Number）。十进制有限小数精确输出；无限循环小数（只会来自除法，
// 本包不做除法）按 34 位有效数字截断。整数不带小数点。
func Format(r *big.Rat) json.Number {
	if r.IsInt() {
		return json.Number(r.Num().String())
	}
	// 分母只含 2 和 5 时是有限小数：位数 = max(2 的幂次, 5 的幂次)
	d := new(big.Int).Set(r.Denom())
	two, five := big.NewInt(2), big.NewInt(5)
	n2, n5 := 0, 0
	m := new(big.Int)
	for m.Mod(d, two).Sign() == 0 {
		d.Div(d, two)
		n2++
	}
	for m.Mod(d, five).Sign() == 0 {
		d.Div(d, five)
		n5++
	}
	if d.Cmp(big.NewInt(1)) == 0 {
		prec := n2
		if n5 > prec {
			prec = n5
		}
		return json.Number(trimZeros(r.FloatString(prec)))
	}
	return json.Number(trimZeros(r.FloatString(34)))
}

func trimZeros(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	return strings.TrimSuffix(s, ".")
}

// Add 返回 a + b（精确）。
func Add(a, b any) (json.Number, error) {
	ra, ok1 := Rat(a)
	rb, ok2 := Rat(b)
	if !ok1 || !ok2 {
		return "", fmt.Errorf("operands must be numbers")
	}
	return Format(new(big.Rat).Add(ra, rb)), nil
}

// Mul 返回 a × b（精确）。
func Mul(a, b any) (json.Number, error) {
	ra, ok1 := Rat(a)
	rb, ok2 := Rat(b)
	if !ok1 || !ok2 {
		return "", fmt.Errorf("operands must be numbers")
	}
	return Format(new(big.Rat).Mul(ra, rb)), nil
}

// IsInteger 判断数字值是否是整数（1.0 也算）。
func IsInteger(v any) bool {
	r, ok := Rat(v)
	return ok && r.IsInt()
}

// Canonical 把数字值写成规范形式（1.0 → 1，1e2 → 100），用于比较两份 JSON 是否语义相同。
func Canonical(v any) (json.Number, bool) {
	r, ok := Rat(v)
	if !ok {
		return "", false
	}
	return Format(r), true
}

// Equal 按语义比较两个解码后的 JSON 值：对象忽略键序，数字按数值比较（1 = 1.0，大整数不丢精度）。
func Equal(a, b any) bool {
	if ra, ok := Rat(a); ok {
		rb, ok := Rat(b)
		return ok && ra.Cmp(rb) == 0
	}
	switch x := a.(type) {
	case nil:
		return b == nil
	case string:
		y, ok := b.(string)
		return ok && x == y
	case bool:
		y, ok := b.(bool)
		return ok && x == y
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, xv := range x {
			yv, ok := y[k]
			if !ok || !Equal(xv, yv) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !Equal(x[i], y[i]) {
				return false
			}
		}
		return true
	}
	return false
}

// EqualJSON 按语义比较两段 JSON（见 Equal）；任一段解析失败返回 false。
func EqualJSON(a, b []byte) bool {
	var x, y any
	if Decode(a, &x) != nil || Decode(b, &y) != nil {
		return false
	}
	return Equal(x, y)
}

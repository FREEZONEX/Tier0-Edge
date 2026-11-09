package fieldpath

// 验证统一字段路径的转义、边界、SQL 常量和对象/数组读取；无数据库或网络依赖。
import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidSegmentBoundaries(t *testing.T) {
	for _, value := range []string{"value", "温度", "0", "00", "a.b", `a\b`, "a?b", "a'b", "field name"} {
		require.True(t, ValidSegment(value), "%q", value)
	}
	for _, value := range []string{"", "$field", "__proto__", "prototype", "constructor", "a\x00b", "a\nb", "a\u007fb", string([]byte{0xff})} {
		require.False(t, ValidSegment(value), "%q", value)
	}
}

func TestParseAndFormatRoundTrip(t *testing.T) {
	for _, parts := range [][]string{
		{"value"}, {"设备", "温度"}, {"a.b", `c\d`, "a?b", "a'b"},
		{"items", "0", "name"}, {strings.Repeat("x", 512)},
		strings.Split("a.b.c.d.e.f.g.h.i.j", "."),
	} {
		path := Format(parts)
		actual, err := Parse(path)
		require.NoError(t, err, "%q", path)
		require.Equal(t, parts, actual)
	}
	require.Equal(t, `a\.b.c\\d`, Format([]string{"a.b", `c\d`}))
	for _, path := range []string{
		"", ".a", "a.", "a..b", `a\`, `a\q`, "a.$b", "a.__proto__", "a.prototype", "a.constructor",
		"a.\nb", string([]byte{'a', '.', 0xff}), strings.Repeat("x", 513),
		strings.Repeat("温", 171), "a.b.c.d.e.f.g.h.i.j.k",
	} {
		parts, err := Parse(path)
		require.ErrorIs(t, err, ErrInvalid, "%q", path)
		require.Nil(t, parts)
	}
}

func TestIndexRequiresCanonicalBoundedDecimal(t *testing.T) {
	for value, expected := range map[string]int{"0": 0, "1": 1, "1000000": 1000000} {
		actual, ok := Index(value)
		require.True(t, ok)
		require.Equal(t, expected, actual)
	}
	for _, value := range []string{"", "-1", "+1", "00", "01", "1.0", " 1", "1 ", "1000001", strings.Repeat("9", 100)} {
		_, ok := Index(value)
		require.False(t, ok, "%q", value)
	}
}

func TestValuePreservesObjectKeysAndDistinguishesMissing(t *testing.T) {
	input := map[string]any{
		"a.b": map[string]any{"items": []any{map[string]any{"value": 27}, nil}},
		"00":  "object key", "null": nil,
	}
	for _, test := range []struct {
		path  []string
		value any
		found bool
	}{
		{nil, input, true},
		{[]string{"a.b", "items", "0", "value"}, 27, true},
		{[]string{"a.b", "items", "1"}, nil, true},
		{[]string{"00"}, "object key", true},
		{[]string{"null"}, nil, true},
		{[]string{"missing"}, nil, false},
		{[]string{"a.b", "items", "00"}, nil, false},
		{[]string{"a.b", "items", "2"}, nil, false},
		{[]string{"a.b", "items", "0", "value", "child"}, nil, false},
		{[]string{"null", "child"}, nil, false},
	} {
		actual, found := Value(input, test.path)
		require.Equal(t, test.found, found, "%v", test.path)
		require.Equal(t, test.value, actual, "%v", test.path)
	}
}

func TestPGTextLiteralQuotesAndAvoidsGORMPlaceholders(t *testing.T) {
	for value, expected := range map[string]string{
		"": "''", "温度": "'温度'", "a'b": "'a''b'", "?": "(''||chr(63)||'')",
		"a?'b?": "('a'||chr(63)||'''b'||chr(63)||'')", "');DROP TABLE t;--": "''');DROP TABLE t;--'",
	} {
		actual := PGTextLiteral(value)
		require.Equal(t, expected, actual)
		require.NotContains(t, actual, "?")
	}
}

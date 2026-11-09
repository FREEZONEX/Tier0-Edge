// Package fieldpath defines the shared public JSON field-path syntax.
package fieldpath

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid field path")

func ValidSegment(s string) bool {
	if s == "" || !utf8.ValidString(s) || strings.HasPrefix(s, "$") || s == "__proto__" || s == "prototype" || s == "constructor" {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// Parse accepts only backslash-dot and backslash-backslash escapes. Numeric
// segments are interpreted as indices only when the current value is an array.
func Parse(path string) ([]string, error) {
	if path == "" || len(path) > 512 || !utf8.ValidString(path) {
		return nil, ErrInvalid
	}
	parts := []string{}
	var b strings.Builder
	for i := 0; i < len(path); i++ {
		switch path[i] {
		case '\\':
			i++
			if i >= len(path) || (path[i] != '.' && path[i] != '\\') {
				return nil, ErrInvalid
			}
			b.WriteByte(path[i])
		case '.':
			if !ValidSegment(b.String()) {
				return nil, ErrInvalid
			}
			parts = append(parts, b.String())
			b.Reset()
		default:
			b.WriteByte(path[i])
		}
	}
	if !ValidSegment(b.String()) {
		return nil, ErrInvalid
	}
	parts = append(parts, b.String())
	if len(parts) > 10 {
		return nil, ErrInvalid
	}
	return parts, nil
}

func Format(parts []string) string {
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.ReplaceAll(strings.ReplaceAll(p, "\\", "\\\\"), ".", "\\.")
	}
	return strings.Join(out, ".")
}

// PGTextLiteral is a trusted SQL expression for a text constant. GORM scans
// question marks even inside SQL quoted strings, so encode them with chr(63)
// rather than allowing a literal field name to consume a bound argument.
func PGTextLiteral(value string) string {
	parts := strings.Split(value, "?")
	for i, part := range parts {
		parts[i] = "'" + strings.ReplaceAll(part, "'", "''") + "'"
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, "||chr(63)||") + ")"
}

func Index(part string) (int, bool) {
	i, err := strconv.Atoi(part)
	return i, err == nil && i >= 0 && i <= 1000000 && strconv.Itoa(i) == part
}

func Value(value any, parts []string) (any, bool) {
	for _, part := range parts {
		switch current := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = current[part]
			if !ok {
				return nil, false
			}
		case []any:
			i, ok := Index(part)
			if !ok || i >= len(current) {
				return nil, false
			}
			value = current[i]
		default:
			return nil, false
		}
	}
	return value, true
}

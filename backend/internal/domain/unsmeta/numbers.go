package unsmeta

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
)

// decodeNumbers preserves the submitted decimal representation throughout data
// validation, equality checks and event creation instead of rounding to float64.
func decodeNumbers(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return ErrInvalid
		}
		return err
	}
	return nil
}

func exactNumber(value any) (*big.Rat, bool) {
	var literal string
	switch number := value.(type) {
	case json.Number:
		literal = string(number)
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, false
		}
		literal = strconv.FormatFloat(number, 'g', -1, 64)
	default:
		// Programmatically constructed schemas may carry integer enum options.
		// Preserve their normal JSON numeric meaning without a float conversion.
		typed := reflect.ValueOf(value)
		switch typed.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			literal = strconv.FormatInt(typed.Int(), 10)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			literal = strconv.FormatUint(typed.Uint(), 10)
		default:
			return nil, false
		}
	}
	// Bound exponent allocation before big.Rat parses it. These limits match
	// PostgreSQL numeric's documented integer/fractional storage bounds.
	if len(literal) == 0 || len(literal) > 256*1024 || (literal[0] != '-' && (literal[0] < '0' || literal[0] > '9')) || !json.Valid([]byte(literal)) {
		return nil, false
	}
	if i := strings.IndexAny(literal, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(literal[i+1:])
		if err != nil || exponent < -16383 || exponent > 131071 {
			return nil, false
		}
	}
	number, ok := new(big.Rat).SetString(literal)
	return number, ok
}

// equalJSONValues follows JSONB numeric equality (1 == 1.0 == 1e0), while
// preserving distinctions beyond IEEE-754's exact integer range.
func equalJSONValues(left, right any) bool {
	if a, ok := exactNumber(left); ok {
		b, ok := exactNumber(right)
		return ok && a.Cmp(b) == 0
	}
	switch a := left.(type) {
	case map[string]any:
		b, ok := right.(map[string]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for key, value := range a {
			other, exists := b[key]
			if !exists || !equalJSONValues(value, other) {
				return false
			}
		}
		return true
	case []any:
		b, ok := right.([]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for i := range a {
			if !equalJSONValues(a[i], b[i]) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(left, right)
	}
}

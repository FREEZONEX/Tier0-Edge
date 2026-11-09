package query

import (
	"encoding/json"
	"math/big"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"backend/internal/domain/document/num"
)

var (
	isoDateRx  = regexp.MustCompile(isoDateRe)
	toNumberRx = regexp.MustCompile(toNumberRe)
	maxMsRat   = new(big.Rat).SetInt64(1_000_000_000_000_000)
)

// EvalExpr 在 Go 里求值表达式（语义与 ExprSQL 相同）。第二个返回值为 false 表示缺失 / 无效（SQL NULL）；
// (nil, true) 是 JSON null。
func EvalExpr(e Expr, doc Doc) (any, bool) {
	switch t := e.(type) {
	case FieldRef:
		return get(doc, t.Path)
	case Literal:
		return t.Val, true
	case ArrayExpr:
		out := make([]any, len(t.Items))
		for i, it := range t.Items {
			v, ok := EvalExpr(it, doc)
			if ok {
				out[i] = v
			}
		}
		return out, true
	case OpExpr:
		return evalOp(t, doc)
	}
	return nil, false
}

func nullish(v any, ok bool) bool { return !ok || v == nil }

func numArgs(vals []any, oks []bool) ([]*big.Rat, bool) {
	out := make([]*big.Rat, len(vals))
	for i, v := range vals {
		if !oks[i] {
			return nil, false
		}
		r, ok := num.Rat(v)
		if !ok {
			return nil, false
		}
		out[i] = r
	}
	return out, true
}

func ratJSON(r *big.Rat) (any, bool) { return num.Format(r), true }

func evalOp(e OpExpr, doc Doc) (any, bool) {
	switch e.Op {
	case "$cond":
		c, ok := EvalExpr(e.Args[0], doc)
		if truthy(c, ok) {
			return EvalExpr(e.Args[1], doc)
		}
		return EvalExpr(e.Args[2], doc)
	case "$switch":
		for _, br := range e.Branches {
			c, ok := EvalExpr(br[0], doc)
			if truthy(c, ok) {
				return EvalExpr(br[1], doc)
			}
		}
		if len(e.Args) == 1 {
			return EvalExpr(e.Args[0], doc)
		}
		return nil, false
	case "$dateTrunc":
		d, ok := EvalExpr(e.Named["date"], doc)
		ts, ok := tsOf(d, ok)
		if !ok {
			return nil, false
		}
		tz := exprTZ(e)
		unit := e.Named["unit"].(Literal).Val.(string)
		return json.Number(big.NewInt(truncTime(ts.In(tz.loc), unit).UnixMilli()).String()), true
	case "$dateToParts":
		d, ok := EvalExpr(e.Named["date"], doc)
		ts, ok := tsOf(d, ok)
		if !ok {
			return nil, false
		}
		l := ts.In(exprTZ(e).loc)
		n := func(i int) json.Number { return json.Number(big.NewInt(int64(i)).String()) }
		return map[string]any{"year": n(l.Year()), "month": n(int(l.Month())), "day": n(l.Day()), "hour": n(l.Hour()),
			"minute": n(l.Minute()), "second": n(l.Second()), "millisecond": n(l.Nanosecond() / 1e6)}, true
	case "$dateFromParts":
		keys := []string{"year", "month", "day", "hour", "minute", "second", "millisecond"}
		lo := map[string]int64{"year": 1, "month": 1, "day": 1}
		hi := map[string]int64{"year": 9999, "month": 12, "day": 31, "hour": 23, "minute": 59, "second": 59, "millisecond": 999}
		vals := map[string]int64{"month": 1, "day": 1}
		for _, k := range keys {
			ex, ok := e.Named[k]
			if !ok {
				continue
			}
			v, ok := EvalExpr(ex, doc)
			if !ok {
				return nil, false
			}
			r, isNum := num.Rat(v)
			if !isNum || !r.IsInt() || !r.Num().IsInt64() {
				return nil, false
			}
			n := r.Num().Int64()
			if n < lo[k] || n > hi[k] {
				return nil, false
			}
			vals[k] = n
		}
		loc := exprTZ(e).loc
		t := time.Date(int(vals["year"]), time.Month(vals["month"]), int(vals["day"]), int(vals["hour"]), int(vals["minute"]),
			int(vals["second"]), int(vals["millisecond"])*1e6, loc)
		// 不做进位（2 月 30 日无效），与 PG 的 pg_input_is_valid 一致
		if t.Year() != int(vals["year"]) || int64(t.Month()) != vals["month"] || int64(t.Day()) != vals["day"] {
			return nil, false
		}
		return json.Number(big.NewInt(t.UnixMilli()).String()), true
	case "$trim":
		v, ok := EvalExpr(e.Named["input"], doc)
		s, isStr := v.(string)
		if !ok || !isStr {
			return nil, false
		}
		chars := defaultTrimChars
		if c, ok := e.Named["chars"].(Literal); ok {
			chars, _ = c.Val.(string)
		}
		return strings.Trim(s, chars), true
	case "$round":
		v, ok := EvalExpr(e.Args[0], doc)
		r, isNum := num.Rat(v)
		if !ok || !isNum {
			return nil, false
		}
		place := int64(0)
		if len(e.Args) == 2 {
			p, ok := EvalExpr(e.Args[1], doc)
			pr, isNum := num.Rat(p)
			if !ok || !isNum || !pr.IsInt() || !pr.Num().IsInt64() || pr.Num().Int64() < -20 || pr.Num().Int64() > 100 {
				return nil, false
			}
			place = pr.Num().Int64()
		}
		return ratJSON(roundHalfAway(r, place))
	}
	vals := make([]any, len(e.Args))
	oks := make([]bool, len(e.Args))
	for i, a := range e.Args {
		vals[i], oks[i] = EvalExpr(a, doc)
	}
	switch e.Op {
	case "$add", "$multiply":
		rs, ok := numArgs(vals, oks)
		if !ok {
			return nil, false
		}
		acc := new(big.Rat).Set(rs[0])
		for _, r := range rs[1:] {
			if e.Op == "$add" {
				acc.Add(acc, r)
			} else {
				acc.Mul(acc, r)
			}
		}
		return ratJSON(acc)
	case "$subtract":
		rs, ok := numArgs(vals, oks)
		if !ok {
			return nil, false
		}
		return ratJSON(new(big.Rat).Sub(rs[0], rs[1]))
	case "$divide":
		rs, ok := numArgs(vals, oks)
		if !ok || rs[1].Sign() == 0 {
			return nil, false
		}
		return ratJSON(new(big.Rat).Quo(rs[0], rs[1]))
	case "$mod":
		rs, ok := numArgs(vals, oks)
		if !ok || rs[1].Sign() == 0 {
			return nil, false
		}
		q := new(big.Rat).Quo(rs[0], rs[1])
		tr := new(big.Int).Quo(q.Num(), q.Denom())
		return ratJSON(new(big.Rat).Sub(rs[0], new(big.Rat).Mul(rs[1], new(big.Rat).SetInt(tr))))
	case "$abs":
		rs, ok := numArgs(vals, oks)
		if !ok {
			return nil, false
		}
		return ratJSON(new(big.Rat).Abs(rs[0]))
	case "$floor", "$ceil":
		rs, ok := numArgs(vals, oks)
		if !ok {
			return nil, false
		}
		var i *big.Int
		if e.Op == "$floor" {
			i = ratFloor(rs[0])
		} else {
			i = ratCeil(rs[0])
		}
		return json.Number(i.String()), true
	case "$eq", "$ne":
		var eq bool
		switch {
		case nullish(vals[0], oks[0]) && nullish(vals[1], oks[1]):
			eq = true
		case nullish(vals[0], oks[0]) || nullish(vals[1], oks[1]):
			eq = false
		default:
			eq = num.Equal(vals[0], vals[1])
		}
		return eq == (e.Op == "$eq"), true
	case "$gt", "$gte", "$lt", "$lte":
		if !oks[0] || !oks[1] {
			return nil, false
		}
		ta, tb := jsonType(vals[0]), jsonType(vals[1])
		if ta != tb || (ta != "number" && ta != "string" && ta != "boolean") {
			return nil, false
		}
		c, _ := compareSameType(vals[0], vals[1])
		switch e.Op {
		case "$gt":
			return c > 0, true
		case "$gte":
			return c >= 0, true
		case "$lt":
			return c < 0, true
		}
		return c <= 0, true
	case "$and":
		for i := range vals {
			if !truthy(vals[i], oks[i]) {
				return false, true
			}
		}
		return true, true
	case "$or":
		for i := range vals {
			if truthy(vals[i], oks[i]) {
				return true, true
			}
		}
		return false, true
	case "$not":
		return !truthy(vals[0], oks[0]), true
	case "$in":
		arr, ok := vals[1].([]any)
		if !oks[1] || !ok {
			return nil, false
		}
		x := vals[0]
		if !oks[0] {
			x = nil
		}
		for _, el := range arr {
			if (el == nil && x == nil) || (el != nil && x != nil && num.Equal(el, x)) {
				return true, true
			}
		}
		return false, true
	case "$ifNull":
		for i := range vals {
			if !nullish(vals[i], oks[i]) {
				return vals[i], true
			}
		}
		return nil, false
	case "$concat":
		var sb strings.Builder
		for i, v := range vals {
			s, ok := v.(string)
			if !oks[i] || !ok {
				return nil, false
			}
			sb.WriteString(s)
		}
		if sb.Len() > MaxConcatBytes {
			return nil, false
		}
		return sb.String(), true
	case "$toLower", "$toUpper":
		s, ok := vals[0].(string)
		if !oks[0] || !ok {
			return nil, false
		}
		if e.Op == "$toLower" {
			return strings.ToLower(s), true
		}
		return strings.ToUpper(s), true
	case "$substrCP":
		s, ok := vals[0].(string)
		st, ok1 := intArg(vals[1])
		cn, ok2 := intArg(vals[2])
		if !oks[0] || !ok || !oks[1] || !oks[2] || !ok1 || !ok2 || st < 0 || cn < 0 || st >= 1e9 || cn >= 1e9 {
			return nil, false
		}
		rs := []rune(s)
		if int(st) >= len(rs) {
			return "", true
		}
		end := int(st + cn)
		if end > len(rs) {
			end = len(rs)
		}
		return string(rs[st:end]), true
	case "$strLenCP":
		s, ok := vals[0].(string)
		if !oks[0] || !ok {
			return nil, false
		}
		return json.Number(big.NewInt(int64(utf8.RuneCountInString(s))).String()), true
	case "$toNumber":
		if !oks[0] {
			return nil, false
		}
		switch v := vals[0].(type) {
		case string:
			if !toNumberRx.MatchString(v) {
				return nil, false
			}
			r, ok := num.Rat(json.Number(v))
			if !ok {
				return nil, false
			}
			return ratJSON(r)
		case bool:
			if v {
				return json.Number("1"), true
			}
			return json.Number("0"), true
		}
		if num.IsNumber(vals[0]) {
			return vals[0], true
		}
		return nil, false
	case "$toString":
		if !oks[0] {
			return nil, false
		}
		switch v := vals[0].(type) {
		case string:
			return v, true
		case bool:
			if v {
				return "true", true
			}
			return "false", true
		}
		if r, ok := num.Rat(vals[0]); ok {
			return string(num.Format(r)), true
		}
		return nil, false
	case "$type":
		if !oks[0] {
			return "missing", true
		}
		return jsonType(vals[0]), true
	case "$size":
		arr, ok := vals[0].([]any)
		if !oks[0] || !ok {
			return nil, false
		}
		return json.Number(big.NewInt(int64(len(arr))).String()), true
	case "$arrayElemAt":
		arr, ok := vals[0].([]any)
		i, ok1 := intArg(vals[1])
		if !oks[0] || !ok || !oks[1] || !ok1 || i <= -1e6 || i >= 1e6 {
			return nil, false
		}
		if i < 0 {
			i += int64(len(arr))
		}
		if i < 0 || i >= int64(len(arr)) {
			return nil, false
		}
		return arr[i], true
	case "$first", "$last":
		arr, ok := vals[0].([]any)
		if !oks[0] || !ok || len(arr) == 0 {
			return nil, false
		}
		if e.Op == "$first" {
			return arr[0], true
		}
		return arr[len(arr)-1], true
	}
	return nil, false
}

// tsOf 把毫秒数或 ISO 8601 字符串换成时间（与 tsSQL 一致；不带时区的字符串按 UTC）。
func tsOf(v any, ok bool) (time.Time, bool) {
	if !ok || v == nil {
		return time.Time{}, false
	}
	if r, isNum := num.Rat(v); isNum {
		if new(big.Rat).Abs(r).Cmp(maxMsRat) >= 0 {
			return time.Time{}, false
		}
		ms := new(big.Int).Quo(r.Num(), r.Denom())
		return time.UnixMilli(ms.Int64()).UTC(), true
	}
	s, isStr := v.(string)
	if !isStr || !isoDateRx.MatchString(s) {
		return time.Time{}, false
	}
	return parseISO(s)
}

// parseISO 解析 isoDateRe 接受的写法。
func parseISO(s string) (time.Time, bool) {
	s = strings.Replace(s, " ", "T", 1)
	// 时区：Z、±HH、±HHMM、±HH:MM
	zone := ""
	if strings.HasSuffix(s, "Z") {
		s, zone = s[:len(s)-1], "Z"
	} else if i := strings.LastIndexAny(s, "+-"); i > 9 {
		s, zone = s[:i], s[i:]
	}
	layouts := []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04", "2006-01-02"}
	var t time.Time
	var err error
	for _, l := range layouts {
		if t, err = time.Parse(l, s); err == nil {
			break
		}
	}
	if err != nil {
		return time.Time{}, false
	}
	switch {
	case zone == "" || zone == "Z":
		return t, true
	default:
		sign := 1
		if zone[0] == '-' {
			sign = -1
		}
		z := strings.ReplaceAll(zone[1:], ":", "")
		if len(z) == 2 {
			z += "00"
		}
		if len(z) != 4 {
			return time.Time{}, false
		}
		h, m := int(z[0]-'0')*10+int(z[1]-'0'), int(z[2]-'0')*10+int(z[3]-'0')
		return t.Add(-time.Duration(sign*(h*60+m)) * time.Minute), true
	}
}

// truncTime 按单位截断本地时间（week 从周一开始，与 PG date_trunc 一致）。
func truncTime(l time.Time, unit string) time.Time {
	y, mo, d := l.Date()
	loc := l.Location()
	switch unit {
	case "minute":
		return time.Date(y, mo, d, l.Hour(), l.Minute(), 0, 0, loc)
	case "hour":
		return time.Date(y, mo, d, l.Hour(), 0, 0, 0, loc)
	case "day":
		return time.Date(y, mo, d, 0, 0, 0, 0, loc)
	case "week":
		back := (int(l.Weekday()) + 6) % 7
		return time.Date(y, mo, d-back, 0, 0, 0, 0, loc)
	case "month":
		return time.Date(y, mo, 1, 0, 0, 0, 0, loc)
	case "year":
		return time.Date(y, 1, 1, 0, 0, 0, 0, loc)
	}
	return l
}

// roundHalfAway 按 place 位小数四舍五入（半数远离零，与 PG round(numeric, int) 一致）。
func roundHalfAway(r *big.Rat, place int64) *big.Rat {
	scale := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(abs64(place)), nil))
	x := new(big.Rat).Set(r)
	if place >= 0 {
		x.Mul(x, scale)
	} else {
		x.Quo(x, scale)
	}
	half := big.NewRat(1, 2)
	if x.Sign() >= 0 {
		x.Add(x, half)
	} else {
		x.Sub(x, half)
	}
	i := new(big.Int).Quo(x.Num(), x.Denom())
	out := new(big.Rat).SetInt(i)
	if place >= 0 {
		out.Quo(out, scale)
	} else {
		out.Mul(out, scale)
	}
	return out
}

func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

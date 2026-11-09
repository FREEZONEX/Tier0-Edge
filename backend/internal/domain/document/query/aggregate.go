package query

import (
	"fmt"
	"strings"
)

// 聚合管线与联表（docs/collection-crud/design.md 第 5.3、5.4 节）。

// 管线与联表的上限。
const (
	MaxPipelineStages  = 16
	MaxGroups          = 2
	MaxLookups         = 2
	MaxAggregateOutput = 10000
	MaxPushElems       = 1000
	MaxPushValueBytes  = 1024
	LookupDefaultLimit = 10
	LookupMaxLimit     = 100
	LookupScanCap      = 1000
	LookupLocalArray   = 100
	LookupMaxBytes     = 1 << 20
	LookupMaxFanout    = 100000
)

// LookupSpec 是一个 $lookup。
type LookupSpec struct {
	From         string
	LocalField   Path
	ForeignField Path
	As           string
	Filter       Node
	Projection   *Projection
	Sort         []SortKey
	Limit        int
	// 由调用方填写：对方集合的 schema 数组判断，foreignField 是否是 _id / 唯一索引（扇出按 1 计算）
	ForeignArray  func(Path) bool
	ForeignUnique bool
}

// ParseLookup 解析 $lookup 的参数（对象）。
func ParseLookup(v any) (*LookupSpec, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errf("$lookup must be an object")
	}
	spec := &LookupSpec{Limit: LookupDefaultLimit}
	for _, k := range sortedKeys(m) {
		x := m[k]
		var err error
		switch k {
		case "from":
			s, ok := x.(string)
			if !ok || s == "" {
				return nil, errf("$lookup.from must be a collection id")
			}
			spec.From = s
		case "localField", "foreignField":
			s, ok := x.(string)
			if !ok {
				return nil, errf("$lookup.%s must be a path", k)
			}
			p, perr := ParsePath(s)
			if perr != nil {
				return nil, &Error{Msg: "$lookup." + k + ": " + perr.Error()}
			}
			if k == "localField" {
				spec.LocalField = p
			} else {
				if p.IsSys() && p.Sys != "id" {
					return nil, errf("$lookup.foreignField must be _id or a document path")
				}
				spec.ForeignField = p
			}
		case "as":
			s, ok := x.(string)
			if !ok || s == "" || strings.ContainsAny(s, ".\\") || strings.HasPrefix(s, "$") || ReservedKey(s) {
				return nil, errf("$lookup.as must be a single field name other than _id and _meta")
			}
			spec.As = s
		case "filter":
			spec.Filter, err = ParseFilter(x)
		case "projection":
			spec.Projection, err = ParseProjection(x)
		case "sort":
			spec.Sort, err = ParseSort(x, nil)
		case "limit":
			n, ok := intArg(x)
			if !ok || n < 1 || n > LookupMaxLimit {
				return nil, errf("$lookup.limit must be between 1 and %d", LookupMaxLimit)
			}
			spec.Limit = int(n)
		case "let", "pipeline":
			return nil, errf("$lookup.%s is not supported (only equality joins with localField / foreignField)", k)
		default:
			return nil, errf("unknown $lookup option %q", k)
		}
		if err != nil {
			return nil, err
		}
	}
	if spec.From == "" || spec.LocalField.Raw == "" || spec.ForeignField.Raw == "" || spec.As == "" {
		return nil, errf("$lookup needs from, localField, foreignField and as")
	}
	if spec.Filter == nil {
		spec.Filter = And{}
	}
	if spec.Sort == nil {
		spec.Sort, _ = ParseSort(nil, nil)
	}
	return spec, nil
}

// Fanout 是每条外层文档最多关联几条（守卫用）：按 _id 或唯一索引关联时为 1。
func (l *LookupSpec) Fanout() int {
	if l.ForeignField.Sys == "id" || l.ForeignUnique {
		return 1
	}
	return l.Limit
}

// LookupOut 是一个联表在 SQL 里的输出列。
type LookupOut struct {
	Alias string
	// Docs 是关联文档数组（jsonb），Truncated / ScanCapped / LocalTruncated 是标记，Mismatch 是类型不一致的本地值个数
	Docs, Truncated, ScanCapped, LocalTruncated, Mismatch string
}

// LookupSQL 编译一个联表：返回 LEFT JOIN LATERAL 子句和输出列。local 是本地值的 jsonb 表达式（外层别名已限定），
// docShape 为 "record" 时关联文档是记录视图 {id, data, revision, createdAt, updatedAt}，为 "doc" 时是管线文档 {_id, _meta, ...data}。
func (b *Builder) LookupSQL(l *LookupSpec, local, alias, docShape string) (string, LookupOut, error) {
	collPH := b.Arg(l.From)
	// 本地值：标量或数组元素（最多 100 个），排除缺失和 JSON null；_id 关联只取字符串
	elems := fmt.Sprintf(`SELECT x FROM (SELECT CASE WHEN jsonb_typeof(%[1]s) = 'array' THEN NULL ELSE %[1]s END AS x
		UNION ALL SELECT e FROM jsonb_array_elements(CASE WHEN jsonb_typeof(%[1]s) = 'array' THEN %[1]s END) WITH ORDINALITY AS t(e, i) WHERE i <= %[2]d) v
		WHERE x IS NOT NULL AND x <> 'null'::jsonb`, local, LookupLocalArray)
	var match, mismatch string
	if l.ForeignField.Sys == "id" {
		match = fmt.Sprintf(`id = ANY(ARRAY(SELECT x #>> '{}' FROM (%s) w WHERE jsonb_typeof(x) = 'string'))`, elems)
		mismatch = fmt.Sprintf(`(SELECT count(*) FROM (%s) w WHERE jsonb_typeof(x) <> 'string')`, elems)
	} else {
		match = fmt.Sprintf(`(%s) = ANY(ARRAY(%s))`, l.ForeignField.SQL(), elems)
		mismatch = "0"
	}
	fb := &Builder{Args: b.Args, ArrayPath: l.ForeignArray}
	filter, err := fb.Where(l.Filter)
	if err != nil {
		return "", LookupOut{}, err
	}
	ob, _ := fb.OrderBy(l.Sort)
	b.Args = fb.Args
	var doc string
	if docShape == "doc" {
		doc = DocSQL("")
	} else {
		doc = "jsonb_build_object('id', id, 'data', data, 'revision', revision, 'createdAt', " + MsSQL("created_at") +
			", 'updatedAt', " + MsSQL("updated_at") + ", 'sourceId', source_id)"
	}
	limitPH := b.Arg(l.Limit)
	cols := "id, data, revision, created_at, updated_at, source_id, source_time, lock_expires_at"
	inner := fmt.Sprintf(`SELECT %s, %s FROM {R} WHERE collection_id = %s AND deleted_at IS NULL AND %s OFFSET 0 LIMIT %d`,
		cols, "1 AS one", collPH, match, LookupScanCap)
	lat := fmt.Sprintf(`LEFT JOIN LATERAL (
		SELECT COALESCE(jsonb_agg(t.d ORDER BY t.n) FILTER (WHERE t.n <= %[1]s AND t.bytes <= %[2]d), '[]'::jsonb) AS docs,
		       COALESCE(bool_or(t.n > %[1]s OR t.bytes > %[2]d), FALSE) AS truncated
		FROM (
			SELECT s.d, row_number() OVER w AS n, sum(octet_length(s.d::text)) OVER w AS bytes
			FROM (SELECT %[3]s AS d, q.* FROM (%[4]s) q) s
			WHERE %[5]s
			WINDOW w AS (ORDER BY %[6]s ROWS UNBOUNDED PRECEDING)
			ORDER BY %[6]s LIMIT %[1]s::int + 1
		) t
	) %[7]s ON TRUE`, limitPH, LookupMaxBytes, doc, inner, filter, ob, alias)
	out := LookupOut{
		Alias:          alias,
		Docs:           alias + ".docs",
		Truncated:      "to_jsonb(" + alias + ".truncated)",
		ScanCapped:     fmt.Sprintf(`to_jsonb((SELECT count(*) = %d FROM (SELECT 1 FROM {R} WHERE collection_id = %s AND deleted_at IS NULL AND %s OFFSET 0 LIMIT %d) z))`, LookupScanCap+1, collPH, match, LookupScanCap+1),
		LocalTruncated: fmt.Sprintf(`to_jsonb(COALESCE(jsonb_typeof(%[1]s) = 'array' AND jsonb_array_length(%[1]s) > %[2]d, FALSE))`, local, LookupLocalArray),
		Mismatch:       "to_jsonb(" + mismatch + ")",
	}
	return lat, out, nil
}

// DocSQL 是把记录行写成管线文档 {_id, _meta, ...data} 的表达式（系统字段覆盖 data 里的同名键）。prefix 是列的表别名前缀（"" 或 "r."）。
func DocSQL(prefix string) string {
	p := prefix
	return p + "data || jsonb_build_object('_id', " + p + "id, '_meta', jsonb_strip_nulls(jsonb_build_object('revision', " + p + "revision, 'createdAt', " +
		MsSQL(p+"created_at") + ", 'updatedAt', " + MsSQL(p+"updated_at") + ", 'sourceId', " + p + "source_id, 'sourceTime', " + MsSQL(p+"source_time") +
		", 'locked', (" + p + "lock_expires_at IS NOT NULL AND " + p + "lock_expires_at > clock_timestamp()))))"
}

// ---------------- 聚合管线 ----------------

// Stage 是管线的一个阶段。
type Stage struct {
	Op      string // $match $group $sort $limit $skip $project $addFields $count $unwind $lookup
	Filter  Node
	Group   *GroupSpec
	Sort    []SortKey
	N       int64
	Project *ProjectSpec
	Add     []NamedExpr
	Name    string // $count 的输出名
	Unwind  *UnwindSpec
	Lookup  *LookupSpec
}

// NamedExpr 是一个输出字段和它的表达式。
type NamedExpr struct {
	Name string
	E    Expr
}

// GroupSpec 是 $group。
type GroupSpec struct {
	ID   Expr // nil 表示 null（整体一组）
	Accs []Accumulator
}

// Accumulator 是一个累加器。
type Accumulator struct {
	Name string
	Op   string // $sum $avg $min $max $count $first $last $push $addToSet
	E    Expr
}

// ProjectSpec 是 $project：包含（路径）、排除（路径）或计算字段（表达式）。
type ProjectSpec struct {
	Include  []Path
	Exclude  []Path
	Computed []NamedExpr
	NoID     bool
}

// UnwindSpec 是 $unwind。
type UnwindSpec struct {
	Path     Path
	Preserve bool
}

// ParsePipeline 解析管线。
func ParsePipeline(v any) ([]Stage, error) {
	arr, ok := v.([]any)
	if !ok {
		return nil, errf("pipeline must be an array of stages")
	}
	if len(arr) > MaxPipelineStages {
		return nil, errf("pipeline has more than %d stages", MaxPipelineStages)
	}
	var out []Stage
	groups, lookups := 0, 0
	for i, x := range arr {
		m, ok := x.(map[string]any)
		if !ok || len(m) != 1 {
			return nil, errf("stage %d must be an object with exactly one operator", i)
		}
		for op, arg := range m {
			st, err := parseStage(op, arg)
			if err != nil {
				return nil, errf("stage %d (%s): %v", i, op, err)
			}
			if st.Op == "$group" {
				groups++
			}
			if st.Op == "$lookup" {
				lookups++
			}
			out = append(out, st)
		}
	}
	if groups > MaxGroups {
		return nil, errf("at most %d $group stages", MaxGroups)
	}
	if lookups > MaxLookups {
		return nil, errf("at most %d $lookup stages", MaxLookups)
	}
	return reorderPipeline(out), nil
}

func parseStage(op string, arg any) (Stage, error) {
	switch op {
	case "$match":
		f, err := ParseFilter(arg)
		return Stage{Op: op, Filter: f}, err
	case "$sort":
		keys, err := ParseSort(arg, nil)
		if err != nil {
			return Stage{}, err
		}
		// 管线里的 $sort 不自动补 _id（分组后的文档没有记录的 _id 语义）
		if n := len(keys); n > 1 && keys[n-1].Path.Sys == "id" && !sortHasID(arg) {
			keys = keys[:n-1]
		}
		return Stage{Op: op, Sort: keys}, nil
	case "$limit", "$skip":
		n, ok := intArg(arg)
		if !ok || n < 0 || (op == "$limit" && n == 0) {
			return Stage{}, errf("%s must be a positive integer", op)
		}
		return Stage{Op: op, N: n}, nil
	case "$count":
		s, ok := arg.(string)
		if !ok || s == "" || strings.ContainsAny(s, ".$") {
			return Stage{}, errf("$count needs an output field name")
		}
		return Stage{Op: op, Name: s}, nil
	case "$unwind":
		var u UnwindSpec
		var ps string
		switch t := arg.(type) {
		case string:
			ps = t
		case map[string]any:
			ps, _ = t["path"].(string)
			if p, ok := t["preserveNullAndEmptyArrays"].(bool); ok {
				u.Preserve = p
			}
			for k := range t {
				if k != "path" && k != "preserveNullAndEmptyArrays" {
					return Stage{}, errf("unknown $unwind option %q", k)
				}
			}
		}
		if !strings.HasPrefix(ps, "$") {
			return Stage{}, errf("$unwind path must start with $")
		}
		p, err := ParsePath(ps[1:])
		if err != nil || p.IsSys() {
			return Stage{}, errf("bad $unwind path %q", ps)
		}
		u.Path = p
		return Stage{Op: op, Unwind: &u}, nil
	case "$lookup":
		l, err := ParseLookup(arg)
		return Stage{Op: op, Lookup: l}, err
	case "$group":
		g, err := parseGroup(arg)
		return Stage{Op: op, Group: g}, err
	case "$project":
		p, err := parseProject(arg)
		return Stage{Op: op, Project: p}, err
	case "$addFields", "$set":
		m, ok := arg.(map[string]any)
		if !ok || len(m) == 0 {
			return Stage{}, errf("%s needs {field: expression}", op)
		}
		var add []NamedExpr
		for _, k := range sortedKeys(m) {
			if strings.ContainsAny(k, ".$\\") || ReservedKey(k) {
				return Stage{}, errf("%s field %q must be a top-level name other than _id / _meta", op, k)
			}
			e, err := ParseExpr(m[k])
			if err != nil {
				return Stage{}, err
			}
			add = append(add, NamedExpr{Name: k, E: e})
		}
		return Stage{Op: "$addFields", Add: add}, nil
	}
	return Stage{}, errf("unsupported stage %s", op)
}

func sortHasID(arg any) bool {
	switch t := arg.(type) {
	case []any:
		for _, x := range t {
			if p, ok := x.([]any); ok && len(p) > 0 && p[0] == "_id" {
				return true
			}
		}
	case map[string]any:
		_, ok := t["_id"]
		return ok
	}
	return false
}

func parseGroup(arg any) (*GroupSpec, error) {
	m, ok := arg.(map[string]any)
	if !ok {
		return nil, errf("$group needs {_id: expression, field: {accumulator: expression}}")
	}
	idv, ok := m["_id"]
	if !ok {
		return nil, errf("$group needs _id (use null for a single group)")
	}
	g := &GroupSpec{}
	if idv != nil {
		if obj, isObj := idv.(map[string]any); isObj && !hasOpKey(obj) {
			// {k: 表达式}：写成一个对象表达式
			var items []Expr
			var names []string
			for _, k := range sortedKeys(obj) {
				e, err := ParseExpr(obj[k])
				if err != nil {
					return nil, err
				}
				items = append(items, e)
				names = append(names, k)
			}
			g.ID = objectExpr{names: names, items: items}
		} else {
			e, err := ParseExpr(idv)
			if err != nil {
				return nil, err
			}
			g.ID = e
		}
	}
	for _, k := range sortedKeys(m) {
		if k == "_id" {
			continue
		}
		if strings.ContainsAny(k, ".$\\") || ReservedKey(k) {
			return nil, errf("$group output %q must be a top-level name", k)
		}
		acc, ok := m[k].(map[string]any)
		if !ok || len(acc) != 1 {
			return nil, errf("$group field %s needs {accumulator: expression}", k)
		}
		for op, x := range acc {
			switch op {
			case "$sum", "$avg", "$min", "$max", "$first", "$last", "$push", "$addToSet":
				e, err := ParseExpr(x)
				if err != nil {
					return nil, err
				}
				g.Accs = append(g.Accs, Accumulator{Name: k, Op: op, E: e})
			case "$count":
				g.Accs = append(g.Accs, Accumulator{Name: k, Op: op})
			default:
				return nil, errf("unsupported accumulator %s", op)
			}
		}
	}
	return g, nil
}

func hasOpKey(m map[string]any) bool {
	for k := range m {
		if strings.HasPrefix(k, "$") {
			return true
		}
	}
	return false
}

// objectExpr 是 $group._id 的 {k: 表达式}（内部使用）。
type objectExpr struct {
	names []string
	items []Expr
}

func (objectExpr) isExpr() {}

func parseProject(arg any) (*ProjectSpec, error) {
	m, ok := arg.(map[string]any)
	if !ok || len(m) == 0 {
		return nil, errf("$project needs {field: 1 | 0 | expression}")
	}
	if len(m) > MaxProjectionKeys {
		return nil, errf("$project has more than %d fields", MaxProjectionKeys)
	}
	p := &ProjectSpec{}
	for _, k := range sortedKeys(m) {
		v := m[k]
		if k == "_id" {
			if n, ok := intArg(v); ok && n == 0 || v == false {
				p.NoID = true
				continue
			}
			if n, ok := intArg(v); ok && n == 1 || v == true {
				continue
			}
		}
		if n, ok := intArg(v); ok && (n == 0 || n == 1) {
			path, err := ParsePath(k)
			if err != nil {
				return nil, &Error{Msg: err.Error()}
			}
			path = docPath(path)
			if n == 1 {
				p.Include = append(p.Include, path)
			} else {
				p.Exclude = append(p.Exclude, path)
			}
			continue
		}
		if b, ok := v.(bool); ok {
			path, err := ParsePath(k)
			if err != nil {
				return nil, &Error{Msg: err.Error()}
			}
			path = docPath(path)
			if b {
				p.Include = append(p.Include, path)
			} else {
				p.Exclude = append(p.Exclude, path)
			}
			continue
		}
		if strings.ContainsAny(k, ".$\\") || ReservedKey(k) {
			return nil, errf("$project computed field %q must be a top-level name", k)
		}
		e, err := ParseExpr(v)
		if err != nil {
			return nil, err
		}
		p.Computed = append(p.Computed, NamedExpr{Name: k, E: e})
	}
	if len(p.Exclude) > 0 && (len(p.Include) > 0 || len(p.Computed) > 0) {
		return nil, errf("$project cannot mix exclusion with inclusion or computed fields")
	}
	return p, nil
}

// reorderPipeline 把只引用本地字段的 $match，以及本地键上的 $sort + $limit，挪到 $lookup 前面（外层规模按过滤后计算）。
func reorderPipeline(st []Stage) []Stage {
	for changed := true; changed; {
		changed = false
		for i := 1; i < len(st); i++ {
			if st[i-1].Op != "$lookup" {
				continue
			}
			as := st[i-1].Lookup.As
			switch st[i].Op {
			case "$match":
				if !nodeUses(st[i].Filter, as) {
					st[i-1], st[i] = st[i], st[i-1]
					changed = true
				}
			case "$sort":
				if i+1 < len(st) && st[i+1].Op == "$limit" && !sortUses(st[i].Sort, as) {
					l := st[i-1]
					st[i-1], st[i], st[i+1] = st[i], st[i+1], l
					changed = true
				}
			}
		}
	}
	return st
}

func sortUses(keys []SortKey, field string) bool {
	for _, k := range keys {
		if len(k.Path.Segs) > 0 && k.Path.Segs[0] == field {
			return true
		}
	}
	return false
}

func nodeUses(n Node, field string) bool {
	for _, p := range Paths(n) {
		if len(p.Segs) > 0 && p.Segs[0] == field {
			return true
		}
	}
	if _, ok := n.(ExprCond); ok {
		return true // 表达式里可能引用任何字段，保守处理
	}
	switch t := n.(type) {
	case And:
		for _, k := range t.Kids {
			if nodeUses(k, field) {
				return true
			}
		}
	case Or:
		for _, k := range t.Kids {
			if nodeUses(k, field) {
				return true
			}
		}
	case Nor:
		for _, k := range t.Kids {
			if nodeUses(k, field) {
				return true
			}
		}
	case Not:
		return nodeUses(t.Kid, field)
	}
	return false
}

// AggregateSQL 编译管线。返回的 SQL 输出一列 doc::text，最多 MaxAggregateOutput+1 行（多出的一行表示截断）。
// arrayPath 用于开头下推到表上的 $match；resolve 为每个 $lookup 填写对方集合的信息（权限、索引检查由调用方做）。
func (b *Builder) AggregateSQL(stages []Stage, arrayPath func(Path) bool, resolve func(*LookupSpec) error) (string, error) {
	// 开头的 $match（以及紧跟的 $sort / $skip / $limit）下推到表上，能用上索引
	tb := &Builder{Args: b.Args, ArrayPath: arrayPath}
	var where []string
	i := 0
	for ; i < len(stages) && stages[i].Op == "$match"; i++ {
		w, err := tb.Where(stages[i].Filter)
		if err != nil {
			return "", err
		}
		where = append(where, w)
	}
	base := "SELECT " + DocSQL("") + " AS doc, NULL::bigint AS ord FROM {R} WHERE collection_id = $1 AND deleted_at IS NULL"
	if len(where) > 0 {
		base += " AND " + strings.Join(where, " AND ")
	}
	if i < len(stages) && stages[i].Op == "$sort" {
		ob, _ := tb.OrderBy(stages[i].Sort)
		base = "SELECT " + DocSQL("") + " AS doc, row_number() OVER (ORDER BY " + ob + ") AS ord FROM {R} WHERE collection_id = $1 AND deleted_at IS NULL"
		if len(where) > 0 {
			base += " AND " + strings.Join(where, " AND ")
		}
		base += " ORDER BY " + ob
		i++
		var skip, limit int64 = 0, -1
		for ; i < len(stages) && (stages[i].Op == "$skip" || stages[i].Op == "$limit"); i++ {
			if stages[i].Op == "$skip" {
				if limit >= 0 {
					break
				}
				skip += stages[i].N
			} else {
				if limit < 0 || stages[i].N < limit {
					limit = stages[i].N
				}
			}
		}
		if limit >= 0 {
			base += " LIMIT " + tb.Arg(limit)
		}
		if skip > 0 {
			base += " OFFSET " + tb.Arg(skip)
		}
	}
	b.Args = tb.Args
	cur := base
	b.DocMode = true
	defer func() { b.DocMode = false }()
	n := 0
	next := func() string { n++; return fmt.Sprintf("p%d", n) }
	for ; i < len(stages); i++ {
		st := stages[i]
		a := next()
		switch st.Op {
		case "$match":
			w, err := b.Where(st.Filter)
			if err != nil {
				return "", err
			}
			cur = fmt.Sprintf("SELECT doc, ord FROM (%s) %s WHERE %s", cur, a, w)
		case "$sort":
			ob, _ := b.OrderBy(st.Sort)
			cur = fmt.Sprintf("SELECT doc, row_number() OVER (ORDER BY %s) AS ord FROM (%s) %s", ob, cur, a)
		case "$limit":
			cur = fmt.Sprintf("SELECT doc, ord FROM (%s) %s ORDER BY ord NULLS LAST LIMIT %s", cur, a, b.Arg(st.N))
		case "$skip":
			cur = fmt.Sprintf("SELECT doc, ord FROM (%s) %s ORDER BY ord NULLS LAST OFFSET %s", cur, a, b.Arg(st.N))
		case "$count":
			cur = fmt.Sprintf("SELECT jsonb_build_object(%s::text, count(*)) AS doc, NULL::bigint AS ord FROM (%s) %s", b.Arg(st.Name), cur, a)
		case "$addFields":
			doc := "doc"
			for _, ne := range st.Add {
				v, err := b.ExprSQL(ne.E)
				if err != nil {
					return "", err
				}
				k := b.Arg(ne.Name)
				doc = fmt.Sprintf("(CASE WHEN (%[2]s) IS NULL THEN %[1]s - %[3]s::text ELSE %[1]s || jsonb_build_object(%[3]s::text, %[2]s) END)", doc, v, k)
			}
			cur = fmt.Sprintf("SELECT %s AS doc, ord FROM (%s) %s", doc, cur, a)
		case "$project":
			doc, err := b.projectSQL(st.Project)
			if err != nil {
				return "", err
			}
			cur = fmt.Sprintf("SELECT %s AS doc, ord FROM (%s) %s", doc, cur, a)
		case "$unwind":
			p := st.Unwind.Path.sqlOn(a + ".doc")
			join := "CROSS JOIN LATERAL"
			on := ""
			if st.Unwind.Preserve {
				join, on = "LEFT JOIN LATERAL", " ON TRUE"
			}
			arr := fmt.Sprintf("CASE WHEN jsonb_typeof(%[1]s) = 'array' THEN %[1]s WHEN %[1]s IS NOT NULL AND %[1]s <> 'null'::jsonb THEN jsonb_build_array(%[1]s) END", p)
			parts := make([]string, len(st.Unwind.Path.Segs))
			for k, s := range st.Unwind.Path.Segs {
				parts[k] = Lit(s)
			}
			cur = fmt.Sprintf("SELECT CASE WHEN e.v IS NULL THEN %[1]s.doc ELSE jsonb_set(%[1]s.doc, ARRAY[%[2]s], e.v) END AS doc, row_number() OVER (ORDER BY %[1]s.ord, e.i) AS ord FROM (%[3]s) %[1]s %[4]s jsonb_array_elements(%[5]s) WITH ORDINALITY AS e(v, i)%[6]s",
				a, strings.Join(parts, ","), cur, join, arr, on)
		case "$group":
			sel, key, err := b.groupSQL(st.Group)
			if err != nil {
				return "", err
			}
			cur = fmt.Sprintf("SELECT %s FROM (%s) %s GROUP BY %s", sel, cur, a, key)
		case "$lookup":
			if err := resolve(st.Lookup); err != nil {
				return "", err
			}
			// 外层扇出守卫：进入 $lookup 的文档数 × 每条关联上限 ≤ 100000，超出时报错函数抛出专用 SQLSTATE（映射成 400）
			maxOuter := LookupMaxFanout / st.Lookup.Fanout()
			guarded := fmt.Sprintf("SELECT doc, ord FROM (SELECT doc, ord, count(*) OVER () AS _n FROM (%s) g) %s WHERE uns_lookup_guard(_n, %s)", cur, a, b.Arg(int64(maxOuter)))
			la := next()
			saved := b.DocMode
			b.DocMode = false
			lat, out, err := b.LookupSQL(st.Lookup, docPathLocal(st.Lookup.LocalField, la), la+"_lk", "doc")
			b.DocMode = saved
			if err != nil {
				return "", err
			}
			cur = fmt.Sprintf("SELECT %[1]s.doc || jsonb_build_object(%[2]s::text, %[3]s) AS doc, %[1]s.ord FROM (%[4]s) %[1]s %[5]s", la, b.Arg(st.Lookup.As), out.Docs, guarded, lat)
		}
	}
	return fmt.Sprintf("SELECT doc::text FROM (%s) final ORDER BY ord NULLS LAST LIMIT %d", cur, MaxAggregateOutput+1), nil
}

// docPathLocal 是管线里本地字段的 jsonb 表达式（外层别名的 doc 列）。
func docPathLocal(p Path, alias string) string {
	return "(" + docPath(p).sqlOn(alias+".doc") + ")"
}

func (b *Builder) projectSQL(p *ProjectSpec) (string, error) {
	if len(p.Exclude) > 0 {
		doc := "doc"
		for _, path := range p.Exclude {
			parts := make([]string, len(path.Segs))
			for i, s := range path.Segs {
				parts[i] = Lit(s)
			}
			doc = "(" + doc + " #- ARRAY[" + strings.Join(parts, ",") + "])"
		}
		if p.NoID {
			doc = "(" + doc + " - '_id')"
		}
		return doc, nil
	}
	// 包含与计算字段：按路径树拼对象，缺失的字段不出现（JSON null 保留）
	type node struct {
		val  string
		kids map[string]*node
		keys []string
	}
	root := &node{kids: map[string]*node{}}
	add := func(segs []string, val string) {
		n := root
		for i, s := range segs {
			k, ok := n.kids[s]
			if !ok {
				k = &node{kids: map[string]*node{}}
				n.kids[s] = k
				n.keys = append(n.keys, s)
			}
			if i == len(segs)-1 {
				k.val = val
			}
			n = k
		}
	}
	if !p.NoID {
		add([]string{"_id"}, "(doc -> '_id')")
	}
	for _, path := range p.Include {
		add(path.Segs, "("+path.sqlOn("doc")+")")
	}
	for _, ne := range p.Computed {
		v, err := b.ExprSQL(ne.E)
		if err != nil {
			return "", err
		}
		add([]string{ne.Name}, "("+v+")")
	}
	var build func(n *node) string
	build = func(n *node) string {
		if n.val != "" && len(n.kids) == 0 {
			return n.val
		}
		parts := []string{"'{}'::jsonb"}
		for _, k := range n.keys {
			v := build(n.kids[k])
			parts = append(parts, fmt.Sprintf("(CASE WHEN (%[1]s) IS NULL THEN '{}'::jsonb ELSE jsonb_build_object(%[2]s::text, %[1]s) END)", v, b.Arg(k)))
		}
		return "(" + strings.Join(parts, " || ") + ")"
	}
	return build(root), nil
}

func (b *Builder) groupSQL(g *GroupSpec) (string, string, error) {
	key := "'null'::jsonb"
	if g.ID != nil {
		k, err := b.exprOrObject(g.ID)
		if err != nil {
			return "", "", err
		}
		key = "COALESCE(" + k + ", 'null'::jsonb)"
	}
	parts := []string{"'_id'", key}
	for _, acc := range g.Accs {
		var v string
		if acc.E != nil {
			s, err := b.exprOrObject(acc.E)
			if err != nil {
				return "", "", err
			}
			v = s
		}
		var a string
		switch acc.Op {
		case "$count":
			a = "to_jsonb(count(*))"
		case "$sum":
			a = fmt.Sprintf("to_jsonb(COALESCE(sum(CASE WHEN jsonb_typeof(%[1]s) = 'number' THEN (%[1]s)::numeric END), 0))", v)
		case "$avg":
			a = fmt.Sprintf("to_jsonb(avg(CASE WHEN jsonb_typeof(%[1]s) = 'number' THEN (%[1]s)::numeric END))", v)
		case "$min", "$max":
			fn := strings.TrimPrefix(acc.Op, "$")
			// 有数字时取数字的最值，否则取字符串的最值（第 5.3 节）
			a = fmt.Sprintf("COALESCE(to_jsonb(%[2]s(CASE WHEN jsonb_typeof(%[1]s) = 'number' THEN (%[1]s)::numeric END)), to_jsonb(%[2]s(CASE WHEN jsonb_typeof(%[1]s) = 'string' THEN %[1]s #>> '{}' END)))", v, fn)
		case "$first", "$last":
			dir := ""
			if acc.Op == "$last" {
				dir = " DESC"
			}
			a = fmt.Sprintf("(array_agg(COALESCE(%s, 'null'::jsonb) ORDER BY ord%s NULLS LAST))[1]", v, dir)
		case "$push":
			a = fmt.Sprintf("jsonb_path_query_array(COALESCE(jsonb_agg(COALESCE(%[1]s, 'null'::jsonb) ORDER BY ord NULLS LAST) FILTER (WHERE octet_length(COALESCE(%[1]s, 'null'::jsonb)::text) <= %[2]d), '[]'::jsonb), '$[0 to %[3]d]')",
				v, MaxPushValueBytes, MaxPushElems-1)
		case "$addToSet":
			a = fmt.Sprintf("jsonb_path_query_array(COALESCE(jsonb_agg(DISTINCT COALESCE(%[1]s, 'null'::jsonb)) FILTER (WHERE octet_length(COALESCE(%[1]s, 'null'::jsonb)::text) <= %[2]d), '[]'::jsonb), '$[0 to %[3]d]')",
				v, MaxPushValueBytes, MaxPushElems-1)
		}
		parts = append(parts, b.Arg(acc.Name)+"::text", a)
	}
	return fmt.Sprintf("jsonb_build_object(%s) AS doc, NULL::bigint AS ord", strings.Join(parts, ", ")), key, nil
}

func (b *Builder) exprOrObject(e Expr) (string, error) {
	if o, ok := e.(objectExpr); ok {
		parts := make([]string, 0, len(o.items)*2)
		for i, it := range o.items {
			s, err := b.ExprSQL(it)
			if err != nil {
				return "", err
			}
			parts = append(parts, b.Arg(o.names[i])+"::text", "COALESCE("+s+", 'null'::jsonb)")
		}
		return "jsonb_build_object(" + strings.Join(parts, ", ") + ")", nil
	}
	return b.ExprSQL(e)
}

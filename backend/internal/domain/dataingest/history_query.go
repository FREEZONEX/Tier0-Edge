package dataingest

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"backend/internal/repo"
	"github.com/jackc/pgx/v5"
)

const (
	historySamplingPointLimit = 1000
	historySamplingMaxCells   = 50000
	historyNumericRegexp      = `^[+-]?(([0-9]+([.][0-9]*)?)|([.][0-9]+))([eE][+-]?[0-9]+)?$`
)

type HistoryAggregationField struct {
	Name     string
	Function string
	Type     string
}

type HistoryQuery struct {
	StartMs    int64
	EndMs      int64
	Page       int
	Size       int
	Auto       bool
	PointLimit int
	Aggregate  bool
	IntervalMs int64
	Fields     []HistoryAggregationField
	SkipCount  bool
}

type historyQueryMeta struct {
	Aggregated          bool
	Sampled             bool
	RawTotal            int64
	ReturnedTotal       int64
	RequestedIntervalMs int64
	ActualIntervalMs    int64
	Strategy            string
	TotalExact          bool
	HasMore             bool
}

func (s *Service) QueryHistory(ctx context.Context, node repo.UnsNode, query HistoryQuery) (map[string]any, error) {
	def := definitionFromNode(node)
	fields := dataFields(def.Fields, def.TopicType)
	if len(fields) == 0 {
		fields = []Field{{Name: "value", Type: "string"}}
	}
	respFields := historyFields(fields)
	countExact := !query.SkipCount
	empty := func() map[string]any {
		total := int64(-1)
		if countExact {
			total = 0
		}
		return historyQueryResponse(respFields, nil, total, historyQueryMeta{
			RawTotal:   total,
			TotalExact: countExact,
		})
	}
	if s == nil || s.writer == nil || s.writer.pool == nil || node.Type != 2 || !def.SaveToDB {
		return empty(), nil
	}
	alias := strings.Trim(def.Alias, "/")
	if alias == "" {
		alias = strings.Trim(def.Namespace, "/")
	}
	if alias == "" {
		return empty(), nil
	}
	startAt, endAt := historyWindow(query.StartMs, query.EndMs)
	tableName := sinkQualifiedName(alias)
	timeColumn := sinkTimeColumn(def)
	rawTotal := int64(-1)
	if countExact {
		var err error
		rawTotal, err = s.countHistoryRows(ctx, tableName, timeColumn, startAt, endAt)
		if err != nil {
			if isMissingHistoryRelation(err) {
				return empty(), nil
			}
			return nil, err
		}
		if rawTotal == 0 {
			return historyQueryResponse(respFields, nil, 0, historyQueryMeta{RawTotal: 0, TotalExact: true}), nil
		}
	}
	if query.Aggregate {
		selected, err := resolveHistoryAggregationFields(fields, query.Fields)
		if err != nil {
			return nil, err
		}
		limit := effectiveHistoryPointLimit(len(selected), query.PointLimit)
		actualInterval := query.IntervalMs
		if query.Auto && (!countExact || rawTotal > int64(limit)) {
			actualInterval = effectiveHistoryInterval(startAt.UnixMilli(), endAt.UnixMilli(), query.IntervalMs, int64(limit))
		}
		page, size := normalizeHistoryPage(query.Page, query.Size, limit, query.Auto)
		anchorMs := int64(0)
		if query.Auto {
			anchorMs = startAt.UnixMilli()
		}
		querySize := size
		if !countExact {
			querySize++
		}
		var list []map[string]any
		bucketTotal := int64(-1)
		if canUseFullWindowBoundaryQuery(query, startAt, endAt, actualInterval, selected) {
			list, err = s.queryFullWindowBoundaryHistoryRow(ctx, tableName, timeColumn, def.TopicType, startAt, endAt, anchorMs, selected)
		} else {
			list, bucketTotal, err = s.queryAggregatedHistoryRows(ctx, tableName, timeColumn, def.TopicType, startAt, endAt, querySize, (page-1)*size, actualInterval, anchorMs, selected, countExact)
		}
		if err != nil {
			if isMissingHistoryRelation(err) {
				return empty(), nil
			}
			return nil, err
		}
		hasMore := !countExact && len(list) > size
		if hasMore {
			list = list[:size]
		}
		if countExact {
			hasMore = int64((page-1)*size+len(list)) < bucketTotal
		}
		meta := historyQueryMeta{
			Aggregated:          true,
			Sampled:             actualInterval > query.IntervalMs,
			RawTotal:            rawTotal,
			ReturnedTotal:       int64(len(list)),
			RequestedIntervalMs: query.IntervalMs,
			ActualIntervalMs:    actualInterval,
			TotalExact:          countExact,
			HasMore:             hasMore,
		}
		if meta.Sampled {
			meta.Strategy = "timeBucket"
		}
		total := int64(-1)
		if countExact {
			total = bucketTotal
			if query.Auto {
				// Preserve the legacy auto-mode contract: total is the number of
				// returned (possibly sampled) points; rawTotal remains in meta.
				total = int64(len(list))
			}
		}
		return historyQueryResponse(respFields, list, total, meta), nil
	}
	if query.Auto {
		limit := effectiveHistoryPointLimit(len(fields), query.PointLimit)
		var list []map[string]any
		var intervalMs int64
		sampled := true
		var err error
		if countExact && rawTotal <= int64(limit) {
			list, err = s.queryHistoryPage(ctx, tableName, timeColumn, fields, def.TopicType, startAt, endAt, int(rawTotal), 0)
			sampled = false
		} else {
			list, intervalMs, err = s.queryAutoSampledHistoryRows(ctx, tableName, timeColumn, fields, def.TopicType, startAt, endAt, limit)
		}
		if err != nil {
			if isMissingHistoryRelation(err) {
				return empty(), nil
			}
			return nil, err
		}
		meta := historyQueryMeta{
			Sampled:          sampled,
			RawTotal:         rawTotal,
			ReturnedTotal:    int64(len(list)),
			ActualIntervalMs: intervalMs,
			TotalExact:       countExact,
		}
		if sampled {
			meta.Strategy = "lastRow"
		}
		total := int64(-1)
		if countExact {
			// Preserve the legacy auto-mode contract. The exact source-row
			// count is still available as meta.rawTotal.
			total = int64(len(list))
		}
		return historyQueryResponse(respFields, list, total, meta), nil
	}
	page, size := normalizeHistoryPage(query.Page, query.Size, historySamplingPointLimit, false)
	querySize := size
	if !countExact {
		querySize++
	}
	list, err := s.queryHistoryPage(ctx, tableName, timeColumn, fields, def.TopicType, startAt, endAt, querySize, (page-1)*size)
	if err != nil {
		if isMissingHistoryRelation(err) {
			return empty(), nil
		}
		return nil, err
	}
	hasMore := !countExact && len(list) > size
	if hasMore {
		list = list[:size]
	}
	total := int64(-1)
	if countExact {
		total = rawTotal
		hasMore = int64((page-1)*size+len(list)) < rawTotal
	}
	return historyQueryResponse(respFields, list, total, historyQueryMeta{
		RawTotal:      rawTotal,
		ReturnedTotal: int64(len(list)),
		TotalExact:    countExact,
		HasMore:       hasMore,
	}), nil
}

func historyQueryResponse(fields []map[string]any, list []map[string]any, total int64, meta historyQueryMeta) map[string]any {
	if list == nil {
		list = []map[string]any{}
	}
	return map[string]any{
		"fields": fields,
		"list":   list,
		"total":  total,
		"meta": map[string]any{
			"aggregated":          meta.Aggregated,
			"sampled":             meta.Sampled,
			"rawTotal":            meta.RawTotal,
			"returnedTotal":       meta.ReturnedTotal,
			"requestedIntervalMs": meta.RequestedIntervalMs,
			"actualIntervalMs":    meta.ActualIntervalMs,
			"strategy":            meta.Strategy,
			"totalExact":          meta.TotalExact,
			"hasMore":             meta.HasMore,
		},
	}
}

func effectiveHistoryPointLimit(fieldCount, requested int) int {
	if fieldCount <= 0 {
		fieldCount = 1
	}
	limit := historySamplingPointLimit
	if requested > 0 && requested < limit {
		limit = requested
	}
	cellLimit := historySamplingMaxCells / fieldCount
	if cellLimit <= 0 {
		return 1
	}
	if cellLimit < limit {
		return cellLimit
	}
	return limit
}

func historySamplingInterval(start, end, limit int64) int64 {
	if limit <= 0 {
		limit = 1
	}
	rangeMs := end - start + 1
	if rangeMs <= 0 {
		return 1
	}
	return (rangeMs + limit - 1) / limit
}

func effectiveHistoryInterval(start, end, requested, limit int64) int64 {
	autoInterval := historySamplingInterval(start, end, limit)
	if requested > autoInterval {
		return requested
	}
	return autoInterval
}

func normalizeHistoryPage(page, size, defaultSize int, auto bool) (int, int) {
	if auto {
		return 1, defaultSize
	}
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = defaultSize
	}
	if size > maxHistoryLimit {
		size = maxHistoryLimit
	}
	return page, size
}

func (s *Service) countHistoryRows(ctx context.Context, tableName, timeColumn string, startAt, endAt time.Time) (int64, error) {
	query := fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s >= $1 AND %s <= $2`, tableName, quoteIdent(timeColumn), quoteIdent(timeColumn))
	var total int64
	if err := s.writer.pool.QueryRow(ctx, query, startAt, endAt).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (s *Service) queryHistoryPage(ctx context.Context, tableName, timeColumn string, fields []Field, topicType int16, startAt, endAt time.Time, limit, offset int) ([]map[string]any, error) {
	query := fmt.Sprintf(`SELECT * FROM %s WHERE %s >= $1 AND %s <= $2 ORDER BY %s ASC LIMIT $3 OFFSET $4`,
		tableName, quoteIdent(timeColumn), quoteIdent(timeColumn), quoteIdent(timeColumn))
	rows, err := s.writer.pool.Query(ctx, query, startAt, endAt, limit, offset)
	if err != nil {
		return nil, err
	}
	return scanHistoryRows(rows, timeColumn, fields, topicType)
}

func (s *Service) queryAutoSampledHistoryRows(ctx context.Context, tableName, timeColumn string, fields []Field, topicType int16, startAt, endAt time.Time, limit int) ([]map[string]any, int64, error) {
	intervalMs := historySamplingInterval(startAt.UnixMilli(), endAt.UnixMilli(), int64(limit))
	query := fmt.Sprintf(`WITH ranked AS (
	SELECT *, row_number() OVER (
		PARTITION BY floor((((extract(epoch from %s) * 1000)::bigint - $3)::numeric) / $4)
		ORDER BY %s DESC
	) AS sample_rank
	FROM %s
	WHERE %s >= $1 AND %s <= $2
)
SELECT * FROM ranked WHERE sample_rank = 1 ORDER BY %s ASC LIMIT $5`,
		quoteIdent(timeColumn), quoteIdent(timeColumn), tableName, quoteIdent(timeColumn), quoteIdent(timeColumn), quoteIdent(timeColumn))
	rows, err := s.writer.pool.Query(ctx, query, startAt, endAt, startAt.UnixMilli(), intervalMs, limit)
	if err != nil {
		return nil, 0, err
	}
	list, err := scanHistoryRows(rows, timeColumn, fields, topicType)
	return list, intervalMs, err
}

func scanHistoryRows(rows pgx.Rows, timeColumn string, fields []Field, topicType int16) ([]map[string]any, error) {
	defer rows.Close()
	desc := rows.FieldDescriptions()
	list := make([]map[string]any, 0)
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(map[string]any, len(desc))
		for idx, field := range desc {
			row[string(field.Name)] = values[idx]
		}
		list = append(list, map[string]any{
			"timestamp": valueTimeFromAny(row[timeColumn]).UnixMilli(),
			"payload":   payloadFromSinkRow(row, fields, topicType),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

func resolveHistoryAggregationFields(fields []Field, requested []HistoryAggregationField) ([]HistoryAggregationField, error) {
	definitions := make(map[string]Field, len(fields))
	for _, field := range fields {
		definitions[strings.ToLower(strings.TrimSpace(field.Name))] = field
	}
	if len(requested) == 0 {
		requested = make([]HistoryAggregationField, 0, len(fields))
		for _, field := range fields {
			requested = append(requested, HistoryAggregationField{Name: field.Name})
		}
	}
	resolved := make([]HistoryAggregationField, 0, len(requested))
	seen := make(map[string]struct{}, len(requested))
	for _, item := range requested {
		name := strings.TrimSpace(item.Name)
		key := strings.ToLower(name)
		field, ok := definitions[key]
		if !ok || name == "" || strings.Contains(name, ".") {
			return nil, fmt.Errorf("history aggregation field not found: %s", name)
		}
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("history aggregation field duplicated: %s", name)
		}
		seen[key] = struct{}{}
		function := strings.ToLower(strings.TrimSpace(item.Function))
		if function == "" {
			if historyFieldTypeNumeric(field.Type) {
				function = "avg"
			} else {
				function = "last"
			}
		}
		if err := validateHistoryAggregationFunction(function); err != nil {
			return nil, err
		}
		if historyFunctionRequiresNumeric(function) && !historyFieldTypeNumeric(field.Type) {
			return nil, fmt.Errorf("history aggregation field %s requires numeric value", field.Name)
		}
		resolved = append(resolved, HistoryAggregationField{Name: field.Name, Function: function, Type: field.Type})
	}
	return resolved, nil
}

func (s *Service) queryAggregatedHistoryRows(ctx context.Context, tableName, timeColumn string, topicType int16, startAt, endAt time.Time, limit, offset int, intervalMs, anchorMs int64, fields []HistoryAggregationField, countExact bool) ([]map[string]any, int64, error) {
	baseColumns := make([]string, 0, len(fields)*2)
	jsonArgs := make([]string, 0, len(fields)*2)
	for idx, field := range fields {
		valueExpr, numericExpr := enterpriseHistoryValueExpr(topicType, field)
		valueAlias := fmt.Sprintf("value_%d", idx)
		numericAlias := fmt.Sprintf("numeric_%d", idx)
		baseColumns = append(baseColumns,
			fmt.Sprintf("%s AS %s", valueExpr, quoteIdent(valueAlias)),
			fmt.Sprintf("%s AS %s", numericExpr, quoteIdent(numericAlias)),
		)
		jsonArgs = append(jsonArgs,
			quoteLiteral(field.Name),
			historyAggregateSQL(field.Function, quoteIdent(valueAlias), quoteIdent(numericAlias), "ts"),
		)
	}
	bucketExpr := fmt.Sprintf(`floor((((extract(epoch from %s) * 1000)::bigint - $1)::numeric) / $2)::bigint`, quoteIdent(timeColumn))
	countSQL := fmt.Sprintf(`SELECT count(*) FROM (
	SELECT %s AS bucket_index FROM %s
	WHERE %s >= $3 AND %s <= $4
	GROUP BY bucket_index
) buckets`, bucketExpr, tableName, quoteIdent(timeColumn), quoteIdent(timeColumn))
	total := int64(-1)
	if countExact {
		total = 0
		if err := s.writer.pool.QueryRow(ctx, countSQL, anchorMs, intervalMs, startAt, endAt).Scan(&total); err != nil {
			return nil, 0, err
		}
		if total == 0 {
			return []map[string]any{}, 0, nil
		}
	}
	query := fmt.Sprintf(`WITH base AS (
	SELECT %s AS bucket_index, %s AS ts, %s
	FROM %s
	WHERE %s >= $3 AND %s <= $4
), aggregated AS (
	SELECT bucket_index, jsonb_strip_nulls(%s) AS payload
	FROM base
	GROUP BY bucket_index
)
SELECT ($1::bigint + bucket_index * $2::bigint) AS time_stamp, payload::text
FROM aggregated
ORDER BY bucket_index ASC
LIMIT $5 OFFSET $6`,
		bucketExpr, quoteIdent(timeColumn), strings.Join(baseColumns, ", "), tableName,
		quoteIdent(timeColumn), quoteIdent(timeColumn), historyJSONBuildObjectExpr(jsonArgs))
	rows, err := s.writer.pool.Query(ctx, query, anchorMs, intervalMs, startAt, endAt, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]map[string]any, 0)
	for rows.Next() {
		var timestamp int64
		var payloadJSON string
		if err := rows.Scan(&timestamp, &payloadJSON); err != nil {
			return nil, 0, err
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return nil, 0, err
		}
		list = append(list, map[string]any{"timestamp": timestamp, "payload": payload})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func canUseFullWindowBoundaryQuery(query HistoryQuery, startAt, endAt time.Time, actualIntervalMs int64, fields []HistoryAggregationField) bool {
	if !query.Auto || !query.SkipCount || query.IntervalMs <= 0 || actualIntervalMs <= 0 || len(fields) == 0 {
		return false
	}
	if actualIntervalMs < historySamplingInterval(startAt.UnixMilli(), endAt.UnixMilli(), 1) {
		return false
	}
	for _, field := range fields {
		if field.Function != "first" && field.Function != "last" {
			return false
		}
	}
	return true
}

func (s *Service) queryFullWindowBoundaryHistoryRow(ctx context.Context, tableName, timeColumn string, topicType int16, startAt, endAt time.Time, anchorMs int64, fields []HistoryAggregationField) ([]map[string]any, error) {
	query := fullWindowBoundaryHistorySQL(tableName, timeColumn, topicType, fields)
	rows, err := s.writer.pool.Query(ctx, query, startAt, endAt, anchorMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := make([]map[string]any, 0, 1)
	if rows.Next() {
		var timestamp int64
		var payloadJSON string
		if err := rows.Scan(&timestamp, &payloadJSON); err != nil {
			return nil, err
		}
		payload := map[string]any{}
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return nil, err
		}
		list = append(list, map[string]any{"timestamp": timestamp, "payload": payload})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return list, nil
}

func fullWindowBoundaryHistorySQL(tableName, timeColumn string, topicType int16, fields []HistoryAggregationField) string {
	quotedTime := quoteIdent(timeColumn)
	jsonArgs := make([]string, 0, len(fields)*2)
	for _, field := range fields {
		valueExpr, _ := enterpriseHistoryValueExpr(topicType, field)
		direction := "ASC"
		if field.Function == "last" {
			direction = "DESC"
		}
		boundaryValue := fmt.Sprintf(`(
		SELECT %s
		FROM %s
		WHERE %s >= $1 AND %s <= $2 AND %s IS NOT NULL
		ORDER BY %s %s
		LIMIT 1
	)`, valueExpr, tableName, quotedTime, quotedTime, valueExpr, quotedTime, direction)
		jsonArgs = append(jsonArgs, quoteLiteral(field.Name), boundaryValue)
	}
	return fmt.Sprintf(`SELECT $3::bigint AS time_stamp, jsonb_strip_nulls(%s)::text AS payload
FROM (
	SELECT 1
	FROM %s
	WHERE %s >= $1 AND %s <= $2
	ORDER BY %s ASC
	LIMIT 1
) AS window_row`, historyJSONBuildObjectExpr(jsonArgs), tableName, quotedTime, quotedTime, quotedTime)
}

func historyJSONBuildObjectExpr(args []string) string {
	const maxArgsPerCall = 100
	if len(args) == 0 {
		return `'{}'::jsonb`
	}
	parts := make([]string, 0, (len(args)+maxArgsPerCall-1)/maxArgsPerCall)
	for start := 0; start < len(args); start += maxArgsPerCall {
		end := start + maxArgsPerCall
		if end > len(args) {
			end = len(args)
		}
		parts = append(parts, fmt.Sprintf("jsonb_build_object(%s)", strings.Join(args[start:end], ", ")))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, " || ") + ")"
}

func enterpriseHistoryValueExpr(topicType int16, field HistoryAggregationField) (string, string) {
	if topicType == metricTopicType {
		column := quoteIdent(field.Name)
		numericExpr := "NULL::double precision"
		if historyFieldTypeNumeric(field.Type) {
			numericExpr = fmt.Sprintf("(%s)::double precision", column)
		}
		return column, numericExpr
	}
	jsonColumn := quoteIdent(eventJSONColumn)
	fieldLiteral := quoteLiteral(field.Name)
	valueExpr := fmt.Sprintf("jsonb_extract_path(%s, %s)", jsonColumn, fieldLiteral)
	textExpr := fmt.Sprintf("jsonb_extract_path_text(%s, %s)", jsonColumn, fieldLiteral)
	numericExpr := fmt.Sprintf("CASE WHEN %s ~ %s THEN (%s)::double precision END", textExpr, quoteLiteral(historyNumericRegexp), textExpr)
	return valueExpr, numericExpr
}

func historyAggregateSQL(function, valueExpr, numericExpr, timestampExpr string) string {
	switch function {
	case "count":
		return fmt.Sprintf("count(%s)", valueExpr)
	case "avg", "min", "max", "sum":
		return fmt.Sprintf("%s(%s)", function, numericExpr)
	case "first":
		return fmt.Sprintf("(array_agg(%s ORDER BY %s ASC) FILTER (WHERE %s IS NOT NULL))[1]", valueExpr, timestampExpr, valueExpr)
	default:
		return fmt.Sprintf("(array_agg(%s ORDER BY %s DESC) FILTER (WHERE %s IS NOT NULL))[1]", valueExpr, timestampExpr, valueExpr)
	}
}

func validateHistoryAggregationFunction(function string) error {
	switch function {
	case "avg", "min", "max", "sum", "count", "first", "last":
		return nil
	default:
		return fmt.Errorf("unsupported history aggregation function: %s", function)
	}
}

func historyFunctionRequiresNumeric(function string) bool {
	switch function {
	case "avg", "min", "max", "sum":
		return true
	default:
		return false
	}
}

func historyFieldTypeNumeric(fieldType string) bool {
	switch strings.ToLower(strings.TrimSpace(fieldType)) {
	case "int", "integer", "long", "float", "double", "number", "decimal", "numeric", "smallint", "bigint", "real":
		return true
	default:
		return false
	}
}

func isMissingHistoryRelation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "does not exist") || strings.Contains(message, "undefined table")
}

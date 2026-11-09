package unsv2

import (
	"context"
	"encoding/json"
	"time"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
)

const defaultTrendPoints = 400
const maxTrendPoints = 1000
const maxTrendRange = 31 * 24 * time.Hour

func trendPointLimit(value int) (int, error) {
	if value == 0 {
		return defaultTrendPoints, nil
	}
	if value < 32 || value > maxTrendPoints {
		return 0, invalid("maxPoints", "expected 32..1000")
	}
	return value, nil
}

type sampleTrendPoint struct {
	Time       int64    `json:"time"`
	Bucket     int64    `json:"bucket"`
	Value      *float64 `json:"value"`
	Min        *float64 `json:"min"`
	Max        *float64 `json:"max"`
	Count      int64    `json:"count"`
	ValidCount int64    `json:"validCount"`
}

// 趋势仅查询一个明确的 Record 和声明为数值的字段，时间桶在数据库聚合。
// 空桶不补零，异常质量的点不进入均值/极值；count 保留实际采样数量。
func sampleTrendQuery(ctx context.Context, schema *unsmeta.Schema, q Query) (map[string]any, error) {
	limit, err := trendPointLimit(q.MaxPoints)
	if err != nil {
		return nil, err
	}
	if len(q.RecordIDs) != 1 || q.RecordIDs[0] == "" {
		return nil, invalid("recordIds", "trend requires exactly one record")
	}
	if q.TimeRange == nil || q.TimeRange.From < 0 || q.TimeRange.To <= q.TimeRange.From || q.TimeRange.To-q.TimeRange.From > maxTrendRange.Milliseconds() {
		return nil, invalid("timeRange", "trend requires [from,to) within 31 days")
	}
	if len(q.FieldPath) < 1 || len(q.FieldPath) > 8 {
		return nil, invalid("fieldPath", "expected 1..8 declared field segments")
	}
	field := *schema
	for _, segment := range q.FieldPath {
		child, ok := field.Properties[segment]
		if field.Type != "object" || !ok || segment == "" {
			return nil, invalid("fieldPath", "field is not declared in sampleSchema")
		}
		field = child
	}
	if field.Type != "number" && field.Type != "integer" {
		return nil, invalid("fieldPath", "trend requires a numeric field")
	}
	if _, err := repo.NewUnsMetaRepo(ctx).Record(ctx, q.CollectionID, q.RecordIDs[0]); err != nil {
		return nil, err
	}
	rangeValue := q.TimeRange
	width := (rangeValue.To - rangeValue.From + int64(limit) - 1) / int64(limit)
	path, err := json.Marshal(q.FieldPath)
	if err != nil {
		return nil, err
	}
	points := []sampleTrendPoint{}
	// JSON 路径和查询值全部参数化；不拼接客户端提供的字段或 SQL 标识符。
	err = repo.GetUnsConn(ctx).WithContext(ctx).Raw(`
WITH values_in_range AS (
 SELECT time,
        floor((extract(epoch FROM time)*1000-?)/?)::bigint AS bucket,
        data #> ARRAY(SELECT jsonb_array_elements_text(?::jsonb)) AS field,
        quality
 FROM public.uns_sample
 WHERE collection_id=? AND record_id=? AND time>=? AND time<?
), numeric_values AS (
 SELECT time, bucket,
        CASE WHEN jsonb_typeof(field)='number' AND lower(quality)='good'
             THEN (field #>> '{}')::numeric ELSE NULL END AS value
 FROM values_in_range
)
SELECT (extract(epoch FROM min(time))*1000)::bigint AS time, bucket,
       avg(value)::double precision AS value,
       min(value)::double precision AS min, max(value)::double precision AS max,
       count(*) AS count, count(value) AS valid_count
FROM numeric_values GROUP BY bucket ORDER BY bucket LIMIT ?`,
		rangeValue.From, width, string(path), q.CollectionID, q.RecordIDs[0],
		time.UnixMilli(rangeValue.From), time.UnixMilli(rangeValue.To), limit).Scan(&points).Error
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"collectionId": q.CollectionID, "recordId": q.RecordIDs[0], "fieldPath": q.FieldPath,
		"timeRange": rangeValue, "bucketMs": width, "aggregation": "mean-min-max",
		"points": points,
	}, nil
}

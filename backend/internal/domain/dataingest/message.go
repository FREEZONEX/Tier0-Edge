package dataingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

type Record struct {
	UnsID     int64
	Alias     string
	Namespace string
	TopicType int16
	Fields    []Field
	Values    map[string]string
	RawValues map[string]string
	// EventPayload keeps the complete JSON object for State/Action messages.
	// Metric records remain schema-driven through Values.
	EventPayload map[string]any
	// Time keeps the existing receive-time semantics used by latest/history.
	// SourceTime and IngestedAt are independent quality observations and must
	// not silently change the history _timestamp contract.
	Time          time.Time
	SourceTime    time.Time
	SourceTimeSet bool
	IngestedAt    time.Time
	Quality       int64
	// Optional source metadata; normal ingest retains its receive-time contract.
	SourceQuality string
	Replayed      bool
}

func NormalizePayload(ctx context.Context, def *Definition, payload []byte) ([]Record, error) {
	if def == nil {
		return nil, ErrDefinitionNotFound
	}
	rawRows, err := parsePayloadValues(payload)
	if err != nil {
		return nil, err
	}
	rows := stringifyPayloadRows(rawRows)
	dataFields := dataFields(def.Fields, def.TopicType)
	if len(dataFields) == 0 {
		dataFields = []Field{{Name: "value", Type: "string"}}
	}
	ingestedAt := time.Now().UTC()
	records := make([]Record, 0, len(rows))
	for idx, row := range rows {
		if contextDone(ctx) {
			return records, ctx.Err()
		}
		// Metric payload quality feeds the platform-owned _quality column without
		// rewriting the original payload field. Explicit _quality wins, followed
		// by the conventional payload quality field and the legacy _qos alias.
		quality := intValue(payloadQualityValue(row, def))
		values := make(map[string]string, len(dataFields))
		if len(dataFields) == 1 {
			field := dataFields[0]
			if value, ok := singleFieldValue(row, field.Name); ok {
				values[field.Name] = value
			}
		}
		if len(values) == 0 {
			for _, field := range dataFields {
				raw, ok := row[field.Name]
				if !ok {
					continue
				}
				values[field.Name] = raw
			}
		}
		if len(values) == 0 {
			for _, field := range dataFields {
				if normalizeFieldType(field.Type) == "datetime" {
					values[field.Name] = ""
				}
			}
		}
		var eventPayload map[string]any
		if def.TopicType != metricTopicType && idx < len(rawRows) {
			eventPayload = clonePayloadMap(rawRows[idx])
		}
		if len(values) == 0 && len(eventPayload) == 0 {
			continue
		}
		rawValues := make(map[string]string, len(row))
		for key, value := range row {
			rawValues[key] = value
		}
		sourceTime, sourceTimeSet := parseTime(row[def.TimestampField])
		records = append(records, Record{
			UnsID:         def.ID,
			Alias:         def.Alias,
			Namespace:     def.Namespace,
			TopicType:     def.TopicType,
			Fields:        dataFields,
			Values:        values,
			RawValues:     rawValues,
			EventPayload:  eventPayload,
			Time:          ingestedAt,
			SourceTime:    sourceTime,
			SourceTimeSet: sourceTimeSet,
			IngestedAt:    ingestedAt,
			Quality:       quality,
		})
	}
	return records, nil
}

func payloadQualityValue(row map[string]string, def *Definition) string {
	if def == nil {
		return ""
	}
	if def.TopicType == metricTopicType {
		for _, key := range []string{metricQualityColumn, "quality", "_qos"} {
			if value, ok := row[key]; ok {
				return value
			}
		}
		return ""
	}
	return row[def.QualityField]
}

func ParsePayload(payload []byte) ([]map[string]string, error) {
	rows, err := parsePayloadValues(payload)
	if err != nil {
		return nil, err
	}
	return stringifyPayloadRows(rows), nil
}

func parsePayloadValues(payload []byte) ([]map[string]any, error) {
	payload = bytes.TrimSpace(payload)
	if len(payload) == 0 {
		return nil, nil
	}
	value, ok := decodeJSONValue(payload)
	if !ok {
		return []map[string]any{{"value": strings.Trim(string(payload), `"`)}}, nil
	}
	return flattenRawJSONValue(value), nil
}

func decodeJSONValue(payload []byte) (any, bool) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, false
	}
	return value, true
}

func stringifyPayloadRows(rows []map[string]any) []map[string]string {
	out := make([]map[string]string, 0, len(rows))
	for _, rawRow := range rows {
		row := make(map[string]string, len(rawRow))
		for key, value := range rawRow {
			row[key] = valueString(value)
		}
		out = append(out, row)
	}
	return out
}

func flattenRawJSONValue(value any) []map[string]any {
	switch v := value.(type) {
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			out = append(out, flattenRawJSONValue(item)...)
		}
		return out
	case map[string]any:
		return []map[string]any{v}
	default:
		return []map[string]any{{"value": v}}
	}
}

func flattenJSONValue(value any) []map[string]string {
	return stringifyPayloadRows(flattenRawJSONValue(value))
}

func clonePayloadMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

func valueString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		if math.Trunc(v) == v {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case map[string]any, []any:
		data, _ := json.Marshal(v)
		return string(data)
	default:
		return fmt.Sprint(v)
	}
}

func dataFields(fields []Field, topicType int16) []Field {
	out := make([]Field, 0, len(fields))
	for _, field := range fields {
		if isSystemField(field, topicType) {
			continue
		}
		out = append(out, field)
	}
	return out
}

func isSystemField(field Field, topicType int16) bool {
	if field.SystemField {
		return true
	}
	name := strings.TrimSpace(field.Name)
	lower := strings.ToLower(name)
	switch lower {
	case "_id", "_timestamp", "_quality", "_ct", "_qos", "created_time":
		return true
	}
	if topicType != metricTopicType {
		return name == "timeStamp" || lower == "timestamp" || lower == "quality"
	}
	return false
}

func singleFieldValue(row map[string]string, fieldName string) (string, bool) {
	if value, ok := row[fieldName]; ok {
		return value, true
	}
	if value, ok := row["value"]; ok && len(row) == 1 {
		return value, true
	}
	return "", false
}

func parseTime(raw string) (time.Time, bool) {
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		switch {
		case n > 1_000_000_000_000_000:
			return time.UnixMicro(n).UTC(), true
		case n > 1_000_000_000_000:
			return time.UnixMilli(n).UTC(), true
		case n > 1_000_000_000:
			return time.Unix(n, 0).UTC(), true
		}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.000", "2006-01-02 15:04:05"} {
		t, err := time.Parse(layout, raw)
		if err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func intValue(raw string) int64 {
	if raw == "" {
		return 0
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return n
	}
	switch normalizeQuality(raw) {
	case qualityUncertain:
		return 2
	case qualityBad:
		return 3
	default:
		return 1
	}
}

func normalizeFieldType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "int", "integer":
		return "integer"
	case "long", "bigint":
		return "long"
	case "float":
		return "float"
	case "double", "decimal", "number":
		return "double"
	case "bool", "boolean":
		return "boolean"
	case "datetime", "date", "time", "timestamp":
		return "datetime"
	case "blob", "lblob":
		return "blob"
	case "", "string", "text", "json", "jsonb":
		return "string"
	default:
		return "string"
	}
}

func contextDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

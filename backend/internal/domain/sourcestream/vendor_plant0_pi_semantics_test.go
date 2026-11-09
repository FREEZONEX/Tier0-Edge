package sourcestream

// 验证 PI recorded 的固定 UTC 窗口、数量、质量及原生对象/null 值保留。
// 仅纯合同夹具，无网络、数据库、真实调用入口或生产代码改动。
// 合同来源：Plant0 historian/documentation（公开文档 SHA c95de40e3d58f893564602f5c67b61fa2ed1cce2882a4fda1cdb9fe5f9ee95af）。
// 不推断插值、统计、等频采样、完整窗口或实际 Bad 质量已被观察。

import (
	"encoding/json"
	"testing"
	"time"

	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

const (
	plant0PIRecordedPointLimit = 2
	plant0PIRecordedRowLimit   = 32
	plant0PIRecordedWindow     = 5 * time.Minute
)

type plant0PIRecordedObservation struct {
	Samples            []map[string]any
	DataObserved       bool
	BadObserved        int
	UnitFieldsObserved int
	TruncationPossible bool
}

func plant0PIRecordedWindowValid(start, end time.Time) bool {
	_, startOffset := start.Zone()
	_, endOffset := end.Zone()
	return !start.IsZero() && !end.IsZero() && startOffset == 0 && endOffset == 0 &&
		end.Sub(start) == plant0PIRecordedWindow
}

// Requests remain plans: no Source, transport or credential is opened here.
// Discovery would cost at most two additional reads; these two plans therefore
// have a four-native-read maximum, with no pagination or repeat on an error.
func plant0PIRecordedRequests(webIDs []string, start, end time.Time) ([]streamexec.ProgramCall, error) {
	if !plant0PIRecordedWindowValid(start, end) || len(webIDs) == 0 || len(webIDs) > plant0PIRecordedPointLimit {
		return nil, plant0AdvancedError("pi-recorded-plan-budget")
	}
	requests := make([]streamexec.ProgramCall, 0, len(webIDs))
	seen := map[string]bool{}
	definition := SourceDefinition{SourceType: "pi-web-api", Endpoint: "https://52.221.121.67:8005/piwebapi", HTTP: &HTTPConnection{}}
	for _, webID := range webIDs {
		if webID == "" || seen[webID] {
			return nil, plant0AdvancedError("pi-recorded-point-reference")
		}
		seen[webID] = true
		options, err := json.Marshal(vendorNativeInput{TimeoutMs: 5000, Query: plant0AdvancedQuery(map[string]string{
			"startTime": start.UTC().Format(time.RFC3339Nano), "endTime": end.UTC().Format(time.RFC3339Nano), "maxCount": "32",
		})})
		if err != nil {
			return nil, plant0AdvancedError("pi-recorded-options")
		}
		call := streamexec.ProgramCall{Kind: "source.read", ResourceRef: "vendor:pi-web-api:recorded:" + webID, Options: options}
		if _, err := vendorNativeRequest(definition, call); err != nil {
			return nil, plant0AdvancedError("pi-recorded-point-reference")
		}
		requests = append(requests, call)
	}
	return requests, nil
}

func plant0PIRecordedUTC(value any, start, end time.Time) error {
	if err := plant0AdvancedTime(value, start, end); err != nil {
		return err
	}
	text, ok := value.(string)
	if !ok {
		return plant0AdvancedError("pi-recorded-utc")
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		parsed, err = time.Parse("2006-01-02T15:04:05.999999999-0700", text)
	}
	_, offset := parsed.Zone()
	if err != nil || offset != 0 {
		return plant0AdvancedError("pi-recorded-utc")
	}
	return nil
}

func plant0PIRecordedInspect(raw []byte, start, end time.Time) (plant0PIRecordedObservation, error) {
	var observation plant0PIRecordedObservation
	if !plant0PIRecordedWindowValid(start, end) {
		return observation, plant0AdvancedError("pi-recorded-plan-budget")
	}
	page, err := plant0AdvancedResponse(raw)
	if err != nil {
		return observation, err
	}
	rows, err := plant0AdvancedRows(page, "Items")
	if err != nil {
		return observation, err
	}
	if len(rows) > plant0PIRecordedRowLimit {
		return observation, plant0AdvancedError("pi-recorded-row-budget")
	}
	unit := func(value map[string]any) error {
		if native, exists := value["UnitsAbbreviation"]; exists {
			// Empty strings represent a valid dimensionless unit; absence is an
			// observation gap, not an invented unit from a different Point.
			if _, ok := native.(string); !ok {
				return plant0AdvancedError("pi-recorded-unit-shape")
			}
			observation.UnitFieldsObserved++
		}
		return nil
	}
	if err := unit(page.Data); err != nil {
		return observation, err
	}
	for _, row := range rows {
		if err := plant0PIRecordedUTC(row["Timestamp"], start, end); err != nil {
			return observation, err
		}
		good, goodOK := row["Good"].(bool)
		_, questionableOK := row["Questionable"].(bool)
		if !goodOK || !questionableOK {
			return observation, plant0AdvancedError("pi-recorded-quality")
		}
		if native, exists := row["Substituted"]; exists {
			if _, ok := native.(bool); !ok {
				return observation, plant0AdvancedError("pi-recorded-quality")
			}
		}
		if _, exists := row["Value"]; !exists {
			return observation, plant0AdvancedError("pi-recorded-value-missing")
		}
		if err := unit(row); err != nil {
			return observation, err
		}
		if !good {
			observation.BadObserved++
		}
	}
	// Validation does not rewrite timestamps, flags, units, unknown fields,
	// object values or explicit null; UseNumber avoids an additional float cast.
	observation.Samples = rows
	observation.DataObserved = len(rows) > 0
	observation.TruncationPossible = page.Next != "" || len(rows) == plant0PIRecordedRowLimit
	return observation, nil
}

func TestPlant0PIRecordedContracts(t *testing.T) {
	end := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	start := end.Add(-plant0PIRecordedWindow)
	row := func() map[string]any {
		return map[string]any{"Timestamp": end.Add(-time.Second).Format(time.RFC3339Nano), "Value": json.Number("7.0001"), "Good": true, "Questionable": false, "UnitsAbbreviation": "kW"}
	}
	envelope := func(rows []map[string]any) []byte {
		raw, err := json.Marshal(map[string]any{"status": 200, "data": map[string]any{"Items": rows}})
		require.NoError(t, err)
		return raw
	}
	t.Run("only-documented-query-keys-and-four-native-read-budget", func(t *testing.T) {
		calls, err := plant0PIRecordedRequests([]string{"fixture-point-a", "fixture-point-b"}, start, end)
		require.NoError(t, err)
		require.Len(t, calls, 2)
		for _, call := range calls {
			var input vendorNativeInput
			require.NoError(t, json.Unmarshal(call.Options, &input))
			require.Equal(t, 5000, input.TimeoutMs)
			require.Len(t, input.Query, 3)
			require.JSONEq(t, `"32"`, string(input.Query["maxCount"]))
			require.JSONEq(t, `"2026-10-11T11:55:00Z"`, string(input.Query["startTime"]))
			require.JSONEq(t, `"2026-10-11T12:00:00Z"`, string(input.Query["endTime"]))
			require.Equal(t, "source.read", call.Kind)
			options, err := vendorNativeRequest(SourceDefinition{SourceType: "pi-web-api", HTTP: &HTTPConnection{}}, call)
			require.NoError(t, err)
			require.Equal(t, "GET", options.Method)
			require.Contains(t, options.Path, "/recorded")
		}
	})
	t.Run("reject-wrong-point-or-window-plan-without-reads", func(t *testing.T) {
		for _, points := range [][]string{nil, {"a", "b", "c"}, {"a", "a"}, {""}, {"../secret"}, {"https://example.test/point"}} {
			_, err := plant0PIRecordedRequests(points, start, end)
			require.Error(t, err)
		}
		for _, candidate := range []time.Time{start.Add(-time.Second), time.Time{}, start.In(time.FixedZone("fixture", 3600))} {
			_, err := plant0PIRecordedRequests([]string{"a"}, candidate, end)
			require.Error(t, err)
		}
	})
	t.Run("irregular-utc-samples-and-quality-preserved", func(t *testing.T) {
		rows := []map[string]any{row(), row()}
		rows[0]["Timestamp"] = start.Format(time.RFC3339Nano)
		rows[1]["Timestamp"] = end.Add(-17 * time.Second).Format(time.RFC3339Nano)
		rows[1]["Questionable"] = true
		result, err := plant0PIRecordedInspect(envelope(rows), start, end)
		require.NoError(t, err)
		require.True(t, result.DataObserved)
		require.Zero(t, result.BadObserved)
		require.Equal(t, 2, result.UnitFieldsObserved)
		require.False(t, result.TruncationPossible)
		require.Equal(t, rows, result.Samples)
	})
	t.Run("bad-object-null-and-unknown-fields-never-become-zero", func(t *testing.T) {
		rows := []map[string]any{row(), row()}
		rows[0]["Good"], rows[0]["Questionable"] = false, true
		rows[0]["Value"] = map[string]any{"Name": "No Data", "Value": json.Number("248"), "IsSystem": true}
		rows[0]["Substituted"] = true
		rows[0]["VendorExtension"] = map[string]any{"qualityCode": "fixture"}
		rows[1]["Good"], rows[1]["Value"] = false, nil
		before, err := json.Marshal(rows)
		require.NoError(t, err)
		result, err := plant0PIRecordedInspect(envelope(rows), start, end)
		require.NoError(t, err)
		require.Equal(t, 2, result.BadObserved)
		after, err := json.Marshal(result.Samples)
		require.NoError(t, err)
		require.Equal(t, before, after)
	})
	t.Run("empty-window-is-not-observed-not-fabricated", func(t *testing.T) {
		result, err := plant0PIRecordedInspect(envelope([]map[string]any{}), start, end)
		require.NoError(t, err)
		require.False(t, result.DataObserved)
		require.Empty(t, result.Samples)
		require.Zero(t, result.BadObserved)
		require.Zero(t, result.UnitFieldsObserved)
	})
	t.Run("limit-is-truncation-possible-not-sampling-interval", func(t *testing.T) {
		rows := make([]map[string]any, plant0PIRecordedRowLimit)
		for index := range rows {
			rows[index] = row()
		}
		result, err := plant0PIRecordedInspect(envelope(rows), start, end)
		require.NoError(t, err)
		require.True(t, result.TruncationPossible)
		_, err = plant0PIRecordedInspect(envelope(append(rows, row())), start, end)
		require.EqualError(t, err, "advanced-readonly:pi-recorded-row-budget")
	})
	t.Run("native-next-is-truncation-evidence-not-followed", func(t *testing.T) {
		raw, err := json.Marshal(map[string]any{"status": 200, "data": map[string]any{"Items": []map[string]any{row()}}, "pagination": map[string]any{"next": "fixture-next"}})
		require.NoError(t, err)
		result, err := plant0PIRecordedInspect(raw, start, end)
		require.NoError(t, err)
		require.True(t, result.TruncationPossible)
		require.Len(t, result.Samples, 1)
	})
	t.Run("bad-response-never-counts-as-empty-window", func(t *testing.T) {
		for _, raw := range []string{`{"status":401,"data":{"Items":[]}}`, `{"status":200,"error":{},"data":{"Items":[]}}`, `{"status":200,"data":{}}`, `{"status":200,"data":{"Items":null}}`, `{"status":200,"data":{"Items":[7]}}`, `not-json`} {
			_, err := plant0PIRecordedInspect([]byte(raw), start, end)
			require.Error(t, err)
		}
	})
	t.Run("timestamps-required-utc-and-inside-fixed-window", func(t *testing.T) {
		for _, timestamp := range []any{nil, json.Number("7"), "invalid", "2026-10-11T20:00:00+08:00", end.Add(time.Second).Format(time.RFC3339Nano), start.Add(-time.Second).Format(time.RFC3339Nano)} {
			sample := row()
			sample["Timestamp"] = timestamp
			_, err := plant0PIRecordedInspect(envelope([]map[string]any{sample}), start, end)
			require.Error(t, err)
		}
		for _, timestamp := range []string{"2026-10-11T11:59:59Z", "2026-10-11T11:59:59+00:00"} {
			sample := row()
			sample["Timestamp"] = timestamp
			result, err := plant0PIRecordedInspect(envelope([]map[string]any{sample}), start, end)
			require.NoError(t, err)
			require.Equal(t, timestamp, result.Samples[0]["Timestamp"])
		}
	})
	t.Run("documented-quality-required-optional-substituted-strict", func(t *testing.T) {
		for _, key := range []string{"Good", "Questionable"} {
			for _, value := range []any{nil, "true", json.Number("1")} {
				sample := row()
				sample[key] = value
				_, err := plant0PIRecordedInspect(envelope([]map[string]any{sample}), start, end)
				require.EqualError(t, err, "advanced-readonly:pi-recorded-quality")
			}
			sample := row()
			delete(sample, key)
			_, err := plant0PIRecordedInspect(envelope([]map[string]any{sample}), start, end)
			require.EqualError(t, err, "advanced-readonly:pi-recorded-quality")
		}
		sample := row()
		sample["Substituted"] = "false"
		_, err := plant0PIRecordedInspect(envelope([]map[string]any{sample}), start, end)
		require.EqualError(t, err, "advanced-readonly:pi-recorded-quality")
	})
	t.Run("units-preserved-empty-valid-missing-observed-as-gap", func(t *testing.T) {
		sample := row()
		sample["UnitsAbbreviation"] = ""
		result, err := plant0PIRecordedInspect(envelope([]map[string]any{sample}), start, end)
		require.NoError(t, err)
		require.Equal(t, 1, result.UnitFieldsObserved)
		require.Equal(t, "", result.Samples[0]["UnitsAbbreviation"])
		delete(sample, "UnitsAbbreviation")
		result, err = plant0PIRecordedInspect(envelope([]map[string]any{sample}), start, end)
		require.NoError(t, err)
		require.Zero(t, result.UnitFieldsObserved)
		sample["UnitsAbbreviation"] = json.Number("1")
		_, err = plant0PIRecordedInspect(envelope([]map[string]any{sample}), start, end)
		require.EqualError(t, err, "advanced-readonly:pi-recorded-unit-shape")
	})
	t.Run("value-key-required-even-though-null-is-valid", func(t *testing.T) {
		sample := row()
		delete(sample, "Value")
		_, err := plant0PIRecordedInspect(envelope([]map[string]any{sample}), start, end)
		require.EqualError(t, err, "advanced-readonly:pi-recorded-value-missing")
	})
}

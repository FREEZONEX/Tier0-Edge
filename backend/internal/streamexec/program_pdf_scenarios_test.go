package streamexec

// These explicit fixtures test the actual Goja executor, not live Plant0 acquisition.
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type pdfScenarioHost struct {
	rows       map[string][]map[string]any
	checkpoint json.RawMessage
	pageSize   int
	failQuery  bool
}

func pdfCollection(n int) string { return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }
func pdfRecord(id string, data map[string]any) map[string]any {
	return map[string]any{"id": id, "data": data, "revision": float64(1)}
}
func pdfSample(id string, time float64, data map[string]any) map[string]any {
	return map[string]any{"id": id, "recordId": "device", "time": time, "receivedAt": time + 10000, "quality": "good", "data": data, "sourceId": "fixture-source", "streamId": "fixture-stream"}
}
func pdfField(row map[string]any, path string) any {
	var value any = row
	for _, part := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[part]
	}
	return value
}
func (h *pdfScenarioHost) call(_ context.Context, call ProgramCall) (json.RawMessage, error) {
	switch call.Kind {
	case "checkpoint.get":
		if h.checkpoint == nil {
			return json.RawMessage(`null`), nil
		}
		return h.checkpoint, nil
	case "checkpoint.set":
		// A host stages the change; actual commits occur only after a successful run.
		return json.RawMessage(`{"stage":"buffered"}`), nil
	case "uns.query":
		if h.failQuery {
			return nil, fmt.Errorf("explicit fixture query failure")
		}
		var options struct {
			Kind      string   `json:"kind"`
			RecordIDs []string `json:"recordIds"`
			TimeRange *struct {
				From float64 `json:"from"`
				To   float64 `json:"to"`
			} `json:"timeRange"`
			Filters []struct {
				Field string `json:"field"`
				Op    string `json:"op"`
				Value any    `json:"value"`
			} `json:"filters"`
			Cursor string `json:"cursor"`
		}
		if err := json.Unmarshal(call.Options, &options); err != nil {
			return nil, err
		}
		rows := []map[string]any{}
		for _, row := range h.rows[call.CollectionID] {
			match := true
			if len(options.RecordIDs) > 0 {
				id := row["id"]
				if options.Kind == "sample" {
					id = row["recordId"]
				}
				match = false
				for _, wanted := range options.RecordIDs {
					if id == wanted {
						match = true
					}
				}
			}
			if options.TimeRange != nil {
				time, ok := row["time"].(float64)
				match = match && ok && time >= options.TimeRange.From && time < options.TimeRange.To
			}
			for _, filter := range options.Filters {
				value := pdfField(row, filter.Field)
				switch filter.Op {
				case "eq":
					match = match && fmt.Sprint(value) == fmt.Sprint(filter.Value)
				case "in":
					values, ok := filter.Value.([]any)
					if !ok {
						return nil, fmt.Errorf("invalid fixture membership filter")
					}
					found := false
					for _, candidate := range values {
						found = found || fmt.Sprint(value) == fmt.Sprint(candidate)
					}
					match = match && found
				case "gte", "lt":
					number, ok := value.(float64)
					expected, valid := filter.Value.(float64)
					match = match && ok && valid && ((filter.Op == "gte" && number >= expected) || (filter.Op == "lt" && number < expected))
				default:
					return nil, fmt.Errorf("unsupported fixture filter %s", filter.Op)
				}
			}
			if match {
				rows = append(rows, row)
			}
		}
		if options.Kind == "sample" {
			sort.SliceStable(rows, func(i, j int) bool { return rows[i]["time"].(float64) < rows[j]["time"].(float64) })
		}
		start := 0
		if options.Cursor != "" {
			var err error
			start, err = strconv.Atoi(options.Cursor)
			if err != nil {
				return nil, err
			}
		}
		if start > len(rows) {
			return nil, fmt.Errorf("fixture cursor outside dataset")
		}
		end := len(rows)
		if h.pageSize > 0 && end > start+h.pageSize {
			end = start + h.pageSize
		}
		cursor := ""
		if end < len(rows) {
			cursor = strconv.Itoa(end)
		}
		return json.Marshal(map[string]any{"items": rows[start:end], "nextCursor": cursor, "hasMore": end < len(rows)})
	default:
		return nil, fmt.Errorf("unexpected host operation %s: examples must not control sources", call.Kind)
	}
}

func pdfRun(t *testing.T, file, entry string, input map[string]any, host *pdfScenarioHost) (ProgramResult, map[string]any) {
	t.Helper()
	code, err := os.ReadFile("testdata/pdf-scenarios/" + file + ".js")
	require.NoError(t, err)
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	request := programRequest(string(code), entry)
	request.Input = raw
	request.TimeoutMs = MaxProgramTimeoutMs
	var checkpoint json.RawMessage
	result, err := ExecuteProgram(context.Background(), request, func(ctx context.Context, call ProgramCall) (json.RawMessage, error) {
		if call.Kind == "checkpoint.set" {
			checkpoint = append(json.RawMessage(nil), call.Input...)
		}
		return host.call(ctx, call)
	})
	require.NoError(t, err)
	var data map[string]any
	require.NoError(t, json.Unmarshal(result.Data, &data))
	for _, output := range result.Outputs {
		var change struct {
			RecordID         string         `json:"recordId"`
			Record           map[string]any `json:"record"`
			ExpectedRevision *int64         `json:"expectedRevision"`
		}
		require.NoError(t, json.Unmarshal(output.Data, &change))
		require.NotEmpty(t, change.RecordID)
		require.NotNil(t, change.ExpectedRevision, "every example protects copied manual fields with a Record revision")
		rows := host.rows[output.CollectionID]
		found := false
		for _, row := range rows {
			if row["id"] == change.RecordID {
				require.EqualValues(t, row["revision"], *change.ExpectedRevision)
				row["data"] = change.Record
				row["revision"] = row["revision"].(float64) + 1
				found = true
			}
		}
		if !found {
			require.Zero(t, *change.ExpectedRevision, "new revision records must be create-only")
			rows = append(rows, pdfRecord(change.RecordID, change.Record))
		}
		host.rows[output.CollectionID] = rows
	}
	// Emulate the commit boundary only after the real executor successfully returned.
	if checkpoint != nil {
		host.checkpoint = checkpoint
	}
	return result, data
}

func TestProgramPDFOperationAttribution(t *testing.T) {
	h := &pdfScenarioHost{rows: map[string][]map[string]any{}, pageSize: 1}
	h.rows[pdfCollection(1)] = []map[string]any{pdfSample("late", 99, map[string]any{}), pdfSample("boundary", 100, map[string]any{}), pdfSample("overlap", 155, map[string]any{}), pdfSample("untrusted", 180, map[string]any{"timeTrusted": false}), pdfSample("end", 200, map[string]any{})}
	h.rows[pdfCollection(2)] = []map[string]any{pdfRecord("old", map[string]any{"deviceId": "device", "start": float64(0), "end": float64(100)}), pdfRecord("new", map[string]any{"deviceId": "device", "start": float64(100), "end": float64(200)}), pdfRecord("ambiguous", map[string]any{"deviceId": "device", "start": float64(150), "end": float64(160)})}
	h.rows[pdfCollection(3)] = []map[string]any{pdfRecord("attribution:late", map[string]any{"manualNote": "retain"})}
	_, data := pdfRun(t, "01-operation-attribution", "attribute", map[string]any{"from": 0, "to": 200, "deviceId": "device", "simulated": true}, h)
	rows := data["results"].([]any)
	require.Len(t, rows, 4)
	require.Equal(t, "old", rows[0].(map[string]any)["operationId"])
	require.Equal(t, "new", rows[1].(map[string]any)["operationId"])
	require.Equal(t, "overlapping-operations", rows[2].(map[string]any)["reason"])
	require.Equal(t, "untrusted-time", rows[3].(map[string]any)["reason"])
	require.Equal(t, "retain", h.rows[pdfCollection(3)][0]["data"].(map[string]any)["manualNote"])
	pdfRun(t, "01-operation-attribution", "attribute", map[string]any{"from": 0, "to": 200, "deviceId": "device", "simulated": true}, h)
	require.Len(t, h.rows[pdfCollection(3)], 4, "stable sample identity prevents duplicate records")
}

func TestProgramPDFSustainedAnomaly(t *testing.T) {
	for _, tc := range []struct {
		name   string
		points []map[string]any
		status string
	}{
		{"recovery", []map[string]any{pdfSample("a", 10, map[string]any{"value": 20, "running": true}), pdfSample("b", 20, map[string]any{"value": 20, "running": true}), pdfSample("c", 30, map[string]any{"value": 20, "running": true}), pdfSample("d", 40, map[string]any{"value": 1, "running": true})}, "recovered"},
		{"stopped", []map[string]any{pdfSample("a", 10, map[string]any{"value": 20, "running": true}), pdfSample("b", 20, map[string]any{"value": 20, "running": true}), pdfSample("c", 30, map[string]any{"value": 20, "running": true}), pdfSample("d", 40, map[string]any{"value": 20, "running": false})}, "interrupted"},
		{"gap", []map[string]any{pdfSample("a", 10, map[string]any{"value": 20, "running": true}), pdfSample("b", 20, map[string]any{"value": 20, "running": true}), pdfSample("c", 30, map[string]any{"value": 20, "running": true}), pdfSample("d", 70, map[string]any{"value": 20, "running": true})}, "interrupted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &pdfScenarioHost{rows: map[string][]map[string]any{pdfCollection(11): tc.points}, pageSize: 2}
			_, data := pdfRun(t, "02-sustained-anomaly", "evaluate", map[string]any{"from": 0, "to": 100, "deviceId": "device", "threshold": 10, "durationMs": 20, "maxGapMs": 20}, h)
			events := data["events"].([]any)
			require.NotEmpty(t, events)
			require.Equal(t, tc.status, events[len(events)-1].(map[string]any)["status"])
		})
	}
	t.Run("bad-quality-and-duplicate", func(t *testing.T) {
		bad := pdfSample("bad", 20, map[string]any{"value": 20, "running": true})
		bad["quality"] = "bad"
		h := &pdfScenarioHost{rows: map[string][]map[string]any{pdfCollection(11): {pdfSample("a", 10, map[string]any{"value": 20, "running": true}), pdfSample("duplicate", 10, map[string]any{"value": 20, "running": true}), bad, pdfSample("c", 30, map[string]any{"value": 20, "running": true})}}}
		_, data := pdfRun(t, "02-sustained-anomaly", "evaluate", map[string]any{"from": 0, "to": 100, "deviceId": "device", "threshold": 10, "durationMs": 20, "maxGapMs": 20}, h)
		require.Empty(t, data["events"])
		require.Equal(t, float64(1), data["diagnostics"].(map[string]any)["badQuality"])
	})
	t.Run("replay-and-empty-window", func(t *testing.T) {
		h := &pdfScenarioHost{rows: map[string][]map[string]any{pdfCollection(11): {pdfSample("a", 10, map[string]any{"value": 20, "running": true}), pdfSample("b", 30, map[string]any{"value": 20, "running": true})}}}
		input := map[string]any{"from": 0, "to": 40, "deviceId": "device", "threshold": 10, "durationMs": 20, "maxGapMs": 30}
		pdfRun(t, "02-sustained-anomaly", "evaluate", input, h)
		_, data := pdfRun(t, "02-sustained-anomaly", "evaluate", input, h)
		require.Empty(t, data["events"])
		require.Equal(t, float64(2), data["diagnostics"].(map[string]any)["alreadyProcessed"])
		input["from"], input["to"] = 40, 100
		_, data = pdfRun(t, "02-sustained-anomaly", "evaluate", input, h)
		require.Equal(t, true, data["missingWindow"])
		require.Equal(t, "missing-samples", data["events"].([]any)[0].(map[string]any)["reason"])
	})
	t.Run("trailing-missing-samples", func(t *testing.T) {
		h := &pdfScenarioHost{rows: map[string][]map[string]any{pdfCollection(11): {pdfSample("a", 10, map[string]any{"value": 20, "running": true}), pdfSample("b", 30, map[string]any{"value": 20, "running": true})}}}
		_, data := pdfRun(t, "02-sustained-anomaly", "evaluate", map[string]any{"from": 0, "to": 100, "deviceId": "device", "threshold": 10, "durationMs": 20, "maxGapMs": 30}, h)
		events := data["events"].([]any)
		require.Equal(t, "missing-samples", events[len(events)-1].(map[string]any)["reason"])
		require.Nil(t, data["state"].(map[string]any)["activeId"])
	})
}

func TestProgramPDFSustainedAnomalyUnknownRunning(t *testing.T) {
	for _, tc := range []struct {
		name    string
		present bool
		value   any
	}{
		{name: "missing"},
		{name: "null", present: true, value: nil},
		{name: "string-false", present: true, value: "false"},
		{name: "string-true", present: true, value: "true"},
		{name: "numeric-zero", present: true, value: 0},
		{name: "numeric-one", present: true, value: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &pdfScenarioHost{rows: map[string][]map[string]any{}, pageSize: 1}
			for i, timestamp := range []float64{10, 20, 30} {
				data := map[string]any{"value": 20}
				if tc.present {
					data["running"] = tc.value
				}
				h.rows[pdfCollection(11)] = append(h.rows[pdfCollection(11)], pdfSample(fmt.Sprint(i), timestamp, data))
			}
			_, data := pdfRun(t, "02-sustained-anomaly", "evaluate", map[string]any{"from": 0, "to": 40, "deviceId": "device", "threshold": 10, "durationMs": 20, "maxGapMs": 30}, h)
			require.Empty(t, data["events"], "unknown operating state must not be treated as confirmed running")
			require.Equal(t, float64(3), data["diagnostics"].(map[string]any)["unknownRunning"])
			require.Nil(t, data["state"].(map[string]any)["startedAt"])
			require.Nil(t, data["state"].(map[string]any)["activeId"])
			require.JSONEq(t, `{"completedTo":40}`, string(h.checkpoint))
		})
	}
	t.Run("active-alarm-interrupted-and-replay", func(t *testing.T) {
		h := &pdfScenarioHost{rows: map[string][]map[string]any{pdfCollection(11): {
			pdfSample("a", 10, map[string]any{"value": 20, "running": true}),
			pdfSample("b", 30, map[string]any{"value": 20, "running": true}),
			pdfSample("unknown", 40, map[string]any{"value": 20}),
		}}}
		input := map[string]any{"from": 0, "to": 50, "deviceId": "device", "threshold": 10, "durationMs": 20, "maxGapMs": 30}
		_, data := pdfRun(t, "02-sustained-anomaly", "evaluate", input, h)
		events := data["events"].([]any)
		require.Len(t, events, 2)
		require.Equal(t, "unknown-running-state", events[1].(map[string]any)["reason"])
		require.Equal(t, "interrupted", events[1].(map[string]any)["status"])
		require.Equal(t, float64(40), events[1].(map[string]any)["end"])
		require.Nil(t, data["state"].(map[string]any)["activeId"])
		require.Len(t, h.rows[pdfCollection(12)], 2, "one stable alarm and one state record")
		_, replay := pdfRun(t, "02-sustained-anomaly", "evaluate", input, h)
		require.Empty(t, replay["events"])
		require.Equal(t, float64(3), replay["diagnostics"].(map[string]any)["alreadyProcessed"])
		require.Len(t, h.rows[pdfCollection(12)], 2)
	})
	t.Run("unknown-state-breaks-pending-duration", func(t *testing.T) {
		h := &pdfScenarioHost{rows: map[string][]map[string]any{pdfCollection(11): {
			pdfSample("a", 10, map[string]any{"value": 20, "running": true}),
			pdfSample("unknown", 20, map[string]any{"value": 20}),
			pdfSample("b", 30, map[string]any{"value": 20, "running": true}),
			pdfSample("c", 50, map[string]any{"value": 20, "running": true}),
			pdfSample("recovered", 55, map[string]any{"value": 1, "running": true}),
		}}}
		_, data := pdfRun(t, "02-sustained-anomaly", "evaluate", map[string]any{"from": 0, "to": 60, "deviceId": "device", "threshold": 10, "durationMs": 20, "maxGapMs": 30}, h)
		events := data["events"].([]any)
		require.Len(t, events, 2)
		require.Equal(t, "anomaly:device:30", events[0].(map[string]any)["id"])
		require.Equal(t, "recovered", events[1].(map[string]any)["status"])
		require.Equal(t, float64(1), data["diagnostics"].(map[string]any)["unknownRunning"])
	})
}

func TestProgramPDFParameterEffect(t *testing.T) {
	for _, tc := range []struct {
		name, state, boot string
		count             int
		status            string
	}{
		{"executed", "executed", "boot", 2, "compared"}, {"accepted-not-applied", "accepted", "boot", 2, "insufficient-evidence"}, {"old-boot", "executed", "old", 2, "insufficient-evidence"}, {"missing", "executed", "boot", 1, "insufficient-evidence"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &pdfScenarioHost{rows: map[string][]map[string]any{}, pageSize: 1}
			h.rows[pdfCollection(21)] = []map[string]any{pdfRecord("command", map[string]any{"state": tc.state, "deviceId": "device", "bootId": tc.boot, "actualAppliedAt": float64(100), "simulated": true})}
			for i := 0; i < tc.count; i++ {
				h.rows[pdfCollection(22)] = append(h.rows[pdfCollection(22)], pdfSample(fmt.Sprint("pre", i), float64(80+i), map[string]any{"value": 10, "running": true, "condition": "job", "workOrderId": "order"}), pdfSample(fmt.Sprint("post", i), float64(100+i), map[string]any{"value": 7, "running": true, "condition": "job", "workOrderId": "order"}))
			}
			h.rows[pdfCollection(22)] = append(h.rows[pdfCollection(22)], pdfSample("stop", 102, map[string]any{"value": 99, "running": false, "condition": "job"}), pdfSample("other", 103, map[string]any{"value": 99, "running": true, "condition": "other"}))
			h.rows[pdfCollection(22)] = append(h.rows[pdfCollection(22)], pdfSample("wrong-order", 104, map[string]any{"value": 99, "running": true, "condition": "job", "workOrderId": "another"}))
			_, data := pdfRun(t, "03-parameter-effect", "review", map[string]any{"commandId": "command", "deviceId": "device", "bootId": "boot", "condition": "job", "workOrderId": "order", "windowMs": 50, "minimumSamples": 2}, h)
			require.Equal(t, tc.status, data["status"])
			require.Equal(t, true, data["simulated"])
			if tc.status == "compared" {
				require.Equal(t, float64(-3), data["delta"])
				require.Equal(t, "not-determined", data["qualityConclusion"])
			}
		})
	}
}

func TestProgramPDFShiftSummary(t *testing.T) {
	h := &pdfScenarioHost{rows: map[string][]map[string]any{}, pageSize: 2}
	r := pdfRecord("r1", map[string]any{"reportId": "r", "workcenter": "center", "time": float64(20), "quantity": float64(10), "accepted": float64(8)})
	h.rows[pdfCollection(31)] = []map[string]any{r, pdfRecord("duplicate", r["data"].(map[string]any))}
	h.rows[pdfCollection(32)] = []map[string]any{pdfSample("e1", 10, map[string]any{"value": 100}), pdfSample("e2", 20, map[string]any{"value": 110}), pdfSample("e3", 30, map[string]any{"value": 2}), pdfSample("e4", 40, map[string]any{"value": 5})}
	input := map[string]any{"from": 0, "to": 100, "shiftId": "day", "timezone": "Asia/Shanghai", "workcenter": "center", "meterId": "device", "maxGapMs": 20, "simulated": true}
	_, data := pdfRun(t, "04-shift-summary", "summarize", input, h)
	metrics := data["metrics"].(map[string]any)
	require.Equal(t, float64(10), metrics["quantity"])
	require.Equal(t, float64(13), metrics["consumption"])
	require.Equal(t, float64(1), metrics["resets"])
	_, data = pdfRun(t, "04-shift-summary", "summarize", input, h)
	require.Equal(t, false, data["changed"])
	for _, row := range h.rows[pdfCollection(33)] {
		if row["id"] == "shift:center:day:v1" {
			row["data"].(map[string]any)["confirmed"] = true
		}
	}
	h.rows[pdfCollection(31)] = append(h.rows[pdfCollection(31)], pdfRecord("late", map[string]any{"reportId": "late", "workcenter": "center", "time": float64(25), "quantity": float64(2), "accepted": float64(1)}))
	_, data = pdfRun(t, "04-shift-summary", "summarize", input, h)
	require.Equal(t, float64(2), data["version"])
	for _, row := range h.rows[pdfCollection(33)] {
		if row["id"] == "shift:center:day:v1" {
			require.Equal(t, true, row["data"].(map[string]any)["confirmed"])
		}
	}
}

func TestProgramPDFShiftNativeRevision(t *testing.T) {
	input := map[string]any{"from": 0, "to": 100, "shiftId": "day", "timezone": "Asia/Shanghai", "workcenter": "center", "meterId": "device", "maxGapMs": 20}
	report := func(id string, nativeRevision any, accepted any, recordRevision float64) map[string]any {
		data := map[string]any{"reportId": "report", "workcenter": "center", "time": float64(20), "quantity": float64(300), "accepted": accepted}
		if nativeRevision != nil {
			data["revision"] = nativeRevision
		}
		row := pdfRecord(id, data)
		row["revision"] = recordRevision
		return row
	}
	for _, reverse := range []bool{false, true} {
		t.Run(fmt.Sprintf("native-version-wins-order-%t", reverse), func(t *testing.T) {
			// Report and inspection are different UNS records. Their platform
			// revisions cannot order the source object's revision 1 and 2.
			rows := []map[string]any{report("event-report", 1, nil, 20), report("event-inspect", 2, 280, 1)}
			if reverse {
				rows[0], rows[1] = rows[1], rows[0]
			}
			h := &pdfScenarioHost{rows: map[string][]map[string]any{pdfCollection(31): rows}, pageSize: 1}
			_, data := pdfRun(t, "04-shift-summary", "summarize", input, h)
			metrics := data["metrics"].(map[string]any)
			require.Equal(t, float64(300), metrics["quantity"])
			require.Equal(t, float64(280), metrics["accepted"])
			require.Equal(t, float64(1), metrics["reportCount"])
		})
	}
	for _, field := range []string{"time", "workcenter"} {
		t.Run("correction-moves-outside-"+field, func(t *testing.T) {
			old := report("old", 1, 280, 1)
			latest := report("latest", 2, 290, 1)
			if field == "time" {
				latest["data"].(map[string]any)[field] = float64(120)
			} else {
				latest["data"].(map[string]any)[field] = "other-center"
			}
			h := &pdfScenarioHost{rows: map[string][]map[string]any{pdfCollection(31): {old, latest}}, pageSize: 1}
			_, data := pdfRun(t, "04-shift-summary", "summarize", input, h)
			metrics := data["metrics"].(map[string]any)
			require.Equal(t, float64(0), metrics["quantity"])
			require.Equal(t, float64(0), metrics["accepted"])
			require.Equal(t, float64(0), metrics["reportCount"])
		})
	}
	for _, tc := range []struct {
		name string
		rows []map[string]any
	}{
		{"single-record-current-version", []map[string]any{report("same", nil, 270, 1), report("same", nil, 280, 2)}},
		{"independent-records-current-values-agree", []map[string]any{report("a", nil, 270, 1), report("b", nil, 280, 1), report("a", nil, 280, 2)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &pdfScenarioHost{rows: map[string][]map[string]any{pdfCollection(31): tc.rows}, pageSize: 1}
			_, data := pdfRun(t, "04-shift-summary", "summarize", input, h)
			metrics := data["metrics"].(map[string]any)
			require.Equal(t, float64(280), metrics["accepted"])
			require.Equal(t, float64(1), metrics["reportCount"])
		})
	}
	oversized := make([]map[string]any, 0, 101)
	for i := 0; i < 101; i++ {
		row := report(fmt.Sprint("report-", i), 1, 280, 1)
		row["data"].(map[string]any)["reportId"] = fmt.Sprint("report-", i)
		oversized = append(oversized, row)
	}
	for _, tc := range []struct {
		name string
		rows []map[string]any
	}{
		{"identity-budget", oversized},
		{"independent-record-edit-counts-not-comparable", []map[string]any{report("a", nil, 280, 20), report("b", nil, 270, 1)}},
		{"independent-copy-becomes-stale", []map[string]any{report("a", nil, 270, 1), report("b", nil, 270, 1), report("a", nil, 280, 2)}},
		{"conflicting-source-version", []map[string]any{report("event-a", 2, 280, 1), report("event-b", 2, 270, 2)}},
		{"older-conflict-after-latest", []map[string]any{report("latest", 3, 290, 1), report("event-a", 2, 280, 1), report("event-b", 2, 270, 2)}},
		{"older-conflict-before-latest", []map[string]any{report("event-a", 2, 280, 1), report("event-b", 2, 270, 2), report("latest", 3, 290, 1)}},
		{"mixed-version-spaces", []map[string]any{report("event-a", 2, 280, 1), report("event-b", nil, 270, 20)}},
		{"zero-source-version", []map[string]any{report("event", 0, 280, 1)}},
		{"fractional-source-version", []map[string]any{report("event", 1.5, 280, 1)}},
		{"string-source-version", []map[string]any{report("event", "2", 280, 1)}},
		{"unsafe-source-version", []map[string]any{report("event", 9007199254740992.0, 280, 1)}},
		{"conflicting-record-version", []map[string]any{report("event-a", nil, 280, 1), report("event-b", nil, 270, 1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, err := os.ReadFile("testdata/pdf-scenarios/04-shift-summary.js")
			require.NoError(t, err)
			raw, err := json.Marshal(input)
			require.NoError(t, err)
			request := programRequest(string(code), "summarize")
			request.Input = raw
			h := &pdfScenarioHost{rows: map[string][]map[string]any{pdfCollection(31): tc.rows}, pageSize: 50}
			checkpointCalls := 0
			result, err := ExecuteProgram(context.Background(), request, func(ctx context.Context, call ProgramCall) (json.RawMessage, error) {
				if call.Kind == "checkpoint.set" {
					checkpointCalls++
				}
				return h.call(ctx, call)
			})
			require.ErrorIs(t, err, ErrExecution)
			require.Empty(t, result.Outputs)
			require.Zero(t, checkpointCalls)
		})
	}
}

func TestProgramPDFLotGenealogy(t *testing.T) {
	h := &pdfScenarioHost{rows: map[string][]map[string]any{}, pageSize: 1}
	for _, edge := range []struct{ id, from, to, relation string }{{"split1", "A", "B", "split"}, {"split2", "A", "C", "split"}, {"merge1", "B", "D", "merge"}, {"merge2", "C", "D", "merge"}, {"cycle", "D", "A", "rework"}, {"missing", "D", "E", "consume"}} {
		h.rows[pdfCollection(41)] = append(h.rows[pdfCollection(41)], pdfRecord(edge.id, map[string]any{"fromLot": edge.from, "toLot": edge.to, "relationship": edge.relation}))
	}
	input := map[string]any{"rootLot": "A", "direction": "forward", "maxNodes": 20, "maxPages": 2, "simulated": true}
	var data map[string]any
	for round := 0; round < 8; round++ {
		_, data = pdfRun(t, "05-lot-genealogy", "trace", input, h)
		if data["complete"] == true {
			break
		}
	}
	require.Equal(t, true, data["complete"])
	require.Len(t, data["links"], 6)
	require.NotEmpty(t, data["cycles"])
	require.Contains(t, data["missing"].([]any), "E")
	h.checkpoint = nil
	input["rootLot"] = "D"
	input["direction"] = "backward"
	input["maxPages"] = 8
	_, data = pdfRun(t, "05-lot-genealogy", "trace", input, h)
	require.NotEmpty(t, data["links"])
	t.Run("acyclic-split-and-merge-is-not-cycle", func(t *testing.T) {
		dag := &pdfScenarioHost{rows: map[string][]map[string]any{}, pageSize: 1}
		for _, edge := range h.rows[pdfCollection(41)] {
			if edge["id"] != "cycle" {
				dag.rows[pdfCollection(41)] = append(dag.rows[pdfCollection(41)], edge)
			}
		}
		_, data := pdfRun(t, "05-lot-genealogy", "trace", map[string]any{"rootLot": "A", "direction": "forward", "maxNodes": 20, "maxPages": 8}, dag)
		require.Equal(t, true, data["complete"])
		require.Empty(t, data["cycles"])
	})
}

func TestProgramPDFRegistrationNeverPerformsIO(t *testing.T) {
	for _, file := range []string{"01-operation-attribution", "02-sustained-anomaly", "03-parameter-effect", "04-shift-summary", "05-lot-genealogy"} {
		t.Run(file, func(t *testing.T) {
			code, err := os.ReadFile("testdata/pdf-scenarios/" + file + ".js")
			require.NoError(t, err)
			request := programRequest(string(code), "")
			request.CheckOnly = true
			result, err := ExecuteProgram(context.Background(), request, func(context.Context, ProgramCall) (json.RawMessage, error) {
				t.Fatal("registration performed I/O")
				return nil, nil
			})
			require.NoError(t, err)
			require.Len(t, result.Manifest.Entries, 1)
			require.Empty(t, result.Manifest.Sources)
		})
	}
}

func TestProgramPDFPageBudgetFailsWithoutCommit(t *testing.T) {
	code, err := os.ReadFile("testdata/pdf-scenarios/01-operation-attribution.js")
	require.NoError(t, err)
	request := programRequest(string(code), "attribute")
	request.Input = json.RawMessage(`{"from":0,"to":200,"deviceId":"device"}`)
	queries, writes := 0, 0
	_, err = ExecuteProgram(context.Background(), request, func(_ context.Context, call ProgramCall) (json.RawMessage, error) {
		if call.Kind == "checkpoint.set" {
			writes++
			return json.RawMessage(`null`), nil
		}
		queries++
		return json.Marshal(map[string]any{"items": []any{}, "nextCursor": fmt.Sprint(queries), "hasMore": true})
	})
	require.ErrorIs(t, err, ErrExecution)
	require.Equal(t, 8, queries)
	require.Zero(t, writes)
}

func TestProgramPDFQueryFailureHasNoBufferedOutput(t *testing.T) {
	code, err := os.ReadFile("testdata/pdf-scenarios/04-shift-summary.js")
	require.NoError(t, err)
	h := &pdfScenarioHost{rows: map[string][]map[string]any{}, failQuery: true}
	request := programRequest(string(code), "summarize")
	request.Input = json.RawMessage(`{"from":0,"to":100,"shiftId":"day","timezone":"Asia/Shanghai","workcenter":"center","meterId":"device","maxGapMs":20}`)
	_, err = ExecuteProgram(context.Background(), request, h.call)
	require.Error(t, err)
	require.Nil(t, h.checkpoint)
	require.Empty(t, h.rows)
}

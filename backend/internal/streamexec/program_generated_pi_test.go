package streamexec

// 显式运行当前 JS 生成器的完整输出，验证非良好质量的对象/null 原值保留。
// 宿主只提供三个有界合成响应；无网络、设备或数据库，不宣称观察到真实坏质量。
import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGeneratedPINonGoodObservation(t *testing.T) {
	file := os.Getenv("PLANT0_PI_GENERATED_PROGRAM_FILE")
	if file == "" {
		t.Skip("provide current plant0-pdf-readonly-ingest.program output for explicit generated-code verification")
	}
	code, err := os.ReadFile(file)
	require.NoError(t, err)
	require.LessOrEqual(t, len(code), MaxJSONBytes)
	const point = "Plant0.PlantA.A-VMC-01.SpindleTemp"
	from := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	rows := []map[string]any{
		{"Value": map[string]any{"Name": "No Data", "Value": 248, "IsSystem": true}, "Good": false, "Questionable": false},
		{"Value": nil, "Good": false, "Questionable": false},
		{"Value": "unverified", "Good": true, "Questionable": true},
		{"Value": map[string]any{"Name": "Substituted", "IsSystem": true}, "Good": true, "Questionable": false, "Substituted": true},
	}
	for index, row := range rows {
		row["Timestamp"] = from.Add(time.Duration(index) * time.Millisecond).Format(time.RFC3339Nano)
	}
	attributes := []map[string]any{}
	for key, value := range map[string]any{"instrumenttag": "A-VMC-01.SpindleTemp", "tag": point,
		"pointtype": "Float64", "engunits": "degC", "scan_seconds": 5, "compressing": true} {
		attributes = append(attributes, map[string]any{"Name": key, "Value": value})
	}
	responses := map[string]any{
		"points/web-A":            map[string]any{"WebId": "web-A", "Name": point, "PointType": "Float64", "EngineeringUnits": "degC"},
		"points/web-A/attributes": map[string]any{"Items": attributes},
		"streams/web-A/recorded":  map[string]any{"Items": rows},
	}
	input, err := json.Marshal(map[string]any{"webId": "web-A", "name": point, "from": from.UnixMilli(), "to": from.Add(2 * time.Minute).UnixMilli(),
		"nativeDeviceId": "A-VMC-01", "instrumentTag": "A-VMC-01.SpindleTemp",
		"pointRecordId": fmt.Sprintf("input-%x", sha256.Sum256([]byte(`["pi-recorded","web-A"]`)))})
	require.NoError(t, err)
	calls := 0
	result, err := ExecuteProgram(context.Background(), ProgramRequest{Code: string(code), EntryID: "pi-history", Input: input, TimeoutMs: 5000},
		func(_ context.Context, call ProgramCall) (json.RawMessage, error) {
			calls++
			require.LessOrEqual(t, calls, 3)
			require.Equal(t, "source.request", call.Kind)
			require.Equal(t, "3", call.SourceID)
			var request struct{ Method, Path string }
			require.NoError(t, json.Unmarshal(call.Options, &request))
			require.Equal(t, "GET", request.Method)
			value, exists := responses[request.Path]
			require.True(t, exists, "only three explicit fixture routes allowed")
			return json.Marshal(map[string]any{"status": 200, "headers": map[string]any{}, "data": value})
		})
	require.NoError(t, err)
	require.Empty(t, result.Error)
	require.Equal(t, 3, calls)
	require.Len(t, result.Manifest.Sources, 1)
	require.Len(t, result.Outputs, 4)
	for index, output := range result.Outputs {
		var data struct {
			Record, Sample map[string]any
			Quality        string
		}
		require.NoError(t, json.Unmarshal(output.Data, &data))
		require.Equal(t, []string{"bad", "bad", "questionable", "substituted"}[index], data.Quality)
		for _, observation := range []map[string]any{data.Record, data.Sample} {
			require.NotContains(t, observation, "value")
			require.NotContains(t, observation, "running")
			require.Equal(t, false, observation["numericObservationVerified"])
			require.Equal(t, false, observation["numericCalculationEligible"])
			expected, _ := json.Marshal(rows[index])
			actual, _ := json.Marshal(observation["native"])
			require.JSONEq(t, string(expected), string(actual))
		}
	}
	var summary struct {
		Observations int
		Blocked      bool
	}
	require.NoError(t, json.Unmarshal(result.Data, &summary))
	require.Equal(t, 4, summary.Observations)
	require.True(t, summary.Blocked, "quality facts do not become eligible measurements")
}

// Uses the real Goja executor with explicit synthetic read-only responses.
// The host stages checkpoint state; it does not simulate a database commit.
func TestGeneratedPIWindowProgress(t *testing.T) {
	file := os.Getenv("PLANT0_PI_GENERATED_PROGRAM_FILE")
	if file == "" {
		t.Skip("provide current generated PI program for explicit execution verification")
	}
	code, err := os.ReadFile(file)
	require.NoError(t, err)
	from := time.Date(2026, 10, 12, 1, 0, 0, 0, time.UTC)
	const point = "Plant0.PlantA.A-VMC-01.SpindleTemp"
	input, err := json.Marshal(map[string]any{"webId": "web-A", "name": point, "from": from.UnixMilli(), "to": from.Add(2 * time.Minute).UnixMilli(),
		"nativeDeviceId": "A-VMC-01", "instrumentTag": "A-VMC-01.SpindleTemp", "pointRecordId": fmt.Sprintf("input-%x", sha256.Sum256([]byte(`["pi-recorded","web-A"]`)))})
	require.NoError(t, err)
	attributes := []map[string]any{}
	for key, value := range map[string]any{"instrumenttag": "A-VMC-01.SpindleTemp", "tag": point, "pointtype": "Float64", "engunits": "degC", "scan_seconds": 5, "compressing": true} {
		attributes = append(attributes, map[string]any{"Name": key, "Value": value})
	}
	committed := json.RawMessage(`null`)
	execute := func(fail bool) (ProgramResult, json.RawMessage, int, error) {
		var staged json.RawMessage
		calls, pages := 0, 0
		result, runErr := ExecuteProgram(context.Background(), ProgramRequest{Code: string(code), EntryID: "pi-window", Input: input, TimeoutMs: 5000},
			func(_ context.Context, call ProgramCall) (json.RawMessage, error) {
				calls++
				require.LessOrEqual(t, calls, 14, "four bounded three-request history reads plus checkpoint get/set")
				switch call.Kind {
				case "checkpoint.get":
					return append(json.RawMessage(nil), committed...), nil
				case "checkpoint.set":
					require.LessOrEqual(t, len(call.Input), MaxCheckpointBytes)
					staged = append(json.RawMessage(nil), call.Input...)
					return json.RawMessage(`{"stage":"buffered"}`), nil
				case "source.request":
					require.Equal(t, "3", call.SourceID)
					var req struct {
						Method, Path string
						Query        map[string]string
					}
					require.NoError(t, json.Unmarshal(call.Options, &req))
					require.Equal(t, "GET", req.Method)
					var data any
					switch req.Path {
					case "points/web-A":
						data = map[string]any{"WebId": "web-A", "Name": point, "PointType": "Float64", "EngineeringUnits": "degC"}
					case "points/web-A/attributes":
						data = map[string]any{"Items": attributes}
					case "streams/web-A/recorded":
						pages++
						if fail && pages == 2 {
							return nil, errors.New("explicit synthetic source outage")
						}
						require.Equal(t, "16", req.Query["maxCount"])
						start, parseErr := time.Parse(time.RFC3339Nano, req.Query["startTime"])
						require.NoError(t, parseErr)
						end, parseErr := time.Parse(time.RFC3339Nano, req.Query["endTime"])
						require.NoError(t, parseErr)
						rows := []map[string]any{}
						for i := 0; i < 120 && len(rows) < 16; i++ {
							at := from.Add(time.Duration(i) * time.Second)
							if !at.Before(start) && !at.After(end) { // Include both boundaries deliberately.
								rows = append(rows, map[string]any{"Timestamp": at.Format(time.RFC3339Nano), "Value": i, "Good": true, "Questionable": false})
							}
						}
						data = map[string]any{"Items": rows}
					default:
						t.Fatalf("unexpected source path %q", req.Path)
					}
					return json.Marshal(map[string]any{"status": 200, "data": data})
				default:
					t.Fatalf("unexpected host call %q", call.Kind)
					return nil, nil
				}
			})
		return result, staged, calls, runErr
	}
	first, staged, _, err := execute(false)
	require.NoError(t, err)
	require.Empty(t, first.Outputs, "initial full pages must not buffer prefixes")
	require.NotEmpty(t, staged)
	committed = staged
	before := string(committed)
	failed, failedProgress, _, err := execute(true)
	require.ErrorIs(t, err, ErrExecution)
	require.Empty(t, failed.Outputs, "an error discards this execution's buffered writes")
	require.Empty(t, failedProgress)
	require.Equal(t, before, string(committed), "failure must not advance staged progress")
	times := map[int64]bool{}
	complete := false
	for step := 0; step < 16; step++ {
		result, next, _, runErr := execute(false)
		require.NoError(t, runErr)
		for _, output := range result.Outputs {
			var row struct {
				Time   int64
				Sample map[string]any
			}
			require.NoError(t, json.Unmarshal(output.Data, &row))
			require.False(t, times[row.Time], "inclusive endpoints cannot duplicate samples")
			times[row.Time] = true
			require.Equal(t, float64((row.Time-from.UnixMilli())/1000), row.Sample["value"])
		}
		require.NotEmpty(t, next)
		committed = next
		var progress struct{ Complete bool }
		require.NoError(t, json.Unmarshal(result.Data, &progress))
		if progress.Complete {
			complete = true
			break
		}
	}
	require.True(t, complete)
	require.Len(t, times, 120)
	replay, next, calls, err := execute(false)
	require.NoError(t, err)
	require.Empty(t, replay.Outputs)
	require.Empty(t, next)
	require.Equal(t, 1, calls, "completed window only reads checkpoint; no source request")
}

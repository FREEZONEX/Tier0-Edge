// 使用前端导出的实际模板验证 Goja 合同；宿主仅提供固定样本，不操作设备。
package streamexec

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProgram_ActualEditorTemplates(t *testing.T) {
	type hostCall struct {
		Kind     string          `json:"kind"`
		SourceID string          `json:"sourceId"`
		Ref      string          `json:"ref,omitempty"`
		Input    json.RawMessage `json:"input,omitempty"`
	}
	var fixture struct {
		Contract string `json:"contract"`
		Cases    []struct {
			Name     string `json:"name"`
			Document struct {
				Script    string `json:"script"`
				TimeoutMs int    `json:"timeoutMs"`
			} `json:"document"`
			Sources []ProgramSource `json:"expectedSources"`
			Topics  []ProgramTopic  `json:"expectedTopics"`
			Runs    []struct {
				EntryID string          `json:"entryId"`
				Input   json.RawMessage `json:"input"`
				Data    json.RawMessage `json:"expectedData"`
				Calls   []hostCall      `json:"expectedCalls"`
				Outputs []ProgramOutput `json:"expectedOutputs"`
			} `json:"executions"`
		} `json:"cases"`
	}
	raw, err := os.ReadFile("testdata/editor-sdk-v2-templates.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &fixture))
	require.NotEmpty(t, fixture.Cases)
	require.Len(t, fixture.Cases, 13)
	for _, item := range fixture.Cases {
		t.Run(item.Name, func(t *testing.T) {
			request := programRequest(item.Document.Script, "")
			request.TimeoutMs, request.CheckOnly = item.Document.TimeoutMs, true
			compiled, err := ExecuteProgram(context.Background(), request, nil)
			require.NoError(t, err)
			require.Equal(t, item.Sources, compiled.Manifest.Sources)
			require.Equal(t, item.Topics, compiled.Manifest.Topics)
			request.CheckOnly = false
			request.ExpectedManifest = &compiled.Manifest
			for _, run := range item.Runs {
				t.Run(run.EntryID, func(t *testing.T) {
					request.EntryID, request.Input = run.EntryID, run.Input
					calls := []hostCall{}
					result, err := ExecuteProgram(context.Background(), request, func(_ context.Context, call ProgramCall) (json.RawMessage, error) {
						require.True(t, AllowsProgramCall(compiled.Manifest, call))
						kind := strings.TrimPrefix(call.Kind, "source.")
						input := call.Input
						if kind == "request" {
							input = call.Options
						}
						calls = append(calls, hostCall{Kind: kind, SourceID: call.SourceID, Ref: call.ResourceRef, Input: input})
						if kind == "parameters" {
							return json.Marshal(map[string]any{"path": "/data"})
						}
						if kind == "request" {
							return json.Marshal(map[string]any{"status": 200, "headers": map[string]string{}, "data": map[string]any{"value": 37}})
						}
						return json.Marshal(map[string]any{"kind": kind, "value": 37})
					})
					require.NoError(t, err)
					require.JSONEq(t, string(run.Data), string(result.Data))
					for _, pair := range [][2]any{{run.Calls, calls}, {run.Outputs, result.Outputs}} {
						expected, err := json.Marshal(pair[0])
						require.NoError(t, err)
						actual, err := json.Marshal(pair[1])
						require.NoError(t, err)
						require.JSONEq(t, string(expected), string(actual))
					}
				})
			}
		})
	}
}

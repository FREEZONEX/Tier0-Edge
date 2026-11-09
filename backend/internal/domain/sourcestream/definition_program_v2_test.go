package sourcestream

// 当前配置只验证契约形状；不依赖Source在线、不运行脚本，也不触及数据库。
import (
	"encoding/json"
	"fmt"
	"testing"

	"backend/internal/streamexec"

	"github.com/stretchr/testify/require"
)

func TestCompleteProgramDefinitionFlatRoundTripAndPureValidation(t *testing.T) {
	var definition StreamDefinition
	require.NoError(t, json.Unmarshal([]byte(`{"name":"完整程序","script":"module.exports = api => {};","extensions":{"vendor":{"enabled":true}}}`), &definition))
	require.True(t, definition.IsCompleteProgram())
	require.Equal(t, ProgramAccess, definition.Model)
	require.Equal(t, 1000, definition.Script.TimeoutMs)
	require.NoError(t, definition.ValidateDraft())
	require.NoError(t, definition.ValidateDeployment(999, SourceDefinition{}), "shape gate does not require a selected Source")
	encoded, err := json.Marshal(definition)
	require.NoError(t, err)
	var flat map[string]any
	require.NoError(t, json.Unmarshal(encoded, &flat))
	require.Equal(t, "module.exports = api => {};", flat["script"])
	require.Equal(t, float64(1000), flat["timeoutMs"])
	for _, field := range []string{"model", "sourceId", "program", "responseScript", "topicIds", "nativeResourceRef", "nativeResourceRefs"} {
		require.NotContains(t, flat, field)
	}
	var decoded StreamDefinition
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, definition, decoded)
	definition.Script.Code = ""
	require.NoError(t, definition.ValidateDraft())
	require.Error(t, definition.ValidateDeployment(0, SourceDefinition{}))
}

func TestCompleteProgramDefinitionRejectsMixedAndInvalidContracts(t *testing.T) {
	for _, field := range []string{"model", "sourceId", "program", "responseScript", "topicIds", "nativeResourceRef", "nativeResourceRefs"} {
		t.Run(field, func(t *testing.T) {
			var definition StreamDefinition
			require.Error(t, json.Unmarshal([]byte(fmt.Sprintf(`{"name":"p","script":"x","%s":null}`, field)), &definition))
		})
	}
	for _, raw := range []string{
		`{"name":"p","script":{"code":"old","timeoutMs":1000}}`,
		`{"name":"p","script":null}`,
		`{"name":"p","script":"x","timeoutMs":null}`,
		`{"name":"p","script":"x","timeoutMs":"1000"}`,
		`{"sdkVersion":123,"name":"p","script":"x"}`,
		`[]`, `null`,
	} {
		var definition StreamDefinition
		require.Error(t, json.Unmarshal([]byte(raw), &definition))
	}
	for _, timeout := range []int{0, -1, streamexec.MaxProgramTimeoutMs + 1} {
		definition := StreamDefinition{Name: "p", Script: ScriptDefinition{Code: "x", TimeoutMs: timeout}}
		require.Error(t, definition.ValidateDraft())
	}
	maximum := StreamDefinition{Name: "p", Script: ScriptDefinition{Code: "x", TimeoutMs: streamexec.MaxProgramTimeoutMs}}
	require.NoError(t, maximum.ValidateDraft())
	for _, field := range []string{"sdkVersion", "configVersion", "supportedVersions"} {
		for _, value := range []string{`"tier0-stream/v2"`, `"unknown"`, `null`, `[]`} {
			var definition StreamDefinition
			require.Error(t, json.Unmarshal([]byte(fmt.Sprintf(`{"name":"p","script":"x","%s":%s}`, field, value)), &definition))
		}
	}
	definition := StreamDefinition{Name: "legacy", Model: Publish, SourceID: 1, Script: ScriptDefinition{Code: "return {data:input};", TimeoutMs: 1000}}
	require.Error(t, definition.ValidateDraft())
	_, err := json.Marshal(definition)
	require.Error(t, err)
}

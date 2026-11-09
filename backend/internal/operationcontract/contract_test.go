package operationcontract

// 验证共享操作契约、显式 Schema 子集、大小边界与输入输出，不访问外部服务。
import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func validResource() Resource {
	return Resource{Code: "device.start", Name: "启动设备", ContractVersion: "1.0", SideEffect: "write", Idempotency: "required", TimeoutMs: 5000,
		InputSchema:  json.RawMessage(`{"type":"object","required":["speed"],"properties":{"speed":{"type":"number","minimum":0}},"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["accepted"],"properties":{"accepted":{"type":"boolean"}}}`)}
}

func TestContractValidation(t *testing.T) {
	r := validResource()
	require.NoError(t, r.Validate())
	require.NoError(t, r.ValidateInput(json.RawMessage(`{"speed":1}`)))
	require.ErrorIs(t, r.ValidateInput(json.RawMessage(`{"speed":-1}`)), ErrInput)
	require.ErrorIs(t, r.ValidateInput(json.RawMessage(`{"speed":1,"secret":true}`)), ErrInput)
	require.ErrorIs(t, r.ValidateInput(json.RawMessage(`null`)), ErrInput)
	require.NoError(t, r.ValidateOutput(json.RawMessage(`{"accepted":true}`)))
	require.ErrorIs(t, r.ValidateOutput(nil), ErrOutput)
	require.ErrorIs(t, r.ValidateOutput(json.RawMessage(`{"accepted":"true"}`)), ErrOutput)
	for name, mutate := range map[string]func(*Resource){
		"identity": func(r *Resource) { r.Code = "../device" }, "name": func(r *Resource) { r.Name = " start " },
		"version": func(r *Resource) { r.ContractVersion = "" }, "effect": func(r *Resource) { r.SideEffect = "arbitrary" },
		"idempotency": func(r *Resource) { r.Idempotency = "optional" }, "timeout": func(r *Resource) { r.TimeoutMs = MaxTimeoutMs + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := validResource()
			mutate(&candidate)
			require.ErrorIs(t, candidate.Validate(), ErrContract)
		})
	}
}

func TestSchemaBoundaries(t *testing.T) {
	for _, schema := range []string{
		`null`, `[]`, `{"type":"object","$ref":"https://example.com/schema"}`,
		`{"type":"object","properties":{"payload":{}}}`, `{"type":"array","items":{}}`,
		`{"type":"string","pattern":".*"}`, `{"type":"array"}`, `{"type":"object","properties":{"__proto__":{"type":"string"}}}`,
		`{"type":"object","required":["missing"]}`, `{"type":"object","additionalProperties":{}}`,
		`{"type":"string","minimum":1}`, `{"type":"number","minimum":null}`,
		`{"type":"string","enum":[]}`, `{"type":"object","properties":null}`,
		`{"type":"object","required":null}`, `{"type":"array","items":null}`,
		`{"type":"object","properties":{"x":{"type":"string"}},"required":["x","x"]}`,
		strings.Repeat(" ", MaxSchemaBytes+1),
	} {
		r := validResource()
		r.InputSchema = json.RawMessage(schema)
		require.ErrorIs(t, r.Validate(), ErrContract, schema)
	}
	nested := `{"type":"string"}`
	for i := 0; i <= MaxSchemaDepth; i++ {
		nested = `{"type":"array","items":` + nested + `}`
	}
	r := validResource()
	r.InputSchema = json.RawMessage(nested)
	require.ErrorIs(t, r.Validate(), ErrContract)
	r.InputSchema = json.RawMessage(`{"type":"array","items":{"type":"integer","enum":[1,2],"minimum":1,"maximum":2}}`)
	require.NoError(t, r.Validate())
	require.NoError(t, r.ValidateInput(json.RawMessage(`[1,2]`)))
	require.ErrorIs(t, r.ValidateInput(json.RawMessage(`[3]`)), ErrInput)
}

func TestRequestContractAndUnconstrainedRoot(t *testing.T) {
	r := validResource()
	r.InputSchema, r.OutputSchema = json.RawMessage(`{}`), json.RawMessage(`{}`)
	r.SideEffect, r.Idempotency, r.TimeoutMs = "read", "optional", MaxRequestTimeoutMs
	require.NoError(t, r.ValidateRequest())
	require.NoError(t, r.ValidateStored())
	require.ErrorIs(t, r.Validate(), ErrContract, "legacy declarations retain their own limits")
	for _, value := range []string{`null`, `42`, `false`, `"value"`, `[1,"x",{}]`, `{"data":{"nested":true}}`} {
		require.NoError(t, r.ValidateInput(json.RawMessage(value)))
		require.NoError(t, r.ValidateOutput(json.RawMessage(value)))
	}
	for _, value := range []string{"", `{} {}`, `{"invalid":`, `"` + strings.Repeat("a", MaxValueBytes) + `"`} {
		require.ErrorIs(t, r.ValidateInput(json.RawMessage(value)), ErrInput)
		require.ErrorIs(t, r.ValidateOutput(json.RawMessage(value)), ErrOutput)
	}
	for _, policy := range []string{"none", "optional", "required"} {
		r.Idempotency = policy
		for _, timeout := range []int{1, MaxRequestTimeoutMs} {
			r.TimeoutMs = timeout
			require.NoError(t, r.ValidateRequest())
		}
	}
	for _, mutate := range []func(*Resource){
		func(r *Resource) { r.SideEffect = "none" },
		func(r *Resource) { r.SideEffect, r.Idempotency = "write", "optional" },
		func(r *Resource) { r.SideEffect, r.Idempotency = "write", "none" },
		func(r *Resource) { r.Code = "Device:Start" },
		func(r *Resource) { r.TimeoutMs = 0 },
		func(r *Resource) { r.TimeoutMs = MaxRequestTimeoutMs + 1 },
	} {
		candidate := r
		mutate(&candidate)
		require.ErrorIs(t, candidate.ValidateRequest(), ErrContract)
	}
}

func TestAppRoutesAreExactPublicControlEndpoints(t *testing.T) {
	for _, path := range []string{"/openapi/v1/controls/discover", "/openapi/v1/controls/device.start/describe", "/openapi/v1/controls/device:start/invoke"} {
		require.True(t, IsAppRoute("POST", path), path)
		for _, method := range []string{"GET", "PUT", "DELETE", "OPTIONS", "post"} {
			require.False(t, IsAppRoute(method, path), method+" "+path)
		}
	}
	for _, path := range []string{
		"/openapi/v1/controls/", "/openapi/v1/controls/discover/", "/openapi/v1/controls/device/invoke/extra",
		"/openapi/v1/controls/device/manage", "/openapi/v1/controls/../invoke", "/openapi/v1/controls/a%2Fb/invoke",
		"/openapi/v1/controls//invoke", "/openapi/v1/controls/" + strings.Repeat("a", 129) + "/invoke",
		"/openapi/v1/uns/write", "/openapi/v1/uns/streams/1/invoke", "/api/core/uns/streams/1/invoke",
	} {
		require.False(t, IsAppRoute("POST", path), path)
	}
}

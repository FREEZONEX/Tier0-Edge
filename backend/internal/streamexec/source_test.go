package streamexec

// 验证只读 Source SDK、依赖隔离、数据副本、错误脱敏与执行预算。
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func sourceRequest(code string) Request {
	return Request{Code: code, Input: json.RawMessage(`{}`), TimeoutMs: 1000, Sources: []SourceGrant{
		{Alias: "boiler", SourceID: 11, ConfigRevision: 2, ReadResources: []string{"opcua:temperature"}},
		{Alias: "pump", SourceID: 12, ConfigRevision: 1, ReadResources: []string{"modbus:pressure"}},
	}}
}

func TestSourceSDKMultipleDependencies(t *testing.T) {
	request := sourceRequest(`
const boiler = sources.get("boiler");
const temperature = boiler.read("opcua:temperature");
JSON.parse = () => { throw new Error("replaced"); };
JSON.stringify = () => { throw new Error("replaced"); };
const pressure = sources.get("pump").read("modbus:pressure", {quantity: 1});
return {outputs:[{topicId:21,data:temperature},{topicId:22,data:pressure}],drop:false,logs:[]};`)
	var calls []SourceCall
	host := func(ctx context.Context, call SourceCall) (json.RawMessage, error) {
		require.NoError(t, ctx.Err())
		calls = append(calls, call)
		return json.RawMessage(fmt.Sprintf(`{"value":%d}`, call.ID*10)), nil
	}
	result, err := ExecuteWithSources(context.Background(), request, host)
	require.NoError(t, err)
	require.Len(t, calls, 2)
	require.Equal(t, "boiler", calls[0].Alias)
	require.JSONEq(t, `{}`, string(calls[0].Input))
	require.JSONEq(t, `{"quantity":1}`, string(calls[1].Input))
	require.JSONEq(t, `{"value":10}`, string(result.Outputs[0].Data))
	require.JSONEq(t, `{"value":20}`, string(result.Outputs[1].Data))
}

func TestSourceSDKRejectsUndeclaredReadsAndWrites(t *testing.T) {
	for _, code := range []string{
		`sources.get("unknown").read("opcua:temperature")`,
		`sources.get("boiler").read("modbus:pressure")`,
		`sources.get("boiler").write("opcua:temperature", 100)`,
		`sources.get("boiler").invoke("opcua:reset", {})`,
		`sources.get("boiler").read({toString(){return "opcua:temperature"}})`,
		`sources.get("boiler").read("opcua:temperature", undefined)`,
		`sources.get("__proto__")`,
	} {
		called := false
		_, err := ExecuteWithSources(context.Background(), sourceRequest(code), func(context.Context, SourceCall) (json.RawMessage, error) {
			called = true
			return json.RawMessage(`{}`), nil
		})
		require.ErrorIs(t, err, ErrExecution, code)
		require.False(t, called, code)
	}
}

func TestSourceSDKBudgetAndFailureIsolation(t *testing.T) {
	calls := 0
	request := sourceRequest(`for(let i=0;i<33;i++) sources.get("boiler").read("opcua:temperature"); return {data:{},drop:false};`)
	_, err := ExecuteWithSources(context.Background(), request, func(context.Context, SourceCall) (json.RawMessage, error) {
		calls++
		return json.RawMessage(`{}`), nil
	})
	require.ErrorIs(t, err, ErrExecution)
	require.Equal(t, MaxSourceCalls, calls)
	request.Code = `try { sources.get("boiler").read("opcua:temperature"); } catch(e) { return {data:{error:e.message},drop:false}; }`
	result, err := ExecuteWithSources(context.Background(), request, func(context.Context, SourceCall) (json.RawMessage, error) {
		return nil, errors.New("password=private")
	})
	require.NoError(t, err)
	require.NotContains(t, string(result.Data), "private")
	require.Contains(t, string(result.Data), ErrSourceCall.Error())
	for _, data := range []json.RawMessage{json.RawMessage(`broken`), json.RawMessage(`"` + strings.Repeat("x", MaxJSONBytes) + `"`)} {
		result, err := ExecuteWithSources(context.Background(), request, func(context.Context, SourceCall) (json.RawMessage, error) { return data, nil })
		require.NoError(t, err)
		require.Contains(t, string(result.Data), ErrSourceCall.Error())
	}
	request.Code = `sources.get("boiler").read("opcua:temperature"); return {data:{},drop:false};`
	request.TimeoutMs = 30
	_, err = ExecuteWithSources(context.Background(), request, func(ctx context.Context, _ SourceCall) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	require.ErrorIs(t, err, ErrTimeout)
}

func TestSourceSDKNoHostAndNoSources(t *testing.T) {
	request := sourceRequest(`return {data:sources.get("boiler").read("opcua:temperature"),drop:false};`)
	_, err := Execute(context.Background(), request)
	require.ErrorIs(t, err, ErrExecution)
	request.Sources = nil
	request.Code = `return {data:input,drop:false};`
	_, err = Execute(context.Background(), request)
	require.NoError(t, err)
	request.Sources = sourceRequest("").Sources
	request.CheckOnly = true
	request.Code = `return {data:sources.get("boiler").read("opcua:temperature"),drop:false};`
	_, err = ExecuteWithSources(context.Background(), request, func(context.Context, SourceCall) (json.RawMessage, error) {
		t.Fatal("syntax check must not read devices")
		return nil, nil
	})
	require.NoError(t, err)
}

func TestSourceGrantValidation(t *testing.T) {
	require.True(t, ValidSourceGrants(nil))
	require.True(t, ValidSourceGrants(sourceRequest("").Sources))
	for _, alias := range []string{"", "__proto__", "constructor", "prototype", "0source", "a.b", strings.Repeat("a", 64)} {
		grants := sourceRequest("").Sources
		grants[0].Alias = alias
		require.False(t, ValidSourceGrants(grants), alias)
	}
	for _, change := range []func([]SourceGrant){
		func(g []SourceGrant) { g[0].SourceID = 0 },
		func(g []SourceGrant) { g[0].ConfigRevision = 0 },
		func(g []SourceGrant) { g[0].ReadResources = nil },
		func(g []SourceGrant) { g[0].ReadResources = []string{"x", "x"} },
		func(g []SourceGrant) { g[0].ReadResources = []string{" "} },
		func(g []SourceGrant) { g[1].Alias = g[0].Alias },
	} {
		grants := sourceRequest("").Sources
		change(grants)
		require.False(t, ValidSourceGrants(grants))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	_, err := ExecuteWithSources(ctx, sourceRequest(`return {data:sources.get("boiler").read("opcua:temperature"),drop:false};`), nil)
	require.ErrorIs(t, err, ErrTimeout)
}

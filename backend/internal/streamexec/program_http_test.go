package streamexec

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProgramHTTPSourceCallsAndRequestEnvelope(t *testing.T) {
	code := `module.exports=api=>{
 const source=api.sources.use({id:"9007199254740993",resources:[]});
 api.stream.onRequest({id:"webhook",source,method:"POST",path:"/orders/:orderId"},async ctx=>{
  const parameters=await source.parameters({});
  const secrets=await source.secrets();
  const response=await source.request({method:"POST",path:"/orders",json:{id:ctx.request.params.orderId},headers:{"X-Signature":secrets.signature}});
  return {prefix:parameters.prefix,response,request:ctx.request,inputMatches:ctx.request===ctx.input};
 });
 api.stream.onRequest({id:"legacy"},ctx=>ctx.input);
};`
	req := programRequest(code, "webhook")
	req.CheckOnly = true
	compiled, err := ExecuteProgram(context.Background(), req, func(context.Context, ProgramCall) (json.RawMessage, error) {
		t.Fatal("registration must not execute host calls")
		return nil, nil
	})
	require.NoError(t, err)
	require.Equal(t, []ProgramEntry{{ID: "webhook", Kind: "http-request", SourceID: "9007199254740993", Method: "POST", Path: "/orders/:orderId"}, {ID: "legacy", Kind: "request"}}, compiled.Manifest.Entries)
	require.Empty(t, compiled.Manifest.Sources[0].Resources)
	req.CheckOnly = false
	req.ExpectedManifest = &compiled.Manifest
	req.Context = json.RawMessage(`{"request":{"forged":true}}`)
	req.Input = json.RawMessage(`{"method":"POST","path":"/orders/a1","params":{"orderId":"a1"},"query":{},"headers":{},"body":{"value":7}}`)
	seen := []ProgramCall{}
	result, err := ExecuteProgram(context.Background(), req, func(_ context.Context, call ProgramCall) (json.RawMessage, error) {
		require.True(t, AllowsProgramCall(compiled.Manifest, call))
		require.Empty(t, call.ResourceRef)
		require.Empty(t, call.TopicID)
		require.Empty(t, call.Input)
		seen = append(seen, call)
		switch call.Kind {
		case "source.parameters":
			require.Empty(t, call.Options)
			return json.RawMessage(`{"prefix":"test"}`), nil
		case "source.secrets":
			require.Empty(t, call.Options)
			return json.RawMessage(`{"signature":"test-signature"}`), nil
		default:
			require.JSONEq(t, `{"method":"POST","path":"/orders","json":{"id":"a1"},"headers":{"X-Signature":"test-signature"}}`, string(call.Options))
			// Source snapshots larger than the request budget remain bounded by
			// the separate response/result budget through Promise settlement.
			return json.RawMessage(`{"status":201,"data":"` + strings.Repeat("x", 2*MaxJSONBytes) + `"}`), nil
		}
	})
	require.NoError(t, err)
	require.Len(t, seen, 3)
	require.Greater(t, len(result.Data), MaxJSONBytes)
	var actual map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result.Data, &actual))
	require.JSONEq(t, string(req.Input), string(actual["request"]))
	require.Equal(t, "true", string(actual["inputMatches"]))
	req.EntryID = "legacy"
	result, err = ExecuteProgram(context.Background(), req, nil)
	require.NoError(t, err)
	require.JSONEq(t, string(req.Input), string(result.Data))
	req.EntryID, req.Input = "webhook", json.RawMessage(`[]`)
	_, err = ExecuteProgram(context.Background(), req, nil)
	require.ErrorIs(t, err, ErrInput)
}

func TestProgramHTTPCallsRejectUnscopedAndInvalidShape(t *testing.T) {
	m := ProgramManifest{Sources: []ProgramSource{{ID: "1", Resources: []string{}}}}
	for _, kind := range []string{"source.parameters", "source.secrets", "source.request"} {
		call := ProgramCall{ID: 1, Kind: kind, SourceID: "1"}
		if kind == "source.request" {
			call.Options = json.RawMessage(`{"method":"GET","path":"/x"}`)
		}
		require.True(t, AllowsProgramCall(m, call))
		for _, change := range []func(*ProgramCall){
			func(c *ProgramCall) { c.SourceID = "2" },
			func(c *ProgramCall) { c.SourceID = "" },
			func(c *ProgramCall) { c.TopicID = "2" },
			func(c *ProgramCall) { c.ResourceRef = "http:/x" },
			func(c *ProgramCall) { c.Input = json.RawMessage(`{}`) },
			func(c *ProgramCall) { c.Options = json.RawMessage(`[]`) },
			func(c *ProgramCall) { c.Options = json.RawMessage(`null`) },
			func(c *ProgramCall) { c.ID = 0 },
		} {
			invalid := call
			change(&invalid)
			require.False(t, AllowsProgramCall(m, invalid))
		}
	}
	require.False(t, AllowsProgramCall(m, ProgramCall{ID: 1, Kind: "source.request", SourceID: "1"}))
	for _, kind := range []string{"source.parameters", "source.secrets"} {
		require.False(t, AllowsProgramCall(m, ProgramCall{ID: 1, Kind: kind, SourceID: "1", Options: json.RawMessage(`{"x":1}`)}))
	}
}

func TestProgramHTTPRegistrationAndCallBoundaries(t *testing.T) {
	for _, body := range []string{
		`source.parameters();`, `source.secrets();`, `source.request({method:"GET",path:"/"});`,
	} {
		req := programRequest(`module.exports=api=>{const source=api.sources.use({id:"1",resources:[]});`+body+`api.stream.onRequest({id:"r"},()=>1);};`, "")
		req.CheckOnly = true
		_, err := ExecuteProgram(context.Background(), req, func(context.Context, ProgramCall) (json.RawMessage, error) {
			t.Fatal("registration dispatched I/O")
			return nil, nil
		})
		require.ErrorIs(t, err, ErrExecution)
	}
	for _, body := range []string{
		`return source.parameters({x:1});`, `return source.secrets(null);`, `return source.parameters({},{});`,
		`return source.request();`, `return source.request([]);`, `return source.request("url");`,
		`return source.request({x:BigInt(1)});`, `return source.request({x:"a".repeat(1048577)});`,
	} {
		req := programRequest(`module.exports=api=>{const source=api.sources.use({id:"1",resources:[]});api.stream.onRequest({id:"r"},async()=>{`+body+`});};`, "r")
		_, err := ExecuteProgram(context.Background(), req, func(context.Context, ProgramCall) (json.RawMessage, error) {
			t.Fatal("invalid SDK call dispatched I/O")
			return nil, nil
		})
		require.ErrorIs(t, err, ErrExecution)
	}
}

func TestProgramHTTPRouteValidation(t *testing.T) {
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
		for _, path := range []string{"/", "/health", "/api/v1/orders/:orderId", "/objects/:id/events/:event_id", "/v1.0/a-b_~"} {
			require.True(t, ValidateHTTPRoute(method, path), method+" "+path)
		}
	}
	for _, path := range []string{"", "relative", "https://example.com/a", "//a", "/a/", "/a//b", "/a/.", "/../a", "/a?x=1", "/a#b", "/a%2fb", "/a/*", "/:x/:x", "/:1id", "/:id?", "/:a-b", "/中文", "/" + strings.Repeat("a", 2048)} {
		require.False(t, ValidateHTTPRoute("GET", path), path)
	}
	for _, method := range []string{"get", "TRACE", "CONNECT", "GET ", "", "*"} {
		require.False(t, ValidateHTTPRoute(method, "/"))
	}
	for _, options := range []string{
		`{id:"r",source,method:"GET",path:"/a%2Fb"}`, `{id:"r",source,method:"POST"}`, `{id:"r",method:"GET",path:"/a"}`,
		`{id:"r",sourceId:"1",method:"GET",path:"/a"}`, `{id:"r",source:{},method:"GET",path:"/a"}`,
		`{id:"r",source,method:"GET",path:"/a",intervalMs:1000}`, `{id:"r",source,method:"GET",path:"/a",resourceRefs:["x"]}`,
	} {
		req := programRequest(`module.exports=api=>{const source=api.sources.use({id:"1",resources:[]});api.stream.onRequest(`+options+`,()=>1);};`, "")
		req.CheckOnly = true
		_, err := ExecuteProgram(context.Background(), req, nil)
		require.ErrorIs(t, err, ErrExecution)
	}
}

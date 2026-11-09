package streamexec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"backend/internal/domain/common"
	"backend/internal/operationcontract"
	"github.com/stretchr/testify/require"
)

const operationDeclaration = `{code:"device.start",name:"Start",contractVersion:"1",inputSchema:{type:"object",required:["value"],properties:{value:{type:"number"}},additionalProperties:false},outputSchema:{type:"object",required:["accepted"],properties:{accepted:{type:"boolean"}},additionalProperties:false},sideEffect:"write",idempotency:"required",timeoutMs:100}`

const requestDeclaration = `{code:"device.read",name:"Read",contractVersion:"1",inputSchema:{},outputSchema:{},sideEffect:"read",idempotency:"optional",timeoutMs:30000}`

func TestProgramImplicitRequestFrozenIdentity(t *testing.T) {
	code := `module.exports=api=>{api.stream.onRequest({id:"plain"},ctx=>ctx.input);api.stream.onRequest({id:"explicit",request:` + requestDeclaration + `},()=>null);api.stream.onRequest({id:"legacy",control:` + operationDeclaration + `},()=>null);api.stream.onTimer({id:"timer",intervalMs:1000},()=>null);};`
	request := programRequest(code, "plain")
	request.CheckOnly = true
	registered, err := ExecuteProgram(context.Background(), request, nil)
	require.NoError(t, err)
	frozen, err := WithImplicitRequests(registered.Manifest, 101, request.TimeoutMs)
	require.NoError(t, err)
	require.Nil(t, registered.Manifest.Entries[0].Request, "generation must not mutate registration")
	require.Equal(t, registered.Manifest.Entries[1:], frozen.Entries[1:], "explicit/legacy and timer entries keep their contracts")
	definition := frozen.Entries[0].Request
	require.Equal(t, "write", definition.SideEffect)
	require.Equal(t, "required", definition.Idempotency)
	other, err := WithImplicitRequests(registered.Manifest, 102, request.TimeoutMs)
	require.NoError(t, err)
	require.NotEqual(t, definition.Code, other.Entries[0].Request.Code)
	again, err := WithImplicitRequests(registered.Manifest, 101, request.TimeoutMs)
	require.NoError(t, err)
	require.Equal(t, frozen, again)
	require.NoError(t, ValidateImplicitRequests(frozen, 101, request.TimeoutMs))
	require.Error(t, ValidateImplicitRequests(frozen, 102, request.TimeoutMs))
	request.CheckOnly, request.ExpectedManifest = false, &frozen
	result, err := ExecuteProgram(context.Background(), request, nil)
	require.NoError(t, err)
	require.Equal(t, frozen, result.Manifest)
	require.JSONEq(t, string(request.Input), string(result.Data))
	request.ExpectedManifest = &registered.Manifest
	_, err = ExecuteProgram(context.Background(), request, nil)
	require.NoError(t, err, "old releases are not implicitly upgraded")
	for _, change := range []func(*operationcontract.Resource){
		func(r *operationcontract.Resource) { r.Name = "forged" },
		func(r *operationcontract.Resource) { r.SideEffect = "read" },
		func(r *operationcontract.Resource) { r.Idempotency = "optional" },
		func(r *operationcontract.Resource) { r.TimeoutMs++ },
		func(r *operationcontract.Resource) { r.InputSchema = json.RawMessage(`{"type":"object"}`) },
		func(r *operationcontract.Resource) { r.Code = "stream." + strings.Repeat("z", 32) },
	} {
		mutated := *definition
		change(&mutated)
		bad := frozen
		bad.Entries = append([]ProgramEntry(nil), frozen.Entries...)
		bad.Entries[0].Request = &mutated
		request.ExpectedManifest = &bad
		_, err = ExecuteProgram(context.Background(), request, nil)
		require.Error(t, err, "altered generated contract must fail before execution")
	}
	raw, err := json.Marshal(definition)
	require.NoError(t, err)
	request = programRequest(`module.exports=api=>api.stream.onRequest({id:"plain",request:`+string(raw)+`},()=>null);`, "plain")
	request.CheckOnly = true
	_, err = ExecuteProgram(context.Background(), request, nil)
	require.Error(t, err, "scripts cannot declare the reserved contract version")
	long, err := ImplicitRequestDefinition(101, strings.Repeat("a", 100), 60000)
	require.NoError(t, err)
	require.Len(t, []rune(long.Name), common.NameMaxLen)
	require.Equal(t, operationcontract.MaxRequestTimeoutMs, long.TimeoutMs)
	unicodeName, err := ImplicitRequestDefinition(101, strings.Repeat("中", 40), 1000)
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("中", 40), unicodeName.Name, "name limit counts Unicode characters, not bytes")
	_, err = ImplicitRequestDefinition(101, strings.Repeat("a", common.NameMaxLen-1)+" rest", 1000)
	require.NoError(t, err, "truncation must not leave an invalid trailing space")
	void := programRequest(`module.exports=api=>api.stream.onRequest({id:"plain"},()=>{});`, "plain")
	void.CheckOnly = true
	registered, err = ExecuteProgram(context.Background(), void, nil)
	require.NoError(t, err)
	void.CheckOnly, void.ExpectedManifest = false, &registered.Manifest
	_, err = ExecuteProgram(context.Background(), void, nil)
	require.NoError(t, err, "pre-migration release may return undefined")
	frozen, err = WithImplicitRequests(registered.Manifest, 101, void.TimeoutMs)
	require.NoError(t, err)
	void.ExpectedManifest = &frozen
	_, err = ExecuteProgram(context.Background(), void, nil)
	require.ErrorIs(t, err, ErrOutput, "new request contracts require a JSON result, including explicit null")
	void.Code = strings.Replace(void.Code, "()=>{}", "()=>null", 1)
	_, err = ExecuteProgram(context.Background(), void, nil)
	require.NoError(t, err)
}

func TestProgramRequestContractAndReadOnlyEffects(t *testing.T) {
	prefix := `module.exports=api=>{const source=api.sources.use({id:"1",resources:["a"]});const topic=api.uns.use({topicId:"2"});`
	code := prefix + `api.stream.onRequest({id:"read",request:` + requestDeclaration + `},async ctx=>await source.read("a"));};`
	request := programRequest(code, "read")
	request.CheckOnly = true
	frozen, err := ExecuteProgram(context.Background(), request, nil)
	require.NoError(t, err)
	require.Nil(t, frozen.Manifest.Entries[0].Control)
	require.Equal(t, "optional", frozen.Manifest.Entries[0].Request.Idempotency)
	request.CheckOnly, request.ExpectedManifest = false, &frozen.Manifest
	result, err := ExecuteProgram(context.Background(), request, func(_ context.Context, call ProgramCall) (json.RawMessage, error) {
		require.Equal(t, "source.read", call.Kind)
		return json.RawMessage(`{"value":42}`), nil
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"value":42}`, string(result.Data))
	for _, effect := range []string{
		`await source.write("a",1)`, `await source.invoke("a",{})`, `await source.publish("a",{})`,
		`await source.request({method:"POST",path:"/write",json:{}})`,
		`await topic.write({value:1})`, `await api.stream.checkpoint.set({value:1})`,
	} {
		request := programRequest(prefix+`api.stream.onRequest({id:"read",request:`+requestDeclaration+`},async ()=>{try{`+effect+`;}catch(e){}return true;});};`, "read")
		_, err := ExecuteProgram(context.Background(), request, func(context.Context, ProgramCall) (json.RawMessage, error) {
			t.Fatal("read-only request dispatched a side effect")
			return nil, nil
		})
		require.Error(t, err, effect+" cannot be hidden by script catch")
	}
	for _, body := range []string{
		`api.stream.onRequest({id:"read",request:` + requestDeclaration + `,control:` + operationDeclaration + `},()=>null);`,
		`api.stream.onTimer({id:"read",intervalMs:1000,request:` + requestDeclaration + `},()=>null);`,
		`api.stream.onRequest({id:"read",request:` + requestDeclaration + `},()=>null);api.stream.onRequest({id:"duplicate",request:` + requestDeclaration + `},()=>null);`,
	} {
		request := programRequest(`module.exports=api=>{`+body+`};`, "")
		request.CheckOnly = true
		_, err := ExecuteProgram(context.Background(), request, nil)
		require.Error(t, err)
	}
	require.ErrorIs(t, frozen.Manifest.ValidateRequestEffects("read", ProgramResult{Outputs: []ProgramOutput{{TopicID: "2", Data: json.RawMessage(`{}`)}}}), ErrOutput)
	require.ErrorIs(t, frozen.Manifest.ValidateRequestEffects("read", ProgramResult{Checkpoint: &ProgramCheckpoint{}}), ErrOutput)
}

func TestProgramOperationMultipleEntriesAndInputOutput(t *testing.T) {
	code := fmt.Sprintf(`module.exports=api=>{
	 const device=api.sources.use({id:"1",resources:["start"]});
	 api.stream.onRequest({id:"start",control:%s},async context=>await device.invoke("start",context.input));
	 api.stream.onRequest({id:"status",control:%s},()=>({accepted:false}));
	 api.stream.onRequest({id:"legacy"},()=>42);
	};`, operationDeclaration, strings.Replace(operationDeclaration, "device.start", "device.status", 1))
	request := programRequest(code, "start")
	request.CheckOnly = true
	compiled, err := ExecuteProgram(context.Background(), request, nil)
	require.NoError(t, err)
	require.Len(t, compiled.Manifest.Entries, 3)
	request.CheckOnly, request.ExpectedManifest = false, &compiled.Manifest
	calls := 0
	host := func(_ context.Context, call ProgramCall) (json.RawMessage, error) {
		calls++
		require.Equal(t, "source.invoke", call.Kind)
		return json.RawMessage(`{"accepted":true}`), nil
	}
	result, err := ExecuteProgram(context.Background(), request, host)
	require.NoError(t, err)
	require.JSONEq(t, `{"accepted":true}`, string(result.Data))
	request.Input = json.RawMessage(`{"value":"wrong"}`)
	_, err = ExecuteProgram(context.Background(), request, host)
	require.ErrorIs(t, err, ErrInput)
	require.Equal(t, 1, calls, "invalid input cannot reach the Source")
	request.Input, request.EntryID = json.RawMessage(`{"value":1}`), "status"
	result, err = ExecuteProgram(context.Background(), request, host)
	require.NoError(t, err)
	require.JSONEq(t, `{"accepted":false}`, string(result.Data))
	require.Equal(t, 1, calls)
	request.EntryID = "legacy"
	result, err = ExecuteProgram(context.Background(), request, host)
	require.NoError(t, err)
	require.Equal(t, "42", string(result.Data))
}

func TestProgramOperationRegistrationRejectsInvalidContracts(t *testing.T) {
	for _, body := range []string{
		fmt.Sprintf(`api.stream.onRequest({id:"a",control:%s},()=>null);api.stream.onRequest({id:"b",control:%s},()=>null);`, operationDeclaration, operationDeclaration),
		fmt.Sprintf(`api.stream.every({id:"a",intervalMs:1000,control:%s},()=>null);`, operationDeclaration),
		fmt.Sprintf(`api.stream.onRequest({id:"a",control:%s},()=>null);`, strings.Replace(operationDeclaration, `timeoutMs:100`, `timeoutMs:6000`, 1)),
		fmt.Sprintf(`api.stream.onRequest({id:"a",control:%s},()=>null);`, strings.Replace(operationDeclaration, `type:"object",required:["value"]`, `type:"object",format:"remote",required:["value"]`, 1)),
	} {
		request := programRequest(`module.exports=api=>{`+body+`};`, "")
		request.CheckOnly = true
		_, err := ExecuteProgram(context.Background(), request, nil)
		require.Error(t, err)
	}
}

func TestProgramOperationOutputContractAndDeadline(t *testing.T) {
	for _, tc := range []struct {
		handler  string
		expected error
	}{
		{`()=>({accepted:"not boolean"})`, ErrOutput},
		{`()=>{while(true){}}`, ErrTimeout},
	} {
		request := programRequest(fmt.Sprintf(`module.exports=api=>{api.stream.onRequest({id:"r",control:%s},%s);};`, operationDeclaration, tc.handler), "r")
		request.TimeoutMs = 1000
		_, err := ExecuteProgram(context.Background(), request, nil)
		require.ErrorIs(t, err, tc.expected)
	}
}

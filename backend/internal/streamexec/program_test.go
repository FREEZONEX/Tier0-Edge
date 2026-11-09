// 完整程序测试：同步注册、多个入口、异步宿主、缓冲输出及限制；不连接真实设备。
package streamexec

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func programRequest(code, entry string) ProgramRequest {
	return ProgramRequest{Code: code, EntryID: entry, Input: json.RawMessage(`{"value":7}`), Context: json.RawMessage(`{"runId":"run-test"}`), TimeoutMs: 1000}
}

func TestProgramTimeoutIndependentOfLegacyLimit(t *testing.T) {
	request := programRequest(`module.exports=api=>{api.stream.onRequest({id:"r"},()=>1);};`, "r")
	for _, timeout := range []int{1, MaxProgramTimeoutMs} {
		request.TimeoutMs, request.CheckOnly = timeout, true
		_, err := ExecuteProgram(context.Background(), request, nil)
		if timeout == 1 {
			require.True(t, err == nil || errors.Is(err, ErrTimeout), "1ms is valid, but need not finish under host load")
		} else {
			require.NoError(t, err)
		}
	}
	for _, timeout := range []int{0, -1, MaxProgramTimeoutMs + 1} {
		request.TimeoutMs = timeout
		_, err := ExecuteProgram(context.Background(), request, nil)
		require.ErrorIs(t, err, ErrInput)
	}
	_, err := Execute(context.Background(), Request{Code: `return {data:1,drop:false};`, Input: json.RawMessage(`null`), Context: json.RawMessage(`{}`), TimeoutMs: MaxTimeoutMs})
	require.NoError(t, err, "the independent legacy script limit remains supported")
}

func TestProgram_RegisterAndRunMultipleEntries(t *testing.T) {
	code := `module.exports = function(api) {
	 const device=api.sources.use({id:"9223372036854775807",resources:["opcua:temperature","opcua:reset"]});
	 const topic=api.uns.use({topicId:"9007199254740993"});
	 api.stream.every({id:"sample",intervalMs:1000},async function(context) {
	  const sample=await device.read("opcua:temperature");
	  const receipt=await topic.write({value:sample.value});
	  api.log.info(receipt.stage);
	 });
	 api.stream.onSource({id:"watch",source:device,resourceRefs:["opcua:temperature"]},async function(context) { await topic.write(context.input); });
	 api.stream.onTopic({id:"echo",topics:[topic]},function(context) { return context.input; });
	 api.stream.onRequest({id:"reset"},async function(context) { return await device.invoke("opcua:reset",context.input); });
	};`
	request := programRequest(code, "sample")
	request.CheckOnly = true
	manifest, err := ExecuteProgram(context.Background(), request, func(context.Context, ProgramCall) (json.RawMessage, error) {
		t.Fatal("registration performed I/O")
		return nil, nil
	})
	require.NoError(t, err)
	require.Len(t, manifest.Manifest.Entries, 4)
	require.NotNil(t, manifest.Manifest.Entries)
	request.CheckOnly = false
	request.ExpectedManifest = &manifest.Manifest
	calls := []ProgramCall{}
	host := func(ctx context.Context, call ProgramCall) (json.RawMessage, error) {
		calls = append(calls, call)
		require.Equal(t, "9223372036854775807", call.SourceID)
		if call.Kind == "source.read" {
			return json.RawMessage(`{"value":42}`), nil
		}
		return json.RawMessage(`{"receipt":"operation-accepted","deviceExecuted":false}`), nil
	}
	result, err := ExecuteProgram(context.Background(), request, host)
	require.NoError(t, err)
	require.Equal(t, "source.read", calls[0].Kind)
	require.Equal(t, "9007199254740993", result.Outputs[0].TopicID)
	require.JSONEq(t, `{"value":42}`, string(result.Outputs[0].Data))
	require.Equal(t, "buffered", result.Logs[0].Message)
	request.EntryID = "reset"
	result, err = ExecuteProgram(context.Background(), request, host)
	require.NoError(t, err)
	require.JSONEq(t, `{"receipt":"operation-accepted","deviceExecuted":false}`, string(result.Data))
	require.Equal(t, "source.invoke", calls[1].Kind)
}

func TestProgramCloudEntryNamesPreserveFrozenManifest(t *testing.T) {
	legacy := `module.exports=api=>{
	 const source=api.sources.use({id:"1",resources:["mqtt:plant/temperature"]});
	 const topic=api.uns.use({topicId:"2"});
	 api.stream.every({id:"timer",intervalMs:1000},ctx=>ctx.input);
	 api.stream.onSource({id:"message",source,resourceRefs:["mqtt:plant/temperature"],qos:1},async ctx=>{await topic.write(ctx.input);return ctx.input;});
	 api.stream.onTopic({id:"event",topics:[topic]},ctx=>ctx.input);
	};`
	cloud := strings.NewReplacer(".every(", ".onTimer(", ".onSource(", ".onMessage(", ".onTopic(", ".onEvent(").Replace(legacy)
	request := programRequest(legacy, "")
	request.CheckOnly = true
	frozen, err := ExecuteProgram(context.Background(), request, nil)
	require.NoError(t, err)
	request.Code = cloud
	compiled, err := ExecuteProgram(context.Background(), request, nil)
	require.NoError(t, err)
	require.Equal(t, frozen.Manifest, compiled.Manifest)
	request.CheckOnly = false
	request.ExpectedManifest = &frozen.Manifest
	for _, entry := range []string{"timer", "message", "event"} {
		request.EntryID = entry
		result, err := ExecuteProgram(context.Background(), request, nil)
		require.NoError(t, err)
		require.JSONEq(t, string(request.Input), string(result.Data))
		if entry == "message" {
			require.Len(t, result.Outputs, 1)
			require.Equal(t, "2", result.Outputs[0].TopicID)
			require.JSONEq(t, string(request.Input), string(result.Outputs[0].Data))
		}
	}
	for _, body := range []string{
		`const source=api.sources.use({id:"1",resources:["allowed"]});api.stream.onMessage({id:"m",source,resourceRefs:["denied"]},()=>null);`,
		`api.stream.onMessage({id:"m",source:{id:"1"},resourceRefs:["allowed"]},()=>null);`,
		`const topic=api.uns.use({topicId:"device",access:"write"});api.stream.onEvent({id:"e",topics:[topic]},()=>null);`,
		`api.stream.onTimer({id:"t",intervalMs:1},()=>null);`,
		`api.stream.onTimer({id:"t",intervalMs:1000},()=>null,{});`,
		`api.stream.onTimer({id:"t",intervalMs:1000},()=>null);api.stream.every({id:"t",intervalMs:1000},()=>null);`,
	} {
		request := programRequest(`module.exports=api=>{`+body+`};`, "")
		request.CheckOnly = true
		_, err := ExecuteProgram(context.Background(), request, nil)
		require.Error(t, err, body)
	}
}

func TestProgram_ParallelPromisesAndUNSRead(t *testing.T) {
	request := programRequest(`module.exports = function(api) {
	 const source=api.sources.use({id:"1",resources:["a","b"]});
	 const topic=api.uns.use({topicId:"2"});
	 api.stream.onRequest({id:"read"},async function(context) {
	  const [a,b,current]=await Promise.all([source.read("a"),source.read("b"),topic.read()]);
	  return {a:a.value,b:b.value,current:current.value};
	 });
	};`, "read")
	var lock sync.Mutex
	seen := map[int]bool{}
	result, err := ExecuteProgram(context.Background(), request, func(ctx context.Context, call ProgramCall) (json.RawMessage, error) {
		lock.Lock()
		seen[call.ID] = true
		lock.Unlock()
		if call.ResourceRef == "a" {
			time.Sleep(20 * time.Millisecond)
		}
		return json.RawMessage(`{"value":13}`), nil
	})
	require.NoError(t, err)
	require.Len(t, seen, 3)
	require.JSONEq(t, `{"a":13,"b":13,"current":13}`, string(result.Data))
}

func TestProgram_RepeatedResourceReferencesRemainBounded(t *testing.T) {
	code := `module.exports=api=>{
	 const first=api.sources.use({id:"1",resources:["a"]});
	 const second=api.sources.use({id:"1",resources:["b","a"]});
	 const empty=api.sources.use({id:"1",resources:[]});
	 const topic=api.uns.use({topicId:"2"});
	 const sameTopic=api.uns.use({topicId:"2"});
	 api.stream.onTopic({id:"event",topics:[sameTopic]},ctx=>ctx.input);
	 api.stream.onRequest({id:"read"},async ctx=>{
	  const a=await first.read("a"),b=await second.read("b");
	  await topic.write(a);await sameTopic.write(b);
	  return {sameTopic:topic===sameTopic,a,b};
	 });
	 api.stream.onRequest({id:"denied"},ctx=>first.read("b"));
	 api.stream.onRequest({id:"empty"},ctx=>empty.read("a"));
	};`
	request := programRequest(code, "read")
	request.CheckOnly = true
	compiled, err := ExecuteProgram(context.Background(), request, nil)
	require.NoError(t, err)
	require.Equal(t, []ProgramSource{{ID: "1", Resources: []string{"a", "b"}}}, compiled.Manifest.Sources)
	require.Equal(t, []ProgramTopic{{TopicID: "2"}}, compiled.Manifest.Topics)
	request.CheckOnly = false
	request.ExpectedManifest = &compiled.Manifest
	calls := 0
	host := func(_ context.Context, call ProgramCall) (json.RawMessage, error) {
		calls++
		require.True(t, AllowsProgramCall(compiled.Manifest, call))
		return json.RawMessage(`{"value":42}`), nil
	}
	result, err := ExecuteProgram(context.Background(), request, host)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	require.Len(t, result.Outputs, 2)
	require.JSONEq(t, `{"sameTopic":true,"a":{"value":42},"b":{"value":42}}`, string(result.Data))
	for _, entry := range []string{"denied", "empty"} {
		request.EntryID = entry
		_, err = ExecuteProgram(context.Background(), request, host)
		require.ErrorIs(t, err, ErrExecution)
		require.Equal(t, 2, calls, "a handle must not inherit another handle's resource scope")
	}

	for _, body := range []string{
		`for(let i=0;i<1001;i++)api.sources.use({id:"1",resources:[]});`,
		`api.sources.use({id:"1",resources:Array.from({length:1000},(_,i)=>"a"+i)});api.sources.use({id:"1",resources:["overflow"]});`,
	} {
		limited := programRequest(`module.exports=api=>{`+body+`api.stream.onRequest({id:"r"},()=>1);};`, "")
		limited.CheckOnly = true
		_, err = ExecuteProgram(context.Background(), limited, nil)
		require.Error(t, err)
	}
}

func TestProgram_RejectInvalidRegistration(t *testing.T) {
	cases := []string{
		`module.exports=async function(api){api.stream.onRequest({id:"r"},()=>1);};`,
		`module.exports=function(api){Date.now();api.stream.onRequest({id:"r"},()=>1);};`,
		`module.exports=function(api){Math.random();api.stream.onRequest({id:"r"},()=>1);};`,
		`module.exports=function(api){api.stream.onRequest({id:"r"},()=>1);api.stream.onRequest({id:"r"},()=>2);};`,
		`module.exports=function(api){const s=api.sources.use({id:"1",resources:["a"]});s.read("a");};`,
		`module.exports=function(api){api.sources.use({id:1,resources:["a"]});api.stream.onRequest({id:"r"},()=>1);};`,
		`module.exports=function(api){api.sources.use({id:"1"});api.stream.onRequest({id:"r"},()=>1);};`,
		`module.exports=function(api){api.sources.use({id:"1",resources:null});api.stream.onRequest({id:"r"},()=>1);};`,
		`module.exports=function(api){api.stream.onTopic({id:"t",topicIds:["404"]},()=>1);};`,
		`module.exports=function(api){api.stream.every({id:"t",intervalMs:1},()=>1);};`,
		`module.exports=function(api){api.stream.onRequest({id:"r",unknown:1},()=>1);};`,
		`require("fs");module.exports=function(api){api.stream.onRequest({id:"r"},()=>1);};`,
		`export default function() {};`,
	}
	for _, code := range cases {
		t.Run(code, func(t *testing.T) {
			request := programRequest(code, "")
			request.CheckOnly = true
			_, err := ExecuteProgram(context.Background(), request, nil)
			require.Error(t, err)
		})
	}
}

func TestProgram_EmptySourceGrantCannotPerformIO(t *testing.T) {
	request := programRequest(`module.exports=function(api){const s=api.sources.use({id:"1",resources:[]});api.stream.onRequest({id:"r"},()=>42);};`, "r")
	result, err := ExecuteProgram(context.Background(), request, nil)
	require.NoError(t, err)
	require.Len(t, result.Manifest.Sources, 1)
	require.NotNil(t, result.Manifest.Sources[0].Resources)
	require.Empty(t, result.Manifest.Sources[0].Resources)
	request.Code = `module.exports=function(api){const s=api.sources.use({id:"1",resources:[]});api.stream.onRequest({id:"r"},()=>s.read("any"));};`
	_, err = ExecuteProgram(context.Background(), request, func(context.Context, ProgramCall) (json.RawMessage, error) {
		t.Fatal("empty resource grant performed I/O")
		return nil, nil
	})
	require.ErrorIs(t, err, ErrExecution)
}

func TestProgram_FreshRuntimeManifestAndContext(t *testing.T) {
	code := `let count=0;module.exports=function(api){api.stream.onRequest({id:"r"},function(context){count++;return {count,runId:context.runId,input:context.input};});};`
	request := programRequest(code, "r")
	for i := 0; i < 2; i++ {
		result, err := ExecuteProgram(context.Background(), request, nil)
		require.NoError(t, err)
		require.JSONEq(t, `{"count":1,"runId":"run-test","input":{"value":7}}`, string(result.Data))
	}
	request.ExpectedManifest = &ProgramManifest{}
	_, err := ExecuteProgram(context.Background(), request, nil)
	require.ErrorIs(t, err, ErrInput)
	request = programRequest(`module.exports=function(api){api.stream.onRequest({id:"r"},function(context){context.runId="forged";});};`, "r")
	_, err = ExecuteProgram(context.Background(), request, nil)
	require.ErrorIs(t, err, ErrExecution)
}

func TestProgram_CancellationRejectionAndLimits(t *testing.T) {
	t.Run("infinite loop", func(t *testing.T) {
		request := programRequest(`module.exports=function(api){api.stream.onRequest({id:"r"},async()=>{while(true){}});};`, "r")
		request.TimeoutMs = 20
		_, err := ExecuteProgram(context.Background(), request, nil)
		require.ErrorIs(t, err, ErrTimeout)
	})
	t.Run("pending host", func(t *testing.T) {
		request := programRequest(`module.exports=function(api){const s=api.sources.use({id:"1",resources:["a"]});api.stream.onRequest({id:"r"},async()=>await s.read("a"));};`, "r")
		request.TimeoutMs = 20
		done := make(chan struct{})
		_, err := ExecuteProgram(context.Background(), request, func(ctx context.Context, _ ProgramCall) (json.RawMessage, error) {
			<-ctx.Done()
			close(done)
			return nil, ctx.Err()
		})
		require.ErrorIs(t, err, ErrTimeout)
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("host context not canceled")
		}
	})
	t.Run("caught and unhandled rejection", func(t *testing.T) {
		for _, caught := range []bool{false, true} {
			body := `s.read("a");return 1;`
			if caught {
				body = `try {await s.read("a");} catch(e) {return "caught";}`
			}
			request := programRequest(`module.exports=function(api){const s=api.sources.use({id:"1",resources:["a"]});api.stream.onRequest({id:"r"},async()=>{`+body+`});};`, "r")
			result, err := ExecuteProgram(context.Background(), request, func(context.Context, ProgramCall) (json.RawMessage, error) {
				return nil, errors.New("secret credential detail")
			})
			if caught {
				require.NoError(t, err)
				require.JSONEq(t, `"caught"`, string(result.Data))
			} else {
				require.ErrorIs(t, err, ErrExecution)
			}
		}
	})
	t.Run("undeclared resource", func(t *testing.T) {
		request := programRequest(`module.exports=function(api){const s=api.sources.use({id:"1",resources:["a"]});api.stream.onRequest({id:"r"},()=>s.read("b"));};`, "r")
		_, err := ExecuteProgram(context.Background(), request, nil)
		require.Error(t, err)
	})
	t.Run("oversized logs", func(t *testing.T) {
		request := programRequest(`module.exports=function(api){api.stream.onRequest({id:"r"},()=>api.log.info("x".repeat(2049)));};`, "r")
		_, err := ExecuteProgram(context.Background(), request, nil)
		require.Error(t, err)
	})
	t.Run("oversized code", func(t *testing.T) {
		_, err := ExecuteProgram(context.Background(), programRequest(strings.Repeat(" ", MaxCodeBytes+1), "r"), nil)
		require.ErrorIs(t, err, ErrInput)
	})
}

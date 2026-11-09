package streamexec

// 验证转换、过滤、请求/响应脚本契约与耗时限制，不接入任何设备或存储。
import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExecuteTransform(t *testing.T) {
	request := Request{Code: `return { data: {value: Number(input.value), source: context.sourceId}, drop: false, logs: [{level:"info",message:"normalized"}] };`, Input: json.RawMessage(`{"value":"42"}`), Context: json.RawMessage(`{"sourceId":10}`), TimeoutMs: 1000}
	result, err := Execute(context.Background(), request)
	require.NoError(t, err)
	require.JSONEq(t, `{"value":42,"source":10}`, string(result.Data))
	require.Len(t, result.Logs, 1)
	request.Code = `return {data: msg.payload, drop: true, logs: []};`
	result, err = Execute(context.Background(), request)
	require.NoError(t, err)
	require.True(t, result.Drop)
	request.Code = `return {data: [typeof require, typeof process, typeof fetch, typeof NATS, typeof database], drop:false};`
	result, err = Execute(context.Background(), request)
	require.NoError(t, err)
	require.JSONEq(t, `["undefined","undefined","undefined","undefined","undefined"]`, string(result.Data))
	request.Code = `input.changed = true; return {data: input, drop:false};`
	_, err = Execute(context.Background(), request)
	require.NoError(t, err)
	require.JSONEq(t, `{"value":"42"}`, string(request.Input), "only JSON copies cross execution boundary")
}

// 多目标使用独立数据，明确禁止未知结构、重复目标和广播混用。
func TestExecuteDirectedOutputs(t *testing.T) {
	request := Request{Code: `return {outputs:[{topicId:11,data:{temperature:input.value}},{topicId:12,data:{pressure:input.value/10}}],drop:false,logs:[]};`, Input: json.RawMessage(`{"value":42}`), TimeoutMs: 1000}
	result, err := Execute(context.Background(), request)
	require.NoError(t, err)
	require.Nil(t, result.Data)
	require.Len(t, result.Outputs, 2)
	require.EqualValues(t, 11, result.Outputs[0].TopicID)
	require.JSONEq(t, `{"temperature":42}`, string(result.Outputs[0].Data))
	require.JSONEq(t, `{"pressure":4.2}`, string(result.Outputs[1].Data))
	request.Code = `return {outputs:[],drop:false};`
	result, err = Execute(context.Background(), request)
	require.NoError(t, err)
	require.NotNil(t, result.Outputs)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"outputs":[]`, "empty selection must survive the executor IPC boundary")
	for _, code := range []string{
		`return {outputs:null,drop:false};`,
		`return {outputs:{},drop:false};`,
		`return {outputs:[],data:{},drop:false};`,
		`return {outputs:[{topicId:11}],drop:false};`,
		`return {outputs:[{topicId:11,data:{},typo:true}],drop:false};`,
		`return {outputs:[{topicId:11,data:{}},{topicId:11,data:{}}],drop:false};`,
		`return {outputs:[{topicId:-1,data:{}}],drop:false};`,
		`return {outputs:[{topicId:1.5,data:{}}],drop:false};`,
		`return {outputs:[{topicId:9007199254740992,data:{}}],drop:false};`,
		`return {outputs:[{topicId:"11",data:{}}],drop:false};`,
		`return {outputs:[{topicId:11,data:{}}],drop:true};`,
	} {
		request.Code = code
		_, err = Execute(context.Background(), request)
		require.ErrorIs(t, err, ErrOutput, code)
	}
}
func TestExecuteFailures(t *testing.T) {
	for _, tt := range []struct {
		code string
		err  error
	}{
		{`return {`, ErrSyntax}, {`throw new Error("private payload");`, ErrExecution}, {`while (true) {}`, ErrTimeout},
		{`return null;`, ErrOutput}, {`return [];`, ErrOutput}, {`return {data:{}};`, ErrOutput}, {`return {data:{},drop:null};`, ErrOutput},
		{`return {data:{},drop:"false"};`, ErrOutput}, {`return {data:{},drop:false,logs:[{level:"unknown",message:"x"}]};`, ErrOutput},
		{`return {data: "x".repeat(1024*1024),drop:false};`, ErrOutput}, {`const x={}; x.self=x; return {data:x,drop:false};`, ErrExecution},
		{`return {data:{},drop:false,logs:[{level:"info",message:"x".repeat(2049)}]};`, ErrOutput},
	} {
		t.Run(tt.err.Error()+tt.code[:min(15, len(tt.code))], func(t *testing.T) {
			start := time.Now()
			timeoutMs := 1000
			if errors.Is(tt.err, ErrTimeout) {
				timeoutMs = 30
			}
			_, err := Execute(context.Background(), Request{Code: tt.code, Input: json.RawMessage(`{}`), TimeoutMs: timeoutMs})
			require.ErrorIs(t, err, tt.err)
			require.Less(t, time.Since(start), time.Second)
		})
	}
	for _, request := range []Request{{}, {Code: "x", TimeoutMs: 1000, Input: json.RawMessage("broken")}, {Code: strings.Repeat("x", MaxCodeBytes+1), TimeoutMs: 1000}, {Code: "x", TimeoutMs: MaxTimeoutMs + 1}} {
		_, err := Execute(context.Background(), request)
		require.ErrorIs(t, err, ErrInput)
	}
	_, err := Execute(context.Background(), Request{Code: `return {data:input,drop:false};`, CheckOnly: true, TimeoutMs: 1000})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = Execute(ctx, Request{Code: `while(true){}`, Input: json.RawMessage(`{}`), TimeoutMs: 1000})
	require.True(t, errors.Is(err, ErrTimeout))
}

func TestExecuteSourceSDK(t *testing.T) {
	calls := 0
	reader := func(ctx context.Context, alias string, raw json.RawMessage) (json.RawMessage, error) {
		calls++
		require.Contains(t, []string{"first", "second"}, alias)
		require.JSONEq(t, `{"topic":"line/temperature"}`, string(raw))
		return json.RawMessage(`{"value":21}`), nil
	}
	request := Request{Code: `const a = await sources.get("first").read({topic:"line/temperature"}); const b = await sources.get("second").read({topic:"line/temperature"}); return {data:{value:a.value+b.value,network:typeof fetch},drop:false};`, Input: json.RawMessage(`{}`), TimeoutMs: 1000, LegacySources: []string{"first", "second"}}
	result, err := ExecuteWithLegacySources(context.Background(), request, reader)
	require.NoError(t, err)
	require.JSONEq(t, `{"value":42,"network":"undefined"}`, string(result.Data))
	require.Equal(t, 2, calls)
	request.Code = `await sources.get("unknown").read({}); return {data:{},drop:false};`
	_, err = ExecuteWithLegacySources(context.Background(), request, reader)
	require.Error(t, err)
	require.Equal(t, 2, calls)
	request.Code = `for(let n=0;n<17;n++) await sources.get("first").read({topic:"line/temperature"}); return {data:{},drop:false};`
	_, err = ExecuteWithLegacySources(context.Background(), request, reader)
	require.Error(t, err)
	require.Equal(t, 18, calls)
}

func TestInjectedSourceDeadlines(t *testing.T) {
	for _, parentTimeout := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		request := Request{Code: `await sources.get("device").read({topic:"test",timeoutMs:20}); return {data:{},drop:false};`, Input: json.RawMessage(`{}`), TimeoutMs: 1000, LegacySources: []string{"device"}}
		if parentTimeout {
			cancel()
			ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
			request.Code = `await sources.get("device").read({topic:"test",timeoutMs:5000}); return {data:{},drop:false};`
		}
		var observed context.Context
		started := time.Now()
		_, err := ExecuteWithLegacySources(ctx, request, func(callCtx context.Context, _ string, raw json.RawMessage) (json.RawMessage, error) {
			observed = callCtx
			require.JSONEq(t, `{"topic":"test"}`, string(raw))
			<-callCtx.Done()
			return nil, callCtx.Err()
		})
		cancel()
		require.Error(t, err)
		require.NotNil(t, observed)
		require.ErrorIs(t, observed.Err(), context.DeadlineExceeded)
		require.Less(t, time.Since(started), 500*time.Millisecond)
	}
}

func TestSourceReadWaitBudget(t *testing.T) {
	req := Request{Code: `const data=await sources.get("device").read({timeoutMs:20000});return {data,drop:false};`, Input: json.RawMessage(`{}`), TimeoutMs: MaxTimeoutMs, LegacySources: []string{"device"}}
	_, err := ExecuteWithLegacySources(context.Background(), req, func(ctx context.Context, _ string, raw json.RawMessage) (json.RawMessage, error) {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.Greater(t, time.Until(deadline), 19*time.Second)
		require.JSONEq(t, `{}`, string(raw))
		return json.RawMessage(`{"value":1}`), nil
	})
	require.NoError(t, err)
	req.Code = `await sources.get("device").read({timeoutMs:10});return {data:{},drop:false};`
	_, err = ExecuteWithLegacySources(context.Background(), req, func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	require.ErrorIs(t, err, ErrSourceReadTimeout)
	req.Code = `try { await sources.get("device").read({timeoutMs:10}); } catch {} throw new Error("other failure");`
	_, err = ExecuteWithLegacySources(context.Background(), req, func(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	require.ErrorIs(t, err, ErrExecution)
}

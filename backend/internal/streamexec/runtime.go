// Package streamexec 仅处理 JSON 和脚本，不持有协议、凭据或数据库客户端。
package streamexec

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/dop251/goja"
)

const MaxJSONBytes = 1024 * 1024
const MaxCodeBytes = 128 * 1024
const MaxTimeoutMs = 30000

type Request struct {
	Code          string          `json:"code"`
	Input         json.RawMessage `json:"input"`
	Context       json.RawMessage `json:"context"`
	TimeoutMs     int             `json:"timeoutMs"`
	CheckOnly     bool            `json:"checkOnly"`
	Sources       []SourceGrant   `json:"sources,omitempty"`
	LegacySources []string        `json:"legacySources,omitempty"`
}
type Log struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}
type Result struct {
	Data       json.RawMessage `json:"data,omitempty"`
	Outputs    []TopicOutput   `json:"outputs"`
	Drop       bool            `json:"drop"`
	Logs       []Log           `json:"logs"`
	DurationMs int64           `json:"durationMs"`
	Error      string          `json:"error,omitempty"`
}

type TopicOutput struct {
	TopicID int64           `json:"topicId"`
	Data    json.RawMessage `json:"data"`
}

var ErrInput = errors.New("SourceStream.scriptInput")
var ErrSyntax = errors.New("SourceStream.scriptSyntax")
var ErrExecution = errors.New("SourceStream.scriptExecution")
var ErrTimeout = errors.New("SourceStream.scriptTimeout")
var ErrOutput = errors.New("SourceStream.scriptOutput")

// Production calls this only in the isolated executor child. Context limits time;
// the separate executor container provides the hard memory boundary.
func Execute(parent context.Context, request Request) (Result, error) {
	return ExecuteWithSources(parent, request, nil)
}

// ExecuteWithSources 不开放任意 I/O，只通过本次执行的 SourceHost 请求只读资源。
func ExecuteWithSources(parent context.Context, request Request, host SourceHost) (Result, error) {
	return executeWithSources(parent, request, host, nil, false)
}
func ExecuteWithLegacySources(parent context.Context, request Request, read SourceReader) (Result, error) {
	return executeWithSources(parent, request, nil, read, true)
}
func executeWithSources(parent context.Context, request Request, host SourceHost, read SourceReader, legacy bool) (Result, error) {
	started := time.Now()
	result := Result{Logs: []Log{}}
	if len(request.Code) == 0 || len(request.Code) > MaxCodeBytes || request.TimeoutMs < 1 || request.TimeoutMs > MaxTimeoutMs || !ValidSourceGrants(request.Sources) {
		return result, ErrInput
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(request.TimeoutMs)*time.Millisecond)
	defer cancel()
	if ctx.Err() != nil {
		return result, ErrTimeout
	}
	program, err := goja.Compile("stream.js", "(async function(input, context) { 'use strict'; const msg = {payload: input};\n"+request.Code+"\n})", true)
	if err != nil {
		return result, ErrSyntax
	}
	if ctx.Err() != nil {
		return result, ErrTimeout
	}
	if request.CheckOnly {
		return result, nil
	}
	if len(request.Input) > MaxJSONBytes || len(request.Context) > MaxJSONBytes || !json.Valid(request.Input) {
		return result, ErrInput
	}
	if len(request.Context) == 0 {
		request.Context = json.RawMessage(`{}`)
	}
	if !json.Valid(request.Context) {
		return result, ErrInput
	}
	vm := goja.New()
	vm.SetMaxCallStackSize(512)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ErrTimeout)
		case <-done:
		}
	}()
	// 在用户代码执行前捕获内置序列化函数，避免脚本覆盖全局 JSON。
	jsonObject := vm.Get("JSON").ToObject(vm)
	stringify, ok := goja.AssertFunction(jsonObject.Get("stringify"))
	if !ok {
		return result, ErrExecution
	}
	if !legacy {
		if err := installSources(vm, ctx, request.Sources, host, stringify, jsonObject); err != nil {
			return result, ErrExecution
		}
	}
	input, err := vm.RunString("JSON.parse(" + strconv.Quote(string(request.Input)) + ")")
	if err != nil {
		return result, executionError(ctx)
	}
	runContext, err := vm.RunString("JSON.parse(" + strconv.Quote(string(request.Context)) + ")")
	if err != nil {
		return result, executionError(ctx)
	}
	failures := map[*goja.Object]error{}
	if legacy {
		if err := installLegacySources(ctx, vm, request.LegacySources, read, failures); err != nil {
			return result, err
		}
	}
	value, err := vm.RunProgram(program)
	if err != nil {
		return result, executionError(ctx)
	}
	transform, ok := goja.AssertFunction(value)
	if !ok {
		return result, ErrExecution
	}
	value, err = transform(goja.Undefined(), input, runContext)
	if err != nil {
		return result, executionError(ctx)
	}
	if promise, ok := value.Export().(*goja.Promise); ok {
		if promise.State() != goja.PromiseStateFulfilled {
			if ctx.Err() == nil {
				if object, ok := promise.Result().(*goja.Object); ok {
					if failure := failures[object]; failure != nil {
						return result, failure
					}
				}
			}
			return result, executionError(ctx)
		}
		value = promise.Result()
	}
	if goja.IsUndefined(value) || goja.IsNull(value) {
		return result, ErrOutput
	}
	encoded, err := stringify(jsonObject, value)
	if err != nil {
		return result, executionError(ctx)
	}
	raw := []byte(encoded.String())
	if len(raw) > MaxJSONBytes {
		return result, ErrOutput
	}
	var shape map[string]json.RawMessage
	if json.Unmarshal(raw, &shape) != nil || shape == nil || shape["drop"] == nil {
		return result, ErrOutput
	}
	// A script chooses broadcast data OR explicit destinations, never both.
	if (shape["data"] == nil) == (shape["outputs"] == nil) {
		return result, ErrOutput
	}
	if outputs := shape["outputs"]; outputs != nil {
		var items []map[string]json.RawMessage
		if json.Unmarshal(outputs, &items) != nil || items == nil || len(items) > 1000 {
			return result, ErrOutput
		}
		seen := map[int64]bool{}
		for _, item := range items {
			var id int64
			if len(item) != 2 || item["data"] == nil || json.Unmarshal(item["topicId"], &id) != nil || id <= 0 || id > 9007199254740991 || seen[id] {
				return result, ErrOutput
			}
			seen[id] = true
		}
		if string(shape["drop"]) == "true" && len(items) != 0 {
			return result, ErrOutput
		}
	}
	if string(shape["drop"]) != "true" && string(shape["drop"]) != "false" {
		return result, ErrOutput
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return Result{}, ErrOutput
	}
	if result.Error != "" {
		return Result{}, ErrOutput
	}
	if len(result.Logs) > 100 {
		return Result{}, ErrOutput
	}
	for _, log := range result.Logs {
		if len(log.Message) > 2048 || (log.Level != "info" && log.Level != "warn" && log.Level != "error" && log.Level != "debug") {
			return Result{}, ErrOutput
		}
	}
	if result.Logs == nil {
		result.Logs = []Log{}
	}
	if ctx.Err() != nil {
		return Result{}, ErrTimeout
	}
	result.DurationMs = time.Since(started).Milliseconds()
	return result, nil
}
func executionError(ctx context.Context) error {
	if ctx.Err() != nil {
		return ErrTimeout
	}
	return ErrExecution
}

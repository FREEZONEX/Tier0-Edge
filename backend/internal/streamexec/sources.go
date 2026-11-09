package streamexec

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/dop251/goja"
)

const MaxLegacySourceCalls = 16

// 宿主注入读取函数；实现必须遵守 Context 取消，不向脚本暴露凭据或内部对象。
type SourceReader func(context.Context, string, json.RawMessage) (json.RawMessage, error)

const MaxSourceCallTimeout = 25 * time.Second
const DefaultSourceCallTimeout = 5 * time.Second

var ErrSourceReadTimeout = errors.New("SourceStream.sourceReadTimeout")

var ErrSourceRead = errors.New("SourceStream.sourceReadFailed")

var ErrSourceAccess = errors.New("SourceStream.sourceAccessDenied")

func installLegacySources(ctx context.Context, vm *goja.Runtime, aliases []string, read SourceReader, failures map[*goja.Object]error) error {
	failureObjects := map[error]*goja.Object{}
	fail := func(err error) *goja.Object {
		safe := ErrSourceRead
		for _, candidate := range []error{ErrSourceReadTimeout, ErrSourceAccess, ErrInput, ErrOutput} {
			if errors.Is(err, candidate) {
				safe = candidate
				break
			}
		}
		if object := failureObjects[safe]; object != nil {
			return object
		}
		object := vm.NewGoError(safe)
		failureObjects[safe] = object
		failures[object] = safe
		return object
	}

	allowed := map[string]bool{}
	for _, alias := range aliases {
		allowed[alias] = true
	}
	count := 0
	object := vm.Get("JSON").ToObject(vm)
	stringify, _ := goja.AssertFunction(object.Get("stringify"))
	parse, _ := goja.AssertFunction(object.Get("parse"))
	sdk := vm.NewObject()
	if err := sdk.Set("get", func(call goja.FunctionCall) goja.Value {
		alias := call.Argument(0).String()
		if !allowed[alias] || read == nil {
			panic(fail(ErrSourceAccess))
		}
		source := vm.NewObject()
		_ = source.Set("read", func(call goja.FunctionCall) goja.Value {
			count++
			if count > MaxLegacySourceCalls || ctx.Err() != nil {
				panic(fail(ErrSourceAccess))
			}
			encoded, err := stringify(object, call.Argument(0))
			if err != nil {
				panic(fail(ErrInput))
			}
			raw := json.RawMessage(encoded.String())
			if len(raw) > MaxJSONBytes || !json.Valid(raw) {
				panic(fail(ErrInput))
			}
			var options map[string]json.RawMessage
			if json.Unmarshal(raw, &options) != nil || options == nil {
				panic(fail(ErrInput))
			}
			timeout := DefaultSourceCallTimeout
			if value, ok := options["timeoutMs"]; ok {
				var millis int
				if json.Unmarshal(value, &millis) != nil || millis < 1 || millis > int(MaxSourceCallTimeout/time.Millisecond) {
					panic(fail(ErrInput))
				}
				timeout = time.Duration(millis) * time.Millisecond
				delete(options, "timeoutMs")
				raw, err = json.Marshal(options)
				if err != nil {
					panic(fail(ErrInput))
				}
			}
			callCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			result, err := read(callCtx, alias, raw)
			if callCtx.Err() != nil {
				panic(fail(ErrSourceReadTimeout))
			}
			if err != nil {
				panic(fail(err))
			}
			if len(result) > MaxJSONBytes || !json.Valid(result) {
				panic(fail(ErrOutput))
			}
			value, err := parse(object, vm.ToValue(string(result)))
			if err != nil {
				panic(fail(ErrOutput))
			}
			return value
		})
		return source
	}); err != nil {
		return err
	}
	return vm.Set("sources", sdk)
}

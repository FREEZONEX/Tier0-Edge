package streamexec

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/dop251/goja"
)

const MaxSourceCalls = 32
const MaxSourceGrants = 64
const MaxSourceResources = 1000

var ErrSourceDenied = errors.New("SourceStream.sourceAccessDenied")
var ErrSourceCall = errors.New("SourceStream.sourceCallFailed")

// SourceGrant 是单次执行的能力清单，不包含连接地址或凭证。
// Backend 必须再次检查生效版本、Source 状态与资源权限，不能信任脚本传参。
type SourceGrant struct {
	Alias          string   `json:"alias"`
	SourceID       int64    `json:"sourceId"`
	ConfigRevision int64    `json:"configRevision"`
	ReadResources  []string `json:"readResources"`
}

type SourceCall struct {
	ID          int             `json:"id"`
	Alias       string          `json:"alias"`
	ResourceRef string          `json:"resourceRef"`
	Input       json.RawMessage `json:"input"`
}

// SourceHost 由受信宿主实现；用户脚本只能请求清单中的只读操作。
type SourceHost func(context.Context, SourceCall) (json.RawMessage, error)

func ValidSourceGrants(grants []SourceGrant) bool {
	if len(grants) > MaxSourceGrants {
		return false
	}
	aliases := map[string]bool{}
	for _, grant := range grants {
		if !validAlias(grant.Alias) || aliases[grant.Alias] || grant.SourceID <= 0 || grant.SourceID > 9007199254740991 || grant.ConfigRevision <= 0 || len(grant.ReadResources) == 0 || len(grant.ReadResources) > MaxSourceResources {
			return false
		}
		aliases[grant.Alias] = true
		refs := map[string]bool{}
		for _, ref := range grant.ReadResources {
			if strings.TrimSpace(ref) == "" || len(ref) > 65535 || refs[ref] {
				return false
			}
			refs[ref] = true
		}
	}
	return true
}

func validAlias(alias string) bool {
	if len(alias) == 0 || len(alias) > 63 || alias == "__proto__" || alias == "prototype" || alias == "constructor" {
		return false
	}
	for i, r := range alias {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

func AllowsSourceRead(grants []SourceGrant, call SourceCall) bool {
	if call.ID < 1 || call.ID > MaxSourceCalls || !json.Valid(call.Input) || len(call.Input) > MaxJSONBytes {
		return false
	}
	for _, grant := range grants {
		if grant.Alias == call.Alias {
			for _, ref := range grant.ReadResources {
				if ref == call.ResourceRef {
					return true
				}
			}
		}
	}
	return false
}

func installSources(vm *goja.Runtime, ctx context.Context, grants []SourceGrant, host SourceHost, stringify goja.Callable, jsonObject *goja.Object) error {
	parse, ok := goja.AssertFunction(jsonObject.Get("parse"))
	if !ok {
		return ErrSourceCall
	}
	api := vm.NewObject()
	if err := api.SetPrototype(nil); err != nil {
		return err
	}
	calls := 0
	err := api.Set("get", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) != 1 || !isString(call.Argument(0)) {
			panic(vm.NewTypeError(ErrSourceDenied.Error()))
		}
		alias := call.Argument(0).String()
		found := false
		for _, grant := range grants {
			found = found || grant.Alias == alias
		}
		if !found {
			panic(vm.NewTypeError(ErrSourceDenied.Error()))
		}
		resource := vm.NewObject()
		if err := resource.SetPrototype(nil); err != nil {
			panic(vm.NewTypeError(ErrSourceCall.Error()))
		}
		if err := resource.Set("read", func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) < 1 || len(call.Arguments) > 2 || !isString(call.Argument(0)) || calls >= MaxSourceCalls {
				panic(vm.NewTypeError(ErrSourceDenied.Error()))
			}
			input := json.RawMessage(`{}`)
			if len(call.Arguments) == 2 {
				encoded, err := stringify(jsonObject, call.Argument(1))
				if err != nil || goja.IsUndefined(encoded) {
					panic(vm.NewTypeError(ErrSourceDenied.Error()))
				}
				input = json.RawMessage(encoded.String())
			}
			calls++
			request := SourceCall{ID: calls, Alias: alias, ResourceRef: call.Argument(0).String(), Input: input}
			if !AllowsSourceRead(grants, request) {
				panic(vm.NewTypeError(ErrSourceDenied.Error()))
			}
			if ctx.Err() != nil {
				panic(vm.NewTypeError(ErrTimeout.Error()))
			}
			if host == nil {
				panic(vm.NewTypeError(ErrSourceCall.Error()))
			}
			data, err := host(ctx, request)
			if ctx.Err() != nil {
				panic(vm.NewTypeError(ErrTimeout.Error()))
			}
			if err != nil || len(data) > MaxJSONBytes || !json.Valid(data) {
				panic(vm.NewTypeError(ErrSourceCall.Error()))
			}
			// 宿主数据只能转成 JS JSON 值，不能转成可反射的 Go 对象。
			value, err := parse(jsonObject, vm.ToValue(string(data)))
			if err != nil {
				panic(vm.NewTypeError(ErrSourceCall.Error()))
			}
			return value
		}); err != nil {
			panic(vm.NewTypeError(ErrSourceCall.Error()))
		}
		return resource
	})
	if err != nil {
		return err
	}
	return vm.Set("sources", api)
}

func isString(value goja.Value) bool {
	_, ok := value.Export().(string)
	return ok
}

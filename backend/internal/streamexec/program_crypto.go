package streamexec

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"hash"

	"github.com/dop251/goja"
)

// installCrypto exposes deterministic signing only, not Node, native modules or I/O.
func (p *programVM) installCrypto() {
	module := p.vm.NewObject()
	create := func(call goja.FunctionCall, keyed bool) goja.Value {
		algorithm, ok := call.Argument(0).Export().(string)
		if !ok || algorithm != "sha256" || (!keyed && len(call.Arguments) != 1) || (keyed && len(call.Arguments) != 2) {
			p.fail("only sha256 hashing is supported")
		}
		var digest hash.Hash
		if keyed {
			key, ok := call.Argument(1).Export().(string)
			if !ok || len(key) > MaxJSONBytes {
				p.fail("HMAC key must be a bounded string")
			}
			digest = hmac.New(sha256.New, []byte(key))
		} else {
			digest = sha256.New()
		}
		object := p.vm.NewObject()
		bytes := 0
		finished := false
		_ = object.Set("update", func(call goja.FunctionCall) goja.Value {
			data, ok := call.Argument(0).Export().(string)
			if !ok || len(call.Arguments) != 1 || finished || len(data) > MaxJSONBytes-bytes {
				p.fail("hash update requires a bounded string before digest")
			}
			bytes += len(data)
			_, _ = digest.Write([]byte(data))
			return object
		})
		_ = object.Set("digest", func(call goja.FunctionCall) goja.Value {
			encoding, ok := call.Argument(0).Export().(string)
			if !ok || len(call.Arguments) != 1 || finished || (encoding != "hex" && encoding != "base64") {
				p.fail("hash digest requires hex or base64 exactly once")
			}
			finished = true
			result := digest.Sum(nil)
			if encoding == "hex" {
				return p.vm.ToValue(hex.EncodeToString(result))
			}
			return p.vm.ToValue(base64.StdEncoding.EncodeToString(result))
		})
		return object
	}
	_ = module.Set("createHash", func(call goja.FunctionCall) goja.Value { return create(call, false) })
	_ = module.Set("createHmac", func(call goja.FunctionCall) goja.Value { return create(call, true) })
	freeze, _ := goja.AssertFunction(p.vm.Get("Object").ToObject(p.vm).Get("freeze"))
	_, _ = freeze(goja.Undefined(), module)
	_ = p.vm.Set("require", func(call goja.FunctionCall) goja.Value {
		name, ok := call.Argument(0).Export().(string)
		if !ok || len(call.Arguments) != 1 || name != "node:crypto" {
			p.fail("module is not allowed")
		}
		return module
	})
}

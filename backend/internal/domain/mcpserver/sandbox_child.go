package mcpserver

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/dop251/goja"
)

// executePrelude 定义 execute 里的 tier0.request()。它在 JS 侧组装请求、解析响应并把 Tier0 的错误信封
// 翻成异常；真正的发送经 __tier0Host 交给父进程。
const executePrelude = `
const tier0 = Object.freeze({
  async request(options) {
    if (options === null || typeof options !== "object") {
      throw new TypeError("tier0.request expects an options object: { method, path, query, body }");
    }
    const method = String(options.method || "GET").toUpperCase();
    const path = String(options.path || "");
    const req = { method, path };
    if (options.fleetNodeId !== undefined) req.fleetNodeId = String(options.fleetNodeId);
    if (options.credentialImport !== undefined) req.credentialImport = String(options.credentialImport);
    if (options.query) {
      req.query = {};
      for (const [k, v] of Object.entries(options.query)) {
        if (v !== undefined && v !== null) req.query[k] = String(v);
      }
    }
    if (options.rawBody) {
      req.rawBody = options.body === undefined || options.body === null ? "" : String(options.body);
      if (options.contentType) req.contentType = String(options.contentType);
    } else if (options.body !== undefined) {
      req.body = options.body;
      if (options.contentType) req.contentType = String(options.contentType);
    }
    const res = __tier0Host(JSON.stringify(req));
    const ct = String(res.contentType || "").split(";")[0].trim().toLowerCase();
    let data = res.body;
    if ((ct === "application/json" || ct.endsWith("+json")) && res.body !== "") {
      try { data = JSON.parse(res.body); } catch (e) { data = res.body; }
    }
    const env = !res.rawJSON && data !== null && typeof data === "object" && !Array.isArray(data) && "code" in data ? data : null;
    const code = env ? env.code : undefined;
    const okCode = code === undefined || code === null || code === 0 || code === 200 || code === "0" || code === "200";
    if (res.status < 200 || res.status >= 300 || !okCode) {
      let detail = "";
      if (env) {
        detail = [env.code, env.msg || env.message].filter(x => x !== undefined && x !== null && x !== "").join(": ");
        if (env.error && env.error.code) detail += " (" + env.error.code + (env.error.execution ? ", execution " + env.error.execution : "") + ")";
      } else if (typeof data === "string") {
        detail = data.slice(0, 500);
      } else {
        detail = JSON.stringify(data).slice(0, 500);
      }
      const err = new Error("Tier0 API error: " + method + " " + path + " returned HTTP " + res.status + (detail ? ": " + detail : ""));
      err.status = res.status;
      err.response = data;
      const fault = env && env.error;
      if (fault) {
        err.code = fault.code;
        err.reason = fault.reason;
        err.field = fault.field || fault.path;
        err.details = fault.details;
        err.execution = fault.execution;
        err.retry = fault.retry;
        err.repair = String(fault.repair || "").replaceAll("api_contract_get", "the current operation request schema with the MCP search tool");
        if (err.field) err.message += "; field=" + err.field;
        if (fault.reason) err.message += "; reason=" + String(fault.reason).replaceAll("api_contract_get", "the current operation request schema with the MCP search tool");
        if (["INVALID_REQUEST", "INVALID_ARGUMENT", "VALIDATION_FAILED"].includes(fault.code)) err.message += "; use the MCP search tool to inspect this operation's requestBody before correcting the input";
      }
      err.message = err.message.replaceAll("api_contract_get", "the current operation request schema with the MCP search tool");
      throw err;
    }
    if (env) {
      const out = { status: res.status, code: env.code, msg: env.msg, data: env.data };
      if (env.requestId) out.requestId = env.requestId;
      return out;
    }
    return { status: res.status, data };
  }
});
`

// consolePrelude 让模型习惯写的 console.log 不报 ReferenceError（输出丢弃，结果只看返回值）。
const consolePrelude = `globalThis.console = Object.freeze({ log() {}, info() {}, warn() {}, error() {}, debug() {} });`

// RunSandboxChild 是沙箱子进程的入口：读一个 job，执行，写回 result，返回退出码。
func RunSandboxChild() int {
	if err := applySandboxLimits(); err != nil {
		writeChildResult(os.Stdout, false, "", fmt.Sprintf("sandbox setup failed: %v", err))
		return 1
	}
	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	dec := json.NewDecoder(in)
	enc := json.NewEncoder(os.Stdout)
	var first sandboxFrame
	if err := dec.Decode(&first); err != nil || first.Type != "job" || first.Job == nil {
		writeChildResult(os.Stdout, false, "", "sandbox received no job")
		return 1
	}
	result, err := runSandboxJob(*first.Job, dec, enc)
	if err != nil {
		writeChildResult(os.Stdout, false, "", err.Error())
		return 0
	}
	writeChildResult(os.Stdout, true, result, "")
	return 0
}

func writeChildResult(w io.Writer, ok bool, result, msg string) {
	_ = json.NewEncoder(w).Encode(sandboxFrame{Type: "result", OK: ok, Result: result, Error: msg})
}

// runSandboxJob 在新的 goja Runtime 里执行用户代码（必须是 async 箭头函数或返回 Promise 的函数）。
func runSandboxJob(job sandboxJob, dec *json.Decoder, enc *json.Encoder) (string, error) {
	vm := goja.New()
	vm.SetMaxCallStackSize(4096)
	timeout := time.Duration(job.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = defaultSandboxTimeout
	}
	timer := time.AfterFunc(timeout, func() { vm.Interrupt("timeout") })
	defer timer.Stop()

	if _, err := vm.RunString(consolePrelude); err != nil {
		return "", err
	}
	switch job.Mode {
	case sandboxModeSearch:
		if err := vm.Set("__specText", job.Spec); err != nil {
			return "", err
		}
		if _, err := vm.RunString(`globalThis.spec = JSON.parse(__specText); delete globalThis.__specText;`); err != nil {
			return "", fmt.Errorf("load spec: %w", err)
		}
	case sandboxModeExecute:
		host := newChildHost(vm, dec, enc, job.MaxRequests)
		if err := vm.Set("__tier0Host", host.call); err != nil {
			return "", err
		}
		if err := vm.Set("workspaceId", job.WorkspaceID); err != nil {
			return "", err
		}
		if _, err := vm.RunString(executePrelude); err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unknown sandbox mode %q", job.Mode)
	}

	// 与 Cloudflare 一致：代码作为表达式嵌进 `(...)()`。
	value, err := vm.RunString("(async () => { const __fn = (\n" + job.Code + "\n); return await __fn(); })()")
	if err != nil {
		return "", jsError(vm, err)
	}
	promise, ok := value.Export().(*goja.Promise)
	if !ok {
		return "", errors.New("internal error: wrapper did not return a promise")
	}
	switch promise.State() {
	case goja.PromiseStateRejected:
		return "", errors.New(rejectionMessage(vm, promise.Result()))
	case goja.PromiseStatePending:
		return "", errors.New("the returned promise never settled (timers and network APIs are not available)")
	}
	stringify, err := vm.RunString(`(v) => { const s = JSON.stringify(v); return s === undefined ? "undefined" : s; }`)
	if err != nil {
		return "", err
	}
	fn, _ := goja.AssertFunction(stringify)
	out, err := fn(goja.Undefined(), promise.Result())
	if err != nil {
		return "", fmt.Errorf("the returned value is not JSON-serializable: %w", jsError(vm, err))
	}
	return out.String(), nil
}

// jsError 把 goja 的异常 / 中断 / 语法错误翻成一行可读信息。
func jsError(vm *goja.Runtime, err error) error {
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) {
		return errors.New("the code did not finish within the time limit")
	}
	var exc *goja.Exception
	if errors.As(err, &exc) {
		return errors.New(rejectionMessage(vm, exc.Value()))
	}
	var syntax *goja.CompilerSyntaxError
	if errors.As(err, &syntax) {
		return fmt.Errorf("SyntaxError: %s", syntax.Error())
	}
	return err
}

func rejectionMessage(vm *goja.Runtime, v goja.Value) string {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return "the code threw " + fmt.Sprint(v)
	}
	if obj, ok := v.(*goja.Object); ok {
		if msg := obj.Get("message"); msg != nil && !goja.IsUndefined(msg) {
			name := "Error"
			if n := obj.Get("name"); n != nil && !goja.IsUndefined(n) {
				name = n.String()
			}
			if name == "Error" {
				return msg.String()
			}
			return name + ": " + msg.String()
		}
	}
	return v.String()
}

// childHost 是子进程侧的 __tier0Host：同步地把请求交给父进程并等回复。
type childHost struct {
	vm          *goja.Runtime
	dec         *json.Decoder
	enc         *json.Encoder
	maxRequests int
	sent        int
}

func newChildHost(vm *goja.Runtime, dec *json.Decoder, enc *json.Encoder, maxRequests int) *childHost {
	return &childHost{vm: vm, dec: dec, enc: enc, maxRequests: maxRequests}
}

func (h *childHost) call(raw string) goja.Value {
	if h.maxRequests > 0 && h.sent >= h.maxRequests {
		h.fail(requestLimitError(h.maxRequests).Error())
	}
	var req apiRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		panic(h.vm.NewTypeError("invalid tier0.request options: " + err.Error()))
	}
	h.sent++
	if err := h.enc.Encode(sandboxFrame{Type: "request", ID: h.sent, Request: &req}); err != nil {
		h.fail("sandbox channel closed: " + err.Error())
	}
	var reply sandboxFrame
	if err := h.dec.Decode(&reply); err != nil || reply.Type != "response" || reply.ID != h.sent {
		h.fail("sandbox channel broken")
	}
	if reply.Error != "" {
		h.fail(reply.Error)
	}
	if reply.Response == nil {
		h.fail("empty response from host")
	}
	obj := h.vm.NewObject()
	_ = obj.Set("status", reply.Response.Status)
	_ = obj.Set("contentType", reply.Response.ContentType)
	_ = obj.Set("rawJSON", reply.Response.RawJSON)
	_ = obj.Set("body", reply.Response.Body)
	return obj
}

// fail 在 JS 里抛一个普通 Error（不是 GoError），模型看到的就是这条消息本身。
func (h *childHost) fail(msg string) {
	ctor, _ := goja.AssertConstructor(h.vm.Get("Error"))
	obj, err := ctor(nil, h.vm.ToValue(msg))
	if err != nil {
		panic(h.vm.NewGoError(errors.New(msg)))
	}
	panic(obj)
}

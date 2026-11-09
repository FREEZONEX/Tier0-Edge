package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// search / execute 的代码在独立子进程里跑（goja，本进程 re-exec 自身）：每次调用一个新进程，
// 墙钟超时 + RSS 上限 + rlimit；子进程没有网络与文件系统 API，唯一出口是经 stdin/stdout 交给父进程的
// tier0.request()，凭据只在父进程注入（参照 Tier0-Backend `share/streamruntime`）。

// sandboxChildEnv 置 1 时进程以沙箱子进程身份运行（见 cmd/server/main.go）。
const sandboxChildEnv = "UNS_MCP_SANDBOX_CHILD"

// IsSandboxChild 报告本进程是不是被 re-exec 出来的沙箱子进程。
func IsSandboxChild() bool { return os.Getenv(sandboxChildEnv) == "1" }

const (
	sandboxModeSearch  = "search"
	sandboxModeExecute = "execute"

	defaultSandboxTimeout     = 25 * time.Second
	defaultSandboxMemoryMB    = 256
	defaultSandboxMaxRequests = 50
	defaultSandboxMaxResult   = 4 << 20
	// sandboxConcurrency 本副本同时存在的沙箱子进程上限，超出的调用排队（受调用方 ctx 约束）。
	sandboxConcurrency = 8
	// sandboxMaxFrameBytes 子进程每一帧（一行 JSON）的独立上限：超了立即杀子进程，不把巨型帧读进内存
	// （结果另有 MaxResultBytes 约束）。
	sandboxMaxFrameBytes = 16 << 20
)

// sandboxJob 是父进程交给子进程的任务。
type sandboxJob struct {
	Mode        string `json:"mode"`
	Code        string `json:"code"`
	Spec        string `json:"spec,omitempty"`
	WorkspaceID string `json:"workspaceId,omitempty"`
	MaxRequests int    `json:"maxRequests,omitempty"`
	TimeoutMs   int64  `json:"timeoutMs"`
}

// sandboxFrame 是父子进程之间一行一个的 JSON 帧。
type sandboxFrame struct {
	Type     string       `json:"type"` // job | request | response | result
	Job      *sandboxJob  `json:"job,omitempty"`
	ID       int          `json:"id,omitempty"`
	Request  *apiRequest  `json:"request,omitempty"`
	Response *apiResponse `json:"response,omitempty"`
	OK       bool         `json:"ok,omitempty"`
	Result   string       `json:"result,omitempty"`
	Error    string       `json:"error,omitempty"`
}

// codeError 是用户代码自己的失败（语法错误、抛出的异常、API 报错）：原样告诉模型。
type codeError struct{ msg string }

func (e *codeError) Error() string { return e.msg }

// sandboxHost 处理子进程发来的一次 tier0.request。
type sandboxHost func(ctx context.Context, req apiRequest) (*apiResponse, error)

type sandboxRunner struct {
	exe         string
	timeout     time.Duration
	maxMemory   uint64
	maxRequests int
	maxResult   int
	maxFrame    int
	sem         chan struct{}
}

func newSandboxRunner(timeout time.Duration, maxMemoryMB, maxRequests, maxResult int) (*sandboxRunner, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable for mcp sandbox: %w", err)
	}
	if timeout <= 0 {
		timeout = defaultSandboxTimeout
	}
	if maxMemoryMB <= 0 {
		maxMemoryMB = defaultSandboxMemoryMB
	}
	if maxRequests <= 0 {
		maxRequests = defaultSandboxMaxRequests
	}
	if maxResult <= 0 {
		maxResult = defaultSandboxMaxResult
	}
	return &sandboxRunner{
		exe: exe, timeout: timeout, maxMemory: uint64(maxMemoryMB) << 20,
		maxRequests: maxRequests, maxResult: maxResult, maxFrame: sandboxMaxFrameBytes,
		sem: make(chan struct{}, sandboxConcurrency),
	}, nil
}

// run 起一个子进程执行 job；host 为 nil 时（search）子进程里没有 tier0.request。
// 返回值是代码返回值的 JSON 文本（undefined 时为 "undefined"）。
func (r *sandboxRunner) run(parent context.Context, job sandboxJob, host sandboxHost) (string, error) {
	select {
	case r.sem <- struct{}{}:
	case <-parent.Done():
		return "", parent.Err()
	}
	defer func() { <-r.sem }()

	ctx, cancel := context.WithTimeout(parent, r.timeout)
	defer cancel()
	job.TimeoutMs = r.timeout.Milliseconds()
	if job.Mode == sandboxModeExecute {
		job.MaxRequests = r.maxRequests
	}

	cmd := exec.Command(r.exe)
	cmd.Dir = os.TempDir()
	// 不继承任何宿主环境（数据库地址、密钥、代理），只给运行时参数。
	cmd.Env = []string{
		sandboxChildEnv + "=1",
		"GOMAXPROCS=1",
		fmt.Sprintf("GOMEMLIMIT=%dMiB", r.maxMemory>>20),
		"GOGC=50",
	}
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	closeProcess, err := startSandboxCommand(cmd, r.maxMemory)
	if err != nil {
		return "", fmt.Errorf("start mcp sandbox: %w", err)
	}
	defer closeProcess()
	var memoryExceeded atomic.Bool
	monitorDone := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				_ = cmd.Process.Kill()
				return
			case <-monitorDone:
				return
			case <-ticker.C:
				if rss, ok := processRSS(cmd.Process.Pid); ok && rss > r.maxMemory {
					memoryExceeded.Store(true)
					_ = cmd.Process.Kill()
					return
				}
			}
		}
	}()
	defer func() {
		close(monitorDone)
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		wg.Wait()
	}()

	result, err := r.converse(ctx, job, host, stdin, stdout)
	if err != nil {
		var ce *codeError
		switch {
		case errors.Is(err, errFrameTooLarge):
			_ = cmd.Process.Kill()
			return "", &codeError{msg: fmt.Sprintf("the code produced a single message over the %d MiB limit (a tier0.request body or the returned value is too large)", r.maxFrame>>20)}
		case errors.As(err, &ce):
			return "", err
		case memoryExceeded.Load():
			return "", &codeError{msg: fmt.Sprintf("the code exceeded the %d MiB memory limit", r.maxMemory>>20)}
		case errors.Is(ctx.Err(), context.DeadlineExceeded) && parent.Err() == nil:
			return "", &codeError{msg: fmt.Sprintf("the code did not finish within %s", r.timeout)}
		case parent.Err() != nil:
			return "", parent.Err()
		}
		return "", err
	}
	return result, nil
}

func (r *sandboxRunner) converse(
	ctx context.Context, job sandboxJob, host sandboxHost, stdin io.Writer, stdout io.Reader,
) (string, error) {
	enc := json.NewEncoder(stdin)
	if err := enc.Encode(sandboxFrame{Type: "job", Job: &job}); err != nil {
		return "", fmt.Errorf("send job to mcp sandbox: %w", err)
	}
	frames := bufio.NewReaderSize(stdout, 64<<10)
	requests := 0
	for {
		line, err := readFrame(frames, r.maxFrame)
		if err != nil {
			if errors.Is(err, errFrameTooLarge) {
				return "", err
			}
			return "", fmt.Errorf("mcp sandbox exited unexpectedly: %w", err)
		}
		var frame sandboxFrame
		if err := json.Unmarshal(line, &frame); err != nil {
			return "", fmt.Errorf("mcp sandbox sent a malformed frame: %w", err)
		}
		switch frame.Type {
		case "request":
			// 子进程不可信：请求次数与是否允许发请求都在父进程这边再判一次。
			resp := sandboxFrame{Type: "response", ID: frame.ID}
			requests++
			switch {
			case host == nil || frame.Request == nil:
				resp.Error = "tier0.request is not available here"
			case requests > r.maxRequests:
				resp.Error = requestLimitError(r.maxRequests).Error()
			default:
				out, err := host(ctx, *frame.Request)
				if err != nil {
					resp.Error = err.Error()
				} else {
					resp.Response = out
				}
			}
			if err := enc.Encode(resp); err != nil {
				return "", fmt.Errorf("reply to mcp sandbox: %w", err)
			}
		case "result":
			if !frame.OK {
				return "", &codeError{msg: frame.Error}
			}
			if len(frame.Result) > r.maxResult {
				return "", &codeError{msg: fmt.Sprintf("the returned value is %d bytes, over the %d byte limit; return less data", len(frame.Result), r.maxResult)}
			}
			return frame.Result, nil
		default:
			return "", fmt.Errorf("mcp sandbox sent unknown frame %q", frame.Type)
		}
	}
}

// errFrameTooLarge 子进程的一帧超过 maxFrame。
var errFrameTooLarge = errors.New("mcp sandbox frame exceeds the size limit")

// readFrame 读一行（一帧），超过 max 字节立刻返回 errFrameTooLarge，不再往下读。
func readFrame(r *bufio.Reader, max int) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(buf)+len(chunk) > max+1 { // +1：结尾的换行不计
			return nil, errFrameTooLarge
		}
		buf = append(buf, chunk...)
		switch {
		case err == nil:
			return bytes.TrimRight(buf, "\r\n"), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF) && len(bytes.TrimSpace(buf)) > 0:
			return buf, nil
		default:
			return nil, err
		}
	}
}

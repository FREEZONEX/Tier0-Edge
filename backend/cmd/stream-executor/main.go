// stream-executor 通过 Unix socket 接收任务，每次脚本在独立子进程执行。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"backend/internal/infra/localipc"
	"backend/internal/runtimeenv"
	"backend/internal/streamexec"
)

func main() {
	if runtimeenv.IdentityQuery() {
		return
	}
	child := flag.Bool("child", false, "execute one isolated request")
	legacyChild := flag.Bool("legacy-child", false, "execute one isolated legacy read-only request")
	controlledChild := flag.Bool("controlled-child", false, "execute with private Source IPC")
	socket := flag.String("socket", "/run/stream-executor/executor.sock", "private Unix socket")
	flag.Parse()
	if *legacyChild {
		executeLegacyChild()
		return
	}
	if *controlledChild {
		if err := streamexec.RunControlledChild(context.Background(), os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		return
	}
	if *child {
		executeChild()
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if err := serve(ctx, *socket); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func executeChild() {
	var request streamexec.Request
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 3*streamexec.MaxJSONBytes))
	result := streamexec.Result{Error: streamexec.ErrInput.Error()}
	if err := decoder.Decode(&request); err == nil {
		value, err := streamexec.Execute(context.Background(), request)
		result = value
		if err != nil {
			result = streamexec.Result{Error: err.Error()}
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
}
func executeLegacyChild() {
	var request streamexec.Request
	decoder := streamexec.NewFrameReader(os.Stdin)
	result := streamexec.Result{Error: streamexec.ErrInput.Error()}
	if err := decoder.Decode(&request); err == nil {
		value, err := streamexec.ExecuteWithLegacySources(context.Background(), request, func(ctx context.Context, alias string, options json.RawMessage) (json.RawMessage, error) {
			deadline, ok := ctx.Deadline()
			if !ok || ctx.Err() != nil {
				return nil, streamexec.ErrSourceReadTimeout
			}
			remaining := int((time.Until(deadline) + time.Millisecond - 1) / time.Millisecond)
			if remaining < 1 {
				return nil, streamexec.ErrSourceReadTimeout
			}
			if err := json.NewEncoder(os.Stdout).Encode(streamexec.Frame{Type: "source", Call: &streamexec.LegacySourceCall{Alias: alias, Options: options, TimeoutMs: remaining}}); err != nil {
				return nil, err
			}
			var reply streamexec.LegacySourceReply
			if err := decoder.Decode(&reply); err != nil {
				return nil, err
			}
			if reply.Error != "" {
				if reply.Error == streamexec.ErrSourceReadTimeout.Error() {
					return nil, streamexec.ErrSourceReadTimeout
				}
				return nil, streamexec.ErrSourceRead
			}
			return reply.Data, nil
		})
		result = value
		if err != nil {
			result = streamexec.Result{Error: err.Error()}
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(streamexec.Frame{Type: "result", Result: &result})
}
func serve(ctx context.Context, socket string) error {
	if !filepath.IsAbs(socket) {
		return fmt.Errorf("absolute socket path required")
	}
	if err := os.MkdirAll(filepath.Dir(socket), 0750); err != nil {
		return err
	}
	if info, err := os.Lstat(socket); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("socket path occupied")
		}
		if connection, err := net.DialTimeout("unix", socket, 100*time.Millisecond); err == nil {
			connection.Close()
			return fmt.Errorf("executor already running")
		} else if !localipc.StaleSocket(err) {
			return fmt.Errorf("probe existing executor socket: %w", err)
		}
		if err := os.Remove(socket); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer listener.Close()
	if err = os.Chmod(socket, 0660); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	// Each container is one execution slot. Admission is shared by both APIs.
	capacity := make(chan struct{}, 1)
	mux := http.NewServeMux()
	registerSourceExecution(mux, executable, capacity)
	mux.HandleFunc("POST /execute-controlled", controlledHandler(executable, capacity))
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"ready":true}`)
	})
	mux.HandleFunc("POST /execute", executeHandler(executable, capacity))
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: streamexec.MaxExecutorTransportTimeout, WriteTimeout: streamexec.MaxExecutorTransportTimeout, IdleTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	if err = server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func executeHandler(executable string, capacity chan struct{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case capacity <- struct{}{}:
			defer func() { <-capacity }()
		default:
			rejectExecution(w, "executor busy", http.StatusServiceUnavailable)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 3*streamexec.MaxJSONBytes)
		var request streamexec.Request
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil || request.TimeoutMs < 1 || request.TimeoutMs > streamexec.MaxTimeoutMs {
			rejectExecution(w, "invalid execution request", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			rejectExecution(w, "trailing JSON", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(request.TimeoutMs)*time.Millisecond+time.Second)
		defer cancel()
		input, err := json.Marshal(request)
		if err != nil {
			rejectExecution(w, "invalid execution request", http.StatusBadRequest)
			return
		}
		command := exec.CommandContext(ctx, executable, "-child")
		command.Env = []string{"GOMEMLIMIT=96MiB", "GOMAXPROCS=1"}
		command.Stdin = bytes.NewReader(input)
		output, err := boundedChildOutput(command, cancel)
		result := streamexec.Result{}
		if err != nil {
			result.Error = streamexec.ErrExecution.Error()
			if errors.Is(err, streamexec.ErrOutput) {
				result.Error = streamexec.ErrOutput.Error()
			} else if ctx.Err() != nil {
				result.Error = streamexec.ErrTimeout.Error()
			}
		} else if len(output) > streamexec.MaxJSONBytes+16*1024 || json.Unmarshal(output, &result) != nil {
			result = streamexec.Result{Error: streamexec.ErrOutput.Error()}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}
}

// 拒绝时不再复用连接，避免 HTTP/1 等待双工请求 EOF 才发出错误响应。
// 只用于执行开始前的拒绝，不重试或更改已经运行的脚本结果。
func rejectExecution(w http.ResponseWriter, message string, status int) {
	w.Header().Set("Connection", "close")
	http.Error(w, message, status)
}

// boundedChildOutput does not buffer stderr or retain output beyond the wire
// limit. Overflow cancels the child immediately; a soft Go memory limit alone
// is not a sandbox or a substitute for the container's hard resource limits.
func boundedChildOutput(command *exec.Cmd, cancel context.CancelFunc) ([]byte, error) {
	output := &boundedOutput{limit: streamexec.MaxJSONBytes + 16*1024, cancel: cancel}
	command.Stdout = output
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	err := command.Run()
	if output.exceeded {
		return nil, streamexec.ErrOutput
	}
	return output.data, err
}

type boundedOutput struct {
	data     []byte
	limit    int
	exceeded bool
	cancel   context.CancelFunc
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	if w.exceeded {
		return 0, streamexec.ErrOutput
	}
	if w.data == nil {
		w.data = make([]byte, 0, w.limit)
	}
	remaining := w.limit - len(w.data)
	if len(p) <= remaining {
		w.data = append(w.data, p...)
		return len(p), nil
	}
	w.data = append(w.data, p[:remaining]...)
	w.exceeded = true
	w.cancel()
	return remaining, streamexec.ErrOutput
}

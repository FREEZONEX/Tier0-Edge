package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"time"

	"backend/internal/streamexec"
)

// 私有 Unix socket 内的双向会话，调用由 Backend 校验并执行。
func registerSourceExecution(mux *http.ServeMux, executable string, capacity chan struct{}) {
	mux.HandleFunc("POST /execute-sources", func(w http.ResponseWriter, r *http.Request) {
		select {
		case capacity <- struct{}{}:
			defer func() { <-capacity }()
		default:
			rejectExecution(w, "executor busy", http.StatusServiceUnavailable)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, (streamexec.MaxLegacySourceCalls+3)*streamexec.MaxJSONBytes)
		decoder := streamexec.NewFrameReader(r.Body)
		var request streamexec.Request
		if decoder.Decode(&request) != nil || request.TimeoutMs < 1 || request.TimeoutMs > streamexec.MaxTimeoutMs || len(request.LegacySources) > streamexec.MaxLegacySourceCalls || len(request.Code) == 0 || len(request.Code) > streamexec.MaxCodeBytes || len(request.Input) > streamexec.MaxJSONBytes || len(request.Context) > streamexec.MaxJSONBytes {
			rejectExecution(w, "invalid execution request", http.StatusBadRequest)
			return
		}
		// Syntax checks and executions share the same bounded frame protocol.
		if request.LegacySources == nil {
			request.LegacySources = []string{}
		}
		controller := http.NewResponseController(w)
		if controller.EnableFullDuplex() != nil {
			rejectExecution(w, "duplex unavailable", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(request.TimeoutMs)*time.Millisecond+time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, executable, "-legacy-child")
		command.Env = []string{"GOMEMLIMIT=96MiB", "GOMAXPROCS=1"}
		stdin, err := command.StdinPipe()
		if err != nil {
			rejectExecution(w, "executor unavailable", http.StatusServiceUnavailable)
			return
		}
		defer stdin.Close()
		stdout, err := command.StdoutPipe()
		if err != nil {
			rejectExecution(w, "executor unavailable", http.StatusServiceUnavailable)
			return
		}
		if err = command.Start(); err != nil {
			rejectExecution(w, "executor unavailable", http.StatusServiceUnavailable)
			return
		}
		defer func() { cancel(); _ = command.Wait() }()
		// 截止时间同时约束客户端回包等待，断开的调用方不能占满执行器。
		_ = controller.SetReadDeadline(time.Now().Add(time.Duration(request.TimeoutMs)*time.Millisecond + time.Second))
		_ = controller.SetWriteDeadline(time.Now().Add(time.Duration(request.TimeoutMs)*time.Millisecond + time.Second))
		if json.NewEncoder(stdin).Encode(request) != nil {
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(http.StatusOK)
		_ = controller.Flush()
		frames := streamexec.NewFrameReader(stdout)
		for calls := 0; calls <= streamexec.MaxLegacySourceCalls; calls++ {
			var frame streamexec.Frame
			if err = frames.Decode(&frame); err != nil {
				frame = streamexec.Frame{Type: "result", Result: &streamexec.Result{Error: streamexec.ErrExecution.Error()}}
				if ctx.Err() != nil {
					frame.Result.Error = streamexec.ErrTimeout.Error()
				}
			}
			if json.NewEncoder(w).Encode(frame) != nil {
				return
			}
			if controller.Flush() != nil {
				return
			}
			if frame.Type == "result" {
				// 原有读取 deadline 与 MaxBytesReader 同样约束终帧后的请求 EOF。
				_, _ = io.Copy(io.Discard, r.Body)
				return
			}
			if frame.Type != "source" || frame.Call == nil {
				return
			}
			var reply streamexec.LegacySourceReply
			if decoder.Decode(&reply) != nil || len(reply.Data) > streamexec.MaxJSONBytes || len(reply.Error) > 256 {
				return
			}
			if json.NewEncoder(stdin).Encode(reply) != nil {
				return
			}
		}
	})
}

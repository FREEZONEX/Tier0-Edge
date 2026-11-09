package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"time"

	"backend/internal/streamexec"
)

// HTTP/1 全双工只用于私有 Unix socket；每次请求独占子进程和有界能力清单。
func controlledHandler(executable string, capacity chan struct{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		select {
		case capacity <- struct{}{}:
			defer func() { <-capacity }()
		default:
			rejectExecution(w, "executor busy", http.StatusServiceUnavailable)
			return
		}
		controller := http.NewResponseController(w)
		if err := controller.EnableFullDuplex(); err != nil {
			rejectExecution(w, "duplex unavailable", http.StatusServiceUnavailable)
			return
		}
		// 总流量和单帧均有上限；超时关闭连接并杀死阻塞在宿主调用的子进程。
		r.Body = http.MaxBytesReader(w, r.Body, int64(streamexec.MaxProgramExchangeBytes))
		reader := streamexec.NewExchangeReader(r.Body)
		message, err := reader.Read()
		timeout, valid := controlledTimeout(message)
		if err != nil || !valid {
			rejectExecution(w, "invalid execution request", http.StatusBadRequest)
			return
		}
		deadline := time.Now().Add(time.Duration(timeout)*time.Millisecond + time.Second)
		if controller.SetReadDeadline(deadline) != nil || controller.SetWriteDeadline(deadline) != nil {
			rejectExecution(w, "deadline unavailable", http.StatusServiceUnavailable)
			return
		}
		ctx, cancel := context.WithDeadline(r.Context(), deadline)
		defer cancel()
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.Header().Set("Cache-Control", "no-store")
		host := func(call streamexec.Exchange) (streamexec.Exchange, error) {
			if err := streamexec.WriteExchange(w, call); err != nil {
				return streamexec.Exchange{}, err
			}
			if err := controller.Flush(); err != nil {
				return streamexec.Exchange{}, err
			}
			reply, err := reader.Read()
			if err != nil || !matchingControlledReply(call, reply) {
				return streamexec.Exchange{}, streamexec.ErrSourceCall
			}
			return reply, nil
		}
		result := runControlledExchange(ctx, executable, message, host)
		if streamexec.WriteExchange(w, result) != nil || controller.Flush() != nil {
			return
		}
		// 客户端收到终帧后结束请求；在原读取截止时间内消费 EOF，完成双向收尾。
		// 避免未消费 Body 的 keep-alive 收尾与下一次请求读取发生竞争。
		_, _ = io.Copy(io.Discard, r.Body)
	}
}

func controlledTimeout(message streamexec.Exchange) (int, bool) {
	var timeout int
	limit := streamexec.MaxTimeoutMs
	switch message.Kind {
	case "execute":
		if message.Request == nil || !streamexec.ValidSourceGrants(message.Request.Sources) {
			return 0, false
		}
		timeout = message.Request.TimeoutMs
	case "program-execute":
		if message.ProgramRequest == nil {
			return 0, false
		}
		timeout = message.ProgramRequest.TimeoutMs
		limit = streamexec.MaxProgramTimeoutMs
	default:
		return 0, false
	}
	return timeout, timeout >= 1 && timeout <= limit
}

func matchingControlledReply(call, reply streamexec.Exchange) bool {
	if call.Kind == "source-read" && call.Call != nil {
		return reply.Kind == "source-reply" && reply.Reply != nil && reply.Reply.ID == call.Call.ID && len(reply.Reply.Data) <= streamexec.MaxJSONBytes
	}
	if call.Kind == "program-call" && call.ProgramCall != nil {
		return reply.Kind == "program-reply" && reply.ProgramReply != nil && reply.ProgramReply.ID == call.ProgramCall.ID && len(reply.ProgramReply.Data) <= streamexec.MaxProgramDataBytes
	}
	return false
}

func runControlledCommand(ctx context.Context, executable string, request streamexec.Request, host func(streamexec.SourceCall) (streamexec.SourceReply, error)) streamexec.Result {
	result := runControlledExchange(ctx, executable, streamexec.Exchange{Kind: "execute", Request: &request}, func(call streamexec.Exchange) (streamexec.Exchange, error) {
		reply, err := host(*call.Call)
		return streamexec.Exchange{Kind: "source-reply", Reply: &reply}, err
	})
	return *result.Result
}

// Only the Backend host authorizes v2 resource/operation references. This
// parent enforces framing, one protocol per task, unique IDs and hard bounds.
func runControlledExchange(ctx context.Context, executable string, initial streamexec.Exchange, host func(streamexec.Exchange) (streamexec.Exchange, error)) streamexec.Exchange {
	program := initial.Kind == "program-execute"
	failure := func(err error) streamexec.Exchange {
		if program {
			return streamexec.Exchange{Kind: "program-result", ProgramResult: &streamexec.ProgramResult{Error: err.Error()}}
		}
		return streamexec.Exchange{Kind: "result", Result: &streamexec.Result{Error: err.Error()}}
	}
	if _, valid := controlledTimeout(initial); !valid {
		return failure(streamexec.ErrInput)
	}
	command := exec.CommandContext(ctx, executable, "-controlled-child")
	command.Env = []string{"GOMEMLIMIT=96MiB", "GOMAXPROCS=1"}
	command.Stderr = io.Discard
	command.WaitDelay = time.Second
	input, err := command.StdinPipe()
	if err != nil {
		return failure(streamexec.ErrExecution)
	}
	defer input.Close()
	output, err := command.StdoutPipe()
	if err != nil {
		return failure(streamexec.ErrExecution)
	}
	defer output.Close()
	if err = command.Start(); err != nil {
		return failure(streamexec.ErrExecution)
	}
	waited := false
	defer func() {
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	}()
	if err = streamexec.WriteExchange(input, initial); err != nil {
		return failure(streamexec.ErrOutput)
	}
	limit := int64(streamexec.MaxProgramExchangeBytes)
	reader := streamexec.NewExchangeReader(io.LimitReader(output, limit+1))
	calls := 0
	seen := make(map[int]bool)
	for {
		message, err := reader.Read()
		if err != nil {
			if ctx.Err() != nil {
				return failure(streamexec.ErrTimeout)
			}
			return failure(streamexec.ErrOutput)
		}
		if (!program && message.Kind == "result") || (program && message.Kind == "program-result") {
			_ = input.Close()
			_, trailingErr := reader.Read()
			err := command.Wait()
			waited = true
			if ctx.Err() != nil {
				return failure(streamexec.ErrTimeout)
			}
			if !errors.Is(trailingErr, io.EOF) {
				return failure(streamexec.ErrOutput)
			}
			if err != nil {
				return failure(streamexec.ErrExecution)
			}
			return message
		}
		calls++
		if program {
			if message.Kind != "program-call" || message.ProgramCall == nil || calls > streamexec.MaxProgramCalls || message.ProgramCall.ID < 1 || message.ProgramCall.ID > streamexec.MaxProgramCalls || seen[message.ProgramCall.ID] {
				return failure(streamexec.ErrSourceDenied)
			}
			seen[message.ProgramCall.ID] = true
		} else if message.Kind != "source-read" || calls > streamexec.MaxSourceCalls || message.Call == nil || message.Call.ID != calls || !streamexec.AllowsSourceRead(initial.Request.Sources, *message.Call) {
			return failure(streamexec.ErrSourceDenied)
		}
		if host == nil {
			return failure(streamexec.ErrSourceCall)
		}
		reply, err := host(message)
		if err != nil || !matchingControlledReply(message, reply) {
			return failure(streamexec.ErrSourceCall)
		}
		if err = streamexec.WriteExchange(input, reply); err != nil {
			return failure(streamexec.ErrExecution)
		}
	}
}

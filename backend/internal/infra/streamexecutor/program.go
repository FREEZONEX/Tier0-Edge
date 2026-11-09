package streamexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
	"time"

	"backend/internal/domain/sourcestream"
	"backend/internal/streamexec"
)

// ExecuteProgram uses the same isolated slots for registration, preview and execution.
// ExpectedManifest is a compiled version contract, not a caller-provided authorization grant.
func (c *Client) ExecuteProgram(parent context.Context, request streamexec.ProgramRequest, host streamexec.ProgramHost) (streamexec.ProgramResult, error) {
	if request.TimeoutMs < 1 || request.TimeoutMs > streamexec.MaxProgramTimeoutMs || len(request.Code) == 0 || len(request.Code) > streamexec.MaxCodeBytes {
		return streamexec.ProgramResult{}, streamexec.ErrInput
	}
	if !request.CheckOnly && request.ExpectedManifest == nil {
		return streamexec.ProgramResult{}, streamexec.ErrInput
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(request.TimeoutMs)*time.Millisecond+time.Second)
	defer cancel()
	operationCtx, operationCancel := context.WithTimeout(parent, time.Duration(request.TimeoutMs)*time.Millisecond)
	defer operationCancel()
	client, release, err := c.acquire(ctx)
	if err != nil {
		return streamexec.ProgramResult{}, err
	}
	defer release()
	reader, writer, closePipe := controlledPipe(ctx)
	defer closePipe()
	initial := make(chan error, 1)
	go func() {
		err := streamexec.WriteExchange(writer, streamexec.Exchange{Kind: "program-execute", ProgramRequest: &request})
		if err != nil {
			_ = writer.CloseWithError(err)
		}
		initial <- err
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://executor/execute-controlled", reader)
	if err != nil {
		return streamexec.ProgramResult{}, err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	var dispatched atomic.Bool
	req = req.WithContext(httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		WroteHeaders: func() { dispatched.Store(true) },
	}))
	response, err := client.Do(req)
	if err != nil {
		if dispatched.Load() {
			return streamexec.ProgramResult{}, sourcestream.ErrUnknownResult
		}
		if ctx.Err() != nil {
			return streamexec.ProgramResult{}, streamexec.ErrTimeout
		}
		return streamexec.ProgramResult{}, sourcestream.ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return streamexec.ProgramResult{}, sourcestream.ErrUnavailable
	}
	if err := <-initial; err != nil {
		return streamexec.ProgramResult{}, sourcestream.ErrUnknownResult
	}
	exchange := streamexec.NewExchangeReader(io.LimitReader(response.Body, streamexec.MaxProgramExchangeBytes))
	seen := map[int]bool{}
	for {
		message, err := exchange.Read()
		if err == nil && message.Kind == "program-result" {
			if err := completeDuplex(writer, response.Body, func() error { _, err := exchange.Read(); return err }); err != nil {
				if ctx.Err() != nil {
					return streamexec.ProgramResult{}, streamexec.ErrTimeout
				}
				return streamexec.ProgramResult{}, sourcestream.ErrUnknownResult
			}
		}
		if errors.Is(operationCtx.Err(), context.DeadlineExceeded) {
			return streamexec.ProgramResult{}, streamexec.ErrTimeout
		}
		if err != nil {
			if ctx.Err() != nil {
				return streamexec.ProgramResult{}, streamexec.ErrTimeout
			}
			// Missing or corrupt IPC frames are not a terminal output validation
			// failure. The child may already have executed; preserve uncertainty
			// in the durable run and never replay this invocation automatically.
			return streamexec.ProgramResult{}, sourcestream.ErrUnknownResult
		}
		if message.Kind == "program-result" {
			result := *message.ProgramResult
			if result.Error != "" {
				// Preserve the stable timeout identity across the isolated IPC boundary.
				if result.Error == streamexec.ErrTimeout.Error() {
					return result, streamexec.ErrTimeout
				}
				if result.Error == streamexec.ErrOutput.Error() {
					return result, streamexec.ErrOutput
				}
				for _, known := range []error{streamexec.ErrSyntax, streamexec.ErrInput, streamexec.ErrExecution, streamexec.ErrSourceDenied} {
					if result.Error == known.Error() {
						return result, known
					}
				}
				return result, streamexec.ErrExecution
			}
			if err := validateProgramResult(request, result); err != nil {
				return streamexec.ProgramResult{}, err
			}
			return result, nil
		}
		if request.CheckOnly || message.Kind != "program-call" || request.ExpectedManifest == nil ||
			seen[message.ProgramCall.ID] || !streamexec.AllowsProgramCall(*request.ExpectedManifest, *message.ProgramCall) {
			return streamexec.ProgramResult{}, streamexec.ErrSourceDenied
		}
		call := *message.ProgramCall
		if request.ExpectedManifest.ReadOnlyRequest(request.EntryID) && !streamexec.ReadOnlyProgramCall(call) {
			return streamexec.ProgramResult{}, streamexec.ErrSourceDenied
		}
		seen[call.ID] = true
		reply := streamexec.SourceReply{ID: call.ID, Error: streamexec.ErrSourceCall.Error()}
		if host != nil {
			data, err := host(operationCtx, call)
			if errors.Is(operationCtx.Err(), context.DeadlineExceeded) {
				return streamexec.ProgramResult{}, streamexec.ErrTimeout
			}
			if err == nil && len(data) > 0 && len(data) <= streamexec.MaxProgramDataBytes && json.Valid(data) {
				reply.Data, reply.Error = data, ""
			}
		}
		if err := streamexec.WriteExchange(writer, streamexec.Exchange{Kind: "program-reply", ProgramReply: &reply}); err != nil {
			return streamexec.ProgramResult{}, sourcestream.ErrUnknownResult
		}
	}
}

func validateProgramResult(request streamexec.ProgramRequest, result streamexec.ProgramResult) error {
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > streamexec.MaxProgramDataBytes {
		return streamexec.ErrOutput
	}
	if request.ExpectedManifest != nil {
		expected, _ := json.Marshal(request.ExpectedManifest)
		actual, _ := json.Marshal(result.Manifest)
		if string(expected) != string(actual) {
			return streamexec.ErrOutput
		}
	}
	if request.CheckOnly && (len(result.Outputs) > 0 || len(result.Data) > 0 || len(result.Logs) > 0) {
		return streamexec.ErrOutput
	}
	if result.Manifest.ValidateRequestEffects(request.EntryID, result) != nil {
		return streamexec.ErrOutput
	}
	allowed := map[string]streamexec.ProgramTopic{}
	for _, topic := range result.Manifest.Topics {
		if !topic.Valid() {
			return streamexec.ErrOutput
		}
		allowed[topic.Key()] = topic
	}
	for _, output := range result.Outputs {
		ref := output.UNSReference()
		declared, ok := allowed[ref.Key()]
		if !ref.ValidIdentity() || !ok || !declared.Writable() || declared.Canonical() != ref.Canonical() || declared.TopicID != output.TopicID || declared.CollectionID != output.CollectionID || len(output.Data) == 0 || !json.Valid(output.Data) {
			return streamexec.ErrOutput
		}
	}
	return nil
}

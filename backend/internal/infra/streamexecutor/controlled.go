package streamexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"backend/internal/domain/sourcestream"
	"backend/internal/streamexec"
)

// TransformWithSources 由 Backend 提供本次调用的 host；能力清单不是脚本授权凭证。
func (c *Client) TransformWithSources(ctx context.Context, script sourcestream.ScriptDefinition, input any, runContext map[string]any, grants []streamexec.SourceGrant, host streamexec.SourceHost) (sourcestream.ScriptResult, error) {
	data, err := json.Marshal(input)
	if err != nil || len(data) > streamexec.MaxJSONBytes {
		return sourcestream.ScriptResult{}, streamexec.ErrInput
	}
	contextJSON, err := json.Marshal(runContext)
	if err != nil || len(contextJSON) > streamexec.MaxJSONBytes {
		return sourcestream.ScriptResult{}, streamexec.ErrInput
	}
	request := streamexec.Request{Code: script.Code, Input: data, Context: contextJSON, TimeoutMs: script.TimeoutMs, Sources: grants}
	result, err := c.executeControlled(ctx, request, host)
	if err != nil {
		return sourcestream.ScriptResult{}, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return sourcestream.ScriptResult{}, streamexec.ErrOutput
	}
	var output sourcestream.ScriptResult
	if json.Unmarshal(encoded, &output) != nil {
		return output, streamexec.ErrOutput
	}
	return output, nil
}

func (c *Client) executeControlled(parent context.Context, request streamexec.Request, host streamexec.SourceHost) (streamexec.Result, error) {
	if !streamexec.ValidSourceGrants(request.Sources) || request.TimeoutMs < 1 || request.TimeoutMs > streamexec.MaxTimeoutMs {
		return streamexec.Result{}, streamexec.ErrInput
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(request.TimeoutMs)*time.Millisecond+time.Second)
	defer cancel()
	client, release, err := c.acquire(ctx)
	if err != nil {
		return streamexec.Result{}, err
	}
	defer release()
	operationCtx, operationCancel := context.WithTimeout(parent, time.Duration(request.TimeoutMs)*time.Millisecond)
	defer operationCancel()
	reader, writer, closePipe := controlledPipe(ctx)
	defer closePipe()
	initial := make(chan error, 1)
	go func() {
		err := streamexec.WriteExchange(writer, streamexec.Exchange{Kind: "execute", Request: &request})
		if err != nil {
			_ = writer.CloseWithError(err)
		}
		initial <- err
	}()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://executor/execute-controlled", reader)
	if err != nil {
		return streamexec.Result{}, err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return streamexec.Result{}, streamexec.ErrTimeout
		}
		return streamexec.Result{}, sourcestream.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return streamexec.Result{}, sourcestream.ErrUnavailable
	}
	if err := <-initial; err != nil {
		return streamexec.Result{}, err
	}
	exchange := streamexec.NewExchangeReader(io.LimitReader(resp.Body, int64((streamexec.MaxSourceCalls+3)*3*streamexec.MaxJSONBytes)))
	calls := 0
	for {
		message, err := exchange.Read()
		if err != nil {
			if ctx.Err() != nil {
				return streamexec.Result{}, streamexec.ErrTimeout
			}
			return streamexec.Result{}, streamexec.ErrOutput
		}
		if message.Kind == "result" {
			if err := completeDuplex(writer, resp.Body, func() error { _, err := exchange.Read(); return err }); err != nil {
				if ctx.Err() != nil {
					return streamexec.Result{}, streamexec.ErrTimeout
				}
				return streamexec.Result{}, err
			}
			if message.Result.Error != "" {
				return *message.Result, errors.New(message.Result.Error)
			}
			return *message.Result, nil
		}
		calls++
		if message.Kind != "source-read" || calls > streamexec.MaxSourceCalls || message.Call.ID != calls || !streamexec.AllowsSourceRead(request.Sources, *message.Call) {
			return streamexec.Result{}, streamexec.ErrSourceDenied
		}
		reply := streamexec.SourceReply{ID: calls, Error: streamexec.ErrSourceCall.Error()}
		if host != nil {
			data, err := host(operationCtx, *message.Call)
			if err == nil && len(data) <= streamexec.MaxJSONBytes && json.Valid(data) {
				reply.Data, reply.Error = data, ""
			}
		}
		if err := streamexec.WriteExchange(writer, streamexec.Exchange{Kind: "source-reply", Reply: &reply}); err != nil {
			return streamexec.Result{}, streamexec.ErrSourceCall
		}
	}
}

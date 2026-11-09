package streamexecutor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"backend/internal/domain/sourcestream"
	"backend/internal/streamexec"
)

type Client struct {
	http      *http.Client
	slots     []*http.Client
	health    []*http.Client
	available chan *http.Client
	admission chan struct{}
}

func New(socket string) *Client {
	paths := strings.Split(socket, ",")
	if len(paths) > 16 {
		paths = []string{""}
	}
	client := &Client{available: make(chan *http.Client, len(paths)), admission: make(chan struct{}, len(paths)*3)}
	seen := map[string]bool{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if seen[path] || !filepath.IsAbs(path) {
			path = ""
		}
		seen[path] = true
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			if path == "" {
				return nil, sourcestream.ErrUnavailable
			}
			return (&net.Dialer{}).DialContext(ctx, "unix", path)
		}, MaxConnsPerHost: 1, IdleConnTimeout: 10 * time.Second}
		httpClient := &http.Client{Transport: transport, Timeout: streamexec.MaxExecutorTransportTimeout}
		client.slots = append(client.slots, httpClient)
		// A busy execution owns its slot's single connection. Health checks need
		// an independent, bounded pool to avoid mistaking admission for failure.
		client.health = append(client.health, &http.Client{Transport: transport.Clone(), Timeout: time.Second})
		client.available <- httpClient
	}
	client.http = client.slots[0]
	return client
}

// acquire bounds both executing jobs and waiters; slots are prebuilt, never created per script.
func (c *Client) acquire(ctx context.Context) (*http.Client, func(), error) {
	if c.available == nil {
		return c.http, func() {}, nil
	}
	select {
	case c.admission <- struct{}{}:
	default:
		return nil, nil, sourcestream.ErrUnavailable
	}
	select {
	case client := <-c.available:
		return client, func() { c.available <- client; <-c.admission }, nil
	case <-ctx.Done():
		<-c.admission
		return nil, nil, ctx.Err()
	}
}
func (c *Client) Ready(ctx context.Context) error {
	if len(c.health) > 0 {
		for _, client := range c.health {
			if err := executorReady(ctx, client); err != nil {
				return err
			}
		}
		return nil
	}
	return executorReady(ctx, c.http)
}
func executorReady(ctx context.Context, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://executor/readyz", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return sourcestream.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return sourcestream.ErrUnavailable
	}
	return nil
}
func (c *Client) execute(ctx context.Context, request streamexec.Request) (streamexec.Result, error) {
	client, release, err := c.acquire(ctx)
	if err != nil {
		return streamexec.Result{}, err
	}
	defer release()
	input, err := json.Marshal(request)
	if err != nil {
		return streamexec.Result{}, streamexec.ErrInput
	}
	if len(input) > 3*streamexec.MaxJSONBytes {
		return streamexec.Result{}, streamexec.ErrInput
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://executor/execute", bytes.NewReader(input))
	if err != nil {
		return streamexec.Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return streamexec.Result{}, sourcestream.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return streamexec.Result{}, sourcestream.ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, streamexec.MaxJSONBytes+16*1024+1))
	if err != nil || len(raw) > streamexec.MaxJSONBytes+16*1024 {
		return streamexec.Result{}, streamexec.ErrOutput
	}
	var result streamexec.Result
	if err = json.Unmarshal(raw, &result); err != nil {
		return result, streamexec.ErrOutput
	}
	if result.Error != "" {
		return result, errors.New(result.Error)
	}
	return result, nil
}
func (c *Client) Check(ctx context.Context, script sourcestream.ScriptDefinition) error {
	_, err := c.execute(ctx, streamexec.Request{Code: script.Code, TimeoutMs: script.TimeoutMs, CheckOnly: true})
	return err
}
func (c *Client) Transform(ctx context.Context, script sourcestream.ScriptDefinition, input any, runContext map[string]any) (sourcestream.ScriptResult, error) {
	return c.TransformWithSources(ctx, script, input, runContext, nil, nil)
}
func (c *Client) TransformWithLegacySources(ctx context.Context, script sourcestream.ScriptDefinition, input any, runContext map[string]any, aliases []string, read sourcestream.SourceReader) (sourcestream.ScriptResult, error) {
	data, err := json.Marshal(input)
	if err != nil || len(data) > streamexec.MaxJSONBytes {
		return sourcestream.ScriptResult{}, streamexec.ErrInput
	}
	contextJSON, err := json.Marshal(runContext)
	if err != nil || len(contextJSON) > streamexec.MaxJSONBytes {
		return sourcestream.ScriptResult{}, streamexec.ErrInput
	}
	result, err := c.executeLegacy(ctx, streamexec.Request{Code: script.Code, TimeoutMs: script.TimeoutMs, Input: data, Context: contextJSON, LegacySources: aliases}, read)
	if err != nil {
		return sourcestream.ScriptResult{}, err
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > streamexec.MaxJSONBytes+16*1024 {
		return sourcestream.ScriptResult{}, streamexec.ErrOutput
	}
	var output sourcestream.ScriptResult
	err = json.Unmarshal(encoded, &output)
	return output, err
}
func (c *Client) executeLegacy(ctx context.Context, request streamexec.Request, read sourcestream.SourceReader) (streamexec.Result, error) {
	if len(request.Code) == 0 || len(request.Code) > streamexec.MaxCodeBytes || request.TimeoutMs < 1 || request.TimeoutMs > streamexec.MaxTimeoutMs || len(request.LegacySources) > streamexec.MaxLegacySourceCalls {
		return streamexec.Result{}, streamexec.ErrInput
	}
	allowed := map[string]bool{}
	for _, alias := range request.LegacySources {
		if len(alias) > 1024 {
			return streamexec.Result{}, streamexec.ErrInput
		}
		allowed[alias] = true
	}
	client, release, err := c.acquire(ctx)
	if err != nil {
		return streamexec.Result{}, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(request.TimeoutMs)*time.Millisecond)
	defer cancel()
	if request.LegacySources == nil {
		request.LegacySources = []string{}
	}
	body, writer, closePipe := controlledPipe(ctx)
	defer closePipe()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://executor/execute-sources", body)
	if err != nil {
		return streamexec.Result{}, err
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	written := make(chan error, 1)
	go func() {
		err := json.NewEncoder(writer).Encode(request)
		written <- err
		if err != nil {
			_ = writer.CloseWithError(err)
		}
	}()
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return streamexec.Result{}, streamexec.ErrTimeout
		}
		return streamexec.Result{}, sourcestream.ErrUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return streamexec.Result{}, sourcestream.ErrUnavailable
	}
	if err = <-written; err != nil {
		return streamexec.Result{}, streamexec.ErrExecution
	}
	frames := streamexec.NewFrameReader(response.Body)
	for calls := 0; calls <= streamexec.MaxLegacySourceCalls; calls++ {
		var frame streamexec.Frame
		if frames.Decode(&frame) != nil {
			if ctx.Err() != nil {
				return streamexec.Result{}, streamexec.ErrTimeout
			}
			return streamexec.Result{}, streamexec.ErrExecution
		}
		if frame.Type == "result" && frame.Result != nil {
			if err := completeDuplex(writer, response.Body, func() error {
				var extra streamexec.Frame
				return frames.Decode(&extra)
			}); err != nil {
				if ctx.Err() != nil {
					return streamexec.Result{}, streamexec.ErrTimeout
				}
				return streamexec.Result{}, err
			}
			if frame.Result.Error != "" {
				return streamexec.Result{}, errors.New(frame.Result.Error)
			}
			return *frame.Result, nil
		}
		if frame.Type != "source" || frame.Call == nil || read == nil || !allowed[frame.Call.Alias] || calls == streamexec.MaxLegacySourceCalls || len(frame.Call.Options) > streamexec.MaxJSONBytes || frame.Call.TimeoutMs < 1 || frame.Call.TimeoutMs > int(streamexec.MaxSourceCallTimeout/time.Millisecond) {
			return streamexec.Result{}, streamexec.ErrSourceAccess
		}
		callCtx, callCancel := context.WithTimeout(ctx, time.Duration(frame.Call.TimeoutMs)*time.Millisecond)
		data, callErr := read(callCtx, frame.Call.Alias, frame.Call.Options)
		reply := streamexec.LegacySourceReply{Data: data}
		if callCtx.Err() != nil {
			reply = streamexec.LegacySourceReply{Error: streamexec.ErrSourceReadTimeout.Error()}
		} else if callErr != nil {
			reply = streamexec.LegacySourceReply{Error: streamexec.ErrSourceRead.Error()}
		}
		callCancel()
		if len(reply.Data) > streamexec.MaxJSONBytes {
			return streamexec.Result{}, streamexec.ErrOutput
		}
		if json.NewEncoder(writer).Encode(reply) != nil {
			return streamexec.Result{}, streamexec.ErrExecution
		}
	}
	return streamexec.Result{}, streamexec.ErrExecution
}

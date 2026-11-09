package scripttest

import (
	"backend/internal/domain/sourcestream"
	"backend/internal/streamexec"
	"context"
	"encoding/json"
	"testing"
)

// TrustedScriptExecutor runs repository-owned fixtures only, never user scripts.
// Production must use the isolated streamexecutor client.
type TrustedScriptExecutor struct{ capacity chan struct{} }

func New(t testing.TB) *TrustedScriptExecutor {
	t.Helper()
	return &TrustedScriptExecutor{capacity: make(chan struct{}, 4)}
}

func (e *TrustedScriptExecutor) ExecuteProgram(ctx context.Context, request streamexec.ProgramRequest, host streamexec.ProgramHost) (streamexec.ProgramResult, error) {
	select {
	case e.capacity <- struct{}{}:
		defer func() { <-e.capacity }()
	case <-ctx.Done():
		return streamexec.ProgramResult{}, ctx.Err()
	default:
		return streamexec.ProgramResult{}, sourcestream.ErrUnavailable
	}
	return streamexec.ExecuteProgram(ctx, request, host)
}
func (e *TrustedScriptExecutor) Check(ctx context.Context, script sourcestream.ScriptDefinition) error {
	_, err := e.execute(ctx, streamexec.Request{Code: script.Code, TimeoutMs: script.TimeoutMs, CheckOnly: true}, nil)
	return err
}
func (e *TrustedScriptExecutor) execute(ctx context.Context, request streamexec.Request, read streamexec.SourceReader) (streamexec.Result, error) {
	select {
	case e.capacity <- struct{}{}:
		defer func() { <-e.capacity }()
	case <-ctx.Done():
		return streamexec.Result{}, ctx.Err()
	default:
		return streamexec.Result{}, sourcestream.ErrUnavailable
	}
	return streamexec.ExecuteWithLegacySources(ctx, request, read)
}
func (e *TrustedScriptExecutor) Transform(ctx context.Context, script sourcestream.ScriptDefinition, input any, runContext map[string]any) (sourcestream.ScriptResult, error) {
	return e.TransformWithLegacySources(ctx, script, input, runContext, nil, nil)
}

// Keep the declared-grant SDK separate from the older read-only alias bridge.
func (e *TrustedScriptExecutor) TransformWithSources(ctx context.Context, script sourcestream.ScriptDefinition, input any, runContext map[string]any, grants []streamexec.SourceGrant, host streamexec.SourceHost) (sourcestream.ScriptResult, error) {
	select {
	case e.capacity <- struct{}{}:
		defer func() { <-e.capacity }()
	case <-ctx.Done():
		return sourcestream.ScriptResult{}, ctx.Err()
	default:
		return sourcestream.ScriptResult{}, sourcestream.ErrUnavailable
	}
	data, err := json.Marshal(input)
	if err != nil {
		return sourcestream.ScriptResult{}, streamexec.ErrInput
	}
	contextJSON, err := json.Marshal(runContext)
	if err != nil {
		return sourcestream.ScriptResult{}, streamexec.ErrInput
	}
	result, err := streamexec.ExecuteWithSources(ctx, streamexec.Request{Code: script.Code, Input: data, Context: contextJSON, TimeoutMs: script.TimeoutMs, Sources: grants}, host)
	if err != nil {
		return sourcestream.ScriptResult{}, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return sourcestream.ScriptResult{}, streamexec.ErrOutput
	}
	var output sourcestream.ScriptResult
	if err = json.Unmarshal(encoded, &output); err != nil {
		return output, streamexec.ErrOutput
	}
	return output, nil
}

func (e *TrustedScriptExecutor) TransformWithLegacySources(ctx context.Context, script sourcestream.ScriptDefinition, input any, runContext map[string]any, aliases []string, read sourcestream.SourceReader) (sourcestream.ScriptResult, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return sourcestream.ScriptResult{}, streamexec.ErrInput
	}
	contextJSON, err := json.Marshal(runContext)
	if err != nil {
		return sourcestream.ScriptResult{}, streamexec.ErrInput
	}
	result, err := e.execute(ctx, streamexec.Request{Code: script.Code, Input: data, Context: contextJSON, TimeoutMs: script.TimeoutMs, LegacySources: aliases}, func(ctx context.Context, alias string, options json.RawMessage) (json.RawMessage, error) {
		if read == nil {
			return nil, streamexec.ErrSourceAccess
		}
		data, err := read(ctx, alias, options)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, streamexec.ErrSourceRead
		}
		return data, nil
	})
	if err != nil {
		return sourcestream.ScriptResult{}, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return sourcestream.ScriptResult{}, streamexec.ErrOutput
	}
	var output sourcestream.ScriptResult
	if err = json.Unmarshal(encoded, &output); err != nil {
		return output, streamexec.ErrOutput
	}
	return output, nil
}

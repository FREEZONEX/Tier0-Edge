package sourcestream

// 预览采用提供的游标快照，缓冲更新不落库，返回 JSON 不共享可变内存。
import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

func TestProgramCheckpointBufferPreviewAndCopy(t *testing.T) {
	ctx, b := withProgramCheckpoint(context.Background(), json.RawMessage(`{"page":8}`))
	require.Same(t, b, ctx.Value(programCheckpointKey{}))
	require.Nil(t, b.result())
	value, err := b.call(ctx, streamexec.ProgramCall{Kind: "checkpoint.get"}, "", "poll", true)
	require.NoError(t, err)
	require.JSONEq(t, `{"page":8}`, string(value))
	value[0] = 'x'
	input := json.RawMessage(`{"page":9}`)
	value, err = b.call(ctx, streamexec.ProgramCall{Kind: "checkpoint.set", Input: input}, "", "poll", true)
	require.NoError(t, err)
	require.JSONEq(t, `{"stage":"buffered"}`, string(value))
	input[0] = 'x'
	result := b.result()
	require.JSONEq(t, `{"page":9}`, string(result.Value))
	require.Zero(t, result.Revision)
	result.Value[0] = 'x'
	require.JSONEq(t, `{"page":9}`, string(b.result().Value))
	_, err = b.call(ctx, streamexec.ProgramCall{Kind: "checkpoint.set", Input: json.RawMessage(`"` + strings.Repeat("x", streamexec.MaxCheckpointBytes) + `"`)}, "", "poll", true)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	_, err = b.call(ctx, streamexec.ProgramCall{Kind: "checkpoint.set", Input: json.RawMessage(`invalid`)}, "", "poll", true)
	require.ErrorIs(t, err, repo.ErrInvalidArgument)
	_, empty := withProgramCheckpoint(context.Background(), nil)
	value, err = empty.call(ctx, streamexec.ProgramCall{Kind: "checkpoint.get"}, "", "poll", true)
	require.NoError(t, err)
	require.JSONEq(t, `null`, string(value))
}

func TestProgramCheckpointSensitiveValueRedaction(t *testing.T) {
	ctx, guard := WithProgramSecretGuard(context.Background())
	recordProgramSecretValues(ctx, map[string]string{"token": "test-checkpoint-secret"})
	result := streamexec.ProgramResult{Checkpoint: &streamexec.ProgramCheckpoint{Value: json.RawMessage(`{"cursor":"test-checkpoint-secret"}`)}}
	guard.RedactResult(&result)
	require.JSONEq(t, `{"cursor":"[REDACTED]"}`, string(result.Checkpoint.Value))
}

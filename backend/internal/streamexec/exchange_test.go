package streamexec

// 验证跨进程帧的边界；混合消息、超大消息和错序回执均拒绝。
import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExchangeFrameValidation(t *testing.T) {
	for _, raw := range []string{
		`broken`, `{}`, `{"kind":"unknown","result":{}}`,
		`{"kind":"execute","result":{}}`,
		`{"kind":"result","result":{},"call":{}}`,
		strings.Repeat("x", MaxExchangeBytes),
	} {
		_, err := NewExchangeReader(strings.NewReader(raw + "\n")).Read()
		require.ErrorIs(t, err, ErrInput)
	}
	_, err := NewExchangeReader(strings.NewReader("")).Read()
	require.ErrorIs(t, err, io.EOF)
	var output bytes.Buffer
	require.NoError(t, WriteExchange(&output, Exchange{Kind: "result", Result: &Result{Drop: true}}))
	frame, err := NewExchangeReader(&output).Read()
	require.NoError(t, err)
	require.True(t, frame.Result.Drop)
}

func TestControlledChildExchange(t *testing.T) {
	request := sourceRequest(`return {data:sources.get("boiler").read("opcua:temperature"),drop:false};`)
	var input, output bytes.Buffer
	require.NoError(t, WriteExchange(&input, Exchange{Kind: "execute", Request: &request}))
	require.NoError(t, WriteExchange(&input, Exchange{Kind: "source-reply", Reply: &SourceReply{ID: 1, Data: json.RawMessage(`{"value":27}`)}}))
	require.NoError(t, RunControlledChild(context.Background(), &input, &output))
	reader := NewExchangeReader(&output)
	frame, err := reader.Read()
	require.NoError(t, err)
	require.Equal(t, "source-read", frame.Kind)
	require.Equal(t, 1, frame.Call.ID)
	frame, err = reader.Read()
	require.NoError(t, err)
	require.JSONEq(t, `{"value":27}`, string(frame.Result.Data))
	// 错序回执不能作为另一条 Source 请求的结果使用。
	input.Reset()
	output.Reset()
	require.NoError(t, WriteExchange(&input, Exchange{Kind: "execute", Request: &request}))
	require.NoError(t, WriteExchange(&input, Exchange{Kind: "source-reply", Reply: &SourceReply{ID: 2, Data: json.RawMessage(`{}`)}}))
	require.NoError(t, RunControlledChild(context.Background(), &input, &output))
	reader = NewExchangeReader(&output)
	_, err = reader.Read()
	require.NoError(t, err)
	frame, err = reader.Read()
	require.NoError(t, err)
	require.Equal(t, ErrExecution.Error(), frame.Result.Error)
}

package streamexecutor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

func TestCompleteDuplexRequiresDecoderAndResponseEOF(t *testing.T) {
	for _, test := range []struct {
		name        string
		decoderErr  error
		response    string
		wantFailure bool
	}{
		{name: "complete", decoderErr: io.EOF},
		{name: "extra frame", decoderErr: nil, wantFailure: true},
		{name: "broken frame", decoderErr: io.ErrUnexpectedEOF, wantFailure: true},
		{name: "budget EOF is not response EOF", decoderErr: io.EOF, response: "x", wantFailure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader, writer := io.Pipe()
			defer reader.Close()
			defer writer.Close()
			err := completeDuplex(writer, strings.NewReader(test.response), func() error { return test.decoderErr })
			if test.wantFailure {
				require.ErrorIs(t, err, streamexec.ErrOutput)
			} else {
				require.NoError(t, err)
			}
			var one [1]byte
			_, err = reader.Read(one[:])
			require.ErrorIs(t, err, io.EOF, "terminal processing must finish the request before awaiting the response")
		})
	}
}

func TestDuplexFinalFrameDoesNotBypassDeadline(t *testing.T) {
	for _, path := range []string{"program-v2", "controlled-v1"} {
		t.Run(path, func(t *testing.T) {
			var requests atomic.Int32
			stopHandler := make(chan struct{})
			client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, http.NewResponseController(w).EnableFullDuplex())
				w.Header().Set("Content-Type", "application/x-ndjson")
				require.NoError(t, streamexec.RunControlledChild(r.Context(), r.Body, duplexFlushWriter{w}))
				_, _ = io.Copy(io.Discard, r.Body)
				if requests.Add(1) == 1 {
					// 最终帧已发出，但响应故意不结束；原调用 deadline 必须仍然有效。
					select {
					case <-r.Context().Done():
					case <-stopHandler:
					}
				}
			})
			t.Cleanup(func() { close(stopHandler) })
			invoke := duplexTestInvoker(t, client, path, 1000)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			started := time.Now()
			err := invoke(ctx)
			require.ErrorIs(t, err, streamexec.ErrTimeout)
			require.Less(t, time.Since(started), 700*time.Millisecond)
			require.Empty(t, client.admission)
			require.Len(t, client.available, 1)
			recovery, stop := context.WithTimeout(context.Background(), time.Second)
			defer stop()
			require.NoError(t, invoke(recovery), "deadline failure must release the slot without retrying the original execution")
			require.EqualValues(t, 2, requests.Load(), "one failed execution and one explicit recovery, no automatic retry")
		})
	}
}

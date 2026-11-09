package streamexecutor

import (
	"context"
	"errors"
	"io"

	"backend/internal/streamexec"
)

// 最终帧先于服务端释放槽位；结束请求并读到响应 EOF 后才能归还本地槽位。
// readNext 复用原有有界解码器及请求截止时间，不重放任何执行。
func completeDuplex(writer *io.PipeWriter, response io.Reader, readNext func() error) error {
	if err := writer.Close(); err != nil {
		return streamexec.ErrOutput
	}
	if err := readNext(); !errors.Is(err, io.EOF) {
		return streamexec.ErrOutput
	}
	// 解码器可能包着 LimitReader；它的预算 EOF 不能冒充 HTTP 响应 EOF。
	var tail [1]byte
	if _, err := io.ReadFull(response, tail[:]); !errors.Is(err, io.EOF) {
		return streamexec.ErrOutput
	}
	return nil
}

// A duplex request remains open for host replies. Cancel must close the pipe
// independently of Do returning: the HTTP transport waits for its body writer.
func controlledPipe(ctx context.Context) (*io.PipeReader, *io.PipeWriter, func()) {
	reader, writer := io.Pipe()
	stop := context.AfterFunc(ctx, func() {
		_ = reader.CloseWithError(ctx.Err())
		_ = writer.CloseWithError(ctx.Err())
	})
	return reader, writer, func() {
		stop()
		_ = reader.Close()
		_ = writer.Close()
	}
}

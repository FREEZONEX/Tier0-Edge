package streamexec

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
)

// Exchange 使用有界 NDJSON；脚本子进程与 Backend 之间不传凭证或任意 URL。
type Exchange struct {
	Kind           string          `json:"kind"`
	Request        *Request        `json:"request,omitempty"`
	Call           *SourceCall     `json:"call,omitempty"`
	Reply          *SourceReply    `json:"reply,omitempty"`
	Result         *Result         `json:"result,omitempty"`
	ProgramRequest *ProgramRequest `json:"programRequest,omitempty"`
	ProgramCall    *ProgramCall    `json:"programCall,omitempty"`
	ProgramReply   *SourceReply    `json:"programReply,omitempty"`
	ProgramResult  *ProgramResult  `json:"programResult,omitempty"`
}

type SourceReply struct {
	ID    int             `json:"id"`
	Data  json.RawMessage `json:"data,omitempty"`
	Error string          `json:"error,omitempty"`
}

type ExchangeReader struct{ scanner *bufio.Scanner }

func NewExchangeReader(reader io.Reader) *ExchangeReader {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), MaxExchangeBytes)
	return &ExchangeReader{scanner: scanner}
}

func (r *ExchangeReader) Read() (Exchange, error) {
	var message Exchange
	if !r.scanner.Scan() {
		if r.scanner.Err() != nil {
			return message, ErrInput
		}
		return message, io.EOF
	}
	if json.Unmarshal(r.scanner.Bytes(), &message) != nil {
		return message, ErrInput
	}
	fields := 0
	for _, present := range []bool{message.Request != nil, message.Call != nil, message.Reply != nil, message.Result != nil, message.ProgramRequest != nil, message.ProgramCall != nil, message.ProgramReply != nil, message.ProgramResult != nil} {
		if present {
			fields++
		}
	}
	if fields != 1 ||
		(message.Kind == "execute") != (message.Request != nil) ||
		(message.Kind == "source-read") != (message.Call != nil) ||
		(message.Kind == "source-reply") != (message.Reply != nil) ||
		(message.Kind == "result") != (message.Result != nil) ||
		(message.Kind == "program-execute") != (message.ProgramRequest != nil) ||
		(message.Kind == "program-call") != (message.ProgramCall != nil) ||
		(message.Kind == "program-reply") != (message.ProgramReply != nil) ||
		(message.Kind == "program-result") != (message.ProgramResult != nil) {
		return Exchange{}, ErrInput
	}
	return message, nil
}

func WriteExchange(writer io.Writer, message Exchange) error {
	raw, err := json.Marshal(message)
	if err != nil || len(raw) >= MaxExchangeBytes-1 {
		return ErrOutput
	}
	n, err := writer.Write(append(raw, '\n'))
	if err == nil && n != len(raw)+1 {
		return io.ErrShortWrite
	}
	return err
}

// RunControlledChild 只依赖父进程的管道，不能自行连接 Backend 或设备。
func RunControlledChild(ctx context.Context, input io.Reader, output io.Writer) error {
	reader := NewExchangeReader(input)
	message, err := reader.Read()
	if err == nil && message.Kind == "program-execute" {
		return runProgramChild(ctx, reader, output, *message.ProgramRequest)
	}
	if err != nil || message.Kind != "execute" {
		return ErrInput
	}
	result, executeErr := ExecuteWithSources(ctx, *message.Request, func(ctx context.Context, call SourceCall) (json.RawMessage, error) {
		if err := WriteExchange(output, Exchange{Kind: "source-read", Call: &call}); err != nil {
			return nil, ErrSourceCall
		}
		reply, err := reader.Read()
		if err != nil || reply.Kind != "source-reply" || reply.Reply.ID != call.ID || reply.Reply.Error != "" {
			return nil, ErrSourceCall
		}
		return reply.Reply.Data, nil
	})
	if executeErr != nil {
		result = Result{Error: executeErr.Error()}
	}
	return WriteExchange(output, Exchange{Kind: "result", Result: &result})
}

func runProgramChild(ctx context.Context, reader *ExchangeReader, output io.Writer, request ProgramRequest) error {
	// The duplex pipe is serialized; async JavaScript still settles on its own VM loop.
	// No Goja values or Promise callbacks are accessed by the IPC goroutines.
	var pipe sync.Mutex
	var writer sync.Mutex
	write := func(message Exchange) error {
		writer.Lock()
		defer writer.Unlock()
		return WriteExchange(output, message)
	}
	result, executeErr := ExecuteProgram(ctx, request, func(ctx context.Context, call ProgramCall) (json.RawMessage, error) {
		pipe.Lock()
		defer pipe.Unlock()
		if ctx.Err() != nil {
			return nil, ErrTimeout
		}
		if err := write(Exchange{Kind: "program-call", ProgramCall: &call}); err != nil {
			return nil, ErrSourceCall
		}
		reply, err := reader.Read()
		if err != nil || reply.Kind != "program-reply" || reply.ProgramReply.ID != call.ID || reply.ProgramReply.Error != "" {
			return nil, ErrSourceCall
		}
		return reply.ProgramReply.Data, nil
	})
	if executeErr != nil {
		result = ProgramResult{Error: executeErr.Error()}
	}
	// A timed-out VM may finish while the host goroutine is waiting for its
	// reply. Serialize only writes: locking the whole pipe here would deadlock.
	return write(Exchange{Kind: "program-result", ProgramResult: &result})
}

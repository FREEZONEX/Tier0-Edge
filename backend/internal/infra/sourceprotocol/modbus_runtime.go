package sourceprotocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"backend/internal/domain/sourcestream"
)

const modbusWireLimit = 1024 * 1024

type modbusRequest struct {
	SourceID    int64                         `json:"sourceId"`
	Revision    int64                         `json:"configRevision"`
	Definition  sourcestream.SourceDefinition `json:"definition"`
	Credential  sourcestream.Credential       `json:"credential"`
	ResourceRef string                        `json:"resourceRef,omitempty"`
	Input       any                           `json:"input,omitempty"`
	Parent      string                        `json:"parent,omitempty"`
	Deadline    int64                         `json:"deadline,omitempty"`
}

type modbusFrame struct {
	Data  json.RawMessage `json:"data,omitempty"`
	Event *Input          `json:"event,omitempty"`
	Ready bool            `json:"ready,omitempty"`
	Error string          `json:"error,omitempty"`
}

func modbusErrorCode(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, sourcestream.ErrUnknownResult):
		return "unknown"
	case errors.Is(err, sourcestream.ErrUnavailable):
		return "unavailable"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "protocol"
	}
}

func modbusWireError(code string) error {
	switch code {
	case "":
		return nil
	case "unknown":
		return sourcestream.ErrUnknownResult
	case "unavailable":
		return sourcestream.ErrUnavailable
	case "timeout":
		return context.DeadlineExceeded
	case "canceled":
		return context.Canceled
	default:
		return ErrProtocol
	}
}

// ModbusRuntime owns protocol connections only. It has no Store, script or
// Docker access. Management is served exclusively on a private Unix socket.
type ModbusRuntime struct {
	runtime *Runtime
	ops     chan struct{}
	watches chan struct{}
}

func NewModbusRuntime() *ModbusRuntime {
	r := New()
	r.Register(sourcestream.ModbusTCP, ModbusDriver{})
	return &ModbusRuntime{runtime: r, ops: make(chan struct{}, 32), watches: make(chan struct{}, sourcePoolLimit)}
}

func (s *ModbusRuntime) Close() { s.runtime.Close() }

func (s *ModbusRuntime) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	controller := http.NewResponseController(w)
	reject := func(message string, status int) {
		w.Header().Set("Connection", "close")
		// Body.Close may drain unfinished chunked input before returning.
		// Cancel that read as well as marking the response non-reusable.
		_ = controller.SetReadDeadline(time.Now())
		http.Error(w, message, status)
	}
	if req.Method == http.MethodGet && req.URL.Path == "/readyz" {
		w.WriteHeader(http.StatusOK)
		return
	}
	watch := req.URL.Path == "/watch"
	if req.Method != http.MethodPost || (req.URL.Path != "/probe" && req.URL.Path != "/discover" && req.URL.Path != "/invoke" && req.URL.Path != "/release" && !watch) {
		reject("not found", http.StatusNotFound)
		return
	}
	slots := s.ops
	if watch {
		slots = s.watches
	}
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	default:
		reject("unavailable", http.StatusServiceUnavailable)
		return
	}
	if watch {
		if controller.EnableFullDuplex() != nil {
			reject("unsupported", http.StatusInternalServerError)
			return
		}
	}
	defer req.Body.Close()
	scanner := bufio.NewScanner(req.Body)
	scanner.Buffer(make([]byte, 4096), modbusWireLimit+1)
	_ = controller.SetReadDeadline(time.Now().Add(5 * time.Second))
	var input modbusRequest
	if !scanner.Scan() || len(scanner.Bytes()) > modbusWireLimit || json.Unmarshal(scanner.Bytes(), &input) != nil || input.SourceID < 0 || input.Revision < 0 {
		reject("invalid request", http.StatusBadRequest)
		return
	}
	if req.URL.Path == "/release" {
		if input.SourceID <= 0 {
			reject("invalid source", http.StatusBadRequest)
			return
		}
		s.runtime.ReleaseSource(input.SourceID)
		_ = json.NewEncoder(w).Encode(modbusFrame{})
		return
	}
	if input.Definition.Protocol != sourcestream.ModbusTCP || input.Definition.ConnectionRole() != sourcestream.ClientRole || input.Definition.Validate() != nil {
		reject("invalid source", http.StatusBadRequest)
		return
	}
	ctx := WithSourceRevision(req.Context(), input.Revision)
	deadline := time.Now().Add(30 * time.Second)
	if input.Deadline > 0 && time.UnixMilli(input.Deadline).Before(deadline) {
		deadline = time.UnixMilli(input.Deadline)
	}
	if !watch {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	write := func(frame modbusFrame) error {
		raw, err := json.Marshal(frame)
		if err != nil || len(raw) > modbusWireLimit {
			return ErrProtocol
		}
		_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err = w.Write(append(raw, '\n')); err != nil {
			return err
		}
		if watch {
			return controller.Flush()
		}
		return nil
	}
	if watch {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_ = controller.SetReadDeadline(time.Time{})
		ctx = sourcestream.WithWatchObserver(ctx, func(ready bool) {
			if ready {
				_ = write(modbusFrame{Ready: true})
			}
		})
		err := s.runtime.Watch(ctx, input.SourceID, input.Definition, input.Credential, func(event Input) error {
			if err := write(modbusFrame{Event: &event}); err != nil {
				return err
			}
			// One event in flight: next device read waits for durable acceptance
			// by Backend, not merely a successful Unix socket write.
			_ = controller.SetReadDeadline(time.Now().Add(30 * time.Second))
			if !scanner.Scan() || string(scanner.Bytes()) != "{\"ack\":true}" {
				return ErrProtocol
			}
			_ = controller.SetReadDeadline(time.Time{})
			return nil
		})
		_ = write(modbusFrame{Error: modbusErrorCode(err)})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var result any
	var err error
	switch req.URL.Path {
	case "/probe":
		err = s.runtime.Probe(ctx, input.SourceID, input.Definition, input.Credential)
	case "/discover":
		result, err = s.runtime.Discover(ctx, input.SourceID, input.Definition, input.Credential, input.Parent)
	case "/invoke":
		result, err = s.runtime.Invoke(ctx, input.SourceID, input.Definition, input.Credential, input.ResourceRef, input.Input)
	}
	raw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		err = ErrProtocol
	}
	_ = write(modbusFrame{Data: raw, Error: modbusErrorCode(err)})
}

// RemoteModbusDriver never dials devices and never falls back to a local driver.
type RemoteModbusDriver struct {
	http    *http.Client
	sources sync.Map
}

func NewRemoteModbus(socket string) *RemoteModbusDriver {
	return &RemoteModbusDriver{http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
		MaxConnsPerHost: sourcePoolLimit + 32, IdleConnTimeout: time.Minute,
	}}}
}

func (d *RemoteModbusDriver) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://source-modbus/readyz", nil)
	resp, err := d.http.Do(req)
	if err != nil {
		return sourcestream.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return sourcestream.ErrUnavailable
	}
	return nil
}

func modbusInput(ctx context.Context, id int64, def sourcestream.RuntimeSource, credential sourcestream.Credential) modbusRequest {
	revision, _ := ctx.Value(sourceRevisionKey{}).(int64)
	input := modbusRequest{SourceID: id, Revision: revision, Definition: def, Credential: credential}
	if deadline, ok := ctx.Deadline(); ok {
		input.Deadline = deadline.UnixMilli()
	}
	return input
}

func (d *RemoteModbusDriver) call(ctx context.Context, path string, input modbusRequest, output any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > modbusWireLimit {
		return ErrProtocol
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://source-modbus"+path, bytes.NewReader(append(raw, '\n')))
	resp, err := d.http.Do(req)
	if err != nil {
		if path == "/invoke" && modbusMutation(input.Input) {
			return sourcestream.ErrUnknownResult
		}
		return sourcestream.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return sourcestream.ErrUnavailable
	}
	raw, err = io.ReadAll(io.LimitReader(resp.Body, modbusWireLimit+1))
	var frame modbusFrame
	if err != nil || len(raw) > modbusWireLimit || json.Unmarshal(raw, &frame) != nil {
		if path == "/invoke" && modbusMutation(input.Input) {
			return sourcestream.ErrUnknownResult
		}
		return ErrProtocol
	}
	if err = modbusWireError(frame.Error); err != nil {
		return err
	}
	if input.SourceID > 0 && path != "/release" {
		d.sources.Store(input.SourceID, struct{}{})
	}
	if output != nil && json.Unmarshal(frame.Data, output) != nil {
		return ErrProtocol
	}
	return nil
}

func modbusMutation(input any) bool {
	raw, err := json.Marshal(input)
	var operation struct {
		Operation string `json:"operation"`
	}
	return err != nil || json.Unmarshal(raw, &operation) != nil || operation.Operation != "read"
}

func (d *RemoteModbusDriver) Probe(ctx context.Context, id int64, def sourcestream.RuntimeSource, c sourcestream.Credential) error {
	return d.call(ctx, "/probe", modbusInput(ctx, id, def, c), nil)
}
func (d *RemoteModbusDriver) Discover(ctx context.Context, id int64, def sourcestream.RuntimeSource, c sourcestream.Credential, parent string) ([]sourcestream.NativeResource, error) {
	input := modbusInput(ctx, id, def, c)
	input.Parent = parent
	var result []sourcestream.NativeResource
	err := d.call(ctx, "/discover", input, &result)
	return result, err
}
func (d *RemoteModbusDriver) Invoke(ctx context.Context, id int64, def sourcestream.RuntimeSource, c sourcestream.Credential, ref string, value any) (any, error) {
	input := modbusInput(ctx, id, def, c)
	input.ResourceRef, input.Input = ref, value
	var result any
	err := d.call(ctx, "/invoke", input, &result)
	return result, err
}
func (d *RemoteModbusDriver) ReleaseSource(id int64) {
	d.sources.Delete(id)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = d.call(ctx, "/release", modbusRequest{SourceID: id}, nil)
}
func (d *RemoteModbusDriver) Close() {
	d.sources.Range(func(key, value any) bool { d.ReleaseSource(key.(int64)); return true })
	d.http.CloseIdleConnections()
}
func (d *RemoteModbusDriver) Watch(ctx context.Context, id int64, def sourcestream.RuntimeSource, c sourcestream.Credential, emit func(Input) error) error {
	defer sourcestream.ReportWatchReady(ctx, false)
	if emit == nil {
		return ErrProtocol
	}
	raw, err := json.Marshal(modbusInput(ctx, id, def, c))
	if err != nil || len(raw) > modbusWireLimit {
		return ErrProtocol
	}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://source-modbus/watch", reader)
	initial := make(chan error, 1)
	go func() { _, err := writer.Write(append(raw, '\n')); initial <- err }()
	resp, err := d.http.Do(req)
	if err != nil {
		return sourcestream.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/x-ndjson" {
		return sourcestream.ErrUnavailable
	}
	if err := <-initial; err != nil {
		return err
	}
	d.sources.Store(id, struct{}{})
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), modbusWireLimit+1)
	for scanner.Scan() {
		var frame modbusFrame
		if len(scanner.Bytes()) > modbusWireLimit || json.Unmarshal(scanner.Bytes(), &frame) != nil {
			return ErrProtocol
		}
		if frame.Error != "" {
			return modbusWireError(frame.Error)
		}
		if frame.Ready {
			sourcestream.ReportWatchReady(ctx, true)
		}
		if frame.Event != nil {
			if frame.Event.ResourceRef == "" {
				return ErrProtocol
			}
			if err := emit(*frame.Event); err != nil {
				return err
			}
			if _, err := writer.Write([]byte("{\"ack\":true}\n")); err != nil {
				return err
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return sourcestream.ErrUnavailable
}

package sourceprotocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"backend/internal/domain/sourcestream"
	"github.com/zeromicro/go-zero/core/logx"
)

// OPC UA runs in the managed Node process, reachable only via a private socket.
// It receives a single Source's configuration, never a database or Docker client.
type OPCUADriver struct {
	http    *http.Client
	sources sync.Map
}

func NewOPCUA(socket string) *OPCUADriver {
	return &OPCUADriver{http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialOPCUA(ctx, socket)
		},
		MaxConnsPerHost: 128, IdleConnTimeout: 10 * time.Second,
	}}}
}

func (d *OPCUADriver) Ready(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://source-opcua/readyz", nil)
	if err != nil {
		return err
	}
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

// 冷启动只等待私有运行时就绪；不重试已经派发的协议操作。
func (d *OPCUADriver) prepareRead(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := bounded.Err(); err != nil {
			return err
		}
		if d.Ready(bounded) == nil {
			return bounded.Err()
		}
		select {
		case <-bounded.Done():
			return bounded.Err()
		case <-ticker.C:
		}
	}
}

type opcuaRequest struct {
	SourceID       int64                         `json:"sourceId"`
	ConfigRevision int64                         `json:"configRevision,omitempty"`
	Definition     sourcestream.SourceDefinition `json:"definition"`
	Credential     sourcestream.Credential       `json:"credential"`
	Parent         string                        `json:"parent,omitempty"`
	ResourceRef    string                        `json:"resourceRef,omitempty"`
	Input          any                           `json:"input,omitempty"`
}

func opcuaInput(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential) opcuaRequest {
	revision, _ := ctx.Value(sourceRevisionKey{}).(int64)
	if revision == 0 {
		revision = sourcestream.WatchConfigRevision(ctx)
	}
	return opcuaRequest{SourceID: id, ConfigRevision: revision, Definition: def, Credential: credential}
}

func (d *OPCUADriver) ReleaseSource(id int64) {
	d.sources.Delete(id)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.call(ctx, "/release", struct {
		SourceID int64 `json:"sourceId"`
	}{id}, nil); err != nil {
		logx.Errorf("Source %d OPC UA client connection cleanup failed: %v", id, err)
	}
}

func (d *OPCUADriver) Close() {
	d.sources.Range(func(key, value any) bool { d.ReleaseSource(key.(int64)); return true })
	d.http.CloseIdleConnections()
}

func (d *OPCUADriver) request(ctx context.Context, path string, input any) (*http.Response, error) {
	if body, ok := input.(opcuaRequest); ok && body.SourceID > 0 && body.Definition.ConnectionRole() == sourcestream.ClientRole {
		d.sources.Store(body.SourceID, struct{}{})
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 1024*1024 {
		return nil, ErrProtocol
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://source-opcua"+path, bytes.NewReader(raw))
	if err != nil {
		return nil, ErrProtocol
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, sourcestream.ErrUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, sourcestream.ErrUnavailable
	}
	return resp, nil
}

func (d *OPCUADriver) call(ctx context.Context, path string, input any, output any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := d.request(ctx, path, input)
	if err != nil {
		if path == "/invoke" {
			return sourcestream.ErrUnknownResult
		}
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if err != nil || len(raw) > 1024*1024 {
		if path == "/invoke" {
			return sourcestream.ErrUnknownResult
		}
		return ErrProtocol
	}
	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Error string          `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return ErrProtocol
	}
	if envelope.Error != "" {
		switch envelope.Error {
		case "SourceStream.resultUnknown":
			return sourcestream.ErrUnknownResult
		case "SourceStream.credentialInvalid":
			return sourcestream.ErrCredential
		case "SourceStream.deviceConnectFailed":
			return sourcestream.ErrDeviceConnect
		case "SourceStream.deviceSessionFailed":
			return sourcestream.ErrDeviceSession
		default:
			return ErrProtocol
		}
	}
	if output != nil && json.Unmarshal(envelope.Data, output) != nil {
		return ErrProtocol
	}
	return nil
}

func (d *OPCUADriver) SyncServerSources(ctx context.Context, sources []sourcestream.ManagedServerSource) error {
	if sources == nil {
		sources = []sourcestream.ManagedServerSource{}
	}
	return d.call(ctx, "/managed/sync", struct {
		Sources []sourcestream.ManagedServerSource `json:"sources"`
	}{Sources: sources}, nil)
}

func (r *Runtime) SyncServerSources(ctx context.Context, sources []sourcestream.ManagedServerSource) error {
	driver, ok := r.serverDrivers[sourcestream.OPCUA].(interface {
		SyncServerSources(context.Context, []sourcestream.ManagedServerSource) error
	})
	if !ok {
		return sourcestream.ErrUnavailable
	}
	return driver.SyncServerSources(ctx, sources)
}

func (d *OPCUADriver) Probe(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential) error {
	if err := d.prepareRead(ctx); err != nil {
		return err
	}
	return d.call(ctx, "/probe", opcuaInput(ctx, id, def, credential), nil)
}
func (d *OPCUADriver) Discover(ctx context.Context, id int64, def sourcestream.RuntimeSource, credential sourcestream.Credential, parent string) ([]sourcestream.NativeResource, error) {
	var result []sourcestream.NativeResource
	body := opcuaInput(ctx, id, def, credential)
	body.Parent = parent
	err := d.call(ctx, "/discover", body, &result)
	return result, err
}
func (d *OPCUADriver) Invoke(ctx context.Context, id int64, def sourcestream.RuntimeSource, credential sourcestream.Credential, resource string, input any) (any, error) {
	var operation struct {
		Operation string `json:"operation"`
	}
	raw, err := json.Marshal(input)
	if err != nil || json.Unmarshal(raw, &operation) != nil {
		return nil, ErrProtocol
	}
	if operation.Operation == "read" {
		if err := d.prepareRead(ctx); err != nil {
			return nil, err
		}
	}
	var result any
	body := opcuaInput(ctx, id, def, credential)
	body.ResourceRef, body.Input = resource, input
	err = d.call(ctx, "/invoke", body, &result)
	return result, err
}
func (d *OPCUADriver) Watch(ctx context.Context, id int64, def sourcestream.SourceDefinition, credential sourcestream.Credential, emit func(Input) error) error {
	defer sourcestream.ReportWatchReady(ctx, false)
	resp, err := d.request(ctx, "/watch", opcuaInput(ctx, id, def, credential))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/x-ndjson") {
		return ErrProtocol
	}
	// Only the managed runtime can confirm MonitoredItem initialization (or
	// installation of a Server watch). Legacy HTTP 200 alone is not readiness.
	if resp.Header.Get("X-Tier0-Watch-Ready") == "1" {
		sourcestream.ReportWatchReady(ctx, true)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var event Input
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.ResourceRef == "" {
			return ErrProtocol
		}
		if err := emit(event); err != nil {
			return err
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return ErrProtocol
}

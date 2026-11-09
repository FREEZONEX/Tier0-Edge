package managedruntime

import (
	"backend/internal/fleetprotocol"
	"backend/internal/runtimeenv"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

type native struct {
	endpoint, token string
	client          *http.Client
}

func New(docker fleetprotocol.RuntimeProvider) fleetprotocol.RuntimeProvider {
	if !runtimeenv.IsNative() {
		return docker
	}
	return &native{endpoint: os.Getenv("TIER0_RUNTIME_URL"), token: os.Getenv("TIER0_RUNTIME_TOKEN"), client: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}}}
}
func (n *native) call(ctx context.Context, req fleetprotocol.RuntimeRequest, target any) error {
	u, err := url.Parse(n.endpoint)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || n.token == "" {
		return errors.New("RUNTIME_UNAVAILABLE")
	}
	b, _ := json.Marshal(req)
	r, err := http.NewRequestWithContext(ctx, "POST", n.endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+n.token)
	r.Header.Set("Content-Type", "application/json")
	resp, err := n.client.Do(r)
	if err != nil {
		return errors.New("RUNTIME_UNAVAILABLE")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		var e struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&e)
		if e.Code == "" {
			e.Code = "RUNTIME_REQUEST_FAILED"
		}
		return errors.New(e.Code)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 3<<20)).Decode(target)
}
func (n *native) List(ctx context.Context) (r fleetprotocol.RuntimeSnapshot, e error) {
	e = n.call(ctx, fleetprotocol.RuntimeRequest{Action: "list"}, &r)
	return
}
func (n *native) Logs(ctx context.Context, q fleetprotocol.RuntimeRequest) (r fleetprotocol.RuntimeLogs, e error) {
	e = n.call(ctx, q, &r)
	return
}
func (n *native) Control(ctx context.Context, q fleetprotocol.RuntimeRequest) (r fleetprotocol.RuntimeOperation, e error) {
	e = n.call(ctx, q, &r)
	return
}
func (n *native) Operation(ctx context.Context, id string) (r fleetprotocol.RuntimeOperation, e error) {
	e = n.call(ctx, fleetprotocol.RuntimeRequest{Action: "operation", OperationID: id}, &r)
	return
}

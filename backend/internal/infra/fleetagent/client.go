package fleetagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"backend/internal/config"
	domaincluster "backend/internal/domain/cluster"
	"backend/internal/fleetprotocol"
)

const (
	statusTimeout    = 5 * time.Second
	queryTimeout     = 10 * time.Second
	logsTimeout      = 35 * time.Second
	restartTimeout   = 125 * time.Second
	syncTimeout      = 35 * time.Second
	maxResponseBytes = 4 << 20
)

type Client struct {
	baseURL string
	token   string
	http    *http.Client
	initErr error
}

func (c *Client) Runtime(ctx context.Context, request fleetprotocol.RuntimeRequest) (json.RawMessage, error) {
	var result json.RawMessage
	err := c.do(ctx, http.MethodPost, "/v1/runtime", request, &result, queryTimeout)
	return result, err
}

func New(cfg config.FleetAgentConf) *Client {
	client := &Client{
		token: strings.TrimSpace(cfg.LocalToken),
		http:  &http.Client{},
	}
	rawURL := strings.TrimSpace(cfg.LocalURL)
	if rawURL == "" || client.token == "" {
		client.initErr = domaincluster.ErrFleetAgentNotConfigured
		return client
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		client.initErr = domaincluster.ErrFleetAgentNotConfigured
		return client
	}
	client.baseURL = strings.TrimRight(parsed.String(), "/")
	return client
}

func (c *Client) Status(ctx context.Context) (*fleetprotocol.AgentStatus, error) {
	result := &fleetprotocol.AgentStatus{}
	if err := c.do(ctx, http.MethodGet, "/v1/status", nil, result, statusTimeout); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) HAStatus(ctx context.Context) (*fleetprotocol.HAStatus, error) {
	result := &fleetprotocol.HAStatus{}
	if err := c.do(ctx, http.MethodGet, "/v1/ha/status", nil, result, statusTimeout); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) SyncHAFileSet(ctx context.Context, fileSet string) error {
	request := fleetprotocol.HASyncRequest{FileSet: strings.TrimSpace(fileSet), Wait: true}
	result := &fleetprotocol.HASyncResponse{}
	return c.do(ctx, http.MethodPost, "/v1/ha/sync", request, result, syncTimeout)
}

func (c *Client) ConfigureConnection(ctx context.Context, accessToken string) (*fleetprotocol.AgentStatus, error) {
	result := &fleetprotocol.AgentStatus{}
	request := map[string]string{"accessToken": strings.TrimSpace(accessToken)}
	if err := c.do(ctx, http.MethodPut, "/v1/connection", request, result, 65*time.Second); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) DisconnectConnection(ctx context.Context) (*fleetprotocol.AgentStatus, error) {
	result := &fleetprotocol.AgentStatus{}
	if err := c.do(ctx, http.MethodDelete, "/v1/connection", nil, result, queryTimeout); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Containers(ctx context.Context) (*fleetprotocol.ContainerSnapshot, error) {
	result := &fleetprotocol.ContainerSnapshot{}
	if err := c.do(ctx, http.MethodGet, "/v1/containers", nil, result, queryTimeout); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) Container(ctx context.Context, containerID string) (*fleetprotocol.Detail, error) {
	result := &fleetprotocol.Detail{}
	if err := c.do(ctx, http.MethodGet, "/v1/containers/"+url.PathEscape(strings.TrimSpace(containerID)), nil, result, queryTimeout); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) ContainerLogs(ctx context.Context, containerID string, request domaincluster.FleetAgentLogsRequest) (*fleetprotocol.ContainerLogsResult, error) {
	result := &fleetprotocol.ContainerLogsResult{}
	if err := c.do(ctx, http.MethodPost, "/v1/containers/"+url.PathEscape(strings.TrimSpace(containerID))+"/logs", request, result, logsTimeout); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) RestartContainer(ctx context.Context, containerID string, request domaincluster.FleetAgentRestartRequest) (*fleetprotocol.ContainerRestartResult, error) {
	result := &fleetprotocol.ContainerRestartResult{}
	if err := c.do(ctx, http.MethodPost, "/v1/containers/"+url.PathEscape(strings.TrimSpace(containerID))+"/restart", request, result, restartTimeout); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) do(ctx context.Context, method, apiPath string, payload, result any, timeout time.Duration) error {
	if c == nil || c.initErr != nil || c.baseURL == "" || c.token == "" {
		return domaincluster.ErrFleetAgentNotConfigured
	}
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode Fleet Agent request: %w", err)
		}
		body = bytes.NewReader(raw)
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, method, c.baseURL+apiPath, body)
	if err != nil {
		return fmt.Errorf("%w: create request", domaincluster.ErrFleetAgentUnavailable)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: request failed", domaincluster.ErrFleetAgentUnavailable)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("%w: read response", domaincluster.ErrFleetAgentUnavailable)
	}
	if len(raw) > maxResponseBytes {
		return fmt.Errorf("%w: response too large", domaincluster.ErrFleetAgentUnavailable)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		var envelope struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(raw, &envelope)
		code := strings.TrimSpace(envelope.Code)
		if code == "" {
			code = "FLEET_AGENT_REQUEST_FAILED"
		}
		statusCode := resp.StatusCode
		if resp.StatusCode == http.StatusUnauthorized || code == "FLEET_LOCAL_AUTH_INVALID" || code == "FLEET_LOCAL_AUTH_NOT_CONFIGURED" {
			statusCode = http.StatusServiceUnavailable
			code = "FLEET_AGENT_AUTH_FAILED"
		}
		return &domaincluster.FleetAgentResponseError{StatusCode: statusCode, Code: code}
	}
	if len(bytes.TrimSpace(raw)) == 0 || !json.Valid(raw) {
		return fmt.Errorf("%w: invalid response", domaincluster.ErrFleetAgentUnavailable)
	}
	if result == nil {
		return fmt.Errorf("%w: response target missing", domaincluster.ErrFleetAgentUnavailable)
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return fmt.Errorf("%w: decode response", domaincluster.ErrFleetAgentUnavailable)
	}
	return nil
}

func (c *Client) NetworkStatus(ctx context.Context) (*fleetprotocol.NetworkStatus, error) {
	result := &fleetprotocol.NetworkStatus{}
	if err := c.do(ctx, http.MethodGet, "/v1/network", nil, result, 15*time.Second); err != nil {
		return nil, err
	}
	return result, nil
}
func (c *Client) EnrollNetwork(ctx context.Context, request fleetprotocol.NetworkEnrollRequest) (*fleetprotocol.NetworkStatus, error) {
	result := &fleetprotocol.NetworkStatus{}
	if err := c.do(ctx, http.MethodPut, "/v1/network", request, result, 45*time.Second); err != nil {
		return nil, err
	}
	return result, nil
}

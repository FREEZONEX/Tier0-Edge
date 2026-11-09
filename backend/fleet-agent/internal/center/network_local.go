package center

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"backend/fleet-agent/internal/config"
	"backend/internal/fleetprotocol"
)

func networkCommand(cfg config.Config) (string, []string) {
	binary := cfg.TailscaleBinary
	if binary == "" {
		binary = "tailscale"
	}
	args := []string{}
	if cfg.TailscaleSocket != "" {
		args = append(args, "--socket="+cfg.TailscaleSocket)
	}
	return binary, args
}

// Local client details are not part of the public network status contract.
type tailscaleClientStatus struct {
	BackendState string
	TailscaleIPs []string
	Self         *struct {
		ID        string
		Expired   bool
		KeyExpiry *time.Time
	}
}

func readTailscaleClientStatus(ctx context.Context, cfg config.Config) (*tailscaleClientStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	binary, args := networkCommand(cfg)
	raw, err := exec.CommandContext(ctx, binary, append(args, "status", "--json")...).Output()
	var status tailscaleClientStatus
	if err != nil || json.Unmarshal(raw, &status) != nil {
		return nil, errors.New("FLEET_NETWORK_CLIENT_UNAVAILABLE")
	}
	return &status, nil
}

func readNetworkStatus(ctx context.Context, cfg config.Config) (*fleetprotocol.NetworkStatus, error) {
	if cfg.UserspaceNetworkDir != "" && cfg.NetworkMode != fleetprotocol.NetworkModeDirect {
		return userspaceNetworkStatus(ctx)
	}
	result := &fleetprotocol.NetworkStatus{Mode: cfg.NetworkMode, State: "unavailable", CheckedAt: time.Now().UnixMilli()}
	if cfg.NetworkMode == fleetprotocol.NetworkModeDirect {
		result.State = "direct"
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	binary, args := networkCommand(cfg)
	status, err := readTailscaleClientStatus(ctx, cfg)
	if err != nil {
		result.ErrorCode = "FLEET_NETWORK_CLIENT_UNAVAILABLE"
		return result, errors.New(result.ErrorCode)
	}
	result.State = "unjoined"
	if status.Self != nil {
		result.NodeID = status.Self.ID
	}
	switch status.BackendState {
	case "Running":
		result.State = "online"
	case "Stopped":
		result.State = "offline"
	case "Starting":
		result.State = "initializing"
	}
	result.Addresses = status.TailscaleIPs
	raw, err := exec.CommandContext(ctx, binary, append(args, "debug", "prefs")...).Output()
	var prefs struct{ ControlURL string }
	if err != nil || json.Unmarshal(raw, &prefs) != nil {
		result.State = "unavailable"
		result.ErrorCode = "FLEET_NETWORK_PREFERENCES_UNAVAILABLE"
		return result, errors.New(result.ErrorCode)
	}
	result.LoginServer = strings.TrimRight(prefs.ControlURL, "/")
	return result, nil
}

func enrollNetwork(ctx context.Context, cfg config.Config, req fleetprotocol.NetworkEnrollRequest) (*fleetprotocol.NetworkStatus, error) {
	if cfg.UserspaceNetworkDir != "" {
		// Native enrollment must go through the authenticated center bootstrap.
		return nil, errors.New("FLEET_NETWORK_STATE_ISOLATION_REQUIRED")
	}
	parsed, err := url.Parse(req.LoginServer)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || len(req.Hostname) > 63 || req.Hostname == "" || strings.Trim(req.Hostname, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" || len(req.AuthKey) > 4096 || strings.ContainsAny(req.AuthKey, "\r\n\t ") {
		return nil, errors.New("FLEET_NETWORK_REQUEST_INVALID")
	}
	current, err := readNetworkStatus(ctx, cfg)
	if err != nil {
		return current, err
	}
	server := strings.TrimRight(req.LoginServer, "/")
	if current.NodeID != "" && current.LoginServer != "" && current.LoginServer != server {
		return current, errors.New("FLEET_NETWORK_CONTROL_PLANE_CONFLICT")
	}
	if current.State == "online" && current.LoginServer == server {
		return current, ensureNetworkIngress(ctx, cfg)
	}
	binary, args := networkCommand(cfg)
	args = append(args, "up", "--login-server="+server, "--accept-dns=false", "--accept-routes=false", "--hostname="+req.Hostname)
	if current.NodeID == "" {
		if req.AuthKey == "" {
			return current, errors.New("FLEET_NETWORK_KEY_REQUIRED")
		}
		if err = os.MkdirAll(cfg.DataDir, 0700); err != nil {
			return current, errors.New("FLEET_NETWORK_CREDENTIAL_UNAVAILABLE")
		}
		file, e := createNetworkCredentialFile(cfg.DataDir)
		if e != nil {
			return current, errors.New("FLEET_NETWORK_CREDENTIAL_UNAVAILABLE")
		}
		defer os.Remove(file.Name())
		_, writeErr := file.WriteString(req.AuthKey)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			return current, errors.New("FLEET_NETWORK_CREDENTIAL_UNAVAILABLE")
		}
		args = append(args, "--auth-key=file:"+file.Name())
	}
	commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if exec.CommandContext(commandCtx, binary, args...).Run() != nil {
		return current, errors.New("FLEET_NETWORK_ENROLLMENT_FAILED")
	}
	if err = ensureNetworkIngress(ctx, cfg); err != nil {
		return current, err
	}
	return readNetworkStatus(ctx, cfg)
}

func (c *Client) NetworkStatus(ctx context.Context) (*fleetprotocol.NetworkStatus, error) {
	c.networkMu.Lock()
	cfg := c.networkCfg
	c.networkMu.Unlock()
	return readNetworkStatus(ctx, cfg)
}
func (c *Client) EnrollNetwork(ctx context.Context, request fleetprotocol.NetworkEnrollRequest) (*fleetprotocol.NetworkStatus, error) {
	c.networkMu.Lock()
	cfg := c.networkCfg
	c.networkMu.Unlock()
	c.networkOperation.Lock()
	defer c.networkOperation.Unlock()
	return enrollNetwork(ctx, cfg, request)
}
func (c *Client) RunNetwork(ctx context.Context) {
	defer stopNativeNetwork()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		c.networkMu.Lock()
		cfg := c.networkCfg
		enabled := c.networkEnabled
		c.networkMu.Unlock()
		if enabled && cfg.CenterConfigured() && cfg.DesiredConnected {
			attempt, cancel := context.WithTimeout(ctx, 45*time.Second)
			done := make(chan error, 1)
			go func() {
				c.networkOperation.Lock()
				defer c.networkOperation.Unlock()
				// Configuration may have changed while this reconciliation waited
				// for an interactive profile switch. Do not stop the new daemon
				// using the previous center's direct-mode snapshot.
				c.networkMu.Lock()
				currentToken := c.networkCfg.AccessToken
				c.networkMu.Unlock()
				if currentToken != cfg.AccessToken {
					done <- context.Canceled
					return
				}
				err := ensureNetwork(attempt, cfg)
				// Stop only an installation-owned client after the direct connection
				// has been committed. A failed candidate must retain the old VPN.
				if err == nil && cfg.NetworkMode == fleetprotocol.NetworkModeDirect {
					err = requestNetworkClient(cfg, "enterprise", fleetprotocol.NetworkModeDirect)
					if err == nil && cfg.UserspaceNetworkDir != "" {
						stopNativeNetwork()
					}
				}
				done <- err
			}()
			select {
			case <-ctx.Done():
				cancel()
				<-done
				return
			case <-c.networkWake:
				cancel()
				<-done
				continue
			case err := <-done:
				cancel()
				if err == context.Canceled {
					continue
				}
				if cfg.TokenNetworkBootstrapURL != "" {
					state := fleetprotocol.ConnectionConnected
					if err != nil {
						state = fleetprotocol.ConnectionDisconnected
					}
					c.status.SetConnectionState(state)
				}
				if err != nil {
					c.logger.Warn("fleet network reconciliation unavailable", "error", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-c.networkWake:
		}
	}
}

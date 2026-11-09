package center

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"backend/fleet-agent/internal/config"
	"backend/internal/fleetprotocol"
)

// Standard tailscale remains an independent host client. The Agent only
// applies a one-time infrastructure enrollment key obtained with its existing
// Fleet credential. No key is placed in argv or logged command output.
func ensureNetwork(ctx context.Context, cfg config.Config) error {
	if cfg.UserspaceNetworkDir != "" && cfg.NetworkMode != fleetprotocol.NetworkModeDirect {
		return prepareUserspaceNetwork(ctx, cfg, func() error { return nil })
	}
	return ensureNetworkOnce(ctx, cfg, true)
}
func ensureNetworkOnce(ctx context.Context, cfg config.Config, repeat bool) error {
	endpoint := cfg.TokenNetworkBootstrapURL
	if endpoint == "" {
		endpoint = cfg.NetworkBootstrapURL
	}
	if endpoint == "" {
		endpoint = cfg.CenterURL
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		if cfg.NetworkBootstrapURL == "" && cfg.TokenNetworkBootstrapURL == "" {
			return nil
		}
		return errors.New("network bootstrap requires HTTPS")
	}
	if cfg.NetworkMode == fleetprotocol.NetworkModeDirect {
		ssh := sshAdvertisement(cfg)
		data, e := requestNetworkBootstrap(ctx, cfg, fleetprotocol.NetworkBootstrapRequest{NodeID: cfg.FleetNodeID, ClientID: cfg.MQTTClientID, SSH: ssh})
		if e != nil {
			return e
		}
		if data.NetworkMode != fleetprotocol.NetworkModeDirect || data.CenterType != "enterprise" || data.Enabled {
			return errors.New("FLEET_CONNECTION_MODE_MISMATCH")
		}
		if data.SSH != nil && ssh.ErrorCode == "" {
			changed, e := installSSHTrust(cfg, data.SSH.CAPublicKey)
			if e != nil {
				return e
			}
			if changed && repeat {
				return ensureNetworkOnce(ctx, cfg, false)
			}
		}
		return nil
	}
	// An authenticated cloud response is required before asking the privileged
	// installation helper to start its own independent client.
	inspected, err := requestNetworkBootstrap(ctx, cfg, fleetprotocol.NetworkBootstrapRequest{NodeID: cfg.FleetNodeID, ClientID: cfg.MQTTClientID, Inspect: true})
	if err != nil {
		return err
	}
	if err := requestNetworkClient(cfg, "cloud", fleetprotocol.NetworkModeTailscale); err != nil {
		return err
	}
	binary := cfg.TailscaleBinary
	if binary == "" {
		binary = "tailscale"
	}
	args := []string{}
	if cfg.TailscaleSocket != "" {
		args = append(args, "--socket="+cfg.TailscaleSocket)
	}
	status, statusErr := readTailscaleClientStatus(ctx, cfg)
	if statusErr != nil {
		return errors.New("standard Tailscale client unavailable")
	}
	if inspected.NetworkNodeID != "" && status.BackendState != "Running" {
		if _, err := waitForRetainedNetwork(ctx, cfg, strings.TrimRight(inspected.LoginServer, "/"), inspected.NetworkNodeID); err != nil {
			return err
		}
		status, statusErr = readTailscaleClientStatus(ctx, cfg)
		if statusErr != nil {
			return statusErr
		}
	}
	if status.BackendState == "Starting" {
		return errors.New("FLEET_NETWORK_INITIALIZING")
	}
	// Startup has its own bounded wait; do not spend the subsequent command
	// budget while restoring retained state. The caller's overall deadline
	// still bounds both phases.
	commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	running := status.BackendState == "Running"
	stopped := status.BackendState == "Stopped"
	networkNodeID := ""
	if status.Self != nil {
		networkNodeID = status.Self.ID
	}
	// A switched, stopped profile can omit Self.ID until resumed. Authenticate
	// and check its retained control plane first; never mint a replacement node
	// or weaken the center's node-ID check just because local status is empty.
	if stopped && networkNodeID == "" {
		inspected, e := requestNetworkBootstrap(commandCtx, cfg, fleetprotocol.NetworkBootstrapRequest{NodeID: cfg.FleetNodeID, ClientID: cfg.MQTTClientID, Inspect: true})
		if e != nil {
			return e
		}
		current, e := readNetworkStatus(commandCtx, cfg)
		if e != nil || !inspected.Enabled || inspected.NetworkNodeID == "" || current.LoginServer != strings.TrimRight(inspected.LoginServer, "/") {
			return errors.New("retained Tailscale identity does not match center")
		}
		if resumeNetwork(commandCtx, cfg, current.LoginServer) != nil {
			return errors.New("standard Tailscale client resume failed")
		}
		resumed, e := readNetworkStatus(commandCtx, cfg)
		if e != nil || resumed.State != "online" || resumed.NodeID != inspected.NetworkNodeID || resumed.LoginServer != current.LoginServer {
			return errors.New("resumed Tailscale identity does not match center")
		}
		return ensureNetworkOnce(ctx, cfg, repeat)
	}

	ssh := sshAdvertisement(cfg)
	data, err := requestNetworkBootstrap(commandCtx, cfg, fleetprotocol.NetworkBootstrapRequest{NodeID: cfg.FleetNodeID, ClientID: cfg.MQTTClientID, NeedKey: inspected.NetworkNodeID == "" && !running && !stopped, NetworkNodeID: networkNodeID, SSH: ssh})
	if err != nil {
		return err
	}

	if !data.Enabled {
		return nil
	}
	login, err := url.Parse(data.LoginServer)
	if err != nil || login.Scheme != "https" || login.User != nil || login.Hostname() == "" {
		return errors.New("network control endpoint invalid")
	}
	if running || stopped {
		prefsRaw, e := exec.CommandContext(commandCtx, binary, append(args, "debug", "prefs")...).Output()
		var prefs struct{ ControlURL string }
		if e != nil || json.Unmarshal(prefsRaw, &prefs) != nil || strings.TrimRight(prefs.ControlURL, "/") != strings.TrimRight(data.LoginServer, "/") {
			return errors.New("existing Tailscale client belongs to another control server")
		}
		if stopped {
			if resumeNetwork(commandCtx, cfg, strings.TrimRight(data.LoginServer, "/")) != nil {
				return errors.New("standard Tailscale client resume failed")
			}
		}
		// SSH is an independently advertised capability. A center may return its
		// CA even when this host has no installed SSH service (e.g. Windows).
		// Keep reporting that capability error, without blocking Fleet enrollment.
		if data.SSH != nil && ssh.ErrorCode == "" {
			changed, e := installSSHTrust(cfg, data.SSH.CAPublicKey)
			if e != nil {
				return e
			}
			if changed && repeat {
				return ensureNetworkOnce(ctx, cfg, false)
			}
		}
		return ensureNetworkIngress(ctx, cfg)
	}
	if data.AuthKey == "" {
		return errors.New("network enrollment key unavailable")
	}
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return errors.New("network credential directory unavailable")
	}
	file, err := createNetworkCredentialFile(cfg.DataDir)
	if err != nil {
		return errors.New("network credential file unavailable")
	}
	defer os.Remove(file.Name())
	if _, err = file.WriteString(data.AuthKey); err != nil {
		file.Close()
		return errors.New("network credential write failed")
	}
	if file.Close() != nil {
		return errors.New("network credential write failed")
	}
	hostname := fmt.Sprintf("fleet-%x", sha256.Sum256([]byte(cfg.FleetNodeID)))[:31]
	up := append(args, "up", "--login-server="+data.LoginServer, "--auth-key=file:"+file.Name(), "--accept-dns=false", "--accept-routes=false", "--hostname="+hostname)
	if cfg.WindowsManagedNetwork {
		up = append(up, "--unattended=true")
	}
	if exec.CommandContext(commandCtx, binary, up...).Run() != nil {
		return errors.New("FLEET_NETWORK_PROFILE_LOGIN_FAILED")
	}
	if repeat {
		return ensureNetworkOnce(ctx, cfg, false)
	}
	return nil
}

func requestNetworkBootstrap(ctx context.Context, cfg config.Config, req fleetprotocol.NetworkBootstrapRequest) (fleetprotocol.NetworkBootstrapResponse, error) {
	endpoint := cfg.TokenNetworkBootstrapURL
	if endpoint == "" {
		endpoint = cfg.NetworkBootstrapURL
	}
	if endpoint == "" {
		endpoint = cfg.CenterURL
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fleetprotocol.NetworkBootstrapResponse{}, errors.New("network bootstrap requires HTTPS")
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		return fleetprotocol.NetworkBootstrapResponse{}, errors.New("network TLS roots unavailable")
	}
	if cfg.NetworkCAFile != "" {
		pem, e := os.ReadFile(cfg.NetworkCAFile)
		if e != nil || !roots.AppendCertsFromPEM(pem) {
			return fleetprotocol.NetworkBootstrapResponse{}, errors.New("network TLS CA unavailable")
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	raw, _ := json.Marshal(req)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/api/node/v1/fleet/network/bootstrap", bytes.NewReader(raw))
	if err != nil {
		return fleetprotocol.NetworkBootstrapResponse{}, errors.New("network bootstrap endpoint invalid")
	}
	request.SetBasicAuth(cfg.MQTTUsername, cfg.MQTTPassword)
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return fleetprotocol.NetworkBootstrapResponse{}, errors.New("FLEET_NETWORK_BOOTSTRAP_UNAVAILABLE")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		// Never include response bodies, credentials, URLs or dynamic messages.
		phase := "report"
		if req.Inspect {
			phase = "inspect"
		} else if req.NeedKey {
			phase = "enroll"
		}
		code := "FLEET_NETWORK_BOOTSTRAP_REJECTED"
		if response.StatusCode == http.StatusNotFound {
			code = "FLEET_CENTER_ENDPOINT_NOT_FOUND"
		}
		return fleetprotocol.NetworkBootstrapResponse{}, fmt.Errorf("%w phase=%s httpStatus=%d", errors.New(code), phase, response.StatusCode)
	}
	var envelope struct {
		Code int                                    `json:"code"`
		Data fleetprotocol.NetworkBootstrapResponse `json:"data"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&envelope) != nil || envelope.Code != 200 {
		return fleetprotocol.NetworkBootstrapResponse{}, errors.New("network bootstrap response invalid")
	}
	if !fleetprotocol.ValidCenterNetworkMode(envelope.Data.CenterType, envelope.Data.NetworkMode) || envelope.Data.NetworkMode != cfg.NetworkMode {
		return fleetprotocol.NetworkBootstrapResponse{}, errors.New("FLEET_CONNECTION_MODE_MISMATCH")
	}
	return envelope.Data, nil
}

// The helper accepts only this fixed mode request. No privileged arguments,
// executable paths, URLs or credentials are supplied by the Agent.
func requestNetworkClient(cfg config.Config, centerType, mode string) error {
	if !fleetprotocol.ValidCenterNetworkMode(centerType, mode) {
		return errors.New("FLEET_CONNECTION_MODE_MISMATCH")
	}
	raw, _ := json.Marshal(map[string]string{"centerType": centerType, "networkMode": mode})
	path := filepath.Join(cfg.DataDir, "network-request.json")
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, raw) {
		return nil
	}
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(cfg.DataDir, ".network-request-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, werr := file.Write(raw)
	serr := file.Sync()
	cerr := file.Close()
	if werr != nil || serr != nil || cerr != nil {
		return errors.New("FLEET_NETWORK_REQUEST_WRITE_FAILED")
	}
	return os.Rename(file.Name(), path)
}

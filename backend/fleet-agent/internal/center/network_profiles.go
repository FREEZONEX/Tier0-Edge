package center

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"backend/fleet-agent/internal/config"
	"backend/internal/fleetprotocol"
)

// Only explicit configuration changes the complete Tailscale state domain.
// Background reconciliation must never enroll a second business node into it.
func (c *Client) PrepareConnection(ctx context.Context, candidate config.Config, apply func() error) (resultErr error) {
	c.networkOperation.Lock()
	defer c.networkOperation.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	if candidate.UserspaceNetworkDir != "" && candidate.NetworkMode != fleetprotocol.NetworkModeDirect {
		return prepareUserspaceNetwork(ctx, candidate, apply)
	}
	inspected, err := requestNetworkBootstrap(ctx, candidate, fleetprotocol.NetworkBootstrapRequest{NodeID: candidate.FleetNodeID, ClientID: candidate.MQTTClientID, Inspect: true})
	if err != nil {
		return err
	}
	if candidate.NetworkMode == fleetprotocol.NetworkModeDirect {
		if inspected.NetworkMode != fleetprotocol.NetworkModeDirect || inspected.CenterType != "enterprise" {
			return errors.New("FLEET_CONNECTION_MODE_MISMATCH")
		}
		if err := ensureNetwork(ctx, candidate); err != nil {
			return err
		}
		if err := apply(); err != nil {
			return err
		}
		if candidate.UserspaceNetworkDir != "" {
			stopNativeNetwork()
		}
		return nil
	}
	if inspected.NetworkMode == fleetprotocol.NetworkModeDirect {
		return errors.New("FLEET_CONNECTION_MODE_MISMATCH")
	}
	if !inspected.Enabled {
		return errors.New("FLEET_NETWORK_DISABLED")
	}
	if err := requestNetworkClient(candidate, inspected.CenterType, inspected.NetworkMode); err != nil {
		return err
	}
	// Both values have been authenticated by bootstrap before selecting a domain.
	server := strings.TrimRight(inspected.LoginServer, "/")
	domain := fmt.Sprintf("%x", sha256.Sum256([]byte(server+"\x00"+candidate.FleetNodeID)))
	oldDomain := ""
	if candidate.NetworkProfileHelper {
		state, e := requestProfileHandoff(ctx, candidate, newProfileRequest("inspect", "", "", ""))
		if e != nil {
			return e
		}
		oldDomain = state.StateDomain
	}
	current, err := readNetworkStatus(ctx, candidate)
	if err != nil {
		return err
	}
	oldProfile, err := activeNetworkProfile(ctx, candidate)
	if err != nil {
		return err
	}
	if candidate.WindowsManagedNetwork {
		return prepareWindowsManagedNetwork(ctx, candidate, inspected, domain, apply)
	}
	sameIdentity := current.LoginServer == server && current.NodeID != "" && current.NodeID == inspected.NetworkNodeID
	if sameIdentity {
		if candidate.NetworkProfileHelper {
			if oldDomain == "" {
				// Preserve an existing installation's currently verified state in place.
				// Never clone its MachineKey into a new connection directory.
				if _, err := requestProfileHandoff(ctx, candidate, newProfileRequest("adopt", oldProfile, oldProfile, domain)); err != nil {
					return err
				}
			} else if oldDomain != domain {
				return errors.New("FLEET_NETWORK_IDENTITY_ALREADY_BOUND")
			}
		}
		if err := ensureNetwork(ctx, candidate); err != nil {
			return err
		}
		return apply()
	}
	// Explicit administrator-managed clients/Windows cannot provide the root
	// state handoff. Never silently fall back to login/profile reuse on them.
	if !candidate.NetworkProfileHelper {
		return errors.New("FLEET_NETWORK_STATE_ISOLATION_REQUIRED")
	}
	identitiesPath := filepath.Join(candidate.DataDir, "network-identities.json")
	identities := map[string]string{}
	if raw, e := os.ReadFile(identitiesPath); e == nil {
		if json.Unmarshal(raw, &identities) != nil {
			return errors.New("FLEET_NETWORK_PROFILES_INVALID")
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if oldDomain != "" && current.NodeID != "" {
		identities[oldDomain] = current.LoginServer + "/" + current.NodeID
	}
	previousTrust, _ := os.ReadFile(filepath.Join(candidate.DataDir, "ssh", "trusted-ca.pub"))
	handoff := newProfileRequest("prepare", oldProfile, "", domain)
	defer func() {
		if resultErr == nil {
			return
		}
		if len(previousTrust) > 0 {
			_, _ = installSSHTrust(candidate, string(previousTrust))
		}
		restore, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		request := handoff
		request.Action = "rollback"
		if _, err := requestProfileHandoff(restore, candidate, request); err != nil {
			resultErr = errors.Join(resultErr, errors.New("FLEET_NETWORK_PROFILE_ROLLBACK_PENDING"))
		}
	}()
	prepared, err := requestProfileHandoff(ctx, candidate, handoff)
	if err != nil {
		return err
	}
	binary, args := networkCommand(candidate)
	selected := prepared.Profile
	needsLogin := selected == ""
	if inspected.NetworkNodeID != "" {
		// LocalAPI/profile enumeration becomes available before the retained
		// state has loaded. Even an empty profile/NoState is not a new identity
		// when authenticated Inspect says this connection already owns a node.
		if _, err := waitForRetainedNetwork(ctx, candidate, server, inspected.NetworkNodeID); err != nil {
			return err
		}
		needsLogin = false
	} else if selected != "" {
		joined, e := readNetworkStatus(ctx, candidate)
		if e != nil || joined.LoginServer != server {
			return errors.New("FLEET_NETWORK_PROFILE_RESTORE_FAILED")
		}
		needsLogin = joined.State == "unjoined"
		if joined.State == "offline" || joined.State == "initializing" {
			if err := resumeNetwork(ctx, candidate, server); err != nil {
				return err
			}
		}
	}
	if needsLogin {
		// Only an unbound attempt may request enrollment in this isolated domain.
		// The center still owns expiry/revocation and key-issuance rules.
		bootstrap, e := requestNetworkBootstrap(ctx, candidate, fleetprotocol.NetworkBootstrapRequest{NodeID: candidate.FleetNodeID, ClientID: candidate.MQTTClientID, NeedKey: true})
		if e != nil {
			return e
		}
		if bootstrap.AuthKey == "" || strings.TrimRight(bootstrap.LoginServer, "/") != server {
			return errors.New("FLEET_NETWORK_KEY_UNAVAILABLE")
		}
		file, e := createNetworkCredentialFile(candidate.DataDir)
		if e != nil {
			return e
		}
		defer os.Remove(file.Name())
		_, werr := file.WriteString(bootstrap.AuthKey)
		cerr := file.Close()
		if werr != nil || cerr != nil {
			return errors.New("FLEET_NETWORK_CREDENTIAL_UNAVAILABLE")
		}
		name := "fleet-" + domain[:25]
		command := append(append([]string{}, args...), "up", "--operator="+candidate.NetworkOperator, "--login-server="+server, "--auth-key=file:"+file.Name(), "--hostname="+name, "--accept-dns=false", "--accept-routes=false", "--timeout=30s")
		if exec.CommandContext(ctx, binary, command...).Run() != nil {
			return errors.New("FLEET_NETWORK_PROFILE_LOGIN_FAILED")
		}
		if exec.CommandContext(ctx, binary, append(args, "set", "--nickname="+name)...).Run() != nil {
			return errors.New("FLEET_NETWORK_PROFILE_NAME_FAILED")
		}
	}
	selected, err = activeNetworkProfile(ctx, candidate)
	if err != nil || selected == "" {
		return errors.New("FLEET_NETWORK_PROFILE_UNAVAILABLE")
	}
	joined, e := waitForNetworkOnline(ctx, candidate)
	if e != nil || joined.State != "online" || joined.NodeID == "" || joined.LoginServer != server || (inspected.NetworkNodeID != "" && joined.NodeID != inspected.NetworkNodeID) {
		return errors.New("FLEET_NETWORK_PROFILE_UNAVAILABLE")
	}
	identity := server + "/" + joined.NodeID
	// Check before ensureNetwork reports the candidate network ID to the center.
	for owner, previous := range identities {
		if previous == identity && owner != domain {
			return errors.New("FLEET_NETWORK_IDENTITY_ALREADY_BOUND")
		}
	}
	if oldDomain != domain && current.LoginServer == server && current.NodeID == joined.NodeID {
		return errors.New("FLEET_NETWORK_IDENTITY_ALREADY_BOUND")
	}
	if err := ensureNetwork(ctx, candidate); err != nil {
		return err
	}
	identities[domain] = identity
	if err := saveNetworkProfiles(candidate.DataDir, identitiesPath, identities); err != nil {
		return err
	}
	request := handoff
	request.Action, request.TargetProfile = "commit", selected
	if _, err := requestProfileHandoff(ctx, candidate, request); err != nil {
		return err
	}
	return apply()
}

// A retained daemon can briefly expose NoState/NeedsLogin and an empty profile
// before loading its keys. Never request another PAK for an already-bound node.
// A confirmed expired key is distinct from startup, and requires center-side
// recovery under the existing ownership rules rather than automatic reissuance.
func waitForRetainedNetwork(ctx context.Context, cfg config.Config, server, nodeID string) (*fleetprotocol.NetworkStatus, error) {
	wait, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	lastState := ""
	for {
		status, err := readTailscaleClientStatus(wait, cfg)
		if err == nil {
			lastState = status.BackendState
			if status.Self != nil {
				if status.Self.ID != "" && status.Self.ID != nodeID {
					return nil, errors.New("FLEET_NETWORK_IDENTITY_MISMATCH")
				}
				if status.Self.Expired || (status.Self.KeyExpiry != nil && !status.Self.KeyExpiry.IsZero() && !status.Self.KeyExpiry.After(time.Now())) {
					return nil, errors.New("FLEET_NETWORK_KEY_EXPIRED")
				}
			}
			switch status.BackendState {
			case "Running":
				current, err := readNetworkStatus(wait, cfg)
				if err != nil || current.NodeID != nodeID || current.LoginServer != server {
					return nil, errors.New("FLEET_NETWORK_IDENTITY_MISMATCH")
				}
				return current, nil
			case "Stopped":
				if err := resumeNetwork(wait, cfg, server); err != nil {
					return nil, err
				}
			}
		}
		select {
		case <-wait.Done():
			if lastState == "NeedsLogin" {
				return nil, errors.New("FLEET_NETWORK_REAUTH_REQUIRED")
			}
			return nil, errors.New("FLEET_NETWORK_INITIALIZING")
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// `up` can return immediately for a retained profile while the restarted
// daemon is still Starting. Do not commit/report until the network is online.
func waitForNetworkOnline(ctx context.Context, cfg config.Config) (*fleetprotocol.NetworkStatus, error) {
	wait, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		current, err := readNetworkStatus(wait, cfg)
		if err == nil && current.State == "online" {
			return current, nil
		}
		select {
		case <-wait.Done():
			return nil, errors.New("FLEET_NETWORK_PROFILE_UNAVAILABLE")
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// Include retained preferences when resuming a custom control server. A bare
// `up` selects the default login server and fails for a stopped Headscale node.
func resumeNetwork(ctx context.Context, cfg config.Config, server string) error {
	binary, args := networkCommand(cfg)
	raw, err := exec.CommandContext(ctx, binary, append(args, "debug", "prefs")...).Output()
	var prefs struct {
		Hostname     string
		ControlURL   string
		OperatorUser string
	}
	if err != nil || json.Unmarshal(raw, &prefs) != nil || strings.TrimRight(prefs.ControlURL, "/") != server {
		return errors.New("FLEET_NETWORK_PROFILE_RESTORE_FAILED")
	}
	command := append(append([]string{}, args...), "up", "--login-server="+server, "--hostname="+prefs.Hostname, "--accept-dns=false", "--accept-routes=false", "--timeout=30s")
	if cfg.WindowsManagedNetwork {
		command = append(command, "--unattended=true")
	}
	if prefs.OperatorUser != "" {
		command = append(command, "--operator="+prefs.OperatorUser)
	}
	if exec.CommandContext(ctx, binary, command...).Run() != nil {
		return errors.New("standard Tailscale client resume failed")
	}
	return nil
}
func saveNetworkProfiles(dir, path string, profiles map[string]string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	raw, _ := json.Marshal(profiles)
	file, err := os.CreateTemp(dir, ".network-profiles-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, werr := file.Write(raw)
	serr := file.Sync()
	cerr := file.Close()
	if werr != nil || serr != nil || cerr != nil {
		return errors.New("FLEET_NETWORK_PROFILES_WRITE_FAILED")
	}
	return os.Rename(file.Name(), path)
}
func activeNetworkProfile(ctx context.Context, cfg config.Config) (string, error) {
	binary, args := networkCommand(cfg)
	raw, err := exec.CommandContext(ctx, binary, append(args, "switch", "--list")...).Output()
	if err != nil {
		return "", errors.New("FLEET_NETWORK_PROFILES_UNAVAILABLE")
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && strings.HasSuffix(fields[len(fields)-1], "*") {
			if strings.Trim(fields[0], "0123456789abcdef") == "" {
				return fields[0], nil
			}
		}
	}
	return "", nil
}

// Fixed loopback targets from the installation, never arbitrary remote proxies.
func ensureNetworkIngress(ctx context.Context, cfg config.Config) error {
	if cfg.BackendIngressPort != 0 {
		if cfg.BackendIngressPort < 1 || cfg.BackendIngressPort > 65535 {
			return errors.New("FLEET_INGRESS_PORT_INVALID")
		}
		if err := ensureLoopbackForward(ctx, cfg, fleetprotocol.DefaultNetworkIngressPort, cfg.BackendIngressPort); err != nil {
			return errors.New("FLEET_INGRESS_FORWARD_FAILED")
		}
	}
	manifest, manifestErr := readSSHManifest(cfg)
	if cfg.SSHUserspaceForward || manifestErr == nil && manifest.Forward {
		// Userspace networking and local port collisions both need a listener.
		// Keep the overlay/ACL port stable; only the installer may choose the
		// loopback target. Reapply after switching centers in native TUN too.
		if manifestErr != nil {
			return manifestErr
		}
		if err := ensureLoopbackForward(ctx, cfg, manifest.Port, manifest.LocalPort); err != nil {
			return errors.New("FLEET_SSH_FORWARD_FAILED")
		}
	}
	return nil
}

func ensureLoopbackForward(ctx context.Context, cfg config.Config, port, target int) error {
	binary, args := networkCommand(cfg)
	// Reconciliation runs repeatedly. Leave an already matching listener alone
	// instead of asking tailscaled to reconfigure its Serve listeners each time.
	raw, err := exec.CommandContext(ctx, binary, append(args, "serve", "status", "--json")...).Output()
	var current struct {
		TCP map[string]map[string]json.RawMessage
	}
	if err != nil || json.Unmarshal(raw, &current) != nil {
		return errors.New("FLEET_FORWARD_STATUS_UNAVAILABLE")
	}
	forward := current.TCP[fmt.Sprint(port)]
	var destination string
	if len(forward) == 1 && json.Unmarshal(forward["TCPForward"], &destination) == nil && destination == fmt.Sprintf("127.0.0.1:%d", target) {
		return nil
	}
	args = append(args, "serve", "--bg", fmt.Sprintf("--tcp=%d", port), fmt.Sprintf("tcp://127.0.0.1:%d", target))
	return exec.CommandContext(ctx, binary, args...).Run()
}

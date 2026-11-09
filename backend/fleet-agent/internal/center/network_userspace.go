package center

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"backend/fleet-agent/internal/config"
	"backend/fleet-agent/internal/nativessh"
	"backend/internal/fleetprotocol"
	"backend/internal/infra/privatefs"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/net/socks5"
	"tailscale.com/tsnet"
)

// One Agent process owns one installation. This state never references the
// host daemon, its profiles, machine keys, routes, DNS, or admin LocalAPI.
var nativeNetwork struct {
	sync.RWMutex
	active *nativeNetworkNode
	proxy  net.Listener
}

type nativeNetworkNode struct {
	server        *tsnet.Server
	domain        string
	control       string
	listener      net.Listener
	terminal      *nativessh.Server
	terminalError string
	// Only the serialized network reconciliation modifies this timestamp.
	offlineSince time.Time
}

func (n *nativeNetworkNode) close() {
	if n.terminal != nil {
		n.terminal.Close()
	}
	_ = n.server.Close()
}

func quietNetworkLog(string, ...any) {} // AuthURLs and key material must not enter Agent logs.

var nativeLogSecrets = regexp.MustCompile(`https?://\S+|[A-Za-z0-9_+/=-]{24,}`)

func nativeNetworkWarning(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	lower := strings.ToLower(message)
	if !(strings.Contains(lower, "error") || strings.Contains(lower, "failed")) {
		return
	}
	for _, secret := range []string{"authurl", "authkey", "private", "token"} {
		if strings.Contains(lower, secret) {
			return
		}
	}
	slog.Warn("native network transport", "detail", nativeLogSecrets.ReplaceAllString(message, "[redacted]"))
}

func prepareUserspaceNetwork(ctx context.Context, cfg config.Config, apply func() error) (resultErr error) {
	inspected, err := requestNetworkBootstrap(ctx, cfg, fleetprotocol.NetworkBootstrapRequest{NodeID: cfg.FleetNodeID, ClientID: cfg.MQTTClientID, Inspect: true})
	if err != nil {
		return err
	}
	if !inspected.Enabled || inspected.NetworkMode != fleetprotocol.NetworkModeTailscale || inspected.CenterType != "cloud" {
		return errors.New("FLEET_CONNECTION_MODE_MISMATCH")
	}
	control := strings.TrimRight(inspected.LoginServer, "/")
	u, err := url.Parse(control)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("FLEET_NETWORK_REQUEST_INVALID")
	}
	domain := fmt.Sprintf("%x", sha256.Sum256([]byte(control+"\x00"+cfg.FleetNodeID)))
	nativeNetwork.RLock()
	previous := nativeNetwork.active
	nativeNetwork.RUnlock()
	node := previous
	if node == nil || node.domain != domain {
		if err := os.MkdirAll(cfg.UserspaceNetworkDir, 0700); err != nil {
			return err
		}
		rootInfo, err := os.Lstat(cfg.UserspaceNetworkDir)
		if err != nil || !rootInfo.IsDir() || !privatefs.IsPrivate(cfg.UserspaceNetworkDir, rootInfo) {
			return errors.New("FLEET_NETWORK_STATE_PERMISSIONS_INVALID")
		}
		dir := filepath.Join(cfg.UserspaceNetworkDir, domain)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || !privatefs.IsPrivate(dir, info) {
			return errors.New("FLEET_NETWORK_STATE_PERMISSIONS_INVALID")
		}
		// Never mint a new identity when the center already bound a retained one.
		statePath := filepath.Join(dir, "tailscaled.state")
		stateInfo, stateErr := os.Lstat(statePath)
		if stateErr == nil && (!stateInfo.Mode().IsRegular() || !privatefs.IsPrivate(statePath, stateInfo)) {
			return errors.New("FLEET_NETWORK_STATE_PERMISSIONS_INVALID")
		}
		if stateErr != nil && !os.IsNotExist(stateErr) {
			return errors.New("FLEET_NETWORK_STATE_PERMISSIONS_INVALID")
		}
		if inspected.NetworkNodeID != "" && stateErr != nil {
			return errors.New("FLEET_NETWORK_IDENTITY_MISMATCH")
		}
		key := ""
		if inspected.NetworkNodeID == "" {
			bootstrap, err := requestNetworkBootstrap(ctx, cfg, fleetprotocol.NetworkBootstrapRequest{NodeID: cfg.FleetNodeID, ClientID: cfg.MQTTClientID, NeedKey: true})
			if err != nil {
				return err
			}
			if bootstrap.AuthKey == "" || strings.TrimRight(bootstrap.LoginServer, "/") != control {
				return errors.New("FLEET_NETWORK_KEY_UNAVAILABLE")
			}
			key = bootstrap.AuthKey
		}
		node = &nativeNetworkNode{domain: domain, control: control, server: &tsnet.Server{Dir: dir, Hostname: "fleet-" + domain[:25], ControlURL: control, AuthKey: key, UserLogf: quietNetworkLog, Logf: nativeNetworkWarning}}
		node.terminal, err = nativessh.New(dir)
		if err != nil {
			node.terminalError = "SSH_NATIVE_INITIALIZATION_FAILED"
			slog.Warn("native terminal initialization failed", "error", err)
		}
		defer func() {
			if resultErr != nil {
				node.close()
			}
		}()
	}
	var status *ipnstate.Status
	if node == previous {
		// Up clears ServeConfig and only waits for Running. Running alone does
		// not mean that the control stream is connected (Self.Online does).
		lc, localErr := node.server.LocalClient()
		if localErr != nil {
			err = localErr
		} else {
			status, err = lc.StatusWithoutPeers(ctx)
		}
	} else {
		status, err = node.server.Up(ctx)
	}
	if err != nil {
		return errors.New("FLEET_NETWORK_PROFILE_LOGIN_FAILED")
	}
	if status.Self == nil || status.Self.ID == "" || status.BackendState != "Running" {
		return errors.New("FLEET_NETWORK_PROFILE_UNAVAILABLE")
	}
	if inspected.NetworkNodeID != "" && string(status.Self.ID) != inspected.NetworkNodeID {
		return errors.New("FLEET_NETWORK_IDENTITY_MISMATCH")
	}
	if !status.Self.Online {
		if node == previous {
			if node.offlineSince.IsZero() {
				node.offlineSince = time.Now()
			} else if time.Since(node.offlineSince) >= 2*time.Minute {
				// Allow normal tsnet reconnection first. A wedged transport must
				// not remain Running indefinitely. Reopen the same retained state
				// on the next reconciliation; never log out or mint a new key.
				slog.Warn("native network control stream unavailable; reopening retained identity")
				stopNativeNetwork()
			}
		}
		return errors.New("FLEET_NETWORK_PROFILE_UNAVAILABLE")
	}
	node.offlineSince = time.Time{}
	if node.listener == nil {
		if cfg.BackendIngressPort < 1 || cfg.BackendIngressPort > 65535 {
			return errors.New("FLEET_INGRESS_PORT_INVALID")
		}
		listener, err := node.server.Listen("tcp", fmt.Sprintf(":%d", fleetprotocol.DefaultNetworkIngressPort))
		if err != nil {
			return errors.New("FLEET_INGRESS_FORWARD_FAILED")
		}
		node.listener = listener
		go forwardNativeIngress(listener, cfg.BackendIngressPort)
		if node.terminal != nil {
			sshListener, listenErr := node.server.Listen("tcp", fmt.Sprintf(":%d", nativessh.Port))
			if listenErr != nil {
				node.terminal.Close()
				node.terminal = nil
				node.terminalError = "SSH_NATIVE_LISTEN_FAILED"
			} else {
				go node.terminal.Serve(sshListener)
			}
		}
	}
	if err := ensureNativeSOCKS(cfg.UserspaceSOCKSAddress); err != nil {
		return err
	}
	advertisement := &fleetprotocol.SSHAdvertisement{ErrorCode: node.terminalError}
	if node.terminal != nil {
		advertisement = node.terminal.Advertisement()
	}
	bootstrap, err := requestNetworkBootstrap(ctx, cfg, fleetprotocol.NetworkBootstrapRequest{NodeID: cfg.FleetNodeID, ClientID: cfg.MQTTClientID, NetworkNodeID: string(status.Self.ID), SSH: advertisement})
	if err != nil {
		return err
	}
	if node.terminal != nil && bootstrap.SSH != nil {
		if err = node.terminal.Trust(bootstrap.SSH.CAPublicKey); err != nil {
			return err
		}
		if advertisement.TrustedCAFingerprint != node.terminal.Advertisement().TrustedCAFingerprint {
			// The center verifies the listener only after its CA has been installed.
			_, err = requestNetworkBootstrap(ctx, cfg, fleetprotocol.NetworkBootstrapRequest{NodeID: cfg.FleetNodeID, ClientID: cfg.MQTTClientID, NetworkNodeID: string(status.Self.ID), SSH: node.terminal.Advertisement()})
			if err != nil {
				return err
			}
		}
	}
	if err := apply(); err != nil {
		return err
	}
	nativeNetwork.Lock()
	nativeNetwork.active = node
	nativeNetwork.Unlock()
	if previous != nil && previous != node {
		previous.close()
	}
	return nil
}

func userspaceNetworkStatus(ctx context.Context) (*fleetprotocol.NetworkStatus, error) {
	r := &fleetprotocol.NetworkStatus{Mode: fleetprotocol.NetworkModeTailscale, State: "unjoined", CheckedAt: time.Now().UnixMilli()}
	nativeNetwork.RLock()
	node := nativeNetwork.active
	nativeNetwork.RUnlock()
	if node == nil {
		return r, nil
	}
	r.LoginServer = node.control
	lc, err := node.server.LocalClient()
	if err != nil {
		return r, err
	}
	status, err := lc.StatusWithoutPeers(ctx)
	if err != nil {
		r.State = "unavailable"
		return r, err
	}
	if status.Self != nil {
		r.NodeID = string(status.Self.ID)
	}
	switch status.BackendState {
	case "Running":
		r.State = "offline"
		if status.Self != nil && status.Self.Online {
			r.State = "online"
		}
	case "Stopped":
		r.State = "offline"
	case "Starting":
		r.State = "initializing"
	}
	for _, ip := range status.TailscaleIPs {
		r.Addresses = append(r.Addresses, ip.String())
	}
	return r, nil
}

func stopNativeNetwork() {
	nativeNetwork.Lock()
	node := nativeNetwork.active
	nativeNetwork.active = nil
	nativeNetwork.Unlock()
	if node != nil {
		node.close()
	}
}

func ensureNativeSOCKS(address string) error {
	nativeNetwork.Lock()
	defer nativeNetwork.Unlock()
	if nativeNetwork.proxy != nil {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	ip, parseErr := netip.ParseAddr(host)
	if err != nil || parseErr != nil || !ip.IsLoopback() {
		return errors.New("FLEET_NETWORK_SOCKS_ADDRESS_INVALID")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return errors.New("FLEET_NETWORK_SOCKS_UNAVAILABLE")
	}
	nativeNetwork.proxy = listener
	proxy := &socks5.Server{Logf: quietNetworkLog, Dialer: func(ctx context.Context, network, address string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(address)
		ip, parseErr := netip.ParseAddr(host)
		if err != nil || parseErr != nil || network != "tcp" || !(netip.MustParsePrefix("100.64.0.0/10").Contains(ip) || netip.MustParsePrefix("fd7a:115c:a1e0::/48").Contains(ip)) {
			return nil, errors.New("overlay TCP address required")
		}
		nativeNetwork.RLock()
		node := nativeNetwork.active
		nativeNetwork.RUnlock()
		if node == nil {
			return nil, errors.New("network unavailable")
		}
		return node.server.Dial(ctx, network, address)
	}}
	go func() { _ = proxy.Serve(listener) }()
	return nil
}

// Only the installation's fixed TLS Fleet listener is reachable, with all
// application authentication and certificate pinning still enforced by Backend.
func forwardNativeIngress(listener net.Listener, port int) {
	for {
		incoming, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer incoming.Close()
			outgoing, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 10*time.Second)
			if err != nil {
				return
			}
			defer outgoing.Close()
			done := make(chan struct{})
			go func() { _, _ = io.Copy(outgoing, incoming); _ = outgoing.Close(); close(done) }()
			_, _ = io.Copy(incoming, outgoing)
			_ = incoming.Close()
			<-done
		}()
	}
}

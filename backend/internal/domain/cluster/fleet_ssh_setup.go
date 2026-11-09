package cluster

import (
	"backend/internal/infra/privatefs"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"github.com/google/uuid"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/license"
	"backend/internal/fleetprotocol"
	"backend/internal/repo"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

// Setup never accepts a client-selected host or credential reference.
var sshSetupMu sync.Mutex

var ErrFleetSSHConnection = errors.New("SSH candidate connection failed")
var ErrFleetSSHNetwork = errors.New("SSH network is unavailable")
var ErrFleetSSHIdentity = errors.New("SSH host identity changed")

func managedSSHReference(nodeID string) string { return "node-" + digest([]byte(nodeID))[:32] }
func (s *Service) nodeSSHCA(nodeID string) (ssh.Signer, error) {
	ref := managedSSHReference(nodeID)
	if s.cfg.IdentityDir == "" {
		return nil, errors.New("SSH identity storage unavailable")
	}
	dir := filepath.Join(s.cfg.IdentityDir, "ssh-credentials")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	for _, path := range []string{s.cfg.IdentityDir, dir} {
		st, err := os.Lstat(path)
		if err != nil || !st.IsDir() || !privatefs.IsPrivate(path, st) {
			return nil, errors.New("SSH identity directory is unsafe")
		}
	}
	path := filepath.Join(dir, ref+".json")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		block, err := ssh.MarshalPrivateKey(key, "Tier0 per-node SSH CA")
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(map[string]any{"caPrivateKey": string(pem.EncodeToMemory(block))})
		// Publish only a complete credential. An interrupted write cannot replace a CA.
		temp, err := os.CreateTemp(dir, ".ssh-ca-*")
		if err != nil {
			return nil, err
		}
		defer os.Remove(temp.Name())
		if _, err = temp.Write(raw); err != nil {
			temp.Close()
			return nil, err
		}
		if err = temp.Sync(); err != nil {
			temp.Close()
			return nil, err
		}
		temp.Close()
		if err = os.Link(temp.Name(), path); err != nil && !os.IsExist(err) {
			return nil, err
		}
	}
	raw, err := s.sshCredential(ref)
	if err != nil {
		return nil, err
	}
	var value struct {
		CAPrivateKey string `json:"caPrivateKey"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return nil, ErrInvalid
	}
	return ssh.ParsePrivateKey([]byte(value.CAPrivateKey))
}

// Called after validating the Agent credential and signed node registration.
func (s *Service) reconcileSSHBootstrap(ctx context.Context, nodeID string, addresses []string, input *fleetprotocol.SSHAdvertisement) (*fleetprotocol.SSHBootstrap, error) {
	if input == nil {
		return nil, nil
	}
	sshSetupMu.Lock()
	defer sshSetupMu.Unlock()
	ca, err := s.nodeSSHCA(nodeID)
	if err != nil {
		return nil, err
	}
	result := &fleetprotocol.SSHBootstrap{CAPublicKey: string(ssh.MarshalAuthorizedKey(ca.PublicKey()))}
	db := repo.GetCommonConn(ctx)
	var p FleetSSHProfile
	err = db.Where("node_id=?", nodeID).Take(&p).Error
	fresh := errors.Is(err, gorm.ErrRecordNotFound) || err == nil && !p.Managed
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if fresh {
		enabled := errors.Is(err, gorm.ErrRecordNotFound) || p.Enabled
		p = FleetSSHProfile{NodeID: nodeID, Enabled: enabled, Revision: p.Revision + 1, Managed: true, AuthType: "ephemeralCertificate", CredentialRef: managedSSHReference(nodeID)}
	}
	p.SetupState, p.SetupError = "initializing", ""
	host := ""
	for _, address := range addresses {
		if validateFleetEnrollmentEndpoint("https://"+net.JoinHostPort(address, "443")) == nil {
			host = address
			break
		}
	}
	key, _, _, _, keyErr := ssh.ParseAuthorizedKey([]byte(input.HostPublicKey))
	switch {
	case input.ErrorCode != "":
		p.SetupState, p.SetupError = "failed", "SSH_INSTALLATION_REQUIRED"
	case host == "":
		p.SetupState, p.SetupError = "failed", "SSH_NETWORK_UNAVAILABLE"
	case keyErr != nil || len(input.HostPublicKey) > 16384 || !sshReference.MatchString(input.Username) || input.Username == "root" || input.Port < 1 || input.Port > 65535:
		p.SetupState, p.SetupError = "failed", "SSH_INSTALLATION_INVALID"
	case !fresh && p.HostKeyFingerprint != "" && p.HostKeyFingerprint != ssh.FingerprintSHA256(key):
		p.SetupState, p.SetupError = "failed", "SSH_HOST_KEY_CHANGED"
	default:
		if p.Host != host {
			p.Host = host
			p.Revision++
		}
		p.HostKeyFingerprint = ssh.FingerprintSHA256(key)
		p.DefaultUsername, p.DefaultPort = input.Username, input.Port
		if p.Username == "" {
			p.Username = input.Username
			p.Port = input.Port
		}
		if input.TrustedCAFingerprint == ssh.FingerprintSHA256(ca.PublicKey()) {
			s.checkManagedSSH(ctx, &p)
		} else {
			p.SetupError = "SSH_CA_PENDING"
		}
	}
	// Preserve an administrator's disabled flag; bootstrap must not re-enable it.
	if !p.Enabled {
		p.SetupState = "disabled"
	}
	if err := db.Save(&p).Error; err != nil {
		return nil, err
	}
	return result, nil
}
func (s *Service) checkManagedSSH(ctx context.Context, p *FleetSSHProfile) {
	p.LastCheckedAt = time.Now().UnixMilli()
	if !p.Enabled {
		p.SetupState, p.SetupError = "disabled", ""
		return
	}
	if err := s.requireRemoteSSH(ctx, p.NodeID); err != nil {
		p.SetupState, p.SetupError = "failed", "SSH_ACCESS_UNAVAILABLE"
		if errors.Is(err, ErrFleetSSHNotAllowed) {
			p.SetupState, p.SetupError = "disabled", "EDGE_SSH_DISABLED"
		}
		return
	}
	if p.Host == "" || p.HostKeyFingerprint == "" || p.Username == "" {
		p.SetupState, p.SetupError = "failed", "SSH_INSTALLATION_REQUIRED"
		return
	}
	connection, err := s.sshConnect(ctx, *p)
	if err != nil {
		p.SetupState, p.SetupError = "failed", sshFailureCode(err)
		return
	}
	connection.Close()
	p.SetupState, p.SetupError = "ready", ""
}

type FleetSSHSetupRequest struct {
	NodeID           string               `json:"nodeId"`
	ExpectedRevision int64                `json:"expectedRevision"`
	Username         string               `json:"username,omitempty"`
	Port             int                  `json:"port,omitempty"`
	Enabled          *bool                `json:"enabled,omitempty"`
	ConnectionMode   string               `json:"connectionMode,omitempty"`
	AuthType         string               `json:"authType,omitempty"`
	Reset            bool                 `json:"reset,omitempty"`
	Credentials      *FleetSSHCredentials `json:"credentials,omitempty"`
}

type FleetSSHCredentials struct {
	Password    string `json:"password,omitempty"`
	PrivateKey  string `json:"privateKey,omitempty"`
	Passphrase  string `json:"passphrase,omitempty"`
	Certificate string `json:"certificate,omitempty"`
}

// Candidate secrets are private files and are never included in API responses or audits.
func (s *Service) storeSSHCredential(value FleetSSHCredentials) (string, error) {
	dir := filepath.Join(s.cfg.IdentityDir, "ssh-credentials")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	for _, path := range []string{s.cfg.IdentityDir, dir} {
		st, err := os.Lstat(path)
		if err != nil || !st.IsDir() || !privatefs.IsPrivate(path, st) {
			return "", ErrInvalid
		}
	}
	ref := "credential-" + uuid.NewString()
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 128*1024 {
		return "", ErrInvalid
	}
	f, err := os.OpenFile(filepath.Join(dir, ref+".json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		os.Remove(f.Name())
		return "", errors.Join(err, closeErr)
	}
	return ref, nil
}

func (s *Service) FleetSSHSetup(ctx context.Context, op string, req FleetSSHSetupRequest) (any, error) {
	actor, ok := contextx.SubjectFrom(ctx)
	if !ok || !license.Default().IsHost() || !contextx.HasResource(actor, "fleet.ssh.manage") || actor.APIKeyAppID != "" {
		return nil, ErrUnauthorized
	}
	if op != "sshSetupGet" && op != "sshSetupRetry" && op != "sshSetupUpdate" {
		return nil, ErrInvalid
	}
	// Retry checks the saved connection. It must not bypass candidate validation
	// by accepting settings that belong to the update operation.
	if op == "sshSetupRetry" && (req.Username != "" || req.Port != 0 || req.Enabled != nil || req.ConnectionMode != "" || req.AuthType != "" || req.Credentials != nil || req.Reset) {
		return nil, ErrInvalid
	}
	node, err := s.fleetRepo.CollaborationNode(ctx, req.NodeID)
	if err != nil {
		return nil, err
	}
	sshSetupMu.Lock()
	defer sshSetupMu.Unlock()
	db := repo.GetCommonConn(ctx)
	var p FleetSSHProfile
	err = db.Where("node_id=?", req.NodeID).Take(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && !p.Managed {
		if op == "sshSetupUpdate" {
			return nil, errors.New("SSH Agent initialization required before changing settings")
		}
		return map[string]any{"nodeId": req.NodeID, "setupState": "initializing", "setupError": "SSH_WAITING_FOR_AGENT", "revision": p.Revision, "enabled": false}, nil
	}
	if err != nil {
		return nil, err
	}
	if op == "sshSetupGet" {
		// Edge consent may be restored independently of the center profile.
		// Refresh this cached denial without requiring a profile edit or re-enrollment.
		if p.Enabled && p.SetupError == "EDGE_SSH_DISABLED" {
			s.checkManagedSSH(ctx, &p)
			if err := db.Save(&p).Error; err != nil {
				return nil, err
			}
		}
		return sshSetupView(p), nil
	}
	if req.ExpectedRevision != p.Revision {
		return nil, &FleetRevisionConflict{CurrentRevision: p.Revision}
	}
	original := p
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if req.Reset {
		if req.Username != "" || req.Port != 0 || req.ConnectionMode != "" || req.AuthType != "" || req.Credentials != nil {
			return nil, ErrInvalid
		}
		if p.DefaultUsername == "" || p.DefaultPort == 0 {
			return nil, ErrFleetSSHConnection
		}
		p.Username, p.Port, p.ConnectionMode, p.AuthType, p.CredentialRef = p.DefaultUsername, p.DefaultPort, "automatic", "ephemeralCertificate", managedSSHReference(req.NodeID)
	} else {
		if req.ConnectionMode != "" {
			if req.ConnectionMode != "automatic" && req.ConnectionMode != "custom" {
				return nil, ErrInvalid
			}
			p.ConnectionMode = req.ConnectionMode
		}
		if req.Username != "" {
			if req.Username == "root" || !sshReference.MatchString(req.Username) {
				return nil, ErrInvalid
			}
			p.Username = req.Username
		}
		if req.Port != 0 {
			if req.Port < 1 || req.Port > 65535 {
				return nil, ErrInvalid
			}
			p.Port = req.Port
		}
		if req.AuthType != "" {
			p.AuthType = req.AuthType
		}
		if p.ConnectionMode != "custom" {
			p.ConnectionMode = "automatic"
			p.AuthType = "ephemeralCertificate"
		}
		switch p.AuthType {
		case "password", "key", "certificate", "ephemeralCertificate":
		default:
			return nil, ErrInvalid
		}
		if p.AuthType == "ephemeralCertificate" {
			if req.Credentials != nil {
				return nil, ErrInvalid
			}
			p.CredentialRef = managedSSHReference(req.NodeID)
		}
	}
	changed := p.Username != original.Username || p.Port != original.Port || p.AuthType != original.AuthType || p.ConnectionMode != original.ConnectionMode || req.Credentials != nil || req.Reset
	if !p.Enabled && changed {
		return nil, ErrInvalid
	}
	if p.Enabled {
		if err := s.requireRemoteSSH(ctx, req.NodeID); err != nil {
			return nil, err
		}
	}
	credentialRef := ""
	committed := false
	if req.Credentials != nil {
		if p.AuthType == "ephemeralCertificate" || p.AuthType == "password" && req.Credentials.Password == "" || p.AuthType != "password" && req.Credentials.PrivateKey == "" || p.AuthType == "certificate" && req.Credentials.Certificate == "" {
			return nil, ErrInvalid
		}
		credentialRef, err = s.storeSSHCredential(*req.Credentials)
		if err != nil {
			return nil, err
		}
		p.CredentialRef = credentialRef
		defer func() {
			if !committed {
				os.Remove(filepath.Join(s.cfg.IdentityDir, "ssh-credentials", credentialRef+".json"))
			}
		}()
	} else if p.AuthType != "ephemeralCertificate" && (p.AuthType != original.AuthType || p.Username != original.Username) {
		// A different account must supply and verify its own credential.
		return nil, ErrInvalid
	}
	// Resolve through the identity registered for this Fleet Node, never a submitted IP.
	state, addresses := "", []string(nil)
	if p.Enabled {
		state, addresses = s.fleetNetworkStatus(ctx, req.NodeID, node.NetworkNodeID)
	}
	if p.Enabled && state != "direct" {
		return nil, ErrFleetSSHNetwork
	}
	matched := false
	for _, address := range addresses {
		if address == p.Host {
			matched = true
		}
	}
	if p.Enabled && !matched {
		return nil, ErrFleetSSHNetwork
	}
	if p.Enabled && original.SetupError == "SSH_HOST_KEY_CHANGED" {
		return nil, ErrFleetSSHIdentity
	}
	s.checkManagedSSH(ctx, &p)
	if p.Enabled && p.SetupState != "ready" && op == "sshSetupUpdate" {
		return nil, sshFailure(p.SetupError)
	}
	if op == "sshSetupUpdate" {
		p.Revision++
	}
	if err := db.Save(&p).Error; err != nil {
		return nil, err
	}
	committed = true
	if !p.Enabled {
		closeNodeTerminals(req.NodeID)
	}
	s.auditSSH(actor.UserID, req.NodeID, op, map[string]any{"revision": p.Revision, "username": p.Username, "port": p.Port, "enabled": p.Enabled, "state": p.SetupState})
	return sshSetupView(p), nil
}
func sshSetupView(p FleetSSHProfile) any {
	return map[string]any{"nodeId": p.NodeID, "connectionMode": p.ConnectionMode, "authType": p.AuthType, "credentialConfigured": p.CredentialRef != "", "defaultUsername": p.DefaultUsername, "defaultPort": p.DefaultPort, "host": p.Host, "port": p.Port, "username": p.Username, "hostKeyFingerprint": p.HostKeyFingerprint, "enabled": p.Enabled, "revision": p.Revision, "setupState": p.SetupState, "setupError": p.SetupError, "lastCheckedAt": p.LastCheckedAt}
}
func closeNodeTerminals(nodeID string) {
	fleetTerminals.Range(func(_, value any) bool {
		t := value.(*fleetTerminal)
		if t.row.NodeID == nodeID {
			t.cancel()
		}
		return true
	})
}

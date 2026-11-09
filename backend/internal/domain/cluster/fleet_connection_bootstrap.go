package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"backend/internal/domain/fleettunnel"
	"backend/internal/domain/license"
	"backend/internal/fleetprotocol"
	"backend/internal/repo"
)

// This is a node protocol, not public CRUD. Both token possession and the
// installation's signed proof are required; neither becomes an IAM subject.
type FleetConnectionBootstrapRequest struct {
	Action    string               `json:"action"`
	ClientID  string               `json:"clientId"`
	Proof     FleetEnrollmentProof `json:"proof"`
	Signature string               `json:"signature"`
}
type fleetCenterRegistration struct {
	CenterType           string `json:"centerType,omitempty"`
	NetworkMode          string `json:"networkMode,omitempty"`
	CenterID             string `json:"centerId"`
	CenterPublicKey      string `json:"centerPublicKey"`
	CenterURL            string `json:"centerUrl,omitempty"`
	BusinessEndpoint     string `json:"businessEndpoint,omitempty"`
	CenterTLSFingerprint string `json:"centerTlsFingerprint,omitempty"`
	NodeID               string `json:"nodeId"`
	KeyID                string `json:"keyId"`
	RegisteredAt         int64  `json:"registeredAt"`
	LoginServer          string `json:"loginServer,omitempty"`
}

func (s *Service) FleetConnectionBootstrap(ctx context.Context, authorization string, req FleetConnectionBootstrapRequest) (any, error) {
	if !s.CollaborationEnabled() || !license.Default().CenterManagementEnabled() {
		return nil, ErrUnauthorized
	}
	header := http.Request{Header: http.Header{"Authorization": []string{authorization}}}
	username, password, ok := header.BasicAuth()
	if !ok {
		return nil, ErrUnauthorized
	}
	if _, err := s.AuthorizeFleetTunnel(ctx, fleettunnel.Identity{Username: username, Password: password, ClientID: req.ClientID, NodeID: req.Proof.NodeID}); err != nil {
		return nil, ErrUnauthorized
	}
	p := req.Proof
	now := time.Now().UnixMilli()
	key, err := base64.RawURLEncoding.DecodeString(p.PublicKey)
	sig, e := base64.RawURLEncoding.DecodeString(req.Signature)
	raw, _ := json.Marshal(p)
	if err != nil || e != nil || len(key) != ed25519.PublicKeySize || p.InstallationID == "" || len(p.InstallationID) > 128 || p.Timestamp < now-60000 || p.Timestamp > now+30000 || len(p.Nonce) < 16 || len(p.Nonce) > 128 || !ed25519.Verify(key, append([]byte("tier0-fleet-connection-v1/"+req.Action+"\n"), raw...), sig) {
		return nil, ErrUnauthorized
	}
	if req.Action != "prepare" && req.Action != "register" && req.Action != "verify" {
		return nil, ErrInvalid
	}
	// Preparing a connection does not depend on a VPN membership or an existing
	// business registration. The authenticated register step creates its projection.
	node, err := s.fleetRepo.CollaborationNode(ctx, p.NodeID)
	if err != nil && err != repo.ErrNotFound {
		return nil, err
	}
	if node.RegistrationStatus == "registered" && (node.InstallationID != p.InstallationID || node.PublicKey != p.PublicKey) {
		return nil, repo.ErrConflict
	}
	s.mu.Lock()
	identity, e := s.fleetIdentity()
	s.mu.Unlock()
	if e != nil {
		return nil, e
	}
	private, _ := base64.RawURLEncoding.DecodeString(identity.PrivateKey)
	result := fleetCenterRegistration{CenterID: identity.InstallationID, CenterPublicKey: base64.RawURLEncoding.EncodeToString(ed25519.PrivateKey(private).Public().(ed25519.PublicKey)), NodeID: p.NodeID, KeyID: digest(key), RegisteredAt: node.RegisteredAt, CenterType: "enterprise", NetworkMode: fleetprotocol.NetworkModeDirect}
	result.BusinessEndpoint, result.CenterTLSFingerprint, err = s.connectionEndpoint(ctx)
	if err != nil {
		return nil, err
	}
	if req.Action == "prepare" {
		return result, nil
	}
	// A retry may read its own receipt, but can never silently take over another
	// installation. Endpoint changes require the same installation proof.
	if err = s.validateNetworkEndpoint(ctx, p); err != nil {
		return nil, err
	}
	if req.Action == "verify" {
		if node.RegistrationStatus != "registered" {
			return nil, ErrUnauthorized
		}
		if _, err = s.CallFleetNode(ctx, p.NodeID, "capabilities.get", "node", struct{}{}); err != nil {
			return nil, err
		}
		return result, nil
	}
	if node.RegistrationStatus == "registered" {
		if node.Endpoint != strings.TrimRight(p.Endpoint, "/") || node.TLSFingerprint != p.TLSFingerprint {
			if p.TLSFingerprint == "" {
				return nil, repo.ErrConflict
			}
			if err = s.fleetRepo.RefreshCollaborationEndpoint(ctx, p.NodeID, p.PublicKey, strings.TrimRight(p.Endpoint, "/"), p.TLSFingerprint); err != nil {
				return nil, err
			}
		}
		return result, nil
	}
	if err = s.fleetRepo.EnsureFleetCollaborationNode(ctx, p.NodeID); err != nil {
		return nil, err
	}
	codeBytes := make([]byte, 32)
	if _, err = rand.Read(codeBytes); err != nil {
		return nil, err
	}
	code := base64.RawURLEncoding.EncodeToString(codeBytes)
	if err = s.fleetRepo.CreateCollaborationEnrollment(ctx, repo.FleetCollaborationEnrollment{NodeID: p.NodeID, CodeHash: digest([]byte(code)), ExpiresAt: now + enrollmentLifetime.Milliseconds()}); err != nil {
		return nil, err
	}
	// Same repository registration transaction and invariants as explicit signed
	// enrollment. Here the authenticated token replaces the one-time UI code.
	if err = validateFleetEnrollmentEndpoint(p.Endpoint); err != nil {
		return nil, err
	}
	enrolled, err := s.fleetRepo.EnrollCollaborationNode(ctx, digest([]byte(code)), digest(raw), repo.FleetCollaborationNode{TLSFingerprint: p.TLSFingerprint, InstallationID: p.InstallationID, NodeID: p.NodeID, PublicKey: p.PublicKey, KeyID: digest(key), Endpoint: strings.TrimRight(p.Endpoint, "/"), NetworkStatus: "direct", BusinessStatus: "unchecked"}, now, p.Timestamp)
	if err != nil {
		return nil, err
	}
	result.RegisteredAt = enrolled.RegisteredAt
	return result, nil
}

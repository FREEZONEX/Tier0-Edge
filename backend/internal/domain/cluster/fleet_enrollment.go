package cluster

import (
	"backend/internal/contextx"
	"backend/internal/domain/license"
	"backend/internal/repo"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"time"
)

const enrollmentLifetime = 10 * time.Minute

func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func (s *Service) CreateCollaborationEnrollment(ctx context.Context, nodeID string) (map[string]any, error) {
	if !license.Default().IsHost() {
		return nil, ErrUnauthorized
	}
	actor, ok := contextx.SubjectFrom(ctx)
	if !ok || !contextx.HasResource(actor, "cluster.edge.manage") {
		return nil, ErrUnauthorized
	}
	if _, err := s.fleetRepo.CollaborationNode(ctx, nodeID); err != nil {
		return nil, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().Add(enrollmentLifetime).UnixMilli()
	if err := s.fleetRepo.CreateCollaborationEnrollment(ctx, repo.FleetCollaborationEnrollment{NodeID: nodeID, CodeHash: digest([]byte(code)), ExpiresAt: expires, CreatedBy: actor.UserID}); err != nil {
		return nil, err
	}
	return map[string]any{"nodeId": nodeID, "enrollmentCode": code, "expiresAt": expires}, nil
}

// Enrollment proof signs these exact JSON fields in declaration order. It is
// domain-separated from MQTT tokens, overlay keys and later task credentials.
type FleetEnrollmentProof struct {
	TLSFingerprint string `json:"tlsFingerprint,omitempty"`
	InstallationID string `json:"installationId"`
	NodeID         string `json:"nodeId"`
	EnrollmentCode string `json:"enrollmentCode"`
	PublicKey      string `json:"publicKey"`
	Endpoint       string `json:"endpoint"`
	NetworkNodeID  string `json:"networkNodeId"`
	Timestamp      int64  `json:"timestamp"`
	Nonce          string `json:"nonce"`
}
type FleetEnrollRequest struct {
	Proof     FleetEnrollmentProof `json:"proof"`
	Signature string               `json:"signature"`
}

func (s *Service) EnrollCollaborationNode(ctx context.Context, req FleetEnrollRequest) (any, error) {
	if !license.Default().IsHost() {
		return nil, ErrUnauthorized
	}
	p := req.Proof
	now := time.Now().UnixMilli()
	if p.InstallationID == "" || len(p.InstallationID) > 128 || p.Timestamp <= 0 || p.Timestamp > now+30000 || len(p.Nonce) < 16 || len(p.Nonce) > 128 || len(p.EnrollmentCode) != 43 || p.NodeID == "" || len(p.NetworkNodeID) > 128 {
		return nil, ErrUnauthorized
	}
	if err := s.validateNetworkEndpoint(ctx, p); err != nil {
		return nil, err
	}
	key, err := base64.RawURLEncoding.DecodeString(p.PublicKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, ErrUnauthorized
	}
	signature, err := base64.RawURLEncoding.DecodeString(req.Signature)
	if err != nil {
		return nil, ErrUnauthorized
	}
	body, err := json.Marshal(p)
	if err != nil {
		return nil, ErrInvalid
	}
	signed := append([]byte("tier0-fleet-enrollment-v1\n"), body...)
	if !ed25519.Verify(ed25519.PublicKey(key), signed, signature) {
		return nil, ErrUnauthorized
	}
	node, err := s.fleetRepo.EnrollCollaborationNode(ctx, digest([]byte(p.EnrollmentCode)), digest(signed), repo.FleetCollaborationNode{InstallationID: p.InstallationID, NodeID: p.NodeID, PublicKey: p.PublicKey, KeyID: digest(key), Endpoint: strings.TrimRight(p.Endpoint, "/"), TLSFingerprint: p.TLSFingerprint, NetworkStatus: "direct", BusinessStatus: "unchecked"}, now, p.Timestamp)
	if err != nil {
		return nil, ErrUnauthorized
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, err := s.fleetIdentity()
	if err != nil {
		return nil, err
	}
	private, _ := base64.RawURLEncoding.DecodeString(identity.PrivateKey)
	centerPublic := ed25519.PrivateKey(private).Public().(ed25519.PublicKey)
	return map[string]any{"centerId": identity.InstallationID, "centerPublicKey": base64.RawURLEncoding.EncodeToString(centerPublic), "nodeId": node.NodeID, "keyId": node.KeyID, "registrationStatus": node.RegistrationStatus, "registeredAt": node.RegisteredAt}, nil
}

func validateFleetEnrollmentEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || len(endpoint) > 2048 {
		return ErrInvalid
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return ErrInvalid
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
		return ErrInvalid
	}
	return nil
}

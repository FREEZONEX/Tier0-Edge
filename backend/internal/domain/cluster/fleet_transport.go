package cluster

import (
	"backend/internal/domain/unsv2"
	"backend/internal/fleetprotocol"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"

	"errors"
	"fmt"
	"golang.org/x/net/proxy"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backend/internal/domain/license"
	"backend/internal/repo"
	"backend/internal/unsref"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Transport identities never become a user Session or an administrator Subject.
// Every handler additionally checks its exact operation and target resource.
var ErrFleetPeerUnavailable = errors.New("Fleet peer connection unavailable")
var ErrFleetAuthorizationRevoked = errors.New("Fleet task authorization explicitly revoked")

func fleetCenterScope(scope string) bool {
	return scope == "storage.definition" || scope == "storage.target"
}

type cachedFleetToken struct {
	Token     string
	ExpiresAt int64
}

type FleetServiceClaims struct {
	Automatic     bool   `json:"automatic,omitempty"`
	Issuer        string `json:"iss"`
	Audience      string `json:"aud"`
	Subject       string `json:"sub"`
	Scope         string `json:"scope"`
	ResourceID    string `json:"resourceId"`
	GrantID       string `json:"grantId,omitempty"`
	GrantRevision int64  `json:"grantRevision,omitempty"`
	IssuedAt      int64  `json:"iat"`
	ExpiresAt     int64  `json:"exp"`
	TokenID       string `json:"jti"`
}
type FleetExchangeProof struct {
	ConsumerID string `json:"consumerId,omitempty"`
	NodeID     string `json:"nodeId"`
	Scope      string `json:"scope"`
	ResourceID string `json:"resourceId"`
	Timestamp  int64  `json:"timestamp"`
	Nonce      string `json:"nonce"`
}
type FleetExchangeRequest struct {
	Proof     FleetExchangeProof `json:"proof"`
	Signature string             `json:"signature"`
}
type fleetServiceGrant struct {
	GrantID    string `gorm:"primaryKey"`
	NodeID     string
	Scope      string
	ResourceID string
	Revision   int64
	Enabled    bool
	CreatedBy  int64
	CreatedAt  int64
}

func (fleetServiceGrant) TableName() string { return "fleet_service_grant" }

func (s *Service) signFleetToken(claims FleetServiceClaims) (string, error) {
	identity, err := s.fleetIdentity()
	if err != nil {
		return "", err
	}
	rawKey, _ := base64.RawURLEncoding.DecodeString(identity.PrivateKey)
	key := ed25519.PrivateKey(rawKey)
	header, _ := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "JWT", "kid": digest(key.Public().(ed25519.PublicKey))})
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signed := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body)
	return signed + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(signed))), nil
}
func verifyFleetToken(token, public string) (FleetServiceClaims, error) {
	var claims FleetServiceClaims
	if len(token) > 8192 {
		return claims, ErrUnauthorized
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims, ErrUnauthorized
	}
	key, err := base64.RawURLEncoding.DecodeString(public)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return claims, ErrUnauthorized
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claims, ErrUnauthorized
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
		Kid string `json:"kid"`
	}
	if json.Unmarshal(raw, &header) != nil || header.Alg != "EdDSA" || header.Typ != "JWT" || header.Kid != digest(key) {
		return claims, ErrUnauthorized
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !ed25519.Verify(key, []byte(parts[0]+"."+parts[1]), sig) {
		return claims, ErrUnauthorized
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(body, &claims) != nil {
		return claims, ErrUnauthorized
	}
	now := time.Now().Unix()
	if claims.IssuedAt > now+30 || claims.IssuedAt < now-120 || claims.ExpiresAt <= now || claims.ExpiresAt > claims.IssuedAt+120 || claims.TokenID == "" {
		return claims, ErrUnauthorized
	}
	return claims, nil
}
func (s *Service) ExchangeFleetToken(ctx context.Context, req FleetExchangeRequest) (any, error) {
	if !s.CollaborationEnabled() || !license.Default().IsHost() {
		return nil, ErrUnauthorized
	}
	p := req.Proof
	now := time.Now().UnixMilli()
	if p.Timestamp < now-60000 || p.Timestamp > now+30000 || len(p.Nonce) < 16 || len(p.Nonce) > 128 || !fleetCenterScope(p.Scope) || p.ResourceID == "" {
		return nil, ErrUnauthorized
	}
	node, err := s.fleetRepo.CollaborationNode(ctx, p.NodeID)
	if err != nil || node.RegistrationStatus != "registered" {
		return nil, ErrUnauthorized
	}
	public, err := base64.RawURLEncoding.DecodeString(node.PublicKey)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return nil, ErrUnauthorized
	}
	body, _ := json.Marshal(p)
	sig, err := base64.RawURLEncoding.DecodeString(req.Signature)
	// Method and endpoint are fixed by the signed domain, all body fields covered.
	if err != nil || !ed25519.Verify(public, append([]byte("POST /api/node/v1/fleet/execution/tokens/exchange\n"), body...), sig) {
		return nil, ErrUnauthorized
	}
	if p.Scope == "storage.definition" {
		if p.ResourceID != "node" || p.ConsumerID != "" {
			return nil, ErrUnauthorized
		}
		base := fleetServiceGrant{GrantID: storageGrant(p.NodeID, "definition"), NodeID: p.NodeID, Scope: p.Scope, ResourceID: "node", Revision: 1, Enabled: true, CreatedAt: now}
		if err := repo.GetCommonConn(ctx).Where("grant_id=?", base.GrantID).FirstOrCreate(&base).Error; err != nil {
			return nil, err
		}
	}
	var grant fleetServiceGrant
	err = repo.GetCommonConn(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where("node_id=? AND scope=? AND resource_id=? AND enabled=true", p.NodeID, p.Scope, p.ResourceID)
		if p.ConsumerID != "" {
			query = query.Where("grant_id=?", p.ConsumerID)
		} else {
			var count int64
			if err := query.Model(&fleetServiceGrant{}).Count(&count).Error; err != nil || count < 1 || count != 1 {
				return ErrUnauthorized
			}
		}
		if err := query.Take(&grant).Error; err != nil {
			// The installation proof was verified above. A known disabled ingress
			// grant is terminal; missing grants and token expiry remain distinct.
			if errors.Is(err, gorm.ErrRecordNotFound) && p.Scope == "ingress.batch" && p.ConsumerID != "" {
				var revoked int64
				if e := tx.Model(&fleetServiceGrant{}).Where("grant_id=? AND node_id=? AND scope=? AND resource_id=? AND enabled=false", p.ConsumerID, p.NodeID, p.Scope, p.ResourceID).Count(&revoked).Error; e != nil {
					return e
				} else if revoked == 1 {
					return ErrFleetAuthorizationRevoked
				}
			}
			return err
		}
		if err := tx.Exec("DELETE FROM fleet_proof_nonce WHERE expires_at < ?", now).Error; err != nil {
			return err
		}
		return tx.Exec("INSERT INTO fleet_proof_nonce(node_id,nonce,expires_at) VALUES (?,?,?)", p.NodeID, p.Nonce, now+120000).Error
	})
	if err != nil {
		if errors.Is(err, ErrFleetAuthorizationRevoked) {
			return nil, err
		}
		return nil, ErrUnauthorized
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, err := s.fleetIdentity()
	if err != nil {
		return nil, err
	}
	claims := FleetServiceClaims{Issuer: identity.InstallationID, Audience: identity.InstallationID, Subject: p.NodeID, Scope: p.Scope, ResourceID: p.ResourceID, GrantID: grant.GrantID, GrantRevision: grant.Revision, IssuedAt: time.Now().Unix(), ExpiresAt: time.Now().Unix() + 90, TokenID: uuid.NewString()}
	token, err := s.signFleetToken(claims)
	if err != nil {
		return nil, err
	}
	return map[string]any{"token": token, "expiresAt": claims.ExpiresAt * 1000}, nil
}

// VerifyFleetServiceToken pins the center identity persisted during TLS enrollment.
// The receiving center also rechecks the live grant for immediate revocation.
func (s *Service) VerifyFleetServiceToken(ctx context.Context, token, scope, resource string) (FleetServiceClaims, error) {
	// Centers accept Edge uploads, but cannot be managed as another center's Edge.
	// Recheck the current role even when a previous connection/key is still saved.
	if !fleetCenterScope(scope) && license.Default().IsHost() {
		return FleetServiceClaims{}, ErrHostCannotConnect
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, err := s.fleetIdentity()
	if err != nil {
		return FleetServiceClaims{}, err
	}
	rawKey, _ := base64.RawURLEncoding.DecodeString(identity.PrivateKey)
	public := base64.RawURLEncoding.EncodeToString(ed25519.PrivateKey(rawKey).Public().(ed25519.PublicKey))
	issuer := identity.InstallationID
	if !fleetCenterScope(scope) {
		raw, err := os.ReadFile(filepath.Join(s.cfg.IdentityDir, "connection.json"))
		if err != nil {
			return FleetServiceClaims{}, ErrUnauthorized
		}
		var connection struct {
			CenterID        string `json:"centerId"`
			CenterPublicKey string `json:"centerPublicKey"`
		}
		if json.Unmarshal(raw, &connection) != nil || connection.CenterID == "" {
			return FleetServiceClaims{}, ErrUnauthorized
		}
		issuer, public = connection.CenterID, connection.CenterPublicKey
	}
	claims, err := verifyFleetToken(token, public)
	if err != nil || claims.Issuer != issuer || claims.Audience != identity.InstallationID || claims.Scope != scope || claims.ResourceID != resource {
		return claims, ErrUnauthorized
	}
	if fleetCenterScope(scope) {
		if !license.Default().IsHost() {
			return claims, ErrUnauthorized
		}
		node, nodeErr := s.fleetRepo.CollaborationNode(ctx, claims.Subject)
		if nodeErr != nil || node.RegistrationStatus != "registered" {
			return claims, ErrUnauthorized
		}
		var count int64
		query := repo.GetCommonConn(ctx).Model(&fleetServiceGrant{}).Where("grant_id=? AND node_id=? AND revision=? AND enabled=true", claims.GrantID, claims.Subject, claims.GrantRevision)
		query = query.Where("scope=? AND resource_id=?", scope, resource)
		err := query.Count(&count).Error
		if err != nil {
			return claims, err
		}
		if count != 1 {
			if scope == "ingress.batch" {
				var revoked int64
				if e := repo.GetCommonConn(ctx).Model(&fleetServiceGrant{}).Where("grant_id=? AND node_id=? AND scope=? AND resource_id=? AND enabled=false", claims.GrantID, claims.Subject, scope, resource).Count(&revoked).Error; e != nil {
					return claims, e
				} else if revoked == 1 {
					return claims, ErrFleetAuthorizationRevoked
				}
			}
			return claims, ErrUnauthorized
		}
	} else if claims.Subject != "center:"+issuer {
		return claims, ErrUnauthorized
	}
	return claims, nil
}

// CallFleetNode only accepts a persisted node and a fixed operation allowlist.
// No caller-provided URL, Cookie, API key or arbitrary forwarding endpoint.
func (s *Service) CallFleetNode(ctx context.Context, nodeID, scope, resource string, payload any) (json.RawMessage, error) {
	paths := map[string]string{"runtime.execute": "/runtime/execute", "infrastructure.snapshot": "/infrastructure/snapshot", "infrastructure.detail": "/infrastructure/detail", "infrastructure.logs": "/infrastructure/logs", "infrastructure.restart": "/infrastructure/restart", "storage.get": "/storage/get", "storage.save": "/storage/save", "collections.catalog": "/collections/catalog", "users.apply": "/users/apply", "capabilities.get": "/capabilities/get"}
	for _, action := range []string{"list", "logs", "operation", "control.start", "control.stop", "control.restart"} {
		paths["infrastructure.runtime."+action] = "/infrastructure/runtime"
	}
	path, ok := paths[scope]
	if !ok || !license.Default().IsHost() {
		return nil, ErrUnauthorized
	}
	node, err := s.fleetRepo.CollaborationNode(ctx, nodeID)
	if err != nil || node.RegistrationStatus != "registered" || node.InstallationID == "" {
		return nil, ErrUnauthorized
	}
	s.mu.Lock()
	identity, err := s.fleetIdentity()
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	now := time.Now().Unix()
	token, err := s.signFleetToken(FleetServiceClaims{Issuer: identity.InstallationID, Audience: node.InstallationID, Subject: "center:" + identity.InstallationID, Scope: scope, ResourceID: resource, IssuedAt: now, ExpiresAt: now + 90, TokenID: uuid.NewString()})
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"resourceId": resource, "payload": payload})
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, fleetTLSTargetKey{}, fleetTLSTarget{origin: node.Endpoint, pin: node.TLSFingerprint})
	return s.fleetHTTP(ctx, node.Endpoint+"/api/node/v1/fleet/execution"+path, token, body)
}
func (s *Service) fleetHTTP(ctx context.Context, endpoint, token string, body []byte) (json.RawMessage, error) {
	return s.fleetHTTPHeaders(ctx, endpoint, token, body, nil)
}

// Control transport preserves the canonical business body and the fixed
// deployment/expiry headers. Only registered targets and exact actions exist.
func (s *Service) CallFleetControl(ctx context.Context, nodeID, code, action, grantID string, revision int64, body any, headers map[string]string) (json.RawMessage, error) {
	return s.callFleetControl(ctx, nodeID, code, action, grantID, revision, body, headers, false)
}

func (s *Service) callFleetControl(ctx context.Context, nodeID, code, action, grantID string, revision int64, body any, headers map[string]string, automatic bool) (json.RawMessage, error) {
	paths := map[string]string{"discover": "/controls/discover", "describe": "/controls/" + code + "/describe", "invoke": "/controls/" + code + "/invoke", "get": "/controls/" + code + "/invocations/get"}
	path, ok := paths[action]
	if !ok || !s.CollaborationEnabled() || !license.Default().IsHost() || !unsref.ValidMetadataID(code) || action == "discover" && code != "node" {
		return nil, ErrUnauthorized
	}
	for key := range headers {
		if action != "invoke" || key != "Idempotency-Key" && key != "X-Control-Capability-Version" && key != "X-Control-Expires-At" {
			return nil, ErrInvalid
		}
	}
	if action == "invoke" {
		if err := s.requireRemoteControl(ctx, nodeID); err != nil {
			return nil, err
		}
	}
	node, err := s.fleetRepo.CollaborationNode(ctx, nodeID)
	if err != nil || node.RegistrationStatus != "registered" || node.InstallationID == "" {
		return nil, ErrUnauthorized
	}
	s.mu.Lock()
	identity, err := s.fleetIdentity()
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	now := time.Now().Unix()
	token, err := s.signFleetToken(FleetServiceClaims{Issuer: identity.InstallationID, Automatic: automatic, Audience: node.InstallationID, Subject: "center:" + identity.InstallationID, Scope: "controls." + action, ResourceID: code, GrantID: grantID, GrantRevision: revision, IssuedAt: now, ExpiresAt: now + 90, TokenID: uuid.NewString()})
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, fleetTLSTargetKey{}, fleetTLSTarget{origin: node.Endpoint, pin: node.TLSFingerprint})
	return s.fleetHTTPHeaders(ctx, node.Endpoint+"/openapi/v1"+path, token, raw, headers)
}

func (s *Service) fleetHTTPHeaders(ctx context.Context, endpoint, token string, body []byte, headers map[string]string) (json.RawMessage, error) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, err
	}
	if s.cfg.TLSCAFile != "" {
		pem, err := os.ReadFile(s.cfg.TLSCAFile)
		if err != nil {
			return nil, err
		}
		if !roots.AppendCertsFromPEM(pem) {
			return nil, ErrInvalid
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = s.dialFleetEndpoint
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	if err := s.pinFleetEndpointTLS(ctx, endpoint, transport.TLSClientConfig); err != nil {
		return nil, err
	}
	defer transport.CloseIdleConnections()
	requestTimeout := 20 * time.Second
	// Restart is the existing bounded Agent command (120s), not a generic RPC.
	if strings.HasSuffix(endpoint, "/api/node/v1/fleet/execution/infrastructure/restart") {
		requestTimeout = 130 * time.Second
	}
	client := http.Client{Transport: transport, Timeout: requestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, ErrFleetPeerUnavailable
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return nil, ErrInvalid
	}
	if response.StatusCode != 200 {
		// Preserve only the documented revision conflict, never arbitrary peer errors.
		var conflict struct {
			Error struct {
				Code   string `json:"code"`
				Path   string `json:"path"`
				Reason string `json:"reason"`
			} `json:"error"`
			Data struct {
				CurrentRevision *int64 `json:"currentRevision"`
			} `json:"data"`
		}
		_ = json.Unmarshal(raw, &conflict)
		if response.StatusCode == 422 && conflict.Error.Code == "RUNTIME_OPERATION_FAILED" {
			return nil, &unsv2.Fault{Code: "RUNTIME_OPERATION_FAILED", Message: "Edge runtime operation failed"}
		}
		if response.StatusCode == 409 && conflict.Error.Code == "VERSION_CONFLICT" {
			if conflict.Data.CurrentRevision == nil {
				return nil, &unsv2.Fault{Code: "VERSION_CONFLICT", Message: "Edge configuration changed; refresh before retrying"}
			}
			return nil, &FleetRevisionConflict{CurrentRevision: *conflict.Data.CurrentRevision}
		}
		if (response.StatusCode == 400 || response.StatusCode == 422) && conflict.Error.Code == "INVALID_REQUEST" {
			return nil, &unsv2.Fault{Code: "INVALID_REQUEST", Message: "Edge rejected the configuration", Path: conflict.Error.Path, Reason: conflict.Error.Reason}
		}
		return nil, &fleetPeerError{Status: response.StatusCode, Code: conflict.Error.Code}
	}
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Code != 200 || len(envelope.Data) == 0 {
		return nil, ErrInvalid
	}
	return envelope.Data, nil
}

func (s *Service) StartCollaborationWorker() {
	if !s.CollaborationEnabled() {
		return
	}
	s.StartCollectionStorage()
	go s.runFleetWorker(s.reconcileConnectionSwitch, 3*time.Second, 90*time.Second)
	go s.runFleetSharedWorker(s.ReconcileStorageDefinitions, 3*time.Second)
	// Ownership transfer and center-authoritative deployment workers are retired.
	go s.runFleetSharedWorker(s.ReconcileFleetControlIntents, time.Second)
	// Source-output consumers and center Stream coordination are retired.
	// Collection storage owns the only cross-node business-data delivery path.
}

type fleetPeerError struct {
	Status int
	Code   string
}

func (e *fleetPeerError) Error() string {
	return fmt.Sprintf("Fleet peer rejected operation: status=%d code=%s", e.Status, e.Code)
}
func (e *fleetPeerError) Unwrap() error {
	if e.Status == 403 && e.Code == "EDGE_METADATA_READ_ONLY" {
		return ErrFleetMetadataNotAllowed
	}
	if e.Status == 403 && e.Code == "EDGE_SSH_DISABLED" {
		return ErrFleetSSHNotAllowed
	}
	if e.Status == 403 && e.Code == "EDGE_CONTROL_DISABLED" {
		return ErrFleetControlNotAllowed
	}
	if e.Status == 403 && e.Code == "AUTHORIZATION_REVOKED" {
		return ErrFleetAuthorizationRevoked
	}
	switch e.Status {
	case 401, 403:
		return ErrUnauthorized
	case 409:
		return repo.ErrConflict
	case 400:
		if e.Code == "UNSUPPORTED_OPERATION" {
			return repo.ErrStorageOperation
		}
		return ErrInvalid
	case 404:
		return repo.ErrNotFound
	case 502, 503, 504:
		return ErrFleetPeerUnavailable
	}
	return nil
}
func (s *Service) CheckFleetHealth(ctx context.Context, nodeID string) (any, error) {
	if !license.Default().IsHost() {
		return nil, ErrUnauthorized
	}
	node, err := s.fleetRepo.CollaborationNode(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	networkStatus, networkAddresses := s.fleetNetworkStatus(ctx, nodeID, node.NetworkNodeID)
	status := map[string]any{"networkMode": "direct", "centerType": "enterprise", "networkStatus": networkStatus, "networkAddresses": networkAddresses, "endpointStatus": "unreachable", "businessStatus": "unavailable", "checkedAt": time.Now().UnixMilli(), "reason": "ENDPOINT_UNREACHABLE"}
	endpoint, err := url.Parse(node.Endpoint)
	if err != nil || endpoint.Hostname() == "" {
		return status, nil
	}
	port := endpoint.Port()
	if port == "" {
		port = "443"
	}
	conn, err := s.dialFleetEndpoint(ctx, "tcp", net.JoinHostPort(endpoint.Hostname(), port))
	if err == nil {
		_ = conn.Close()
		status["endpointStatus"] = "reachable"
		capabilities, err := s.CallFleetNode(ctx, nodeID, "capabilities.get", "node", struct{}{})
		if err == nil {
			status["businessStatus"] = "available"
			status["reason"] = ""
			status["capabilities"] = capabilities
			status["infrastructureStatus"] = "unavailable"
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			snapshot, probeErr := s.fleetInfrastructureSnapshot(probeCtx, nodeID)
			cancel()
			if probeErr == nil {
				status["infrastructureStatus"] = "available"
				status["containerCount"] = fleetprotocol.SummarizeContainers(snapshot.Snapshot.Containers.Containers).Total
				status["unhealthyContainerCount"] = fleetAbnormalContainerCount(snapshot.Snapshot.Containers.Containers)
			}
		} else {
			status["reason"] = "TLS_AUTH_OR_BACKEND_UNAVAILABLE"
		}
	}
	lastConnectedAt, err := s.fleetRepo.RecordCollaborationHealth(ctx, nodeID, networkStatus, status["businessStatus"].(string), status["checkedAt"].(int64))
	status["lastConnectedAt"] = lastConnectedAt
	return status, err
}

// Native installs use OS routes. Isolated/userspace Tailscale deployments may
// explicitly provide their own SOCKS listener; only literal overlay IPs use it.
func (s *Service) dialFleetEndpoint(ctx context.Context, network, address string) (net.Conn, error) {
	// Relayed overlay paths can need a TCP retransmission. Keep a bounded dial
	// budget within the existing 20s request/30s handler deadlines.
	base := &net.Dialer{Timeout: 10 * time.Second}
	host, _, _ := net.SplitHostPort(address)
	ip, err := netip.ParseAddr(host)
	if s.cfg.NetworkSOCKSAddress != "" && err == nil && (netip.MustParsePrefix("100.64.0.0/10").Contains(ip) || netip.MustParsePrefix("fd7a:115c:a1e0::/48").Contains(ip)) {
		dialer, err := proxy.SOCKS5("tcp", s.cfg.NetworkSOCKSAddress, nil, base)
		if err != nil {
			return nil, err
		}
		return dialer.(proxy.ContextDialer).DialContext(ctx, network, address)
	}
	return base.DialContext(ctx, network, address)
}

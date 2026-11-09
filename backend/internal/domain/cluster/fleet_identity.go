package cluster

import (
	"backend/internal/domain/license"
	"backend/internal/fleetprotocol"
	"backend/internal/infra/privatefs"
	"backend/internal/repo"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type fleetInstallationIdentity struct {
	InstallationID string `json:"installationId"`
	PrivateKey     string `json:"privateKey"`
}

// Installation keys are host-owned runtime state, never database response data.
// O_EXCL prevents accidental replacement and copying state between instances.
func (s *Service) fleetIdentity() (fleetInstallationIdentity, error) {
	var identity fleetInstallationIdentity
	if !s.cfg.CollaborationEnabled || s.cfg.IdentityDir == "" {
		return identity, ErrInvalid
	}
	if err := os.MkdirAll(s.cfg.IdentityDir, 0700); err != nil {
		return identity, err
	}
	dir, err := os.Lstat(s.cfg.IdentityDir)
	if err != nil {
		return identity, err
	}
	if !dir.IsDir() || !privatefs.IsPrivate(s.cfg.IdentityDir, dir) {
		return identity, ErrInvalid
	}
	path := filepath.Join(s.cfg.IdentityDir, "installation.json")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || !privatefs.IsPrivate(path, info) {
			return identity, ErrInvalid
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return identity, err
		}
		if json.Unmarshal(raw, &identity) != nil {
			return identity, ErrInvalid
		}
		key, err := base64.RawURLEncoding.DecodeString(identity.PrivateKey)
		if err != nil || len(key) != ed25519.PrivateKeySize || identity.InstallationID == "" {
			return identity, ErrInvalid
		}
		return identity, nil
	} else if !os.IsNotExist(err) {
		return identity, err
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return identity, err
	}
	identity = fleetInstallationIdentity{InstallationID: uuid.NewString(), PrivateKey: base64.RawURLEncoding.EncodeToString(key)}
	raw, _ := json.Marshal(identity)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fleetInstallationIdentity{}, err
	}
	_, writeErr := file.Write(raw)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fleetInstallationIdentity{}, err
	}
	if err := privatefs.SyncDir(s.cfg.IdentityDir); err != nil {
		return fleetInstallationIdentity{}, err
	}
	return identity, nil
}
func (s *Service) CollaborationCurrent(ctx context.Context) (any, error) {
	result, err := s.collaborationCurrentState()
	if err == nil {
		if agent, ok := s.localAgent.(localFleetNetwork); ok {
			network, e := agent.NetworkStatus(ctx)
			if e == nil {
				result["network"] = network
			} else {
				result["network"] = &fleetprotocol.NetworkStatus{State: "unavailable", ErrorCode: "FLEET_AGENT_UNAVAILABLE", CheckedAt: time.Now().UnixMilli()}
			}
		}
	}
	if err == nil {
		access, e := s.FleetAccess()
		if e != nil {
			return nil, e
		}
		result["access"] = access
		selection, e := repo.CollectionSyncScope(ctx)
		if e != nil {
			return nil, e
		}
		result["collectionScope"] = selection
	}
	if err == nil && !license.Default().IsHost() {
		if job, e := s.loadConnectionSwitch(); e == nil {
			result["switch"] = map[string]any{"state": job.State, "networkMode": job.NetworkMode, "centerUrl": job.CenterURL, "nodeId": job.NodeID, "errorCode": job.ErrorCode, "causeCode": job.CauseCode, "updatedAt": job.UpdatedAt}
			if job.State != "ready" && job.State != "cancelled" {
				result["tokenConfigured"] = true
				result["registrationStatus"] = "pending"
				result["businessStatus"] = "unavailable"
				result["connectionReason"] = "BUSINESS_REGISTRATION_PENDING"
				result["requestedConnection"] = map[string]any{"centerUrl": job.CenterURL, "nodeId": job.NodeID, "state": job.State}
				return result, nil
			}
		}
	}
	if err == nil && !license.Default().IsHost() && s.localAgent != nil {
		status, statusErr := s.localAgent.Status(ctx)
		if statusErr == nil && status.Configured {
			result["tokenConfigured"] = true
			requested := map[string]any{"centerUrl": status.CenterURL, "nodeId": status.FleetNodeID, "state": status.ConnectionState}
			result["requestedConnection"] = requested
			current, _ := result["connection"].(map[string]any)
			if current == nil || strings.TrimRight(fmt.Sprint(current["centerUrl"]), "/") != strings.TrimRight(status.CenterURL, "/") || current["nodeId"] != status.FleetNodeID {
				result["registrationStatus"] = "pending"
				result["businessStatus"] = "unavailable"
				result["connectionReason"] = "BUSINESS_REGISTRATION_PENDING"
				result["checkedAt"] = time.Now().UnixMilli()
				return result, nil
			}
		}
	}
	if err != nil || result["registrationStatus"] != "registered" || license.Default().IsHost() {
		return result, err
	}
	connection := result["connection"].(map[string]any)
	centerURL, _ := connection["centerUrl"].(string)
	if endpoint, ok := connection["businessEndpoint"].(string); ok && endpoint != "" {
		centerURL = endpoint
	}
	nodeID, _ := connection["nodeId"].(string)
	// Registration and cached task tokens are not proof of a live connection.
	// A fresh signed exchange includes TLS and the center's callback checks.
	// Keep it bounded below the API's 20s budget without declaring a slower
	// healthy connection offline after only three seconds.
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, _, probeErr := s.exchangeFleetPeerToken(probeCtx, centerURL, nodeID, "storage.definition", "node", "", 0, false)
	result["checkedAt"] = time.Now().UnixMilli()
	result["businessStatus"] = "available"
	if probeErr != nil {
		result["businessStatus"] = "unavailable"
		result["connectionReason"] = "FLEET_BUSINESS_CHECK_FAILED"
		if errors.Is(probeCtx.Err(), context.DeadlineExceeded) {
			result["connectionReason"] = "FLEET_BUSINESS_CHECK_TIMEOUT"
		}
	}
	return result, nil
}

func (s *Service) collaborationCurrentState() (map[string]any, error) {
	if !s.cfg.CollaborationEnabled {
		return map[string]any{"enabled": false, "registrationStatus": "unconfigured"}, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, err := s.fleetIdentity()
	if err != nil {
		return nil, err
	}
	key, _ := base64.RawURLEncoding.DecodeString(identity.PrivateKey)
	public := ed25519.PrivateKey(key).Public().(ed25519.PublicKey)
	result := map[string]any{"enabled": true, "installationId": identity.InstallationID, "keyId": digest(public), "publicKey": base64.RawURLEncoding.EncodeToString(public), "endpoint": s.cfg.NodeAPIBaseURL, "registrationStatus": "unconfigured"}
	raw, readErr := os.ReadFile(filepath.Join(s.cfg.IdentityDir, "connection.json"))
	if readErr == nil {
		var connection map[string]any
		if json.Unmarshal(raw, &connection) != nil {
			return nil, ErrInvalid
		}
		result["connection"] = connection
		result["registrationStatus"] = "registered"
	} else if !os.IsNotExist(readErr) {
		return nil, readErr
	}
	return result, nil
}

// Configure uses the standard OS route and TLS verification. It never executes
// tailscale commands, accepts remote administrator keys or forwards Cookies.
func (s *Service) ConfigureCollaboration(ctx context.Context, centerURL, nodeID, code, networkNodeID string) (any, error) {
	if license.Default().IsHost() {
		return nil, ErrHostCannotConnect
	}
	center, err := url.Parse(centerURL)
	if err != nil || center.Scheme != "https" || center.Hostname() == "" || center.User != nil || center.RawQuery != "" || center.Fragment != "" || (center.Path != "" && center.Path != "/") {
		return nil, ErrInvalid
	}
	local, parseErr := url.Parse(s.cfg.NodeAPIBaseURL)
	if parseErr != nil || local.Scheme != "https" || local.Hostname() == "" || local.User != nil || local.RawQuery != "" || local.Fragment != "" || (local.Path != "" && local.Path != "/") || nodeID == "" || len(code) != 43 || len(networkNodeID) > 128 {
		return nil, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, err := s.fleetIdentity()
	if err != nil {
		return nil, err
	}
	if raw, readErr := os.ReadFile(filepath.Join(s.cfg.IdentityDir, "connection.json")); readErr == nil {
		var connection map[string]any
		if json.Unmarshal(raw, &connection) != nil {
			return nil, ErrInvalid
		}
		if connection["centerUrl"] != centerURL || connection["nodeId"] != nodeID {
			return nil, ErrInvalid
		}
		return connection, nil
	} else if !os.IsNotExist(readErr) {
		return nil, readErr
	}
	key, _ := base64.RawURLEncoding.DecodeString(identity.PrivateKey)
	public := ed25519.PrivateKey(key).Public().(ed25519.PublicKey)
	proof := FleetEnrollmentProof{InstallationID: identity.InstallationID, NodeID: nodeID, EnrollmentCode: code, PublicKey: base64.RawURLEncoding.EncodeToString(public), Endpoint: s.cfg.NodeAPIBaseURL, NetworkNodeID: networkNodeID, Timestamp: time.Now().UnixMilli(), Nonce: uuid.NewString()}
	pendingPath := filepath.Join(s.cfg.IdentityDir, "enrollment-pending.json")
	if raw, readErr := os.ReadFile(pendingPath); readErr == nil {
		var pending struct {
			CenterURL string               `json:"centerUrl"`
			Proof     FleetEnrollmentProof `json:"proof"`
		}
		if json.Unmarshal(raw, &pending) != nil {
			return nil, ErrInvalid
		}
		if pending.CenterURL == centerURL && pending.Proof.NodeID == nodeID && pending.Proof.EnrollmentCode == code && pending.Proof.NetworkNodeID == networkNodeID {
			proof = pending.Proof
		} else {
			return nil, ErrInvalid
		}
	} else if !os.IsNotExist(readErr) {
		return nil, readErr
	} else {
		if err := writeFleetState(s.cfg.IdentityDir, "enrollment-pending.json", map[string]any{"centerUrl": centerURL, "proof": proof}); err != nil {
			return nil, err
		}
	}
	body, _ := json.Marshal(proof)
	signature := ed25519.Sign(ed25519.PrivateKey(key), append([]byte("tier0-fleet-enrollment-v1\n"), body...))
	body, _ = json.Marshal(FleetEnrollRequest{Proof: proof, Signature: base64.RawURLEncoding.EncodeToString(signature)})
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
	transport.Proxy = nil // Overlay traffic follows host routes, never a public proxy.
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(centerURL, "/")+"/api/node/v1/fleet/execution/enroll", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 16385))
	if err != nil || len(raw) > 16384 {
		return nil, ErrInvalid
	}
	if response.StatusCode != 200 {
		return nil, ErrUnauthorized
	}
	var result struct {
		Data struct {
			CenterID           string `json:"centerId"`
			CenterPublicKey    string `json:"centerPublicKey"`
			NodeID             string `json:"nodeId"`
			KeyID              string `json:"keyId"`
			RegistrationStatus string `json:"registrationStatus"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Data.NodeID != nodeID || result.Data.KeyID != digest(public) || result.Data.RegistrationStatus != "registered" {
		return nil, ErrUnauthorized
	}
	centerKey, keyErr := base64.RawURLEncoding.DecodeString(result.Data.CenterPublicKey)
	if keyErr != nil || len(centerKey) != ed25519.PublicKeySize || result.Data.CenterID == "" {
		return nil, ErrUnauthorized
	}
	if err := writeFleetState(s.cfg.IdentityDir, "connection.json", map[string]any{"centerId": result.Data.CenterID, "centerPublicKey": result.Data.CenterPublicKey, "centerUrl": centerURL, "nodeId": nodeID, "keyId": result.Data.KeyID, "registeredAt": time.Now().UnixMilli()}); err != nil {
		return nil, err
	}
	if err := os.Remove(pendingPath); err != nil {
		return nil, err
	}
	return result.Data, nil
}

func writeFleetState(dir, name string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".fleet-state-")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if _, err = file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		return err
	}
	return privatefs.SyncDir(dir)
}

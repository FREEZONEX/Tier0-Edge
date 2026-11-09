package cluster

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"backend/internal/domain/license"
	"backend/internal/fleetprotocol"
	"backend/internal/repo"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrFleetCenterType = errors.New("fleet.edge.centerTypeMismatch")

type fleetConnectionSwitch struct {
	NetworkMode     string `json:"networkMode,omitempty"`
	Token           string `json:"token,omitempty"` // private runtime journal only; never returned to the browser
	CenterURL       string `json:"centerUrl"`
	NodeID          string `json:"nodeId"`
	CenterID        string `json:"centerId"`
	CenterPublicKey string `json:"centerPublicKey"`
	State           string `json:"state"`
	ErrorCode       string `json:"errorCode,omitempty"`
	CauseCode       string `json:"causeCode,omitempty"`
	UpdatedAt       int64  `json:"updatedAt"`
}

func (s *Service) loadConnectionSwitch() (fleetConnectionSwitch, error) {
	var job fleetConnectionSwitch
	raw, err := os.ReadFile(filepath.Join(s.cfg.IdentityDir, "connection-switch.json"))
	if err == nil {
		err = json.Unmarshal(raw, &job)
	}
	return job, err
}
func (s *Service) bootstrapConnection(ctx context.Context, token fleetprotocol.ConnectionToken, action, networkID string) (fleetCenterRegistration, error) {
	var result fleetCenterRegistration
	if !fleetprotocol.ValidCenterNetworkMode(token.CenterType, token.NetworkMode) {
		return result, ErrFleetCenterType
	}
	if validateFleetEnrollmentEndpoint(token.HTTPBaseURL) != nil {
		return result, ErrInvalid
	}
	s.mu.Lock()
	identity, err := s.fleetIdentity()
	s.mu.Unlock()
	if err != nil {
		return result, err
	}
	key, _ := base64.RawURLEncoding.DecodeString(identity.PrivateKey)
	proof := FleetEnrollmentProof{InstallationID: identity.InstallationID, NodeID: token.NodeID, PublicKey: base64.RawURLEncoding.EncodeToString(ed25519.PrivateKey(key).Public().(ed25519.PublicKey)), Endpoint: s.cfg.NodeAPIBaseURL, NetworkNodeID: networkID, Timestamp: time.Now().UnixMilli(), Nonce: uuid.NewString()}
	if action == "register" || action == "verify" {
		endpoint, pin, e := s.connectionEndpointForMode(ctx, token.NetworkMode)
		if e != nil {
			return result, e
		}
		proof.Endpoint, proof.TLSFingerprint = endpoint, pin
	}
	body, _ := json.Marshal(proof)
	req := FleetConnectionBootstrapRequest{Action: action, ClientID: token.ClientID, Proof: proof, Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, append([]byte("tier0-fleet-connection-v1/"+action+"\n"), body...)))}
	body, _ = json.Marshal(req)
	header := http.Request{Header: make(http.Header)}
	header.SetBasicAuth(token.Username, token.Password)
	raw, err := s.fleetHTTPHeaders(ctx, strings.TrimRight(token.HTTPBaseURL, "/")+"/api/node/v1/fleet/connection/bootstrap", "", body, map[string]string{"Authorization": header.Header.Get("Authorization")})
	if err != nil {
		return result, err
	}
	if json.Unmarshal(raw, &result) != nil || result.CenterID == "" || result.NodeID != token.NodeID || result.KeyID != digest(ed25519.PrivateKey(key).Public().(ed25519.PublicKey)) {
		return result, ErrUnauthorized
	}
	public, e := base64.RawURLEncoding.DecodeString(result.CenterPublicKey)
	if e != nil || len(public) != ed25519.PublicKeySize {
		return result, ErrUnauthorized
	}
	if result.NetworkMode != token.NetworkMode || !fleetprotocol.ValidCenterNetworkMode(result.CenterType, result.NetworkMode) {
		return result, ErrFleetCenterType
	}
	result.CenterURL = strings.TrimRight(token.HTTPBaseURL, "/")
	return result, nil
}
func (s *Service) requestConnectionSwitch(ctx context.Context, raw string) (*fleetprotocol.AgentStatus, error) {
	token, err := fleetprotocol.ParseConnectionToken(raw)
	if err != nil {
		return nil, ErrInvalid
	}
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()
	// Candidate validation precedes *every* local side effect, including Agent
	// configuration. TLS failure and an invalid token leave the current link intact.
	prepared, err := s.bootstrapConnection(ctx, token, "prepare", "")
	if err != nil {
		return nil, err
	}
	if active, e := s.storageConnection(); e == nil && active.CenterID == prepared.CenterID && active.NodeID != token.NodeID {
		// Rebinding the same installation to a second row in the same center is
		// not a center switch. Do not reuse its existing Collection mappings.
		return nil, repo.ErrConflict
	}
	job := fleetConnectionSwitch{NetworkMode: prepared.NetworkMode, Token: raw, CenterURL: prepared.CenterURL, NodeID: token.NodeID, CenterID: prepared.CenterID, CenterPublicKey: prepared.CenterPublicKey, State: "network", UpdatedAt: time.Now().UnixMilli()}
	status, err := s.localAgent.Status(ctx)
	if err != nil {
		return nil, err
	}
	if err = writeFleetState(s.cfg.IdentityDir, "connection-switch.json", job); err != nil {
		return nil, err
	}
	// This response acknowledges a durable request; current/get reports completion.
	copy := *status
	copy.CenterURL = job.CenterURL
	copy.FleetNodeID = job.NodeID
	copy.ConnectionState = "connecting"
	return &copy, nil
}
func (s *Service) reconcileConnectionSwitch(ctx context.Context) {
	if license.Default().IsHost() || s.localAgent == nil {
		return
	}
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()
	job, err := s.loadConnectionSwitch()
	if err != nil || job.State == "ready" || job.Token == "" {
		return
	}
	err = s.advanceConnectionSwitch(ctx, &job)
	job.UpdatedAt = time.Now().UnixMilli()
	job.CauseCode = ""
	if err != nil {
		job.ErrorCode = "CENTER_SWITCH_" + strings.ToUpper(job.State) + "_FAILED"
		var agentErr *FleetAgentResponseError
		var peerErr *fleetPeerError
		if errors.As(err, &agentErr) {
			job.CauseCode = fleetprotocol.ConnectionDiagnosticCode(agentErr.Code)
		} else if errors.As(err, &peerErr) && peerErr.Status == http.StatusNotFound {
			job.CauseCode = "FLEET_CENTER_ENDPOINT_NOT_FOUND"
		} else if errors.Is(err, ErrFleetAgentUnavailable) || errors.Is(err, ErrFleetAgentNotConfigured) {
			job.CauseCode = "FLEET_AGENT_UNAVAILABLE"
		}
	} else {
		job.ErrorCode = ""
	}
	if job.State == "ready" {
		job.Token = ""
	}
	_ = writeFleetState(s.cfg.IdentityDir, "connection-switch.json", job)
}
func (s *Service) advanceConnectionSwitch(ctx context.Context, job *fleetConnectionSwitch) error {
	token, err := fleetprotocol.ParseConnectionToken(job.Token)
	if err != nil {
		return err
	}
	if job.State == "network" {
		if _, err = s.localAgent.ConfigureConnection(ctx, job.Token); err != nil {
			return err
		}
		job.State = "registration"
		job.ErrorCode, job.CauseCode = "", ""
		job.UpdatedAt = time.Now().UnixMilli()
		if err = writeFleetState(s.cfg.IdentityDir, "connection-switch.json", job); err != nil {
			return err
		}
	}
	networkID := ""
	if token.NetworkMode == fleetprotocol.NetworkModeTailscale {
		agent, ok := s.localAgent.(localFleetNetwork)
		if !ok {
			return ErrFleetAgentUnavailable
		}
		network, e := agent.NetworkStatus(ctx)
		if e != nil || network.State != "online" || network.NodeID == "" {
			return ErrFleetPeerUnavailable
		}
		networkID = network.NodeID
	}
	if job.State == "registration" {
		registration, e := s.bootstrapConnection(ctx, token, "register", networkID)
		if e != nil {
			return e
		}
		if registration.CenterID != job.CenterID || registration.CenterPublicKey != job.CenterPublicKey {
			return ErrUnauthorized
		}
		if e = s.commitCenterConnection(ctx, registration); e != nil {
			return e
		}
		job.State = "verification"
		job.ErrorCode, job.CauseCode = "", ""
		job.UpdatedAt = time.Now().UnixMilli()
		if e = writeFleetState(s.cfg.IdentityDir, "connection-switch.json", job); e != nil {
			return e
		}
	}
	if err = s.syncStorageDefinitions(ctx, true); err != nil {
		return err
	}
	connection, err := s.storageConnection()
	if err != nil {
		return err
	}
	// Ask the new center to verify its own authenticated callback path too.
	if _, _, err = s.exchangeFleetPeerToken(ctx, connection.endpoint(), job.NodeID, "storage.definition", "node", "", 0, false); err != nil {
		return err
	}
	if _, err = s.bootstrapConnection(ctx, token, "verify", networkID); err != nil {
		return err
	}
	// A verified reconnection is new reachability evidence. Resume that center's
	// retained deliveries without waiting out the previous offline backoff. The
	// normal worker still owns ordering, leases, retries and committed receipts.
	if err = repo.ResumeCollectionStorageDelivery(repo.GetUnsConn(ctx), job.CenterID); err != nil {
		return err
	}
	job.State = "ready"
	return nil
}
func (s *Service) commitCenterConnection(ctx context.Context, next fleetCenterRegistration) error {
	raw, err := os.ReadFile(filepath.Join(s.cfg.IdentityDir, "connection.json"))
	if err == nil {
		var previous fleetCenterRegistration
		if json.Unmarshal(raw, &previous) != nil || previous.CenterID == "" {
			return ErrInvalid
		}
		if previous.CenterID != next.CenterID || previous.NodeID != next.NodeID {
			name := "connection-archive-" + digest([]byte(previous.CenterID+"/"+previous.NodeID)) + ".json"
			if err = writeFleetState(s.cfg.IdentityDir, name, previous); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	// Commit the DB fence first. If the process stops before the file rename,
	// remote writes require the new mapping and queries fail on the identity
	// mismatch, rather than silently delivering new writes to the old center.
	err = repo.GetUnsConn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(814206108)").Error; err != nil {
			return err
		}
		p, err := repo.GlobalStorage(tx)
		if err != nil {
			return err
		}
		if p.CenterID != next.CenterID {
			p.CenterID = next.CenterID
			p.Revision++
			p.UpdatedAt = time.Now().UnixMilli()
			if p.Revision == 1 {
				p.Mode = "both"
			}
			return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&p).Error
		}
		return nil
	})
	if err != nil {
		return err
	}
	return writeFleetState(s.cfg.IdentityDir, "connection.json", next)
}

// Resolve only a locally retained, exact target. Never send old events through
// the new center or accept archived center credentials for inbound requests.
func (s *Service) deliveryConnection(centerID, targetCollectionID, sourceCollectionID string) (storageConnection, error) {
	if license.Default().IsHost() {
		return storageConnection{}, ErrHostCannotConnect
	}
	current, err := s.storageConnection()
	if err == nil && current.CenterID == centerID && storageMetadataID(current.NodeID, sourceCollectionID) == targetCollectionID {
		return current, nil
	}
	entries, err := os.ReadDir(s.cfg.IdentityDir)
	if err != nil {
		return storageConnection{}, err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "connection-archive-") || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		raw, e := os.ReadFile(filepath.Join(s.cfg.IdentityDir, entry.Name()))
		if e != nil {
			return storageConnection{}, e
		}
		var old storageConnection
		if json.Unmarshal(raw, &old) == nil && old.CenterID == centerID && storageMetadataID(old.NodeID, sourceCollectionID) == targetCollectionID {
			return old, nil
		}
	}
	return storageConnection{}, errors.New("ORIGINAL_CENTER_UNAVAILABLE")
}

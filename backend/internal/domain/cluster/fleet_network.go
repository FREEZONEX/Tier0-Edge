package cluster

import (
	"backend/internal/domain/fleettunnel"
	"backend/internal/domain/license"
	"backend/internal/fleetprotocol"
	"backend/internal/repo"
	"context"
	"errors"
	"golang.org/x/crypto/ssh"
	"net/http"
	"net/url"
)

type FleetNetworkRequest = fleetprotocol.NetworkBootstrapRequest

type localFleetNetwork interface {
	NetworkStatus(context.Context) (*fleetprotocol.NetworkStatus, error)
	EnrollNetwork(context.Context, fleetprotocol.NetworkEnrollRequest) (*fleetprotocol.NetworkStatus, error)
}

// Enterprise uses customer-provided connectivity. This authenticated host
// bootstrap exchanges SSH public keys without provisioning a network identity.
func (s *Service) FleetNetworkBootstrap(ctx context.Context, authorization string, req FleetNetworkRequest) (any, error) {
	if !s.CollaborationEnabled() || !license.Default().CenterManagementEnabled() {
		return nil, ErrUnauthorized
	}
	header := http.Request{Header: http.Header{"Authorization": []string{authorization}}}
	username, password, ok := header.BasicAuth()
	if !ok {
		return nil, ErrUnauthorized
	}
	if _, err := s.AuthorizeFleetTunnel(ctx, fleettunnel.Identity{Username: username, Password: password, ClientID: req.ClientID, NodeID: req.NodeID}); err != nil {
		return nil, ErrUnauthorized
	}
	result := fleetprotocol.NetworkBootstrapResponse{CenterType: "enterprise", NetworkMode: fleetprotocol.NetworkModeDirect}
	if req.Inspect || req.SSH == nil {
		return result, nil
	}
	node, err := s.fleetRepo.CollaborationNode(ctx, req.NodeID)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}
	if errors.Is(err, repo.ErrNotFound) || node.RegistrationStatus != "registered" {
		// Prepare trust before registration. Never probe an address supplied only by
		// a host bootstrap; the signed business enrollment binds the target first.
		ca, err := s.nodeSSHCA(req.NodeID)
		if err != nil {
			return nil, err
		}
		result.SSH = &fleetprotocol.SSHBootstrap{CAPublicKey: string(ssh.MarshalAuthorizedKey(ca.PublicKey()))}
		return result, nil
	}
	_, addresses := s.fleetNetworkStatus(ctx, req.NodeID, nil)
	result.SSH, err = s.reconcileSSHBootstrap(ctx, req.NodeID, addresses, req.SSH)
	return result, err
}

// Direct reports who manages connectivity, not a fabricated online VPN state.
// Business and SSH probes separately establish actual reachability.
func (s *Service) fleetNetworkStatus(ctx context.Context, nodeID string, _ *string) (string, []string) {
	node, err := s.fleetRepo.CollaborationNode(ctx, nodeID)
	if err != nil || node.RegistrationStatus != "registered" || validateFleetEnrollmentEndpoint(node.Endpoint) != nil {
		return "direct", nil
	}
	endpoint, _ := url.Parse(node.Endpoint)
	return "direct", []string{endpoint.Hostname()}
}

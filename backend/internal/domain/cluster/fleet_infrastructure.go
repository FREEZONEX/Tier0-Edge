package cluster

import (
	"context"
	"encoding/json"
	"strings"

	"backend/internal/domain/unsv2"
	"backend/internal/fleetprotocol"
)

// FleetInfrastructure exposes the existing local Agent operations through the
// pinned center identity. Each token is bound to one operation and one resource.
// Container ownership, redaction, restart restrictions and operation deduplication
// remain enforced by the local Agent; this is not a generic RPC or Docker proxy.
func (s *Service) FleetInfrastructure(ctx context.Context, scope, resource string, payload json.RawMessage) (any, error) {
	if strings.HasPrefix(scope, "infrastructure.runtime.") {
		var request fleetprotocol.RuntimeRequest
		if unsv2.Decode(payload, &request) != nil {
			return nil, ErrInvalid
		}
		bound := request.ResourceID
		switch request.Action {
		case "list":
			bound = "node"
		case "operation":
			bound = request.OperationID
		case "logs", "control":
		default:
			return nil, ErrInvalid
		}
		if bound == "" || resource != bound {
			return nil, ErrUnauthorized
		}
		if request.Action == "control" {
			access, err := s.FleetAccess()
			if err != nil {
				return nil, err
			}
			if !access.AllowControl {
				return nil, ErrFleetControlNotAllowed
			}
		}
		agent, ok := s.localAgent.(interface {
			Runtime(context.Context, fleetprotocol.RuntimeRequest) (json.RawMessage, error)
		})
		if !ok {
			return nil, ErrFleetAgentNotConfigured
		}
		return agent.Runtime(ctx, request)
	}
	var request fleetTunnelRequest
	if unsv2.Decode(payload, &request) != nil {
		return nil, ErrInvalid
	}
	operations := map[string]string{
		"infrastructure.snapshot": fleetTunnelOperationSnapshot,
		"infrastructure.detail":   fleetTunnelOperationDetail,
		"infrastructure.logs":     fleetTunnelOperationLogs,
		"infrastructure.restart":  fleetTunnelOperationRestart,
	}
	operation, ok := operations[scope]
	if !ok || request.Operation != operation {
		return nil, ErrUnauthorized
	}
	if operation == fleetTunnelOperationSnapshot {
		if resource != "node" || request.ContainerID != "" {
			return nil, ErrUnauthorized
		}
	} else if strings.TrimSpace(request.ContainerID) == "" || resource != request.ContainerID {
		return nil, ErrUnauthorized
	}
	// Keep the bounded command implementation shared with the older tunnel.
	raw, err := s.ApplyFleetTunnelRequest(ctx, payload)
	return json.RawMessage(raw), err
}

func (s *Service) hasFleetInfrastructure(ctx context.Context, nodeID string) bool {
	if !s.CollaborationEnabled() {
		return false
	}
	node, err := s.fleetRepo.CollaborationNode(ctx, nodeID)
	return err == nil && node.RegistrationStatus == "registered"
}

func (s *Service) callFleetInfrastructure(ctx context.Context, nodeID string, request fleetTunnelRequest, result any) error {
	if !s.hasFleetInfrastructure(ctx, nodeID) {
		return s.callFleetTunnel(ctx, nodeID, request, result)
	}
	scopes := map[string]string{
		fleetTunnelOperationSnapshot: "infrastructure.snapshot",
		fleetTunnelOperationDetail:   "infrastructure.detail",
		fleetTunnelOperationLogs:     "infrastructure.logs",
		fleetTunnelOperationRestart:  "infrastructure.restart",
	}
	scope, ok := scopes[request.Operation]
	if !ok {
		return ErrInvalid
	}
	resource := request.ContainerID
	if request.Operation == fleetTunnelOperationSnapshot {
		resource = "node"
	}
	raw, err := s.CallFleetNode(ctx, nodeID, scope, resource, request)
	if err != nil {
		return err
	}
	var response fleetTunnelResponse
	if json.Unmarshal(raw, &response) != nil {
		return ErrInvalid
	}
	if !response.OK {
		return &FleetAgentResponseError{StatusCode: response.StatusCode, Code: response.ErrorCode}
	}
	return json.Unmarshal(response.Payload, result)
}

func (s *Service) fleetInfrastructureSnapshot(ctx context.Context, nodeID string) (*fleetTunnelSnapshotResult, error) {
	var result fleetTunnelSnapshotResult
	if err := s.callFleetInfrastructure(ctx, nodeID, fleetTunnelRequest{Operation: fleetTunnelOperationSnapshot}, &result); err != nil {
		return nil, err
	}
	// Cache the observed snapshot for explicit stale reads, without pretending that
	// the legacy MQTT Agent session was joined or that a probe is a heartbeat.
	if err := s.storeFleetTunnelSnapshot(ctx, nodeID, result.Snapshot); err != nil {
		return nil, err
	}
	return &result, nil
}

package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"backend/internal/domain/fleettunnel"
	"backend/internal/fleetprotocol"
	"backend/internal/repo"
)

const (
	fleetTunnelOperationSnapshot = "snapshot"
	fleetTunnelOperationDetail   = "container.detail"
	fleetTunnelOperationLogs     = "container.logs"
	fleetTunnelOperationRestart  = "container.restart"
)

type fleetTunnelRequest struct {
	Operation   string                             `json:"operation"`
	ContainerID string                             `json:"containerID,omitempty"`
	OperationID string                             `json:"operationID,omitempty"`
	Logs        fleetprotocol.ContainerLogsRequest `json:"logs,omitempty"`
}

type fleetTunnelResponse struct {
	OK         bool            `json:"ok"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	ErrorCode  string          `json:"errorCode,omitempty"`
	StatusCode int             `json:"statusCode,omitempty"`
}

type fleetTunnelSnapshotResult struct {
	Snapshot fleetprotocol.SnapshotPayload `json:"snapshot"`
	Agent    fleetprotocol.AgentStatus     `json:"agent"`
}

// ApplyFleetTunnelRequest is the edge-side fixed RPC dispatcher. It exposes
// only Fleet host/container operations and delegates them to the local Agent.
func (s *Service) ApplyFleetTunnelRequest(ctx context.Context, raw []byte) ([]byte, error) {
	var request fleetTunnelRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return json.Marshal(fleetTunnelFailure(ErrInvalid))
	}
	var result any
	var err error
	switch strings.TrimSpace(request.Operation) {
	case fleetTunnelOperationSnapshot:
		result, err = s.currentFleetTunnelSnapshot(ctx)
	case fleetTunnelOperationDetail:
		result, err = s.CurrentContainer(ctx, request.ContainerID)
	case fleetTunnelOperationLogs:
		result, err = s.CurrentContainerLogs(ctx, CurrentContainerLogsCommand{
			ContainerID: request.ContainerID,
			Tail:        request.Logs.Tail, Since: request.Logs.Since,
			Timestamps: request.Logs.Timestamps, LimitBytes: request.Logs.LimitBytes,
		})
	case fleetTunnelOperationRestart:
		result, err = s.RestartCurrentContainer(ctx, CurrentContainerRestartCommand{
			ContainerID: request.ContainerID,
			RequestID:   request.OperationID,
		})
	default:
		err = ErrInvalid
	}
	if err != nil {
		return json.Marshal(fleetTunnelFailure(err))
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	return json.Marshal(fleetTunnelResponse{OK: true, Payload: payload})
}

func (s *Service) currentFleetTunnelSnapshot(ctx context.Context) (*fleetTunnelSnapshotResult, error) {
	if s.localAgent == nil {
		return nil, ErrFleetAgentNotConfigured
	}
	status, err := s.localAgent.Status(ctx)
	if err != nil {
		return nil, err
	}
	containers, err := s.CurrentContainers(ctx)
	if err != nil {
		return nil, err
	}
	observedAt := containers.ObservedAt
	if observedAt <= 0 {
		observedAt = time.Now().UTC().UnixMilli()
	}
	snapshotID := strings.TrimSpace(containers.SnapshotID)
	if snapshotID == "" {
		snapshotID = "tunnel-" + time.Now().UTC().Format("20060102150405.000000000")
		containers.SnapshotID = snapshotID
	}
	snapshot := fleetprotocol.SnapshotPayload{
		ConnectionID: "fleet-tunnel",
		SessionID:    "fleet-tunnel",
		SnapshotID:   snapshotID,
		ObservedAt:   observedAt,
		Host:         status.Host,
		Containers:   *containers,
	}
	return &fleetTunnelSnapshotResult{Snapshot: snapshot, Agent: *status}, nil
}

func fleetTunnelFailure(err error) fleetTunnelResponse {
	statusCode := http.StatusBadGateway
	code := "FLEET_AGENT_COMMAND_FAILED"
	switch {
	case errors.Is(err, ErrInvalid):
		statusCode, code = http.StatusBadRequest, "INVALID_REQUEST"
	case errors.Is(err, ErrFleetAgentNotConfigured):
		statusCode, code = http.StatusServiceUnavailable, ErrFleetAgentNotConfigured.Error()
	case errors.Is(err, ErrFleetAgentUnavailable):
		statusCode, code = http.StatusServiceUnavailable, ErrFleetAgentUnavailable.Error()
	}
	var responseErr *FleetAgentResponseError
	if errors.As(err, &responseErr) {
		statusCode = responseErr.StatusCode
		code = strings.TrimSpace(responseErr.Code)
	}
	return fleetTunnelResponse{ErrorCode: code, StatusCode: statusCode}
}

func (s *Service) callFleetTunnel(ctx context.Context, fleetNodeID string, request fleetTunnelRequest, result any) error {
	if s.tunnel == nil || s.fleetTunnelStatus(fleetNodeID).Status != "online" {
		return &FleetAgentResponseError{StatusCode: http.StatusServiceUnavailable, Code: "FLEET_NODE_OFFLINE"}
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return err
	}
	raw, err = s.tunnel.SendFleetRPC(ctx, fleetNodeID, raw)
	if err != nil {
		if errors.Is(err, fleettunnel.ErrTimeout) {
			return &FleetAgentResponseError{StatusCode: http.StatusGatewayTimeout, Code: "FLEET_OPERATION_TIMEOUT"}
		}
		return &FleetAgentResponseError{StatusCode: http.StatusServiceUnavailable, Code: "FLEET_NODE_OFFLINE"}
	}
	var response fleetTunnelResponse
	if json.Unmarshal(raw, &response) != nil {
		return &FleetAgentResponseError{StatusCode: http.StatusBadGateway, Code: "FLEET_AGENT_INVALID_RESPONSE"}
	}
	if !response.OK {
		statusCode := response.StatusCode
		if statusCode <= 0 {
			statusCode = http.StatusBadGateway
		}
		return &FleetAgentResponseError{StatusCode: statusCode, Code: response.ErrorCode}
	}
	if result == nil {
		return nil
	}
	if len(response.Payload) == 0 || json.Unmarshal(response.Payload, result) != nil {
		return &FleetAgentResponseError{StatusCode: http.StatusBadGateway, Code: "FLEET_AGENT_INVALID_RESPONSE"}
	}
	return nil
}

func (s *Service) fleetTunnelSnapshot(ctx context.Context, fleetNodeID string) (fleetprotocol.SnapshotPayload, error) {
	var result fleetTunnelSnapshotResult
	err := s.callFleetTunnel(ctx, fleetNodeID, fleetTunnelRequest{Operation: fleetTunnelOperationSnapshot}, &result)
	if err != nil {
		return result.Snapshot, err
	}
	if err = s.fleetRepo.ActivateNodeSession(ctx, repo.ActivateFleetNodeSessionInput{
		FleetNodeID: fleetNodeID, SessionID: "fleet-tunnel",
		AgentVersion: result.Agent.Version, ProtocolVersion: result.Agent.ProtocolVersion,
		OS: result.Agent.Host.OS, Arch: result.Agent.Host.Arch,
		ConnectedAt: result.Snapshot.ObservedAt,
	}); err != nil {
		return result.Snapshot, err
	}
	if err = s.storeFleetTunnelSnapshot(ctx, fleetNodeID, result.Snapshot); err != nil {
		return result.Snapshot, err
	}
	return result.Snapshot, nil
}

func (s *Service) storeFleetTunnelSnapshot(ctx context.Context, fleetNodeID string, snapshot fleetprotocol.SnapshotPayload) error {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = s.fleetRepo.SaveContainerSnapshot(ctx, repo.FleetContainerSnapshot{
		FleetNodeID: fleetNodeID, SnapshotID: snapshot.SnapshotID,
		ObservedAt: snapshot.ObservedAt, InstallationID: snapshot.Containers.InstallationID,
		Payload: payload,
	})
	if err != nil {
		return err
	}
	s.fleet.mu.Lock()
	s.fleet.snapshots[fleetNodeID] = snapshot
	s.fleet.mu.Unlock()
	return nil
}

package cluster

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"backend/internal/fleetprotocol"
	"backend/internal/repo"

	"github.com/google/uuid"
)

type FleetNodeContainerLogsCommand struct {
	FleetNodeID string
	ContainerID string
	Tail        int
	Since       int64
	Timestamps  *bool
	LimitBytes  int
	UserID      int64
}

type FleetNodeContainerRestartCommand struct {
	FleetNodeID string
	ContainerID string
	RequestID   string
	UserID      int64
}

func (s *Service) FleetNodeContainers(ctx context.Context, fleetNodeID string, refresh bool) (map[string]any, error) {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	if fleetNodeID == "" {
		return nil, ErrInvalid
	}
	if _, err := s.fleetRepo.GetNode(ctx, fleetNodeID, false); err != nil {
		return nil, err
	}
	if s.hasFleetInfrastructure(ctx, fleetNodeID) {
		result, err := s.fleetInfrastructureSnapshot(ctx, fleetNodeID)
		if err == nil {
			response := remoteSnapshotResponse(result.Snapshot, false)
			response["agentVersion"] = result.Agent.Version
			response["protocolVersion"] = result.Agent.ProtocolVersion
			return response, nil
		}
		if cached, ok := s.fleet.latestSnapshot(fleetNodeID); ok {
			return remoteSnapshotResponse(cached, true), nil
		}
		return nil, err
	}
	if s.fleetTunnelStatus(fleetNodeID).Status == "online" {
		snapshot, err := s.fleetTunnelSnapshot(ctx, fleetNodeID)
		if err == nil {
			return remoteSnapshotResponse(snapshot, false), nil
		}
		if cached, ok := s.fleet.latestSnapshot(fleetNodeID); ok {
			return remoteSnapshotResponse(cached, true), nil
		}
		return nil, err
	}
	session := s.fleet.active(fleetNodeID)
	_, hasSnapshot := s.fleet.latestSnapshot(fleetNodeID)
	if session != nil && (refresh || !hasSnapshot) {
		if err := session.requestSnapshot(ctx); err != nil && !hasSnapshot {
			return nil, err
		}
	}
	snapshot, ok := s.fleet.latestSnapshot(fleetNodeID)
	if !ok {
		return nil, &FleetAgentResponseError{StatusCode: http.StatusServiceUnavailable, Code: "FLEET_AGENT_UNAVAILABLE"}
	}
	return remoteSnapshotResponse(snapshot, session == nil), nil
}

func (s *Service) FleetNodeContainer(ctx context.Context, fleetNodeID, containerID string) (map[string]any, error) {
	fleetNodeID = strings.TrimSpace(fleetNodeID)
	containerID = strings.TrimSpace(containerID)
	if fleetNodeID == "" || containerID == "" {
		return nil, ErrInvalid
	}
	if _, err := s.fleetRepo.GetNode(ctx, fleetNodeID, false); err != nil {
		return nil, err
	}
	if s.hasFleetInfrastructure(ctx, fleetNodeID) || s.fleetTunnelStatus(fleetNodeID).Status == "online" {
		var detail fleetprotocol.Detail
		if err := s.callFleetInfrastructure(ctx, fleetNodeID, fleetTunnelRequest{
			Operation: fleetTunnelOperationDetail, ContainerID: containerID,
		}, &detail); err != nil {
			return nil, err
		}
		if err := s.fleet.cacheDetail(fleetNodeID, detail); err != nil {
			return nil, err
		}
		return remoteDetailResponse(detail, false), nil
	}
	if _, err := s.fleet.requireContainer(fleetNodeID, containerID); err != nil {
		return nil, err
	}
	session := s.fleet.active(fleetNodeID)
	if session == nil {
		if detail, ok := s.fleet.cachedDetail(fleetNodeID, containerID); ok {
			return remoteDetailResponse(detail, true), nil
		}
		return nil, &FleetAgentResponseError{StatusCode: http.StatusServiceUnavailable, Code: "FLEET_NODE_OFFLINE"}
	}
	raw, err := session.execute(ctx, fleetprotocol.OperationContainerDetail, containerID, uuid.NewString(), struct{}{}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	var detail fleetprotocol.Detail
	if err := unmarshalFleetResult(raw, &detail); err != nil {
		return nil, err
	}
	if err := s.fleet.cacheDetail(fleetNodeID, detail); err != nil {
		return nil, err
	}
	return remoteDetailResponse(detail, false), nil
}

func (s *Service) FleetNodeContainerLogs(ctx context.Context, command FleetNodeContainerLogsCommand) (*fleetprotocol.ContainerLogsResult, error) {
	command.FleetNodeID = strings.TrimSpace(command.FleetNodeID)
	command.ContainerID = strings.TrimSpace(command.ContainerID)
	if command.FleetNodeID == "" || command.ContainerID == "" {
		return nil, ErrInvalid
	}
	if _, err := s.fleetRepo.GetNode(ctx, command.FleetNodeID, false); err != nil {
		return nil, err
	}
	request := fleetprotocol.ContainerLogsRequest{
		Tail: command.Tail, Since: command.Since,
		Timestamps: command.Timestamps, LimitBytes: command.LimitBytes,
	}
	if s.hasFleetInfrastructure(ctx, command.FleetNodeID) || s.fleetTunnelStatus(command.FleetNodeID).Status == "online" {
		var result fleetprotocol.ContainerLogsResult
		requestID := uuid.NewString()
		if err := s.callFleetInfrastructure(ctx, command.FleetNodeID, fleetTunnelRequest{
			Operation: fleetTunnelOperationLogs, ContainerID: command.ContainerID,
			OperationID: requestID, Logs: request,
		}, &result); err != nil {
			return nil, err
		}
		result.OperationID = requestID
		return &result, nil
	}
	if _, err := s.fleet.requireContainer(command.FleetNodeID, command.ContainerID); err != nil {
		return nil, err
	}
	session, err := s.fleet.requireSession(command.FleetNodeID)
	if err != nil {
		return nil, err
	}
	requestID := uuid.NewString()
	raw, err := session.execute(ctx, fleetprotocol.OperationContainerLogs, command.ContainerID, requestID, request, 30*time.Second)
	if err != nil {
		return nil, err
	}
	var result fleetprotocol.ContainerLogsResult
	if err := unmarshalFleetResult(raw, &result); err != nil {
		return nil, err
	}
	result.OperationID = requestID
	return &result, nil
}

func (s *Service) RestartFleetNodeContainer(ctx context.Context, command FleetNodeContainerRestartCommand) (map[string]any, error) {
	command.FleetNodeID = strings.TrimSpace(command.FleetNodeID)
	command.ContainerID = strings.TrimSpace(command.ContainerID)
	if command.FleetNodeID == "" || command.ContainerID == "" {
		return nil, ErrInvalid
	}
	container, err := s.fleet.requireContainer(command.FleetNodeID, command.ContainerID)
	if err != nil {
		return nil, err
	}
	if !container.Restartable || fleetContainerRestartPermanentlyDenied(container) {
		return nil, &FleetAgentResponseError{StatusCode: http.StatusForbidden, Code: "FLEET_CONTAINER_RESTART_FORBIDDEN"}
	}
	if !s.hasFleetInfrastructure(ctx, command.FleetNodeID) && s.fleetTunnelStatus(command.FleetNodeID).Status != "online" {
		if _, err := s.fleet.requireSession(command.FleetNodeID); err != nil {
			return nil, err
		}
	}
	operationID := strings.TrimSpace(command.RequestID)
	if operationID == "" {
		operationID = uuid.NewString()
	}
	if len(operationID) > 64 {
		return nil, ErrInvalid
	}
	operation, created, err := s.createFleetOperation(ctx, repo.CreateFleetOperationInput{
		OperationID:   operationID,
		FleetNodeID:   command.FleetNodeID,
		ContainerID:   command.ContainerID,
		OperationType: fleetprotocol.OperationContainerRestart,
		ActorID:       command.UserID,
		RequestJSON:   json.RawMessage(`{}`),
	})
	if err != nil {
		return nil, err
	}
	if created {
		go s.executeFleetRestart(operation)
	}
	return fleetOperationResponse(operation), nil
}

func fleetContainerRestartPermanentlyDenied(container fleetprotocol.Summary) bool {
	values := []string{container.ServiceName, container.DisplayName, container.ComposeService}
	for _, value := range container.Labels {
		values = append(values, value)
	}
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "backend", "pg", "tsdb", "timescale", "timescaledb", "postgres", "postgresql", "redis":
			return true
		}
	}
	return false
}

func remoteSnapshotResponse(snapshot fleetprotocol.SnapshotPayload, stale bool) map[string]any {
	normalizeFleetContainerSummary(&snapshot.Containers)
	response := structToMap(snapshot.Containers)
	response["host"] = snapshot.Host
	response["stale"] = stale
	return response
}

func normalizeFleetContainerSummary(snapshot *fleetprotocol.ContainerSnapshot) {
	if snapshot == nil {
		return
	}
	snapshot.Summary = fleetprotocol.SummarizeContainers(snapshot.Containers)
}

func fleetAbnormalContainerCount(containers []fleetprotocol.Summary) int {
	return fleetprotocol.SummarizeContainers(containers).Unhealthy
}

func remoteDetailResponse(detail fleetprotocol.Detail, stale bool) map[string]any {
	response := structToMap(detail)
	response["stale"] = stale
	return response
}

func structToMap(value any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	result := map[string]any{}
	_ = json.Unmarshal(raw, &result)
	return result
}

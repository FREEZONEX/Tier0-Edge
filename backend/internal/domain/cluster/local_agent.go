package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	domainlicense "backend/internal/domain/license"
	"backend/internal/fleetprotocol"

	"github.com/google/uuid"
)

var (
	ErrFleetAgentNotConfigured = errors.New("FLEET_AGENT_NOT_CONFIGURED")
	ErrFleetAgentUnavailable   = errors.New("FLEET_AGENT_UNAVAILABLE")
)

type FleetAgentResponseError struct {
	StatusCode int
	Code       string
}

func (e *FleetAgentResponseError) Error() string {
	if e == nil || strings.TrimSpace(e.Code) == "" {
		return ErrFleetAgentUnavailable.Error()
	}
	return strings.TrimSpace(e.Code)
}

type FleetAgentLogsRequest = fleetprotocol.ContainerLogsRequest
type FleetAgentRestartRequest = fleetprotocol.ContainerRestartRequest

type LocalFleetAgent interface {
	Status(context.Context) (*fleetprotocol.AgentStatus, error)
	HAStatus(context.Context) (*fleetprotocol.HAStatus, error)
	ConfigureConnection(context.Context, string) (*fleetprotocol.AgentStatus, error)
	DisconnectConnection(context.Context) (*fleetprotocol.AgentStatus, error)
	Containers(context.Context) (*fleetprotocol.ContainerSnapshot, error)
	Container(context.Context, string) (*fleetprotocol.Detail, error)
	ContainerLogs(context.Context, string, FleetAgentLogsRequest) (*fleetprotocol.ContainerLogsResult, error)
	RestartContainer(context.Context, string, FleetAgentRestartRequest) (*fleetprotocol.ContainerRestartResult, error)
}

func (s *Service) ConfigureCurrentConnection(ctx context.Context, accessToken string) (*fleetprotocol.AgentStatus, error) {
	if licenseService := domainlicense.Default(); licenseService != nil && licenseService.IsHost() {
		return nil, ErrHostCannotConnect
	}
	if s.localAgent == nil {
		return nil, ErrFleetAgentNotConfigured
	}
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, ErrInvalid
	}
	return s.requestConnectionSwitch(ctx, accessToken)
}

func (s *Service) DisconnectCurrentConnection(ctx context.Context) (*fleetprotocol.AgentStatus, error) {
	if s.localAgent == nil {
		return nil, ErrFleetAgentNotConfigured
	}
	s.connectionMu.Lock()
	defer s.connectionMu.Unlock()
	if job, err := s.loadConnectionSwitch(); err == nil {
		job.Token, job.State, job.ErrorCode = "", "cancelled", ""
		if err = writeFleetState(s.cfg.IdentityDir, "connection-switch.json", job); err != nil {
			return nil, err
		}
	}
	return s.localAgent.DisconnectConnection(ctx)
}

type CurrentContainerLogsCommand struct {
	ContainerID string
	Tail        int
	Since       int64
	Timestamps  *bool
	LimitBytes  int
}

type CurrentContainerRestartCommand struct {
	ContainerID string
	RequestID   string
}

func (s *Service) CurrentNode(ctx context.Context) (map[string]any, error) {
	result := map[string]any{
		"planCode": "edge",
		"role":     "edge",
	}
	if s.localAgent == nil {
		result["agent"] = map[string]any{"availability": "notConfigured"}
		return result, nil
	}
	status, err := s.localAgent.Status(ctx)
	if err != nil {
		availability := "error"
		if errors.Is(err, ErrFleetAgentNotConfigured) {
			availability = "notConfigured"
		} else if errors.Is(err, ErrFleetAgentUnavailable) {
			availability = "unavailable"
		}
		agent := map[string]any{"availability": availability}
		var responseErr *FleetAgentResponseError
		if errors.As(err, &responseErr) && responseErr.Code != "" {
			agent["errorCode"] = responseErr.Code
		}
		result["agent"] = agent
		return result, nil
	}
	var agentStatus map[string]any
	rawStatus, err := json.Marshal(status)
	if err != nil {
		result["agent"] = map[string]any{"availability": "error", "errorCode": "FLEET_AGENT_INVALID_RESPONSE"}
		return result, nil
	}
	if err := json.Unmarshal(rawStatus, &agentStatus); err != nil {
		result["agent"] = map[string]any{"availability": "error", "errorCode": "FLEET_AGENT_INVALID_RESPONSE"}
		return result, nil
	}
	agentStatus["availability"] = "available"
	result["agent"] = agentStatus
	haStatus, haErr := s.localAgent.HAStatus(ctx)
	if haErr != nil {
		result["ha"] = map[string]any{"enabled": false, "errorCode": fleetAgentErrorCode(haErr)}
	} else if haStatus.Enabled {
		result["ha"] = map[string]any{"enabled": true, "status": haStatus}
	} else {
		result["ha"] = map[string]any{"enabled": false}
	}
	return result, nil
}

func fleetAgentErrorCode(err error) string {
	var responseErr *FleetAgentResponseError
	if errors.As(err, &responseErr) && strings.TrimSpace(responseErr.Code) != "" {
		return responseErr.Code
	}
	if errors.Is(err, ErrFleetAgentNotConfigured) {
		return ErrFleetAgentNotConfigured.Error()
	}
	return ErrFleetAgentUnavailable.Error()
}

// CurrentHost uses the Agent's platform-specific host collector on native installs.
func (s *Service) CurrentHost(ctx context.Context) (*fleetprotocol.HostSnapshot, error) {
	if s.localAgent == nil {
		return nil, ErrFleetAgentNotConfigured
	}
	status, err := s.localAgent.Status(ctx)
	if err != nil {
		return nil, err
	}
	return &status.Host, nil
}

func (s *Service) CurrentContainers(ctx context.Context) (*fleetprotocol.ContainerSnapshot, error) {
	if s.localAgent == nil {
		return nil, ErrFleetAgentNotConfigured
	}
	snapshot, err := s.localAgent.Containers(ctx)
	if err != nil {
		return nil, err
	}
	normalizeFleetContainerSummary(snapshot)
	return snapshot, nil
}

func (s *Service) CurrentContainer(ctx context.Context, containerID string) (*fleetprotocol.Detail, error) {
	if s.localAgent == nil {
		return nil, ErrFleetAgentNotConfigured
	}
	return s.localAgent.Container(ctx, strings.TrimSpace(containerID))
}

func (s *Service) CurrentContainerLogs(ctx context.Context, cmd CurrentContainerLogsCommand) (*fleetprotocol.ContainerLogsResult, error) {
	if s.localAgent == nil {
		return nil, ErrFleetAgentNotConfigured
	}
	return s.localAgent.ContainerLogs(ctx, strings.TrimSpace(cmd.ContainerID), FleetAgentLogsRequest{
		Tail:       cmd.Tail,
		Since:      cmd.Since,
		Timestamps: cmd.Timestamps,
		LimitBytes: cmd.LimitBytes,
	})
}

func (s *Service) RestartCurrentContainer(ctx context.Context, cmd CurrentContainerRestartCommand) (*fleetprotocol.ContainerRestartResult, error) {
	if s.localAgent == nil {
		return nil, ErrFleetAgentNotConfigured
	}
	operationID := strings.TrimSpace(cmd.RequestID)
	if operationID == "" {
		operationID = uuid.NewString()
	}
	return s.localAgent.RestartContainer(ctx, strings.TrimSpace(cmd.ContainerID), FleetAgentRestartRequest{OperationID: operationID})
}

package status

import (
	"sync"
	"time"

	"backend/fleet-agent/internal/config"
	"backend/fleet-agent/internal/hostinfo"
	"backend/internal/fleetprotocol"
	"backend/internal/runtimeenv"
)

const (
	ConnectionUnconfigured = fleetprotocol.ConnectionUnconfigured
	ConnectionDisconnected = fleetprotocol.ConnectionDisconnected
)

type Snapshot = fleetprotocol.AgentStatus

type Service struct {
	mu       sync.RWMutex
	snapshot Snapshot
}

func New(version, protocolVersion string, startedAt time.Time, cfg config.Config, host hostinfo.Snapshot) *Service {
	capabilities := []string{"local.health", "local.status", "local.host", "container.list"}
	if !runtimeenv.IsNative() {
		capabilities = append(capabilities, "container.detail", "container.logs", "container.restart")
	}
	connectionState := ConnectionUnconfigured
	if cfg.CenterConfigured() {
		connectionState = ConnectionDisconnected
	}

	return &Service{
		snapshot: Snapshot{
			Service:            "tier0-fleet-agent",
			Version:            version,
			ProtocolVersion:    protocolVersion,
			StartedAt:          startedAt.UnixMilli(),
			Configured:         cfg.CenterConfigured(),
			ConnectionState:    connectionState,
			FleetNodeID:        cfg.FleetNodeID,
			CenterURL:          cfg.CenterURL,
			InstallationID:     cfg.InstallationID,
			ComposeProjectName: cfg.ComposeProjectName,
			Host:               host,
			Capabilities:       capabilities,
		},
	}
}

func (s *Service) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}

func (s *Service) SetConnectionState(state string) {
	s.mu.Lock()
	s.snapshot.ConnectionState = state
	s.mu.Unlock()
}

func (s *Service) SetConfiguration(cfg config.Config) {
	s.mu.Lock()
	s.snapshot.Configured = cfg.CenterConfigured()
	s.snapshot.FleetNodeID = cfg.FleetNodeID
	s.snapshot.CenterURL = cfg.CenterURL
	if cfg.CenterConfigured() {
		s.snapshot.ConnectionState = ConnectionDisconnected
	} else {
		s.snapshot.ConnectionState = ConnectionUnconfigured
	}
	s.mu.Unlock()
}

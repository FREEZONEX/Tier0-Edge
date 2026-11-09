package connection

import (
	"context"
	"errors"
	"strings"
	"sync"

	"backend/fleet-agent/internal/config"
)

type store interface {
	LoadConfig(context.Context) (config.Persisted, bool, error)
	SaveConfig(context.Context, config.Persisted) error
}

type centerUpdater interface {
	UpdateConfig(config.Config)
}

type statusUpdater interface {
	SetConfiguration(config.Config)
}

type Service struct {
	mu     sync.Mutex
	cfg    config.Config
	store  store
	center centerUpdater
	status statusUpdater
}

func New(cfg config.Config, store store, center centerUpdater, status statusUpdater) *Service {
	return &Service{cfg: cfg, store: store, center: center, status: status}
}

func (s *Service) Configure(ctx context.Context, accessToken string) error {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return errors.New("FLEET_INVALID_TOKEN")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := s.cfg
	candidate.AccessToken = accessToken
	candidate.DesiredConnected = true
	if err := config.ApplyConnectionToken(&candidate); err != nil {
		return errors.New("FLEET_INVALID_TOKEN")
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	apply := func() error {
		persisted, _, err := s.store.LoadConfig(ctx)
		if err != nil {
			return err
		}
		persisted.AccessToken = candidate.AccessToken
		persisted.CenterURL = candidate.CenterURL
		persisted.FleetNodeID = candidate.FleetNodeID
		persisted.DesiredConnected = true
		persisted.InstallationID = candidate.InstallationID
		persisted.ComposeProjectName = candidate.ComposeProjectName
		persisted.LocalTokenHash = append([]byte(nil), candidate.LocalTokenHash...)
		if err := s.store.SaveConfig(ctx, persisted); err != nil {
			return err
		}
		s.cfg = candidate
		s.status.SetConfiguration(candidate)
		s.center.UpdateConfig(candidate)
		return nil
	}
	if preparer, ok := s.center.(interface {
		PrepareConnection(context.Context, config.Config, func() error) error
	}); ok {
		return preparer.PrepareConnection(ctx, candidate, apply)
	}
	return apply()
}

func (s *Service) Disconnect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate := s.cfg
	candidate.DesiredConnected = false
	persisted, _, err := s.store.LoadConfig(ctx)
	if err != nil {
		return err
	}
	persisted.DesiredConnected = false
	if err := s.store.SaveConfig(ctx, persisted); err != nil {
		return err
	}
	s.cfg = candidate
	s.status.SetConfiguration(candidate)
	s.center.UpdateConfig(candidate)
	return nil
}

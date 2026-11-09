package cluster

import (
	"context"
	"errors"
	"fmt"
)

func (s *Service) stopChildConnectionsForHost(ctx context.Context) error {
	if s.tunnel != nil {
		s.tunnel.StopClient()
	}
	if s.localAgent != nil {
		if _, err := s.localAgent.DisconnectConnection(ctx); err != nil {
			return errors.Join(fmt.Errorf("disconnect local Fleet Agent: %w", err))
		}
	}
	return nil
}
func (s *Service) Close() {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() { close(s.closed) })
	if s.fleet != nil {
		s.fleet.closeAll()
	}
}

func (s *Service) localMqttBrokers() string { return s.ingestCfg.MqttBrokers }

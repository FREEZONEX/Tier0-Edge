package container

import (
	"context"
	"fmt"
	dockerclient "github.com/moby/moby/client"
)

// SetRunning is an infrastructure operation on an inspected owned container.
// It shares the same protection and in-flight guard as the existing restart API.
// Operation receipts and idempotency live in RuntimeProvider.
func (s *Service) SetRunning(ctx context.Context, id, operationID string, running bool) error {
	if !validContainerID(id) || !validOperationID(operationID) {
		return ErrInvalid
	}
	if err := s.ready(); err != nil {
		return err
	}
	s.operationMu.Lock()
	if _, busy := s.running[id]; busy {
		s.operationMu.Unlock()
		return ErrConflict
	}
	s.running[id] = operationID
	s.operationMu.Unlock()
	defer func() { s.operationMu.Lock(); delete(s.running, id); s.operationMu.Unlock() }()
	detail, err := s.inspect(ctx, id)
	if err != nil {
		return err
	}
	if !detail.Restartable {
		return ErrRestartDenied
	}
	if running {
		_, err = s.client.ContainerStart(ctx, id, dockerclient.ContainerStartOptions{})
	} else {
		timeout := int(restartStopTimeout.Seconds())
		_, err = s.client.ContainerStop(ctx, id, dockerclient.ContainerStopOptions{Timeout: &timeout})
	}
	if err != nil {
		return classifyDockerError(err)
	}
	after, err := s.inspect(ctx, id)
	if err != nil {
		return err
	}
	if (after.State == "running") != running {
		return fmt.Errorf("runtime transition did not reach requested state")
	}
	return nil
}

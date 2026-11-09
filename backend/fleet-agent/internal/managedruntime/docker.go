package managedruntime

import (
	fleetcontainer "backend/fleet-agent/internal/container"
	"backend/internal/fleetprotocol"
	"backend/internal/product"
	"context"
	"errors"
	"sync"
	"time"
)

type Docker struct {
	Containers *fleetcontainer.Service
	mu         sync.Mutex
	operations map[string]fleetprotocol.RuntimeOperation
}

func (d *Docker) List(ctx context.Context) (fleetprotocol.RuntimeSnapshot, error) {
	s, err := d.Containers.Snapshot(ctx)
	r := fleetprotocol.RuntimeSnapshot{RuntimeMode: "docker", ObservedAt: s.ObservedAt, Items: []fleetprotocol.RuntimeItem{}}
	for _, c := range s.Containers {
		actions := []string{"logs"}
		if c.Restartable {
			actions = append(actions, "restart")
			if c.State == "running" {
				actions = append(actions, "stop")
			} else {
				actions = append(actions, "start")
			}
		}
		component := product.Service(c.ServiceName)
		r.Items = append(r.Items, fleetprotocol.RuntimeItem{InstallationID: s.InstallationID, Dependencies: component.Dependencies, Protection: component.Protection, ID: c.ContainerID, Name: c.DisplayName, State: c.State, Health: c.HealthStatus, Version: c.Image, StartedAt: c.StartedAt, RestartCount: c.RestartCount, Actions: actions})
	}
	return r, err
}
func (d *Docker) Logs(ctx context.Context, q fleetprotocol.RuntimeRequest) (fleetprotocol.RuntimeLogs, error) {
	r, e := d.Containers.Logs(ctx, q.ResourceID, fleetcontainer.LogsRequest{Tail: q.Tail})
	return fleetprotocol.RuntimeLogs{ResourceID: q.ResourceID, Text: r.Logs, Truncated: r.Truncated}, e
}
func (d *Docker) Control(ctx context.Context, q fleetprotocol.RuntimeRequest) (fleetprotocol.RuntimeOperation, error) {
	if q.Verb != "restart" && q.Verb != "start" && q.Verb != "stop" {
		return fleetprotocol.RuntimeOperation{}, errors.New("RUNTIME_ACTION_UNSUPPORTED")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if old, ok := d.operations[q.OperationID]; ok {
		if old.ResourceID != q.ResourceID || old.Verb != q.Verb {
			return old, fleetprotocol.ErrRuntimeRequest
		}
		return old, nil
	}
	if len(d.operations) >= 2048 {
		return fleetprotocol.RuntimeOperation{}, errors.New("RUNTIME_OPERATION_LIMIT")
	}
	for _, prior := range d.operations {
		if prior.ResourceID == q.ResourceID && prior.State == "pending" {
			return fleetprotocol.RuntimeOperation{}, errors.New("RUNTIME_OPERATION_CONFLICT")
		}
	}
	if d.operations == nil {
		d.operations = map[string]fleetprotocol.RuntimeOperation{}
	}
	op := fleetprotocol.RuntimeOperation{ID: q.OperationID, ResourceID: q.ResourceID, Verb: q.Verb, State: "pending", UpdatedAt: time.Now().UnixMilli()}
	d.operations[op.ID] = op
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 125*time.Second)
		defer cancel()
		var err error
		if q.Verb == "restart" {
			_, err = d.Containers.Restart(ctx, q.ResourceID, fleetcontainer.RestartRequest{OperationID: q.OperationID})
		} else {
			err = d.Containers.SetRunning(ctx, q.ResourceID, q.OperationID, q.Verb == "start")
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		op.State = "succeeded"
		if err != nil {
			op.State = "failed"
			op.Error = err.Error()
		}
		op.UpdatedAt = time.Now().UnixMilli()
		d.operations[op.ID] = op
	}()
	return op, nil
}
func (d *Docker) Operation(_ context.Context, id string) (fleetprotocol.RuntimeOperation, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.operations[id]
	if !ok {
		return r, errors.New("RUNTIME_OPERATION_UNKNOWN")
	}
	return r, nil
}

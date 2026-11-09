package sourcestream

import (
	"context"
	"encoding/json"
	"sync"

	"backend/internal/repo"
	"backend/internal/streamexec"
)

type programCheckpointKey struct{}
type programCheckpointBuffer struct {
	mu      sync.Mutex
	loaded  bool
	changed bool
	state   repo.ProgramCheckpoint
}

func withProgramCheckpoint(ctx context.Context, sample json.RawMessage) (context.Context, *programCheckpointBuffer) {
	buffer := &programCheckpointBuffer{}
	if len(sample) > 0 {
		buffer.loaded = true
		buffer.state.Value = append(json.RawMessage{}, sample...)
	}
	return context.WithValue(ctx, programCheckpointKey{}, buffer), buffer
}

func (b *programCheckpointBuffer) result() *streamexec.ProgramCheckpoint {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.changed {
		return nil
	}
	return &streamexec.ProgramCheckpoint{Revision: b.state.Revision, Value: append(json.RawMessage{}, b.state.Value...)}
}

func (b *programCheckpointBuffer) call(ctx context.Context, call streamexec.ProgramCall, runID, entryID string, dryRun bool) (json.RawMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.loaded {
		if dryRun {
			b.state.Value = json.RawMessage(`null`)
		} else {
			r := repo.NewSourceStreamRepo(ctx)
			run, err := r.Run(ctx, runID)
			if err != nil || run.EntryID != entryID {
				return nil, errInactivePlan
			}
			state, err := r.ProgramCheckpoint(ctx, run.StreamID, entryID)
			if err != nil {
				return nil, err
			}
			b.state = state
		}
		b.loaded = true
	}
	if call.Kind == "checkpoint.get" {
		return append(json.RawMessage{}, b.state.Value...), nil
	}
	if call.Kind != "checkpoint.set" || len(call.Input) > streamexec.MaxCheckpointBytes || !json.Valid(call.Input) {
		return nil, repo.ErrInvalidArgument
	}
	b.state.Value = append(json.RawMessage{}, call.Input...)
	b.changed = true
	return json.RawMessage(`{"stage":"buffered"}`), nil
}

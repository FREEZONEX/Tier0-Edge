package sourcestreamworker

import (
	"context"
	"sync"

	"backend/internal/domain/sourcestream"
)

type observedWatch struct {
	ctx    context.Context
	spec   sourceWatchSpec
	state  string
	closed bool
}

// A watch attempt owns its observation. Delayed callbacks from a canceled
// attempt cannot mark a replacement subscription ready, even at the same revision.
type watchScope struct {
	sourceID int64
	key      string
}
type watchReadiness struct {
	mu      sync.RWMutex
	entries map[watchScope]*observedWatch
}

func (r *watchReadiness) begin(ctx context.Context, id int64, spec sourceWatchSpec) (func(bool), func(string)) {
	return r.beginScope(ctx, id, "program", spec)
}
func (r *watchReadiness) beginScope(ctx context.Context, id int64, key string, spec sourceWatchSpec) (func(bool), func(string)) {
	owner := watchScope{sourceID: id, key: key}
	spec.Refs = append([]string(nil), spec.Refs...)
	entry := &observedWatch{ctx: ctx, spec: spec, state: "starting"}
	r.mu.Lock()
	if r.entries == nil {
		r.entries = make(map[watchScope]*observedWatch)
	}
	r.entries[owner] = entry
	r.mu.Unlock()
	observe := func(ready bool) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.entries[owner] != entry || entry.closed {
			return
		}
		entry.state = "starting"
		if ready && ctx.Err() == nil {
			entry.state = "ready"
		}
	}
	finish := func(state string) {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.entries[owner] != entry {
			return
		}
		entry.closed = true
		if ctx.Err() != nil {
			delete(r.entries, owner)
		} else {
			entry.state = state
		}
	}
	return observe, finish
}

func (r *watchReadiness) state(query sourcestream.WatchQuery) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(query.ResourceRefs) == 0 {
		return "starting"
	}
	for _, ref := range query.ResourceRefs {
		covered := false
		failed := false
		for owner, entry := range r.entries {
			if owner.sourceID != query.SourceID || entry.ctx.Err() != nil || entry.spec.Revision != query.ConfigRevision || entry.spec.QoS < query.QoS || entry.spec.AcquisitionKey != query.AcquisitionKey {
				continue
			}
			for _, watched := range entry.spec.Refs {
				if ref == watched {
					if entry.state == "ready" && !entry.closed {
						covered = true
					}
					if entry.state == "source-watch-failed" {
						failed = true
					}
				}
			}
		}
		if !covered {
			if failed {
				return "source-watch-failed"
			}
			return "starting"
		}
	}
	return "ready"
}

package sourcestream

import (
	"context"
	"encoding/json"

	"backend/internal/repo"
)

// WatchQuery contains only connection identity and subscription scope, never
// credentials. Readiness is process-local observation, not a deployed version.
type WatchQuery struct {
	SourceID       int64
	ConfigRevision int64
	ResourceRefs   []string
	QoS            int
	AcquisitionKey string
}

type watchObserverKey struct{}

type watchConfigRevisionKey struct{}

func WatchConfigRevision(ctx context.Context) int64 {
	revision, _ := ctx.Value(watchConfigRevisionKey{}).(int64)
	return revision
}

// WithWatchObserver binds a single watch attempt. Drivers report ready only
// after the protocol has confirmed the complete requested acquisition scope.
func WithWatchObserver(ctx context.Context, observer func(bool)) context.Context {
	return context.WithValue(ctx, watchObserverKey{}, observer)
}

func ReportWatchReady(ctx context.Context, ready bool) {
	if observer, ok := ctx.Value(watchObserverKey{}).(func(bool)); ok && observer != nil {
		observer(ready && ctx.Err() == nil)
	}
}

func watchQuery(version repo.StreamVersion) (WatchQuery, bool, error) {
	var def StreamDefinition
	if json.Unmarshal([]byte(version.Document), &def) != nil {
		return WatchQuery{}, false, repo.ErrInvalidArgument
	}
	if def.Model != Publish {
		return WatchQuery{}, false, nil
	}
	query := WatchQuery{SourceID: def.SourceID, ConfigRevision: version.SourceRevision, ResourceRefs: def.ResourceRefs()}
	if def.Program != nil {
		if def.Program.Trigger.Kind != SourceTrigger {
			return WatchQuery{}, false, nil
		}
		snapshots, err := version.Sources()
		if err != nil {
			return query, false, err
		}
		query.ResourceRefs, query.QoS = def.Program.Trigger.ResourceRefs, def.Program.Trigger.QoS
		for _, snapshot := range snapshots {
			if snapshot.Alias == def.Program.Trigger.SourceAlias {
				query.SourceID, query.ConfigRevision = snapshot.SourceID, snapshot.ConfigRevision
			}
		}
	}
	if query.SourceID <= 0 || query.ConfigRevision <= 0 || len(query.ResourceRefs) == 0 {
		return query, false, repo.ErrInvalidArgument
	}
	return query, true, nil
}

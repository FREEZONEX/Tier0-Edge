package sourcestream

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"

	"backend/internal/repo"
)

func programV2Plans(item repo.StreamExecution, candidate bool) ([]ExecutionPlan, error) {
	if !candidate && item.Version.ActivatedAt <= 0 {
		return nil, errInactivePlan
	}
	manifest, err := item.Version.ProgramV2Manifest()
	if err != nil {
		return nil, err
	}
	snapshots, err := item.Version.Sources()
	if err != nil || len(snapshots) != len(manifest.Sources) {
		return nil, repo.ErrInvalidArgument
	}
	byID := map[string]repo.StreamSourceSnapshot{}
	for i, source := range manifest.Sources {
		frozen := snapshots[i]
		if strconv.FormatInt(frozen.SourceID, 10) != source.ID || frozen.Alias != source.ID || frozen.ConfigRevision <= 0 || !reflect.DeepEqual(frozen.Resources, source.Resources) {
			return nil, repo.ErrInvalidArgument
		}
		byID[source.ID] = frozen
	}
	var def StreamDefinition
	if decodeStoredProgram([]byte(item.Version.Document), &def) != nil || !def.IsCompleteProgram() || def.ValidateDraft() != nil {
		return nil, repo.ErrInvalidArgument
	}
	plans := make([]ExecutionPlan, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		entry := entry
		ref := ExecutionRef{EntryID: entry.ID, StreamID: item.Stream.ID, VersionID: item.Version.ID}
		if entry.Kind == "source-event" || entry.Kind == "http-request" {
			frozen, ok := byID[entry.SourceID]
			if !ok {
				return nil, repo.ErrInvalidArgument
			}
			ref.SourceID, ref.SourceRevision = frozen.SourceID, frozen.ConfigRevision
		}
		plans = append(plans, ExecutionPlan{ExecutionRef: ref, Definition: def, Manifest: &manifest, Entry: &entry, Candidate: candidate})
	}
	return plans, nil
}

// WatchPlans includes staged candidates only to establish protocol readiness.
// Callbacks and schedulers must use Plans, which exposes activated versions only.
func (s *Service) WatchPlans(ctx context.Context) ([]ExecutionPlan, error) {
	plans, err := s.Plans(ctx)
	if err != nil {
		return nil, err
	}
	items, err := repo.NewSourceStreamRepo(ctx).PendingProgramV2Executions(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		candidate, err := programV2Plans(item, true)
		if err != nil {
			return nil, err
		}
		plans = append(plans, candidate...)
	}
	return plans, nil
}

// Source-backed entries all require actual SUBACK/MonitoredItem/first-read
// confirmation. Probe health or a candidate's existence is never readiness.
func ProgramV2WatchQueries(version repo.StreamVersion) ([]WatchQuery, error) {
	plans, err := programV2Plans(repo.StreamExecution{Stream: repo.Stream{ID: version.StreamID}, Version: version}, true)
	if err != nil {
		return nil, err
	}
	queries := []WatchQuery{}
	for _, plan := range plans {
		if plan.Entry.Kind == "source-event" {
			queries = append(queries, plan.ProgramWatchQuery())
		}
	}
	return queries, nil
}

func (s *Service) ActivateReadyProgramV2(ctx context.Context) error {
	if s.asyncReady == nil || !s.asyncReady() {
		return nil
	}
	r := repo.NewSourceStreamRepo(ctx)
	items, err := r.PendingProgramV2Executions(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, item := range items {
		queries, err := ProgramV2WatchQueries(item.Version)
		if err != nil {
			return err
		}
		ready := true
		for _, query := range queries {
			ready = ready && s.watchState != nil && s.watchState(query) == "ready"
		}
		if !ready {
			continue
		}
		if _, err = r.ActivateStagedProgramV2(ctx, item.Stream.ID, item.Stream.Revision, item.Version.ID); err != nil && !errors.Is(err, repo.ErrConflict) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (p ExecutionPlan) SourceEventScope() ([]string, int, bool) {
	if p.Entry != nil {
		return p.Entry.ResourceRefs, p.Entry.QoS, p.Entry.Kind == "source-event" && p.SourceID > 0
	}
	return nil, 0, false
}

func (p ExecutionPlan) MatchesSourceInput(sourceID int64, ref string) bool {
	_, _, watched := p.SourceEventScope()
	if !watched || p.Candidate || p.SourceID != sourceID {
		return false
	}

	if p.Entry.Kind != "source-event" {
		return false
	}
	for _, selected := range p.Entry.ResourceRefs {
		if selected == ref || strings.HasPrefix(selected, "mqtt:") && strings.HasPrefix(ref, "mqtt:") && mqttFilterMatches(strings.TrimPrefix(selected, "mqtt:"), strings.TrimPrefix(ref, "mqtt:")) {
			return true
		}
	}
	return false
}

func (s *Service) loadProgramV2Plan(ctx context.Context, stream repo.Stream, version repo.StreamVersion, ref ExecutionRef) (StreamDefinition, error) {
	if stream.Status != "enabled" || version.ActivatedAt <= 0 {
		return StreamDefinition{}, errInactivePlan
	}
	if _, err := repo.NewSourceStreamRepo(ctx).ProgramVersionSources(ctx, version); err != nil {
		return StreamDefinition{}, errInactivePlan
	}
	plans, err := programV2Plans(repo.StreamExecution{Stream: stream, Version: version}, false)
	if err != nil {
		return StreamDefinition{}, err
	}
	for _, plan := range plans {
		if plan.EntryID == ref.EntryID {
			if plan.SourceID != ref.SourceID || plan.SourceRevision != ref.SourceRevision {
				return StreamDefinition{}, repo.ErrInvalidArgument
			}
			return plan.Definition, nil
		}
	}
	return StreamDefinition{}, repo.ErrInvalidArgument
}

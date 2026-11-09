package sourcestream

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"backend/internal/repo"
)

// A retained draft or release may no longer satisfy the current execution
// contract. Keep it visible for repair without admitting it for execution or
// allowing one historical record to break the entire management list.
func (s *Service) programV2View(ctx context.Context, item repo.Stream) (map[string]any, error) {
	view, err := s.programV2ExecutionView(ctx, item)
	if !errors.Is(err, repo.ErrInvalidArgument) {
		return view, err
	}
	view = streamView(item)
	view["pendingVersion"] = item.PendingVersion
	view["draftManifest"], view["manifest"] = nil, nil
	view["executionState"], view["deploymentState"] = "needs-redeploy", "needs-redeploy"
	return view, nil
}

func (s *Service) programV2ExecutionView(ctx context.Context, item repo.Stream) (map[string]any, error) {
	view := streamView(item)
	view["pendingVersion"] = item.PendingVersion
	draft, err := item.ProgramV2DraftManifest()
	if err != nil {
		return nil, err
	}
	view["draftManifest"], view["manifest"] = draft, nil
	state, deployment := item.Status, item.Status
	r := repo.NewSourceStreamRepo(ctx)
	if item.ActiveVersion > 0 {
		release, err := r.ReleaseNumber(ctx, item.ID, item.ActiveVersion)
		if err != nil {
			return nil, err
		}
		view["activeRelease"] = release
		version, err := r.Version(ctx, item.ID, item.ActiveVersion)
		if err != nil {
			return nil, err
		}
		manifest, err := version.ProgramV2Manifest()
		if err != nil {
			return nil, err
		}
		view["manifest"] = manifest
		snapshots, err := version.Sources()
		if err != nil {
			return nil, err
		}
		view["dependencies"] = snapshots
		plans, err := programV2Plans(repo.StreamExecution{Stream: item, Version: version}, false)
		if err != nil {
			return nil, err
		}
		if item.Status == "enabled" {
			state, deployment = "ready", "active"
			for _, snapshot := range snapshots {
				source, err := r.Source(ctx, snapshot.SourceID)
				if repo.IsSourceStreamMissing(err) {
					state = "needs-redeploy"
					break
				}
				if err != nil {
					return nil, err
				}
				if source.ConfigRevision != snapshot.ConfigRevision {
					state = "needs-redeploy"
					break
				}
				if source.DesiredState == "disabled" {
					state = "source-disabled"
					break
				}
				if source.Health != "healthy" {
					state = "source-unhealthy"
				}
			}
			entryStates := []map[string]any{}
			if state == "ready" && !s.ProgramExecutionReady() {
				state = "starting"
			}
			byID := map[string]repo.StreamSourceSnapshot{}
			for _, snapshot := range snapshots {
				byID[snapshot.Alias] = snapshot
			}
			for _, entry := range manifest.Entries {
				observed := state
				if observed == "ready" && entry.Kind != "request" && entry.Kind != "http-request" && (s.asyncReady == nil || !s.asyncReady()) {
					observed = "starting"
				}
				if observed == "ready" && entry.Kind == "http-request" {
					frozen := byID[entry.SourceID]
					observed = "starting"
					if s.httpServers.View(frozen.SourceID, 0)["state"] == "listening" {
						observed = "ready"
					}
				}
				if observed == "ready" && entry.Kind == "source-event" {
					observed = "starting"
					if s.watchState != nil {
						for _, plan := range plans {
							if plan.EntryID == entry.ID {
								observed = s.watchState(plan.ProgramWatchQuery())
								break
							}
						}
					}
				}
				entryStates = append(entryStates, map[string]any{"entryId": entry.ID, "kind": entry.Kind, "state": observed})
			}
			view["entryStates"] = entryStates
			if state == "ready" {
				for _, entry := range entryStates {
					if entry["state"] != "ready" {
						state = entry["state"].(string)
						break
					}
				}
			}
		}
	}
	if item.PendingVersion > 0 {
		version, err := r.Version(ctx, item.ID, item.PendingVersion)
		if err != nil {
			return nil, err
		}
		manifest, err := version.ProgramV2Manifest()
		if err != nil {
			return nil, err
		}
		view["pendingManifest"], deployment = manifest, "pending"
		queries, err := ProgramV2WatchQueries(version)
		if err != nil {
			return nil, err
		}
		pending := []map[string]any{}
		for _, query := range queries {
			observed := "starting"
			if s.watchState != nil {
				observed = s.watchState(query)
			}
			pending = append(pending, map[string]any{"sourceId": strconv.FormatInt(query.SourceID, 10), "state": observed})
		}
		view["pendingReadiness"] = pending
	}
	view["executionState"], view["deploymentState"] = state, deployment
	return view, nil
}

// Registration produces an explicit preview contract for the editor; it does
// not execute a handler, contact a device, save a draft or activate a version.
func (s *Service) ValidationView(ctx context.Context, input any, sourceRevision int64) (map[string]any, error) {
	def, _, err := parseDocument[StreamDefinition](input)
	if err != nil {
		return nil, err
	}
	manifest, _, err := s.compileProgramV2(ctx, def, false)
	if err != nil {
		return nil, err
	}
	return map[string]any{"valid": true, "issues": []any{}, "manifest": manifest, "document": input, "sideEffects": false}, nil
}

func programVersionView(item repo.StreamVersion) (map[string]any, error) {
	var doc any
	if json.Unmarshal([]byte(item.Document), &doc) != nil {
		return nil, repo.ErrInvalidArgument
	}
	view := map[string]any{"id": item.ID, "streamId": item.StreamID, "sourceRevision": item.SourceRevision, "sourceConfigRevision": item.SourceRevision, "document": doc, "createdAt": item.CreatedAt, "activatedAt": item.ActivatedAt}
	if item.ProgramManifest != "" && item.ProgramManifest != "{}" {
		// History remains readable in its original shape. This projection never
		// admits a release for execution; deployment uses the strict decoder.
		var manifest map[string]json.RawMessage
		if json.Unmarshal([]byte(item.ProgramManifest), &manifest) != nil || manifest == nil {
			return nil, repo.ErrInvalidArgument
		}
		delete(manifest, "_unsBindings")
		delete(manifest, "_executionPlan")
		delete(manifest, "_requestPolicies")
		view["manifest"] = manifest
	}
	return view, nil
}

package sourcestream

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"backend/internal/contextx"
	"backend/internal/domain/dataingest"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/unsref"
)

type controlledExecutor interface {
	TransformWithSources(context.Context, ScriptDefinition, any, map[string]any, []streamexec.SourceGrant, streamexec.SourceHost) (ScriptResult, error)
}

// UNS input is already durable in EVENTS. Freeze matching program versions
// separately from legacy downlink subscriptions before executing any target.
func (s *Service) MatchProgramEvent(ctx context.Context, event dataingest.StreamEvent, sink StreamSink) error {
	if event.MessageID == "" || len(event.MessageID) > 128 || event.ReceivedAt.IsZero() || sink == nil {
		return repo.ErrInvalidArgument
	}
	metadata := event.Origin == "metadata" || event.CollectionID != "" || event.MetadataTopicID != ""
	if metadata {
		if event.Origin != "metadata" || event.TopicID != 0 || !unsref.ValidMetadataID(event.CollectionID) || event.MetadataTopicID != "" && !unsref.ValidMetadataID(event.MetadataTopicID) {
			return repo.ErrInvalidArgument
		}
		// Collections are readable/writable, but only Topic subscriptions are
		// currently deployable. The matching Topic view has its own event.
		if event.MetadataTopicID == "" {
			return nil
		}
	} else if event.TopicID <= 0 {
		return repo.ErrInvalidArgument
	}
	targetID := event.MetadataTopicID
	resourceRef := "uns:" + targetID
	if !metadata {
		targetID = strconv.FormatInt(event.TopicID, 10)
		resourceRef = fmt.Sprintf("uns:%d", event.TopicID)
	}
	if len(event.Lineage) >= dataingest.MaxStreamLineage {
		return nil
	}
	plans, err := s.Plans(ctx)
	if err != nil {
		return err
	}
	refs := []ExecutionRef{}
	for _, plan := range plans {
		if plan.Candidate {
			continue
		}
		var topicIDs []string
		if plan.Entry != nil {
			if plan.Entry.Kind != "uns-event" {
				continue
			}
			topicIDs = plan.Entry.TopicIDs
		} else if plan.Definition.Program != nil && plan.Definition.Program.Trigger.Kind == UNSTrigger {
			if metadata {
				continue
			}
			for _, id := range plan.Definition.Program.Trigger.TopicIDs {
				topicIDs = append(topicIDs, strconv.FormatInt(id, 10))
			}
		} else {
			continue
		}
		visited := false
		for _, streamID := range event.Lineage {
			visited = visited || streamID == plan.StreamID
		}
		if visited {
			continue
		}
		for _, topicID := range topicIDs {
			matched := !metadata && plan.Manifest == nil && topicID == targetID
			if plan.Manifest != nil {
				matched = plan.Manifest.MatchesTopicReference(topicID, targetID, metadata)
			}
			if matched {
				refs = append(refs, plan.ExecutionRef)
				break
			}
		}
	}
	raw, err := json.Marshal(refs)
	if err != nil {
		return err
	}
	frozen, err := repo.NewSourceStreamRepo(ctx).BindEventMatch(ctx, "program:"+event.MessageID, string(raw))
	if err != nil {
		return err
	}
	if json.Unmarshal([]byte(frozen), &refs) != nil {
		return repo.ErrInvalidArgument
	}
	return s.ProcessRaw(ctx, RawInput{MessageID: event.MessageID, Trigger: UNSTrigger, TopicID: event.TopicID, MetadataTopicID: event.MetadataTopicID, CollectionID: event.CollectionID, Lineage: event.Lineage, Plans: refs, Input: SourceInput{ResourceRef: resourceRef, ReceivedAt: event.ReceivedAt.UnixMilli(), Data: event.Data}}, sink)
}

func authorizeProgramReferences(ctx context.Context, def StreamDefinition) error {
	if def.Program == nil {
		return repo.ErrInvalidArgument
	}
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || !contextx.HasResource(subject, "uns.stream.manage") {
		return ErrForbidden
	}
	if err := authorizeTopics(ctx, def); err != nil {
		return err
	}
	if len(def.Program.Sources) > 0 && (!contextx.HasResource(subject, "uns.source") || !contextx.HasResource(subject, "uns.read")) {
		return ErrForbidden
	}
	for _, dependency := range def.Program.Sources {
		if _, err := repo.NewSourceStreamRepo(ctx).Source(ctx, dependency.SourceID); err != nil {
			return err
		}
	}
	if len(def.Program.Trigger.TopicIDs) > 0 {
		inputs := StreamDefinition{Model: Subscribe, TopicIDs: def.Program.Trigger.TopicIDs}
		return authorizeTopics(ctx, inputs)
	}
	return nil
}

func (s *Service) validateProgram(ctx context.Context, def StreamDefinition, deployment bool) ([]repo.StreamSourceSnapshot, error) {
	if def.Program == nil {
		return nil, repo.ErrInvalidArgument
	}
	if deployment {
		if err := def.ValidateDeployment(0, SourceDefinition{}); err != nil {
			return nil, err
		}
	} else if err := def.ValidateDraft(); err != nil {
		return nil, err
	}
	if err := authorizeTopics(ctx, def); err != nil {
		return nil, err
	}
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || !contextx.HasResource(subject, "uns.stream.manage") {
		return nil, ErrForbidden
	}
	if len(def.Program.Sources) > 0 && (!contextx.HasResource(subject, "uns.source") || !contextx.HasResource(subject, "uns.read")) {
		return nil, ErrForbidden
	}
	if len(def.Program.Trigger.TopicIDs) > 0 {
		if !contextx.HasResource(subject, "uns.read") {
			return nil, ErrForbidden
		}
		for _, id := range def.Program.Trigger.TopicIDs {
			node, err := repo.NewUnsRepo(ctx).GetUnsNode(ctx, id, false)
			if err != nil {
				return nil, err
			}
			if node.Type != 2 {
				return nil, invalid("program.trigger.topicIds", "topicRequired")
			}
		}
	}
	snapshots := make([]repo.StreamSourceSnapshot, 0, len(def.Program.Sources))
	for _, dependency := range def.Program.Sources {
		source, err := repo.NewSourceStreamRepo(ctx).Source(ctx, dependency.SourceID)
		if err != nil {
			return nil, err
		}
		if deployment {
			if source.Health != "healthy" {
				return nil, repo.ErrConflict
			}
			var resources []NativeResource
			if json.Unmarshal([]byte(source.Resources), &resources) != nil {
				return nil, repo.ErrInvalidArgument
			}
			reads := append([]string{}, dependency.ReadResources...)
			if len(reads) > 0 && source.Protocol != string(OPCUA) && source.Protocol != string(ModbusTCP) {
				return nil, invalid("program.sources.readResources", "readUnsupported")
			}
			if dependency.Alias == def.Program.Trigger.SourceAlias {
				reads = append(reads, def.Program.Trigger.ResourceRefs...)
			}
			for _, ref := range reads {
				found := false
				for _, resource := range resources {
					if resource.ID != ref {
						continue
					}
					for _, model := range resource.Models {
						found = found || model == Publish
					}
				}
				if !found {
					return nil, invalid("program.sources", "missingOrUnsupported")
				}
			}
		}
		snapshots = append(snapshots, repo.StreamSourceSnapshot{Alias: dependency.Alias, SourceID: source.ID, ConfigRevision: source.ConfigRevision, ReadResources: dependency.ReadResources})
	}
	if s.executor == nil {
		return nil, ErrUnavailable
	}
	if len(programGrants(snapshots)) > 0 {
		if _, ok := s.executor.(controlledExecutor); !ok {
			return nil, ErrUnavailable
		}
	}
	if err := s.executor.Check(ctx, def.Script); err != nil {
		return nil, err
	}
	if def.ResponseScript != nil {
		if err := s.executor.Check(ctx, *def.ResponseScript); err != nil {
			return nil, err
		}
	}
	return snapshots, nil
}

func programGrants(snapshots []repo.StreamSourceSnapshot) []streamexec.SourceGrant {
	grants := make([]streamexec.SourceGrant, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if len(snapshot.ReadResources) == 0 {
			continue
		}
		grants = append(grants, streamexec.SourceGrant{Alias: snapshot.Alias, SourceID: snapshot.SourceID, ConfigRevision: snapshot.ConfigRevision, ReadResources: snapshot.ReadResources})
	}
	return grants
}

func (s *Service) deployedProgramHost(ctx context.Context, streamID, versionID int64) ([]streamexec.SourceGrant, streamexec.SourceHost, error) {
	r := repo.NewSourceStreamRepo(ctx)
	stream, err := r.Stream(ctx, streamID)
	if err != nil {
		return nil, nil, err
	}
	if stream.SourceID != 0 || stream.Status != "enabled" {
		return nil, nil, errInactivePlan
	}
	version, err := r.Version(ctx, streamID, versionID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := r.ProgramVersionSources(ctx, version); err != nil {
		return nil, nil, errInactivePlan
	}
	snapshots, err := version.Sources()
	if err != nil {
		return nil, nil, err
	}
	var def StreamDefinition
	if json.Unmarshal([]byte(version.Document), &def) != nil || def.Program == nil || def.ValidateDeployment(0, SourceDefinition{}) != nil || len(def.Program.Sources) != len(snapshots) {
		return nil, nil, repo.ErrInvalidArgument
	}
	for i, dependency := range def.Program.Sources {
		frozen := snapshots[i]
		if dependency.Alias != frozen.Alias || dependency.SourceID != frozen.SourceID || len(dependency.ReadResources) != len(frozen.ReadResources) {
			return nil, nil, repo.ErrInvalidArgument
		}
		for j, ref := range dependency.ReadResources {
			if ref != frozen.ReadResources[j] {
				return nil, nil, repo.ErrInvalidArgument
			}
		}
	}
	grants := programGrants(snapshots)
	reader, err := s.sourceReader(grants)
	if err != nil {
		return nil, nil, err
	}
	host := func(callCtx context.Context, call streamexec.SourceCall) (json.RawMessage, error) {
		current, err := repo.NewSourceStreamRepo(callCtx).Stream(callCtx, streamID)
		if err != nil || current.Status != "enabled" {
			return nil, errInactivePlan
		}
		return reader(callCtx, call)
	}
	return grants, host, nil
}

func (s *Service) transformProgram(ctx context.Context, script ScriptDefinition, input any, metadata map[string]any, grants []streamexec.SourceGrant, host streamexec.SourceHost) (ScriptResult, error) {
	if executor, ok := s.executor.(controlledExecutor); ok {
		return executor.TransformWithSources(ctx, script, input, metadata, grants, host)
	}
	if len(grants) > 0 || s.executor == nil {
		return ScriptResult{}, ErrUnavailable
	}
	return s.executor.Transform(ctx, script, input, metadata)
}

// Dry-runs use only explicitly provided samples. They never read a connection,
// write a Topic, or silently claim supplied JSON is live device telemetry.
func (s *Service) testProgram(ctx context.Context, def StreamDefinition, payload any, phase string) (map[string]any, error) {
	snapshots, err := s.validateProgram(ctx, def, false)
	if err != nil {
		return nil, err
	}
	script := def.Script
	if phase == "response" {
		if def.ResponseScript == nil || def.Model != RequestReply {
			return nil, invalid("phase", "unsupported")
		}
		script = *def.ResponseScript
	} else if phase != "input" && phase != "request" {
		return nil, invalid("phase", "unsupported")
	}
	samples := map[string]map[string]json.RawMessage{}
	data := payload
	metadata := map[string]any{"dryRun": true, "model": def.Model, "trigger": def.Program.Trigger.Kind}
	if def.Program.Trigger.Kind == IntervalTrigger {
		metadata["resourceRef"] = "interval"
	}
	if envelope, ok := payload.(map[string]any); ok {
		if rawSamples, present := envelope["sourceSamples"]; present {
			raw, err := json.Marshal(rawSamples)
			if err != nil || json.Unmarshal(raw, &samples) != nil || samples == nil {
				return nil, invalid("input.sourceSamples", "invalidJSON")
			}
			var present bool
			data, present = envelope["data"]
			if !present {
				return nil, invalid("input.data", "required")
			}
			if ref, present := envelope["resourceRef"]; present {
				resourceRef, valid := ref.(string)
				if !valid || def.Program.Trigger.Kind != SourceTrigger {
					return nil, invalid("input.resourceRef", "invalid")
				}
				for _, dependency := range def.Program.Sources {
					if dependency.Alias == def.Program.Trigger.SourceAlias {
						if !def.Program.MatchesInput(dependency.SourceID, resourceRef) {
							return nil, invalid("input.resourceRef", "mismatch")
						}
						metadata["sourceId"], metadata["resourceRef"] = dependency.SourceID, resourceRef
					}
				}
			}
		}
	}
	grants := programGrants(snapshots)
	host := func(_ context.Context, call streamexec.SourceCall) (json.RawMessage, error) {
		if !streamexec.AllowsSourceRead(grants, call) {
			return nil, streamexec.ErrSourceDenied
		}
		var options map[string]json.RawMessage
		if json.Unmarshal(call.Input, &options) != nil || options == nil || len(options) != 0 {
			return nil, streamexec.ErrSourceDenied
		}
		value, ok := samples[call.Alias][call.ResourceRef]
		if !ok || !json.Valid(value) || len(value) > streamexec.MaxJSONBytes {
			return nil, invalid("input.sourceSamples", "required")
		}
		return value, nil
	}
	result, err := s.transformProgram(ctx, script, data, metadata, grants, host)
	if err != nil {
		return nil, err
	}
	targets, err := def.OutputTargets(result)
	if err != nil {
		return nil, err
	}
	return map[string]any{"result": result, "targets": targets, "dryRun": true, "sideEffects": false, "sourceInputMode": "provided-samples"}, nil
}

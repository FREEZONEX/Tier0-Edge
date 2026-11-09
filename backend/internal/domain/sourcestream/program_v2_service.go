package sourcestream

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/streamexec"
)

type programV2Executor interface {
	ExecuteProgram(context.Context, streamexec.ProgramRequest, streamexec.ProgramHost) (streamexec.ProgramResult, error)
}

// 真实取数仍使用预览执行：只开放已授权 Source 的 GET/HEAD，不提交 UNS 输出。
type programFetchPreviewKey struct{}

// Registration runs in the managed executor without a host. Its manifest is
// the only dependency contract; arbitrary script strings are never scanned.
func (s *Service) compileProgramV2(ctx context.Context, def StreamDefinition, deployment bool) (streamexec.ProgramManifest, []repo.StreamSourceSnapshot, error) {
	empty := streamexec.ProgramManifest{}
	if !def.IsCompleteProgram() {
		return empty, nil, repo.ErrInvalidArgument
	}
	if err := def.ValidateDraft(); err != nil {
		return empty, nil, err
	}
	if deployment {
		if err := def.ValidateDeployment(0, SourceDefinition{}); err != nil {
			return empty, nil, err
		}
	}
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || !contextx.HasResource(subject, "uns.stream.manage") {
		return empty, nil, ErrForbidden
	}
	executor, ok := s.executor.(programV2Executor)
	if !ok {
		return empty, nil, ErrUnavailable
	}
	result, err := executor.ExecuteProgram(ctx, streamexec.ProgramRequest{SourceAliases: def.sourceAliases(), Code: def.Script.Code, TimeoutMs: def.Script.TimeoutMs, CheckOnly: true}, nil)
	if err != nil {
		return empty, nil, err
	}
	if result.Error != "" || len(result.Data) > 0 || len(result.Outputs) > 0 || len(result.Logs) > 0 {
		return empty, nil, streamexec.ErrOutput
	}
	encoded, err := json.Marshal(result.Manifest)
	if err != nil {
		return empty, nil, err
	}
	manifest, err := (repo.StreamVersion{ProgramManifest: string(encoded)}).ProgramV2Manifest()
	if err != nil {
		return empty, nil, err
	}
	declared := map[string]ProgramSourceBinding{}
	for _, binding := range def.programBindings() {
		declared[binding.SourceID] = binding
	}
	if def.ProgramSources != nil || def.ProgramSourceBindings != nil {
		if len(declared) != len(manifest.Sources) {
			return empty, nil, invalid("sources", "manifestMismatch")
		}
		for _, dependency := range manifest.Sources {
			binding, ok := declared[dependency.ID]
			if !ok {
				return empty, nil, invalid("sources", "undeclared")
			}
			if (def.ProgramSources != nil || binding.Resources != nil) && !reflect.DeepEqual(append([]string{}, binding.Resources...), dependency.Resources) {
				return empty, nil, invalid("sources.resources", "manifestMismatch")
			}
		}
	}
	if len(manifest.Sources) > 0 && (!contextx.HasResource(subject, "uns.source") || !contextx.HasResource(subject, "uns.read")) {
		return empty, nil, ErrForbidden
	}
	for _, reference := range manifest.Topics {
		if reference.Readable() && !contextx.HasResource(subject, "uns.read") || reference.Writable() && !contextx.HasResource(subject, "uns.write") {
			return empty, nil, ErrForbidden
		}
		if reference.Canonical() {
			continue
		}
		id, _ := strconv.ParseInt(reference.TopicID, 10, 64) // Manifest validation enforces canonical positive IDs.
		node, err := repo.NewUnsRepo(ctx).GetUnsNode(ctx, id, false)
		if err != nil {
			return empty, nil, err
		}
		if node.Type != 2 {
			return empty, nil, invalid("script.topics", "topicRequired")
		}
	}
	if _, err := repo.FreezeProgramV2Bindings(ctx, manifest); err != nil {
		return empty, nil, err
	}
	snapshots := make([]repo.StreamSourceSnapshot, 0, len(manifest.Sources))
	for _, dependency := range manifest.Sources {
		id, _ := strconv.ParseInt(dependency.ID, 10, 64)
		source, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
		if err != nil {
			return empty, nil, err
		}
		if binding, ok := declared[dependency.ID]; ok && source.ConfigRevision != binding.ConfigRevision {
			return empty, nil, repo.ErrConflict
		}
		var definition SourceDefinition
		var resources []NativeResource
		if source.ConfigRevision < 1 || json.Unmarshal([]byte(source.Document), &definition) != nil || definition.Validate() != nil ||
			source.Protocol != string(definition.Protocol) || source.DecodeProgramResources(&resources) != nil {
			return empty, nil, repo.ErrInvalidArgument
		}
		if deployment && (source.Health != "healthy") {
			return empty, nil, repo.ErrConflict
		}
		resources = programDeclaredResources(definition, source, resources)
		if _, err := programAcquiredSource(definition, dependency.Resources, declared[dependency.ID].Acquisition); err != nil {
			return empty, nil, err
		}
		for _, ref := range dependency.Resources {
			found := false
			for _, resource := range resources {
				if resource.ID == ref && len(resource.Models) > 0 {
					found = true
					break
				}
			}
			if !found {
				return empty, nil, invalid("script.sources.resources", "missingOrUnsupported")
			}
		}
		for _, entry := range manifest.Entries {
			if entry.Kind == "http-request" && entry.SourceID == dependency.ID {
				if definition.Protocol != HTTP || definition.ConnectionRole() != ServerRole || len(dependency.Resources) != 0 {
					return empty, nil, invalid("script.entries.source", "httpServerRequired")
				}
				if deployment && !httpServerEnabled() {
					return empty, nil, invalid("http.server", "notConfigured")
				}
			}
			if entry.Kind == "source-event" && entry.SourceID == dependency.ID && (definition.Protocol == HTTP || definition.Protocol == OAuth) {
				return empty, nil, invalid("script.entries.resources", "publishUnsupported")
			}
			if entry.Kind != "source-event" || entry.SourceID != dependency.ID {
				continue
			}
			plan := ExecutionPlan{ExecutionRef: ExecutionRef{SourceID: id, SourceRevision: source.ConfigRevision}, Definition: def, Manifest: &manifest, Entry: &entry}
			if plan.SourceAcquisitionKey() != "" {
				if _, err := plan.programWatchSource(definition); err != nil {
					return empty, nil, err
				}
			}
			for _, ref := range entry.ResourceRefs {
				found := false
				for _, resource := range resources {
					if resource.ID == ref {
						for _, model := range resource.Models {
							found = found || model == Publish
						}
					}
				}
				if !found {
					return empty, nil, invalid("script.entries.resources", "publishUnsupported")
				}
			}
		}
		snapshots = append(snapshots, repo.StreamSourceSnapshot{Alias: dependency.ID, SourceID: id, ConfigRevision: source.ConfigRevision,
			Resources: append([]string{}, dependency.Resources...)})
	}
	return manifest, snapshots, nil
}

func (s *Service) saveProgramV2(ctx context.Context, id, revision int64, def StreamDefinition, raw string) (map[string]any, error) {
	manifest, _, err := s.compileProgramV2(ctx, def, false)
	if err != nil {
		return nil, err
	}
	item, err := repo.NewSourceStreamRepo(ctx).SaveProgramV2(ctx, repo.Stream{ID: id, SourceID: 0, Name: def.Name, Model: string(ProgramAccess), Document: raw}, revision, manifest)
	if err != nil {
		return nil, err
	}
	return s.executionView(ctx, item)
}

func (s *Service) deployProgramV2(ctx context.Context, item repo.Stream, revision, versionID int64, def StreamDefinition, raw string) (map[string]any, error) {
	manifest, snapshots, err := s.compileProgramV2(ctx, def, true)
	if err != nil {
		return nil, err
	}
	if versionID == 0 {
		manifest, err = streamexec.WithImplicitRequests(manifest, item.ID, def.Script.TimeoutMs)
	} else {
		var version repo.StreamVersion
		version, err = repo.NewSourceStreamRepo(ctx).Version(ctx, item.ID, versionID)
		if err == nil {
			var frozen streamexec.ProgramManifest
			frozen, err = version.ProgramV2Manifest()
			if err == nil {
				manifest, err = streamexec.RestoreImplicitRequests(manifest, frozen, def.Script.TimeoutMs)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	watching, asynchronous := false, false
	for _, entry := range manifest.Entries {
		watching = watching || entry.Kind == "source-event"
		asynchronous = asynchronous || entry.Kind != "request" && entry.Kind != "http-request"
	}
	if asynchronous && (s.asyncReady == nil || !s.asyncReady()) {
		return nil, ErrUnavailable
	}
	r := repo.NewSourceStreamRepo(ctx)
	bindings, err := repo.FreezeProgramV2Bindings(ctx, manifest)
	if err != nil {
		return nil, err
	}
	staged, version, err := r.StageProgramV2WithRequestPolicies(ctx, item.ID, revision, versionID, raw, manifest, snapshots, bindings, programV2RequestPolicies)
	if err != nil {
		return nil, err
	}
	if !watching {
		staged, err = r.ActivateStagedProgramV2(ctx, staged.ID, staged.Revision, version.ID)
		if err != nil {
			return nil, err
		}
		for _, entry := range manifest.Entries {
			if entry.Kind == "http-request" {
				if err := s.ReconcileHTTPServers(ctx); err != nil {
					return nil, err
				}
				break
			}
		}
	}
	view, err := s.executionView(ctx, staged)
	if err != nil {
		return nil, err
	}
	view["pendingVersion"] = staged.PendingVersion
	if watching {
		// Only the worker may activate this candidate after all protocol watches
		// report real readiness. An existing active version is left untouched.
		view["deploymentState"] = "pending"
		view["pendingManifest"] = manifest
	} else {
		view["deploymentState"] = "active"
	}
	return view, nil
}

func (s *Service) testProgramV2(ctx context.Context, def StreamDefinition, payload any, entryID string) (map[string]any, error) {
	fetch, _ := ctx.Value(programFetchPreviewKey{}).(bool)
	if fetch {
		subject, ok := contextx.SubjectFrom(ctx)
		if !ok || !contextx.HasResource(subject, "uns.stream.execute") {
			return nil, ErrForbidden
		}
	}
	manifest, snapshots, err := s.compileProgramV2(ctx, def, false)
	if err != nil {
		return nil, err
	}
	if entryID == "" {
		if len(manifest.Entries) != 1 {
			return nil, invalid("entryId", "required")
		}
		entryID = manifest.Entries[0].ID
	}
	var selected streamexec.ProgramEntry
	for _, entry := range manifest.Entries {
		if entry.ID == entryID {
			selected = entry
			break
		}
	}
	if selected.ID == "" {
		return nil, invalid("entryId", "unknown")
	}
	encoded, err := json.Marshal(payload)
	var envelope map[string]json.RawMessage
	if err != nil || len(encoded) > streamexec.MaxJSONBytes || json.Unmarshal(encoded, &envelope) != nil || envelope == nil {
		return nil, invalid("input", "invalidJSON")
	}
	for field := range envelope {
		if field != "data" && field != "sourceSamples" && field != "topicSamples" && field != "checkpointSample" && field != "previewWrites" && field != "event" {
			return nil, invalid("input", "unsupportedField")
		}
	}
	previewContext, err := programV2PreviewEvent(selected, envelope["event"])
	if err != nil {
		return nil, err
	}
	compareWrites := false
	if raw, present := envelope["previewWrites"]; present {
		if string(raw) != "true" && string(raw) != "false" {
			return nil, invalid("input.previewWrites", "invalidJSON")
		}
		compareWrites = string(raw) == "true"
		if compareWrites {
			subject, ok := contextx.SubjectFrom(ctx)
			if !ok || !contextx.HasResource(subject, "uns.read") {
				return nil, ErrForbidden
			}
		}
	}
	data, exists := envelope["data"]
	if !exists {
		return nil, invalid("input.data", "required")
	}
	sourceSamples := map[string]map[string]json.RawMessage{}
	topicSamples := map[string]json.RawMessage{}
	if raw, present := envelope["sourceSamples"]; present && (json.Unmarshal(raw, &sourceSamples) != nil || sourceSamples == nil) {
		return nil, invalid("input.sourceSamples", "invalidJSON")
	}
	if raw, present := envelope["topicSamples"]; present && (json.Unmarshal(raw, &topicSamples) != nil || topicSamples == nil) {
		return nil, invalid("input.topicSamples", "invalidJSON")
	}
	ctx, guard := WithProgramSecretGuard(ctx)
	checkpointSample := envelope["checkpointSample"]
	if len(checkpointSample) > streamexec.MaxCheckpointBytes {
		return nil, invalid("input.checkpointSample", "invalidJSON")
	}
	ctx, checkpoint := withProgramCheckpoint(ctx, checkpointSample)
	host, err := s.ProgramV2Host(ctx, manifest, snapshots, "", entryID, nil, true, sourceSamples, topicSamples)
	if err != nil {
		return nil, err
	}
	inputMode := "provided-samples"
	if fetch {
		inputMode = "live-read"
	}
	previewContext["dryRun"], previewContext["entryId"], previewContext["sourceInputMode"] = true, entryID, inputMode
	metadata, _ := json.Marshal(previewContext)
	result, err := s.executor.(programV2Executor).ExecuteProgram(ctx, streamexec.ProgramRequest{SourceAliases: def.sourceAliases(), Code: def.Script.Code, TimeoutMs: def.Script.TimeoutMs,
		EntryID: entryID, Input: data, Context: metadata, ExpectedManifest: &manifest}, host)
	if err != nil {
		return nil, err
	}
	result.Checkpoint = checkpoint.result()
	guard.RedactResult(&result)
	if result.Error != "" || !reflect.DeepEqual(result.Manifest, manifest) {
		return nil, streamexec.ErrOutput
	}
	if err := manifest.ValidateRequestEffects(entryID, result); err != nil {
		return nil, err
	}
	if err := validateProgramV2Outputs(manifest, result); err != nil {
		return nil, err
	}
	targets := make([]map[string]any, 0, len(result.Outputs))
	canonical := len(result.Outputs) > 0 && result.Outputs[0].UNSReference().Canonical()
	if canonical {
		targets, err = previewProgramMetadataOutputs(ctx, manifest, result.Outputs, compareWrites)
		if err != nil {
			return nil, err
		}
	} else {
		for _, output := range result.Outputs {
			targets = append(targets, map[string]any{"topicId": output.TopicID, "data": output.Data})
		}
	}
	return map[string]any{"result": result, "targets": targets, "manifest": manifest, "entryId": entryID, "dryRun": true,
		"sideEffects": false, "externalRequests": fetch, "sourceInputMode": inputMode}, nil
}

// 仅模拟入口已声明的事件身份，不能覆盖权限、真实执行状态或运行版本。
// 单个精确资源可推导；通配范围和多个资源必须提供实际资源引用。
func programV2PreviewEvent(entry streamexec.ProgramEntry, raw json.RawMessage) (map[string]any, error) {
	metadata := map[string]any{}
	if entry.Kind != "source-event" {
		if len(raw) != 0 {
			return nil, invalid("input.event", "unsupportedField")
		}
		return metadata, nil
	}
	ref := ""
	if len(raw) != 0 {
		var event map[string]json.RawMessage
		if json.Unmarshal(raw, &event) != nil || len(event) != 1 || json.Unmarshal(event["resourceRef"], &ref) != nil || ref == "" {
			return nil, invalid("input.event", "invalidJSON")
		}
	} else if len(entry.ResourceRefs) == 1 {
		ref = entry.ResourceRefs[0]
	}
	if ref == "" || strings.HasPrefix(ref, "mqtt:") && !validMQTTTopic(strings.TrimPrefix(ref, "mqtt:"), false) {
		return nil, invalid("input.event.resourceRef", "required")
	}
	sourceID, err := strconv.ParseInt(entry.SourceID, 10, 64)
	if err != nil || sourceID <= 0 || !(ExecutionPlan{ExecutionRef: ExecutionRef{SourceID: sourceID}, Entry: &entry}).MatchesSourceInput(sourceID, ref) {
		return nil, invalid("input.event.resourceRef", "missingOrUnsupported")
	}
	metadata["sourceId"], metadata["resourceRef"], metadata["receivedAt"] = entry.SourceID, ref, time.Now().UnixMilli()
	return metadata, nil
}

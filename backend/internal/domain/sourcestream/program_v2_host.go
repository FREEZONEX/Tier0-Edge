package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"github.com/zeromicro/go-zero/core/logx"
)

// ProgramV2Host is a private domain broker, not an API accepting arbitrary
// manifests. Live authority comes from an admitted run's immutable version;
// interactive previews use declared samples and the caller's existing rights.
func (s *Service) ProgramV2Host(ctx context.Context, manifest streamexec.ProgramManifest, snapshots []repo.StreamSourceSnapshot, runID, entryID string, reader func(context.Context, int64) (json.RawMessage, error), dryRun bool, sourceSamples map[string]map[string]json.RawMessage, topicSamples map[string]json.RawMessage) (streamexec.ProgramHost, error) {
	fetch, _ := ctx.Value(programFetchPreviewKey{}).(bool)
	if fetch {
		subject, ok := contextx.SubjectFrom(ctx)
		if !dryRun || !ok || !contextx.HasResource(subject, "uns.stream.execute") {
			return nil, ErrForbidden
		}
	}
	if subject, ok := contextx.SubjectFrom(ctx); dryRun && ok {
		if !contextx.HasResource(subject, "uns.stream.manage") || len(manifest.Sources) > 0 && (!contextx.HasResource(subject, "uns.source") || !contextx.HasResource(subject, "uns.read")) {
			return nil, ErrForbidden
		}
	} else if dryRun {
		return nil, ErrForbidden
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return nil, repo.ErrInvalidArgument
	}
	manifest, err = (repo.StreamVersion{ProgramManifest: string(raw)}).ProgramV2Manifest()
	if err != nil {
		return nil, err
	}
	bindings := []repo.ProgramUNSBinding{}
	var executionVersion repo.StreamVersion
	if dryRun {
		bindings, err = repo.FreezeProgramV2Bindings(ctx, manifest)
	} else {
		run, runErr := repo.NewSourceStreamRepo(ctx).Run(ctx, runID)
		if runErr != nil {
			return nil, errInactivePlan
		}
		version, versionErr := repo.NewSourceStreamRepo(ctx).Version(ctx, run.StreamID, run.VersionID)
		if versionErr != nil {
			return nil, errInactivePlan
		}
		bindings, err = version.ProgramV2Bindings()
		executionVersion = version
	}
	if err != nil {
		return nil, err
	}
	// Copy every mutable authority/sample value before exposing the closure.
	var frozen []repo.StreamSourceSnapshot
	if raw, err = json.Marshal(snapshots); err != nil || json.Unmarshal(raw, &frozen) != nil || frozen == nil || len(frozen) != len(manifest.Sources) {
		return nil, repo.ErrInvalidArgument
	}
	byID := map[string]repo.StreamSourceSnapshot{}
	for i, source := range manifest.Sources {
		item := frozen[i]
		if strconv.FormatInt(item.SourceID, 10) != source.ID || item.Alias != source.ID || item.ConfigRevision <= 0 || !reflect.DeepEqual(item.Resources, source.Resources) {
			return nil, repo.ErrInvalidArgument
		}
		byID[source.ID] = item
	}
	var copiedSources map[string]map[string]json.RawMessage
	if raw, err = json.Marshal(sourceSamples); err != nil || json.Unmarshal(raw, &copiedSources) != nil {
		return nil, repo.ErrInvalidArgument
	}
	var copiedTopics map[string]json.RawMessage
	if raw, err = json.Marshal(topicSamples); err != nil || json.Unmarshal(raw, &copiedTopics) != nil {
		return nil, repo.ErrInvalidArgument
	}
	checkRun := func(callCtx context.Context) error {
		if err := checkControlExecution(callCtx); err != nil {
			return err
		}
		if dryRun {
			return nil
		}
		r := repo.NewSourceStreamRepo(callCtx)
		run, err := r.Run(callCtx, runID)
		if err != nil || run.Model != "program" || run.Status != "processing" || run.EntryID != entryID {
			return errInactivePlan
		}
		stream, err := r.Stream(callCtx, run.StreamID)
		if err != nil || stream.Model != "program" || stream.Status != "enabled" {
			return errInactivePlan
		}
		version, err := r.Version(callCtx, run.StreamID, run.VersionID)
		if err != nil || version.ActivatedAt <= 0 {
			return errInactivePlan
		}
		stored, err := version.ProgramV2Manifest()
		refs, refsErr := version.Sources()
		if err != nil || refsErr != nil || !reflect.DeepEqual(stored, manifest) || !reflect.DeepEqual(refs, frozen) {
			return errInactivePlan
		}
		storedBindings, err := version.ProgramV2Bindings()
		if err != nil || !reflect.DeepEqual(storedBindings, bindings) {
			return errInactivePlan
		}
		return repo.ValidateProgramV2Bindings(callCtx, manifest, bindings)
	}
	if err := checkRun(ctx); err != nil {
		return nil, err
	}
	checkpoint, _ := ctx.Value(programCheckpointKey{}).(*programCheckpointBuffer)
	if checkpoint == nil {
		checkpoint = &programCheckpointBuffer{}
	}
	return func(callCtx context.Context, call streamexec.ProgramCall) (data json.RawMessage, callErr error) {
		started := time.Now()
		defer func() {
			if callErr == nil {
				return
			}
			sourceID := "0"
			if id, err := strconv.ParseInt(call.SourceID, 10, 64); err == nil && id > 0 && strconv.FormatInt(id, 10) == call.SourceID {
				sourceID = call.SourceID
			}
			kind := "invalid"
			switch call.Kind {
			case "source.read", "source.write", "source.invoke", "source.publish", "source.parameters", "source.secrets", "source.request", "uns.read", "uns.query", "checkpoint.get", "checkpoint.set":
				kind = call.Kind
			}
			// Never interpolate driver errors, resource addresses, payloads or
			// credentials. Protocol runtime logs carry their own lease generation.
			logx.WithContext(callCtx).Errorf("Stream SDK call failed: sourceId=%s callId=%d kind=%s code=%s durationMs=%d sourceConfigRevision=%d",
				sourceID, call.ID, kind, programV2CallCode(callErr), time.Since(started).Milliseconds(), byID[sourceID].ConfigRevision)
		}()
		if subject, ok := contextx.SubjectFrom(ctx); ok {
			if (call.Kind == "uns.read" || call.Kind == "uns.query") && !contextx.HasResource(subject, "uns.read") || !dryRun && call.Kind != "source.read" && call.Kind != "uns.read" && call.Kind != "uns.query" && !contextx.HasResource(subject, "uns.stream.execute") {
				return nil, ErrForbidden
			}
		}
		if !streamexec.AllowsProgramCall(manifest, call) {
			return nil, streamexec.ErrSourceDenied
		}
		if !dryRun {
			if err := s.checkAppRequestCall(callCtx, executionVersion, entryID, call); err != nil {
				return nil, err
			}
		}
		if manifest.ReadOnlyRequest(entryID) && !streamexec.ReadOnlyProgramCall(call) {
			return nil, streamexec.ErrSourceDenied
		}
		// A native vendor read is authorized again against its code-owned operation
		// below. Scripts cannot turn an arbitrary HTTP POST into a preview read.
		vendorFetch := fetch && call.Kind == "source.read" && strings.HasPrefix(call.ResourceRef, "vendor:")
		nativeFetch := fetch && call.Kind == "source.read" && strings.HasPrefix(call.ResourceRef, "sql:")
		if fetch && call.Kind != "source.request" && !vendorFetch && !nativeFetch {
			return nil, streamexec.ErrSourceDenied
		}
		timeout := streamexec.MaxProgramSourceTimeoutMs
		var err error
		canonicalRead := (call.Kind == "uns.read" || call.Kind == "uns.query") && call.UNSReference().Canonical()
		if call.Kind != "source.request" && !canonicalRead && !strings.HasPrefix(call.ResourceRef, "vendor:") && !programNativePageRef(call.ResourceRef) {
			timeout, err = programV2Timeout(call.Options)
		}
		if err != nil {
			return nil, err
		}
		operationCtx, cancel := context.WithTimeout(callCtx, time.Duration(timeout)*time.Millisecond)
		defer cancel()
		if err := checkRun(operationCtx); err != nil {
			return nil, err
		}
		if call.Kind == "checkpoint.get" || call.Kind == "checkpoint.set" {
			return checkpoint.call(operationCtx, call, runID, entryID, dryRun)
		}
		if call.Kind == "uns.query" {
			if !canonicalRead {
				return nil, streamexec.ErrSourceDenied
			}
			for _, binding := range bindings {
				if binding.Reference.Key() == call.UNSReference().Key() && binding.Reference.Readable() {
					return queryProgramMetadata(operationCtx, call, binding, dryRun, copiedTopics[binding.Reference.Key()])
				}
			}
			return nil, streamexec.ErrSourceDenied
		}
		if call.Kind == "uns.read" {
			if dryRun {
				return programV2Sample(copiedTopics[call.UNSReference().Key()])
			}
			if canonicalRead {
				binding := repo.ProgramUNSBinding{}
				for _, item := range bindings {
					if item.Reference.Key() == call.UNSReference().Key() {
						binding = item
						break
					}
				}
				if binding.Reference.Key() == "" {
					return nil, streamexec.ErrSourceDenied
				}
				return readProgramMetadata(operationCtx, call, binding.TopicBindingHash)
			}
			if reader == nil {
				return nil, ErrUnavailable
			}
			id, _ := strconv.ParseInt(call.TopicID, 10, 64)
			data, err := reader(operationCtx, id)
			if err != nil {
				return nil, streamexec.ErrSourceCall
			}
			return programV2Sample(data)
		}
		frozen := byID[call.SourceID]
		r := repo.NewSourceStreamRepo(operationCtx)
		source, err := r.Source(operationCtx, frozen.SourceID)
		if err != nil || source.ConfigRevision != frozen.ConfigRevision {
			return nil, repo.ErrConflict
		}
		if !dryRun {
			if err := executionVersion.ValidateProgramExecutionSource(source); err != nil {
				return nil, err
			}
		}
		var def SourceDefinition
		var resources []NativeResource
		if json.Unmarshal([]byte(source.Document), &def) != nil || def.Validate() != nil || source.Protocol != string(def.Protocol) || source.DecodeProgramResources(&resources) != nil {
			return nil, streamexec.ErrSourceDenied
		}
		if supportsExtendedPage(def) && programNativePageRef(call.ResourceRef) || programBusinessPageRef(def, call.ResourceRef) {
			declared := false
			for _, resource := range declaredResources(def, source) {
				declared = declared || resource.ResourceRef == call.ResourceRef
			}
			if !declared || call.Kind != "source.read" {
				return nil, streamexec.ErrSourceDenied
			}
			if dryRun && !nativeFetch && !vendorFetch {
				return programV2Sample(copiedSources[call.SourceID][call.ResourceRef])
			}
			caller, hasCaller := contextx.SubjectFrom(ctx)
			if hasCaller && !canReadNativePage(caller) {
				return nil, ErrForbidden
			}
			if potentiallyWritingSQL(def) {
				if dryRun || manifest.ReadOnlyRequest(entryID) {
					return nil, streamexec.ErrSourceDenied
				}
				return s.programV2SQLCall(operationCtx, source, def, call, runID, entryID)
			}
			return s.programNativePage(operationCtx, source, def, call, !dryRun && !hasCaller)
		}
		resources = programDeclaredResources(def, source, resources)
		if vendorFetch && def.SourceType == "" {
			return nil, streamexec.ErrSourceDenied
		}
		if call.Kind == "source.parameters" || call.Kind == "source.secrets" || call.Kind == "source.request" {
			if def.HTTP == nil || (def.Protocol != HTTP && def.Protocol != OAuth) {
				return nil, streamexec.ErrSourceDenied
			}
			if call.Kind == "source.request" {
				options, err := parseHTTPRequest(call.Options)
				if err != nil || def.ConnectionRole() != ClientRole {
					return nil, streamexec.ErrSourceDenied
				}
				if _, err := httpRequestURL(def.Endpoint, options); err != nil {
					return nil, streamexec.ErrSourceDenied
				}
			}
			if dryRun {
				// 显式 fetch 仅开放 GET/HEAD；普通样本预览不打开凭据、不请求外部系统。
				if fetch {
					options, err := parseHTTPRequest(call.Options)
					if err != nil || (options.Method != "GET" && options.Method != "HEAD") {
						return nil, streamexec.ErrSourceDenied
					}
					return s.HTTPProgramCall(operationCtx, source, def, call)
				}
				return programV2Sample(copiedSources[call.SourceID][strings.TrimPrefix(call.Kind, "source.")])
			}
			return s.programV2HTTPCall(operationCtx, source, def, call, runID, entryID)
		}
		var selected *NativeResource
		for i := range resources {
			if resources[i].ID == call.ResourceRef {
				selected = &resources[i]
				break
			}
		}
		if def.SourceType != "" {
			if selected == nil {
				return nil, streamexec.ErrSourceDenied
			}
			if _, err := vendorNativeRequest(def, call); err != nil {
				return nil, err
			}
			if dryRun {
				if call.Kind == "source.read" {
					if vendorFetch {
						// vendorNativeRequest only admits registered read-only operations,
						// including query POSTs; credentials and session stay Source-owned.
						return s.vendorNativeCall(operationCtx, source, def, call, "", entryID)
					}
					return programV2Sample(copiedSources[call.SourceID][call.ResourceRef])
				}
				return json.Marshal(map[string]any{"status": "planned", "dryRun": true, "dispatched": false, "sourceId": call.SourceID, "resourceRef": call.ResourceRef})
			}
			return s.vendorNativeCall(operationCtx, source, def, call, runID, entryID)
		}
		def, err = programV2ResourceDefinition(def, selected)
		if err != nil {
			return nil, err
		}
		input, err := programV2Operation(def, selected, call)
		if err != nil {
			return nil, err
		}
		if dryRun {
			if call.Kind == "source.read" {
				return programV2Sample(copiedSources[call.SourceID][call.ResourceRef])
			}
			return json.Marshal(map[string]any{"status": "planned", "dryRun": true, "dispatched": false, "deviceExecuted": false, "sourceId": call.SourceID, "resourceRef": call.ResourceRef, "operation": call.Kind})
		}
		if s.runtime == nil {
			return nil, ErrUnavailable
		}
		_, credential, _, err := s.connection(operationCtx, source.ID, nil)
		if err != nil {
			return nil, ErrCredential
		}
		current, err := r.Source(operationCtx, source.ID)
		if err != nil || current.ConfigRevision != frozen.ConfigRevision || current.Resources != source.Resources {
			return nil, repo.ErrConflict
		}
		dispatch := func() (json.RawMessage, error) {
			if err := checkControlExecution(operationCtx); err != nil {
				return nil, err
			}
			value, err := s.runtime.Invoke(operationCtx, source.ID, def, credential, call.ResourceRef, input)
			if err != nil {
				return nil, streamexec.ErrSourceCall
			}
			raw, err := json.Marshal(value)
			limit := streamexec.MaxJSONBytes // Device effect receipts retain their journal budget.
			if call.Kind == "source.read" {
				limit = streamexec.MaxProgramDataBytes
			}
			if err != nil || len(raw) > limit {
				return nil, streamexec.ErrSourceCall
			}
			return raw, nil
		}
		if call.Kind == "source.read" {
			return dispatch()
		}
		// Include frozen configuration in the hash, never credential material.
		connectionConfig := def
		connectionConfig.Name, connectionConfig.Description = "", ""
		request, _ := json.Marshal(struct {
			Call           streamexec.ProgramCall
			ConfigRevision int64
			Definition     SourceDefinition
		}{call, frozen.ConfigRevision, connectionConfig})
		hash := sha256.Sum256(request)
		opHash := sha256.Sum256([]byte(call.SourceID + "\x00" + call.ResourceRef + "\x00" + call.Kind))
		effect := repo.StreamProgramEffect{RunID: runID, EntryID: entryID, CallID: strconv.Itoa(call.ID), OpID: call.Kind + ":" + hex.EncodeToString(opHash[:]), RequestHash: hex.EncodeToString(hash[:])}
		stored, err := r.ReserveProgramEffect(operationCtx, effect)
		if err != nil {
			return nil, programV2EffectError(err)
		}
		if stored.Status == "completed" {
			return programV2Sample(json.RawMessage(stored.Result))
		}
		stored, claimed, err := r.ClaimProgramEffect(operationCtx, runID, entryID, effect.CallID, effect.RequestHash)
		if err != nil {
			return nil, programV2EffectError(err)
		}
		if !claimed {
			if stored.Status == "completed" {
				return programV2Sample(json.RawMessage(stored.Result))
			}
			return nil, ErrUnknownResult
		}
		data, dispatchErr := dispatch()
		status, result := "completed", string(data)
		if dispatchErr != nil {
			status, result = "unknown", `{"status":"unknown","deviceExecuted":false}`
		}
		// Persist even after a caller deadline. Failure leaves dispatching intact
		// and must never cause a second device operation on replay.
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(callCtx), time.Second)
		defer finishCancel()
		if err := repo.NewSourceStreamRepo(finishCtx).FinishProgramEffect(finishCtx, runID, entryID, effect.CallID, effect.RequestHash, status, result); err != nil || dispatchErr != nil {
			return nil, ErrUnknownResult
		}
		return data, nil
	}, nil
}

func programV2CallCode(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, streamexec.ErrTimeout):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, errInactivePlan):
		return "inactive-reference"
	case errors.Is(err, ErrForbidden), errors.Is(err, streamexec.ErrSourceDenied):
		return "denied"
	case errors.Is(err, ErrUnknownResult):
		return "result-unknown"
	case errors.Is(err, ErrCredential):
		return "credential-unavailable"
	case errors.Is(err, repo.ErrConflict):
		return "configuration-conflict"
	case errors.Is(err, repo.ErrInvalidArgument):
		return "invalid-request"
	case errors.Is(err, ErrUnavailable):
		return "unavailable"
	default:
		return "source-call-failed"
	}
}

// Never send driver/database errors containing endpoints or credentials to JS.
func programV2EffectError(err error) error {
	if errors.Is(err, repo.ErrConflict) {
		return repo.ErrConflict
	}
	if errors.Is(err, repo.ErrInvalidArgument) {
		return repo.ErrInvalidArgument
	}
	return ErrUnavailable
}

func programV2Sample(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > streamexec.MaxProgramDataBytes || !json.Valid(raw) {
		return nil, streamexec.ErrSourceCall
	}
	return append(json.RawMessage(nil), raw...), nil
}

// HTTP effects record only replay identity and a receipt. Header/body/response
// values may contain authorized secrets and must never enter the effect journal.
func (s *Service) programV2HTTPCall(ctx context.Context, source repo.Source, def SourceDefinition, call streamexec.ProgramCall, runID, entryID string) (json.RawMessage, error) {
	if call.Kind != "source.request" {
		return s.HTTPProgramCall(ctx, source, def, call)
	}
	options, err := parseHTTPRequest(call.Options)
	if err != nil {
		return nil, err
	}
	if options.Method == "GET" || options.Method == "HEAD" || options.Method == "OPTIONS" {
		return s.HTTPProgramCall(ctx, source, def, call)
	}
	configuration := def
	configuration.Name, configuration.Description = "", ""
	raw, err := json.Marshal(struct {
		Call           streamexec.ProgramCall
		ConfigRevision int64
		Definition     SourceDefinition
	}{call, source.ConfigRevision, configuration})
	if err != nil {
		return nil, streamexec.ErrSourceDenied
	}
	hash := sha256.Sum256(raw)
	opHash := sha256.Sum256([]byte(call.SourceID + "\x00" + options.Method + "\x00" + options.Path))
	effect := repo.StreamProgramEffect{RunID: runID, EntryID: entryID, CallID: strconv.Itoa(call.ID), OpID: "source.request:" + hex.EncodeToString(opHash[:]), RequestHash: hex.EncodeToString(hash[:])}
	r := repo.NewSourceStreamRepo(ctx)
	stored, err := r.ReserveProgramEffect(ctx, effect)
	if err != nil {
		return nil, programV2EffectError(err)
	}
	if stored.Status == "completed" {
		return programV2Sample(json.RawMessage(stored.Result))
	}
	stored, claimed, err := r.ClaimProgramEffect(ctx, runID, entryID, effect.CallID, effect.RequestHash)
	if err != nil {
		return nil, programV2EffectError(err)
	}
	if !claimed {
		if stored.Status == "completed" {
			return programV2Sample(json.RawMessage(stored.Result))
		}
		return nil, ErrUnknownResult
	}
	if err := checkControlExecution(ctx); err != nil {
		return nil, err
	}
	data, dispatchErr := s.HTTPProgramCall(ctx, source, def, call)
	status, receipt := "unknown", `{"status":"unknown","responseAvailable":false}`
	if dispatchErr == nil {
		var response HTTPResponse
		if json.Unmarshal(data, &response) != nil {
			return nil, ErrUnknownResult
		}
		// No response hash: even a digest can reveal low-entropy secret data.
		safe, _ := json.Marshal(map[string]any{"status": response.Status, "headers": map[string]any{}, "data": nil, "replayed": true, "responseAvailable": false})
		status, receipt = "completed", string(safe)
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if err := repo.NewSourceStreamRepo(finishCtx).FinishProgramEffect(finishCtx, runID, entryID, effect.CallID, effect.RequestHash, status, receipt); err != nil || dispatchErr != nil {
		return nil, ErrUnknownResult
	}
	return data, nil
}

func programV2Timeout(raw json.RawMessage) (int, error) {
	if len(raw) == 0 {
		return int(streamexec.DefaultSourceCallTimeout / time.Millisecond), nil
	}
	var options map[string]json.RawMessage
	if json.Unmarshal(raw, &options) != nil || options == nil || len(options) > 1 {
		return 0, streamexec.ErrSourceDenied
	}
	timeout := int(streamexec.DefaultSourceCallTimeout / time.Millisecond)
	for key, value := range options {
		if key != "timeoutMs" || json.Unmarshal(value, &timeout) != nil || timeout < 1 || timeout > streamexec.MaxProgramSourceTimeoutMs {
			return 0, streamexec.ErrSourceDenied
		}
	}
	return timeout, nil
}

func programV2Operation(def SourceDefinition, resource *NativeResource, call streamexec.ProgramCall) (any, error) {
	if resource == nil || call.ResourceRef != resource.ID || !slices.Contains(programV2NativeActions(def, resource), call.Kind) {
		return nil, streamexec.ErrSourceDenied
	}
	var payload map[string]json.RawMessage
	decodeObject := func(keys ...string) bool {
		if json.Unmarshal(call.Input, &payload) != nil || payload == nil || len(payload) != len(keys) {
			return false
		}
		for _, key := range keys {
			if _, ok := payload[key]; !ok {
				return false
			}
		}
		return true
	}
	switch def.Protocol {
	case MQTT:
		if call.Kind == "source.publish" {
			var value any
			if json.Unmarshal(call.Input, &value) == nil {
				return value, nil
			}
		}
		if call.Kind == "source.invoke" {
			var value map[string]any
			if json.Unmarshal(call.Input, &value) == nil && value != nil {
				return value, nil
			}
		}
	case OPCUA:
		if call.Kind == "source.read" {
			return map[string]any{"operation": "read"}, nil
		}
		if call.Kind == "source.write" && decodeObject("value") {
			return map[string]any{"operation": "write", "value": payload["value"]}, nil
		}
		if call.Kind == "source.invoke" && decodeObject("arguments") {
			var args []json.RawMessage
			if json.Unmarshal(payload["arguments"], &args) == nil && args != nil && len(args) <= 64 {
				return map[string]any{"operation": "call", "arguments": args}, nil
			}
		}
	case ModbusTCP:
		if call.Kind == "source.read" {
			return map[string]any{"operation": "read"}, nil
		}
		if call.Kind == "source.write" && decodeObject("values") {
			for _, selected := range def.Modbus.Ranges {
				if call.ResourceRef != "modbus:"+selected.ID {
					continue
				}
				if selected.Table == "holding-register" {
					var values []uint16
					if json.Unmarshal(payload["values"], &values) == nil && len(values) == selected.Quantity {
						return map[string]any{"operation": "write", "values": values}, nil
					}
				}
				if selected.Table == "coil" {
					var values []bool
					if json.Unmarshal(payload["values"], &values) == nil && len(values) == selected.Quantity {
						return map[string]any{"operation": "write", "values": values}, nil
					}
				}
			}
		}
	}
	return nil, streamexec.ErrSourceDenied
}

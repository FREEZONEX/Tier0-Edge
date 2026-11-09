package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/dataingest"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/streamexec"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

type SourceInput struct {
	ResourceRef string `json:"resourceRef"`
	Data        any    `json:"data"`
	ReceivedAt  int64  `json:"receivedAt"`
}

type ExecutionRef struct {
	EntryID   string `json:"entryId,omitempty"`
	SourceID  int64  `json:"sourceId"`
	StreamID  int64  `json:"streamId"`
	VersionID int64  `json:"versionId"`
	// Pins the connection configuration, not the Source editing revision.
	SourceRevision int64 `json:"sourceRevision"`
}

type ExecutionPlan struct {
	ExecutionRef
	Definition StreamDefinition
	Manifest   *streamexec.ProgramManifest
	Entry      *streamexec.ProgramEntry
	Candidate  bool
}

type RawInput struct {
	MessageID       string         `json:"messageId"`
	SourceID        int64          `json:"sourceId"`
	Input           SourceInput    `json:"input"`
	Plans           []ExecutionRef `json:"plans"`
	Trigger         TriggerKind    `json:"trigger,omitempty"`
	TopicID         int64          `json:"topicId,omitempty"`
	MetadataTopicID string         `json:"metadataTopicId,omitempty"`
	CollectionID    string         `json:"collectionId,omitempty"`
	Lineage         []int64        `json:"lineage,omitempty"`
}

type StreamSink interface {
	CommitStreamEvent(context.Context, repo.UnsNode, dataingest.StreamEvent) (bool, error)
}

var errInactivePlan = errors.New("SourceStream.inactiveReference")

func (s *Service) Plans(ctx context.Context) ([]ExecutionPlan, error) {
	items, err := repo.NewSourceStreamRepo(ctx).ActiveExecutions(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]ExecutionPlan, 0, len(items))
	for _, item := range items {
		if item.Stream.Model != string(ProgramAccess) {
			return nil, repo.ErrInvalidArgument
		}
		plans, err := programV2Plans(item, false)
		if errors.Is(err, errInactivePlan) {
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, plans...)
	}
	return result, nil
}

func (s *Service) loadPlan(ctx context.Context, ref ExecutionRef) (StreamDefinition, error) {
	r := repo.NewSourceStreamRepo(ctx)
	stream, err := r.Stream(ctx, ref.StreamID)
	if err != nil {
		return StreamDefinition{}, err
	}
	if stream.Model != string(ProgramAccess) || stream.SourceID != 0 {
		return StreamDefinition{}, errInactivePlan
	}
	version, err := r.Version(ctx, ref.StreamID, ref.VersionID)
	if err != nil {
		return StreamDefinition{}, err
	}
	return s.loadProgramV2Plan(ctx, stream, version, ref)
}

func (s *Service) WatchMappedInputs(ctx context.Context, id, revision int64, plans []ExecutionPlan, emit func(SourceInput) error) error {
	if len(plans) == 0 {
		return invalid("collection", "required")
	}
	runtime, ok := s.runtime.(interface {
		Watch(context.Context, int64, RuntimeSource, Credential, func(SourceInput) error) error
	})
	if !ok {
		return ErrUnavailable
	}
	source, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
	if err != nil {
		return err
	}
	if source.ConfigRevision != revision {
		return repo.ErrConflict
	}
	def, credential, actual, err := s.connection(ctx, id, nil)
	if err != nil {
		return err
	}
	if actual != source.Revision {
		return repo.ErrConflict
	}
	var assembled RuntimeSource
	for i, plan := range plans {
		var next RuntimeSource
		var e error
		if plan.Entry != nil {
			next, e = plan.programWatchSource(def)
		} else {
			next, e = plan.Definition.EffectiveSource(def)
		}
		if e != nil {
			return e
		}
		if i == 0 {
			assembled = next
		} else if assembled.Protocol == MQTT {
			for filter, qos := range next.MQTT.Subscriptions {
				assembled.MQTT.Subscriptions[filter] = max(assembled.MQTT.Subscriptions[filter], qos)
			}
		}
	}
	observedCtx := WithWatchObserver(ctx, func(ready bool) {
		ReportWatchReady(ctx, ready)
		if ready {
			observeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			if err := repo.NewSourceStreamRepo(observeCtx).ObserveSourceWatch(observeCtx, id, revision); err != nil {
				ReportWatchReady(ctx, false)
				logx.WithContext(observeCtx).Error("SourceStream collection watch health observation failed")
			}
		}
	})
	err = runtime.Watch(observedCtx, id, assembled, credential, emit)
	if err != nil && ctx.Err() == nil {
		observeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = repo.NewSourceStreamRepo(observeCtx).ObserveSource(observeCtx, id, actual, "unhealthy", "SourceStream.protocolFailed", nil)
	}
	return err
}

func discardReference(ctx context.Context, messageID string, ref ExecutionRef, model AccessModel) error {
	id := taskIdentity(messageID, ref)
	if ref.EntryID != "" {
		id, model = repo.ProgramRunIdentity(messageID, ref.StreamID, ref.VersionID, ref.EntryID), ProgramAccess
	}
	return repo.NewSourceStreamRepo(ctx).DiscardQueuedExecution(ctx, repo.StreamRun{
		ID: id, EntryID: ref.EntryID, SourceID: ref.SourceID, StreamID: ref.StreamID,
		VersionID: ref.VersionID, Model: string(model), Status: "dropped", MessageID: messageID,
		Summary:   `{"phase":"reference-validation","reason":"inactiveReference","usesNATS":true,"operationRepeated":false}`,
		CreatedAt: time.Now().UnixMilli(),
	})
}

func (s *Service) Watch(ctx context.Context, id, revision int64, emit func(SourceInput) error) error {
	return s.watchInputs(ctx, id, revision, nil, 0, nil, emit)
}

func (s *Service) WatchInputs(ctx context.Context, id, revision int64, refs []string, qos int, emit func(SourceInput) error) error {
	if len(refs) == 0 || !uniqueResourceRefs(refs) || qos < 0 || qos > 2 {
		return repo.ErrInvalidArgument
	}
	return s.watchInputs(ctx, id, revision, refs, qos, nil, emit)
}

func (s *Service) watchInputs(ctx context.Context, id, revision int64, refs []string, qos int, collection *CollectionDefinition, emit func(SourceInput) error) error {
	ctx = context.WithValue(ctx, watchConfigRevisionKey{}, revision)
	runtime, ok := s.runtime.(interface {
		Watch(context.Context, int64, RuntimeSource, Credential, func(SourceInput) error) error
	})
	if !ok {
		return ErrUnavailable
	}
	source, err := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
	if err != nil {
		return err
	}
	if source.ConfigRevision != revision {
		return repo.ErrConflict
	}
	def, credential, actual, err := s.connection(ctx, id, nil)
	if err != nil {
		return err
	}
	if actual != source.Revision {
		return repo.ErrConflict
	}
	knownMQTTFilters := []string{}
	if def.Protocol == MQTT {
		knownMQTTFilters = append(knownMQTTFilters, def.MQTT.TopicFilters...)
		var resources []NativeResource
		if source.Resources != "" && json.Unmarshal([]byte(source.Resources), &resources) != nil {
			return repo.ErrInvalidArgument
		}
		for _, resource := range resources {
			if strings.HasPrefix(resource.ID, "mqtt:") {
				knownMQTTFilters = append(knownMQTTFilters, strings.TrimPrefix(resource.ID, "mqtt:"))
			}
		}
	}
	if len(refs) > 0 {
		switch def.Protocol {
		case USBHID:
			if len(refs) != 1 || refs[0] != HIDTextResource {
				return repo.ErrInvalidArgument
			}
		case MQTT:
			def.MQTT.TopicFilters = nil
			def.MQTT.QoS = qos
			for _, ref := range refs {
				if !strings.HasPrefix(ref, "mqtt:") {
					return repo.ErrInvalidArgument
				}
				def.MQTT.TopicFilters = append(def.MQTT.TopicFilters, strings.TrimPrefix(ref, "mqtt:"))
			}
		case OPCUA:
			def.OPCUA.NodeIDs = nil
			for _, ref := range refs {
				if !strings.HasPrefix(ref, "opcua:") {
					return repo.ErrInvalidArgument
				}
				def.OPCUA.NodeIDs = append(def.OPCUA.NodeIDs, strings.TrimPrefix(ref, "opcua:"))
			}
		case ModbusTCP:
			ranges := []ModbusRange{}
			for _, item := range def.Modbus.Ranges {
				for _, ref := range refs {
					if ref == "modbus:"+item.ID {
						ranges = append(ranges, item)
						break
					}
				}
			}
			if len(ranges) != len(refs) {
				return repo.ErrInvalidArgument
			}
			def.Modbus.Ranges = ranges
		default:
			return repo.ErrInvalidArgument
		}
	}
	if collection != nil {
		def, err = (StreamDefinition{Model: Publish, NativeResourceRefs: refs, Collection: collection}).EffectiveSource(def)
		if err != nil {
			return err
		}
	}
	observedCtx := WithWatchObserver(ctx, func(ready bool) {
		// Publish the protocol observation before the DB health write, so a
		// concurrent disconnect cannot be overwritten by a delayed ready update.
		ReportWatchReady(ctx, ready)
		if ready {
			observeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			if err := repo.NewSourceStreamRepo(observeCtx).ObserveSourceWatch(observeCtx, id, revision); err != nil {
				ReportWatchReady(ctx, false)
				logx.WithContext(observeCtx).Error("SourceStream watch readiness health observation failed")
				return
			}
		}
	})
	if scoped, ok := s.runtime.(interface {
		WatchWithKnownSubscriptions(context.Context, int64, SourceDefinition, Credential, []string, func(SourceInput) error) error
	}); ok && def.Protocol == MQTT {
		err = scoped.WatchWithKnownSubscriptions(observedCtx, id, def, credential, knownMQTTFilters, emit)
	} else {
		err = runtime.Watch(observedCtx, id, def, credential, emit)
	}
	if err != nil && ctx.Err() == nil {
		observeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = repo.NewSourceStreamRepo(observeCtx).ObserveSource(observeCtx, id, actual, "unhealthy", "SourceStream.protocolFailed", nil)
	}
	return err
}

func taskIdentity(messageID string, ref ExecutionRef) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("event:%s:%d:%d", messageID, ref.StreamID, ref.VersionID)))
	return hex.EncodeToString(hash[:])
}

func (s *Service) prepareTask(ctx context.Context, messageID string, ref ExecutionRef, def StreamDefinition, input any, receivedAt int64, resourceRef string, topicID int64) (repo.StreamTask, error) {
	r := repo.NewSourceStreamRepo(ctx)
	id := taskIdentity(messageID, ref)
	if existing, err := r.Task(ctx, id); err == nil {
		return existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return repo.StreamTask{}, err
	}
	if s.executor == nil || messageID == "" || len(messageID) > 128 || receivedAt <= 0 {
		return repo.StreamTask{}, repo.ErrInvalidArgument
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 1024*1024 {
		return repo.StreamTask{}, invalid("input", "invalidOrTooLarge")
	}
	metadata := map[string]any{"sourceId": ref.SourceID, "streamId": ref.StreamID, "versionId": ref.VersionID, "runId": id, "resourceRef": resourceRef, "topicId": topicID, "model": def.Model, "dryRun": false}
	var result ScriptResult
	if def.Program != nil {
		metadata["trigger"] = def.Program.Trigger.Kind
		grants, host, hostErr := s.deployedProgramHost(ctx, ref.StreamID, ref.VersionID)
		err = hostErr
		if err == nil {
			result, err = s.transformProgram(ctx, def.Script, input, metadata, grants, host)
		}
	} else {
		result, err = s.executor.Transform(ctx, def.Script, input, metadata)
	}
	if err == nil {
		_, err = def.OutputTargets(result)
	}
	if err != nil {
		summary, _ := json.Marshal(map[string]any{"phase": "transform", "input": dataSummary(raw), "error": "SourceStream.executionFailed", "usesNATS": true})
		_, recordErr := r.ReserveRun(ctx, repo.StreamRun{ID: id, SourceID: ref.SourceID, StreamID: ref.StreamID, VersionID: ref.VersionID, Model: string(def.Model), Status: "failed", MessageID: messageID, Summary: string(summary), CreatedAt: time.Now().UnixMilli()})
		return repo.StreamTask{}, errors.Join(err, recordErr)
	}
	output, err := json.Marshal(result)
	if err != nil || len(output) > 1024*1024 {
		return repo.StreamTask{}, invalid("output", "invalidOrTooLarge")
	}
	summary, _ := json.Marshal(map[string]any{"phase": "transformed", "input": dataSummary(raw), "output": dataSummary(output), "transport": "nats", "usesNATS": true})
	task := repo.StreamTask{ID: id, SourceID: ref.SourceID, StreamID: ref.StreamID, VersionID: ref.VersionID, SourceRevision: ref.SourceRevision, Model: string(def.Model), Output: string(output), ReceivedAt: receivedAt}
	run := repo.StreamRun{ID: id, SourceID: ref.SourceID, StreamID: ref.StreamID, VersionID: ref.VersionID, Model: string(def.Model), Status: "processing", MessageID: messageID, Summary: string(summary), CreatedAt: time.Now().UnixMilli()}
	if def.Program != nil {
		return r.PutProgramTask(ctx, task, run)
	}
	return r.PutTask(ctx, task, run)
}

// Each reference is frozen at RAW ingress. Paths/targets complete independently;
// successful target receipts survive a retry of the remaining failed target.
func (s *Service) ProcessRaw(ctx context.Context, raw RawInput, sink StreamSink) error {
	if sink == nil || raw.MessageID == "" || raw.SourceID < 0 || len(raw.Plans) > 1000 || len(raw.Lineage) >= dataingest.MaxStreamLineage {
		return repo.ErrInvalidArgument
	}
	var failures []error
	for _, ref := range raw.Plans {
		if ref.SourceID != raw.SourceID {
			failures = append(failures, repo.ErrInvalidArgument)
			continue
		}
		def, err := s.loadPlan(ctx, ref)
		if errors.Is(err, errInactivePlan) {
			if err := discardReference(ctx, raw.MessageID, ref, Publish); err != nil {
				failures = append(failures, err)
			}
			continue
		}
		if err == nil && def.Model == ScriptMode {
			sub, e := parseSubscription(def.Script.Code)
			if e == nil && sub != nil {
				watch, _ := sub.acquisition(def)
				if ref.SourceID != raw.SourceID || !watch.MatchesResource(raw.Input.ResourceRef) {
					e = repo.ErrInvalidArgument
				} else {
					r := repo.NewSourceStreamRepo(ctx)
					stream, se := r.Stream(ctx, ref.StreamID)
					version, ve := r.Version(ctx, ref.StreamID, ref.VersionID)
					e = errors.Join(se, ve)
					if e == nil {
						runCtx := contextx.WithSubject(ctx, contextx.Subject{ResourceKeys: map[string]struct{}{"uns.source": {}}})
						_, e = s.runScript(runCtx, stream, version, taskIdentity(raw.MessageID, ref), raw.Input.Data, def.Script.TimeoutMs, &raw.Input, ref.SourceID)
					}
				}
			} else if e == nil {
				e = repo.ErrInvalidArgument
			}
			if e != nil {
				failures = append(failures, e)
			}
			continue
		}
		if err == nil && def.IsCompleteProgram() {
			if err := s.processProgramV2Raw(ctx, raw, ref, sink); err != nil {
				failures = append(failures, err)
			}
			continue
		}
		if err == nil {
			matched := def.Model == Publish && def.MatchesResource(raw.Input.ResourceRef)
			if def.Program != nil {
				matched = false
				switch def.Program.Trigger.Kind {
				case IntervalTrigger:
					matched = raw.Trigger == IntervalTrigger && raw.SourceID == 0 && raw.Input.ResourceRef == "interval"
				case SourceTrigger:
					matched = (raw.Trigger == SourceTrigger || raw.Trigger == "") && def.Program.MatchesInput(raw.SourceID, raw.Input.ResourceRef)
				case UNSTrigger:
					for _, id := range def.Program.Trigger.TopicIDs {
						matched = matched || (raw.Trigger == UNSTrigger && raw.SourceID == 0 && raw.TopicID == id)
					}
				}
			}
			if !matched {
				err = repo.ErrInvalidArgument
			}
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		loop := false
		for _, id := range raw.Lineage {
			loop = loop || id == ref.StreamID
		}
		if loop {
			continue
		}
		task, err := s.prepareTask(ctx, raw.MessageID, ref, def, raw.Input.Data, raw.Input.ReceivedAt, raw.Input.ResourceRef, raw.TopicID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if task.Status == "succeeded" || task.Status == "dropped" {
			continue
		}
		var output ScriptResult
		if json.Unmarshal([]byte(task.Output), &output) != nil {
			failures = append(failures, repo.ErrInvalidArgument)
			continue
		}
		targets, err := def.OutputTargets(output)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		started := time.Now()
		results := []map[string]any{}
		failed := false
		if def.MetadataTarget != nil && !output.Drop {
			targetErr := unsmeta.CommitMapped(ctx, task.ID, *def.MetadataTarget, output.Data, task.ReceivedAt, task)
			status := "succeeded"
			if targetErr != nil {
				status = "failed"
				failed = true
				failures = append(failures, targetErr)
			}
			results = append(results, map[string]any{"topicId": def.MetadataTarget.TopicID, "collectionId": def.MetadataTarget.CollectionID, "status": status})
		}
		for _, target := range targets {
			topicID := target.TopicID
			node, targetErr := repo.NewUnsRepo(ctx).GetUnsNode(ctx, topicID, false)
			if targetErr == nil {
				lineage := append(append([]int64{}, raw.Lineage...), ref.StreamID)
				event := dataingest.StreamEvent{MessageID: fmt.Sprintf("%s:%d", task.ID, topicID), SourceID: ref.SourceID, StreamID: ref.StreamID, VersionID: ref.VersionID, TopicID: topicID, ReceivedAt: time.UnixMilli(task.ReceivedAt), Data: target.Data, Drop: output.Drop, Lineage: lineage}
				_, targetErr = sink.CommitStreamEvent(ctx, node, event)
			}
			status := "succeeded"
			if output.Drop {
				status = "dropped"
			}
			if targetErr != nil {
				status = "failed"
				failed = true
				failures = append(failures, targetErr)
			}
			results = append(results, map[string]any{"topicId": topicID, "status": status})
		}
		status := "succeeded"
		if output.Drop || (len(targets) == 0 && def.MetadataTarget == nil) {
			status = "dropped"
		}
		if failed {
			status = "partial-failure"
		}
		summary, _ := json.Marshal(map[string]any{"phase": "uns-commit", "targets": results, "usesNATS": true, "versionId": ref.VersionID})
		if err := repo.NewSourceStreamRepo(ctx).FinishTask(ctx, task.ID, status, string(summary), time.Since(started).Milliseconds()); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// The durable task is the delivery outbox. EVENTS is acknowledged only after
// all matching subscription intents have been persisted.
func (s *Service) MatchEvent(ctx context.Context, event dataingest.StreamEvent) error {
	if event.MessageID == "" || event.TopicID <= 0 || event.ReceivedAt.IsZero() {
		return repo.ErrInvalidArgument
	}
	plans, err := s.Plans(ctx)
	if err != nil {
		return err
	}
	refs := []ExecutionRef{}
	for _, plan := range plans {
		if plan.Definition.Model != Subscribe {
			continue
		}
		matched := false
		for _, topicID := range plan.Definition.TopicIDs {
			matched = matched || topicID == event.TopicID
		}
		if !matched {
			continue
		}
		refs = append(refs, plan.ExecutionRef)
	}
	rawRefs, _ := json.Marshal(refs)
	frozen, err := repo.NewSourceStreamRepo(ctx).BindEventMatch(ctx, event.MessageID, string(rawRefs))
	if err != nil || json.Unmarshal([]byte(frozen), &refs) != nil {
		if err == nil {
			err = repo.ErrInvalidArgument
		}
		return err
	}
	var failures []error
	for _, ref := range refs {
		def, err := s.loadPlan(ctx, ref)
		if errors.Is(err, errInactivePlan) {
			if err := discardReference(ctx, event.MessageID, ref, Subscribe); err != nil {
				failures = append(failures, err)
			}
			continue
		}
		if err != nil {
			failures = append(failures, err)
			continue
		}
		task, err := s.prepareTask(ctx, event.MessageID, ref, def, event.Data, event.ReceivedAt.UnixMilli(), "", event.TopicID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		var result ScriptResult
		if json.Unmarshal([]byte(task.Output), &result) != nil {
			failures = append(failures, repo.ErrInvalidArgument)
		} else if result.Drop && task.Status == "pending" {
			if err := repo.NewSourceStreamRepo(ctx).FinishTask(ctx, task.ID, "dropped", `{"phase":"subscription-filter","usesNATS":true}`, 0); err != nil {
				failures = append(failures, err)
			}
		}
	}
	return errors.Join(failures...)
}

func (s *Service) Deliver(ctx context.Context, id string) error {
	r := repo.NewSourceStreamRepo(ctx)
	task, err := r.Task(ctx, id)
	if err != nil {
		return err
	}
	if task.Model != string(Subscribe) {
		return repo.ErrInvalidArgument
	}
	if task.Status == "dispatching" {
		// A previous process may have sent the operation before it crashed.
		// Never automatically repeat a side-effecting device command.
		if err := r.FinishTask(ctx, id, "result-unknown", `{"phase":"native-operation","error":"SourceStream.resultUnknown","operationRepeated":false}`, 0); err != nil {
			return err
		}
		return ErrUnknownResult
	}
	if task.Status != "pending" {
		if task.Status == "result-unknown" {
			return ErrUnknownResult
		}
		if task.Status == "failed" {
			return errors.New("SourceStream.protocolFailed")
		}
		return nil
	}
	ref := ExecutionRef{SourceID: task.SourceID, StreamID: task.StreamID, VersionID: task.VersionID, SourceRevision: task.SourceRevision}
	def, err := s.loadPlan(ctx, ref)
	if errors.Is(err, errInactivePlan) {
		return r.DiscardQueuedExecution(ctx, repo.StreamRun{
			ID: id, SourceID: ref.SourceID, StreamID: ref.StreamID, VersionID: ref.VersionID,
			Model: string(Subscribe), Status: "dropped", MessageID: id, CreatedAt: time.Now().UnixMilli(),
			Summary: `{"phase":"reference-validation","reason":"inactiveReference","usesNATS":true,"operationRepeated":false}`,
		})
	}
	if err != nil {
		return err
	}
	source, credential, revision, err := s.connection(ctx, task.SourceID, nil)
	if err != nil {
		return err
	}
	current, err := r.Source(ctx, task.SourceID)
	if err != nil {
		return err
	}
	if current.Revision != revision || current.ConfigRevision != task.SourceRevision {
		return repo.ErrConflict
	}
	var output ScriptResult
	if json.Unmarshal([]byte(task.Output), &output) != nil {
		return repo.ErrInvalidArgument
	}
	claimed, err := r.ClaimDelivery(ctx, task)
	if err != nil || !claimed {
		return err
	}
	started := time.Now()
	opCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	runtimeSource, err := def.EffectiveSource(source)
	if err != nil {
		return err
	}
	receipt, invokeErr := s.runtime.Invoke(opCtx, task.SourceID, runtimeSource, credential, def.NativeResourceRef, output.Data)
	status := "accepted"
	summary := map[string]any{"phase": "native-operation", "deviceExecuted": false, "usesNATS": true}
	if invokeErr != nil {
		status = "failed"
		summary["error"] = "SourceStream.protocolFailed"
		if errors.Is(invokeErr, ErrUnknownResult) || opCtx.Err() != nil {
			status = "result-unknown"
			invokeErr = ErrUnknownResult
			summary["error"] = ErrUnknownResult.Error()
		}
	} else if raw, err := json.Marshal(receipt); err == nil {
		summary["receipt"] = dataSummary(raw)
	}
	raw, _ := json.Marshal(summary)
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer finishCancel()
	if err := r.FinishTask(finishCtx, id, status, string(raw), time.Since(started).Milliseconds()); err != nil {
		return err
	}
	return invokeErr
}

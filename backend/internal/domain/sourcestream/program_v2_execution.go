package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/dataingest"
	"backend/internal/repo"
	"backend/internal/streamexec"
	"backend/internal/unsref"
	"gorm.io/gorm"
)

// Admission precedes user code. A missing result after admission is ambiguous:
// replay may resume persisted Topic outputs, but must never repeat device I/O.
func (s *Service) prepareProgramV2Task(ctx context.Context, messageID string, ref ExecutionRef, input any, receivedAt int64, resourceRef string, topicID int64, activeOnly bool, lineage []int64, metadataRefs ...string) (repo.StreamTask, bool, error) {
	ctx, ownerID, release, err := s.acquireProgramExecution(ctx)
	if err != nil {
		return repo.StreamTask{}, false, err
	}
	defer release()
	metadataTopicID, collectionID := "", ""
	if len(metadataRefs) > 2 {
		return repo.StreamTask{}, false, repo.ErrInvalidArgument
	}
	if len(metadataRefs) > 0 {
		metadataTopicID = metadataRefs[0]
	}
	if len(metadataRefs) > 1 {
		collectionID = metadataRefs[1]
	}
	if metadataTopicID != "" && (!unsref.ValidMetadataID(metadataTopicID) || topicID != 0) || collectionID != "" && !unsref.ValidMetadataID(collectionID) {
		return repo.StreamTask{}, false, repo.ErrInvalidArgument
	}
	r := repo.NewSourceStreamRepo(ctx)
	version, err := r.Version(ctx, ref.StreamID, ref.VersionID)
	if err != nil {
		return repo.StreamTask{}, false, err
	}
	ctx, err = s.withRequestPublisher(ctx, version, ref.EntryID)
	if err != nil {
		return repo.StreamTask{}, false, err
	}
	manifest, err := version.ProgramV2Manifest()
	if err != nil {
		return repo.StreamTask{}, false, err
	}
	var def StreamDefinition
	if decodeStoredProgram([]byte(version.Document), &def) != nil {
		return repo.StreamTask{}, false, repo.ErrInvalidArgument
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > streamexec.MaxJSONBytes || messageID == "" || len(messageID) > 128 || receivedAt <= 0 {
		return repo.StreamTask{}, false, repo.ErrInvalidArgument
	}
	hashInput := []any{ref.StreamID, ref.VersionID, ref.EntryID, ref.SourceID, resourceRef, topicID, json.RawMessage(raw)}
	if metadataTopicID != "" || collectionID != "" {
		hashInput = append(hashInput, metadataTopicID, collectionID)
	}
	hashBytes, _ := json.Marshal(hashInput)
	hash := sha256.Sum256(hashBytes)
	requestHash := hex.EncodeToString(hash[:])
	runID := repo.ProgramRunIdentity(messageID, ref.StreamID, ref.VersionID, ref.EntryID)
	summaryFields := map[string]any{"requestHash": requestHash, "phase": "admitted", "usesNATS": !activeOnly, "operationRepeated": false, "input": dataSummary(raw)}
	for _, entry := range manifest.Entries {
		if entry.ID == ref.EntryID && entry.Request != nil {
			if subject, ok := contextx.SubjectFrom(ctx); ok {
				summaryFields["requestIdentity"] = appRequestIdentity{AppID: subject.APIKeyAppID, KeyID: subject.APIKeyID, UserID: subject.UserID, AuthType: subject.AuthType}
			}
		}
	}
	summary, _ := json.Marshal(summaryFields)
	run := repo.StreamRun{ID: runID, SourceID: ref.SourceID, StreamID: ref.StreamID, VersionID: ref.VersionID, EntryID: ref.EntryID,
		Model: "program", Status: "processing", MessageID: messageID, Summary: string(summary), CreatedAt: time.Now().UnixMilli()}
	created, err := r.AdmitOwnedProgramV2Run(ctx, run, activeOnly, ownerID, lineage)
	if err != nil {
		return repo.StreamTask{}, false, err
	}
	if !created {
		task, taskErr := r.Task(ctx, runID)
		if taskErr == nil {
			return task, true, nil
		}
		if !errors.Is(taskErr, gorm.ErrRecordNotFound) {
			return task, true, taskErr
		}
		stored, runErr := r.Run(ctx, runID)
		if runErr != nil {
			return repo.StreamTask{}, true, runErr
		}
		if stored.Status == "failed" {
			// The admitted execution already ended without recoverable outputs.
			// Do not rerun it, but do not misreport a known failure as unknown.
			return repo.StreamTask{}, true, ErrExecutionFailed
		}
		return repo.StreamTask{}, true, ErrUnknownResult
	}
	executor, ok := s.executor.(interface {
		ExecuteProgram(context.Context, streamexec.ProgramRequest, streamexec.ProgramHost) (streamexec.ProgramResult, error)
	})
	if !ok {
		return repo.StreamTask{}, false, s.failProgramV2Run(ctx, run, ErrUnavailable)
	}
	snapshots, err := version.Sources()
	if err != nil {
		return repo.StreamTask{}, false, s.failProgramV2Run(ctx, run, err)
	}
	ctx, guard := WithProgramSecretGuard(ctx)
	ctx, checkpoint := withProgramCheckpoint(ctx, nil)
	host, err := s.ProgramV2Host(ctx, manifest, snapshots, runID, ref.EntryID, s.programReader, false, nil, nil)
	if err != nil {
		return repo.StreamTask{}, false, s.failProgramV2Run(ctx, run, err)
	}
	inputTopicID := strconv.FormatInt(topicID, 10)
	if metadataTopicID != "" {
		inputTopicID = metadataTopicID
	}
	metadata, _ := json.Marshal(map[string]any{"sourceId": strconv.FormatInt(ref.SourceID, 10), "streamId": strconv.FormatInt(ref.StreamID, 10), "versionId": strconv.FormatInt(ref.VersionID, 10),
		"runId": runID, "resourceRef": resourceRef, "topicId": inputTopicID, "collectionId": collectionID, "receivedAt": receivedAt, "dryRun": false})
	observed := &programHostDiagnostics{}
	result, err := executor.ExecuteProgram(ctx, streamexec.ProgramRequest{SourceAliases: def.sourceAliases(), Code: def.Script.Code, TimeoutMs: def.Script.TimeoutMs,
		Input: raw, Context: metadata, EntryID: ref.EntryID, ExpectedManifest: &manifest}, observed.wrap(host))
	if err != nil {
		return repo.StreamTask{}, false, s.failProgramV2Run(ctx, run, observed.failure(err))
	}
	result.Checkpoint = checkpoint.result()
	if err := checkRequestExecution(ctx); err != nil {
		return repo.StreamTask{}, false, s.failProgramV2Run(ctx, run, err)
	}
	guard.RedactResult(&result)
	if err = manifest.ValidateRequestEffects(ref.EntryID, result); err != nil {
		return repo.StreamTask{}, false, s.failProgramV2Run(ctx, run, err)
	}
	if err = validateProgramV2Outputs(manifest, result); err != nil {
		return repo.StreamTask{}, false, s.failProgramV2Run(ctx, run, observed.failure(errors.Join(streamexec.ErrOutput, err)))
	}
	output, err := json.Marshal(result)
	if err != nil || len(output) > streamexec.MaxProgramDataBytes {
		return repo.StreamTask{}, false, s.failProgramV2Run(ctx, run, observed.failure(errors.Join(streamexec.ErrOutput, repo.ErrInvalidArgument)))
	}
	task := repo.StreamTask{ID: runID, SourceID: ref.SourceID, StreamID: ref.StreamID, VersionID: ref.VersionID, EntryID: ref.EntryID,
		SourceRevision: ref.SourceRevision, Model: "program", Output: string(output), ReceivedAt: receivedAt}
	task, err = r.PutProgramV2Task(ctx, task, run)
	if err != nil {
		// A failed commit response is not proof the transaction rolled back.
		// Resolve under the admission lock without evaluating user code again.
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		stored, recovered, finishErr := r.ResolveProgramV2PreparationFailure(finishCtx, run)
		if finishErr != nil {
			return task, false, errors.Join(err, finishErr)
		}
		if recovered {
			return stored, false, nil
		}
		return task, false, recordedProgramFailure{cause: err, unknown: true}
	}
	return task, false, err
}

func (s *Service) failProgramV2Run(ctx context.Context, run repo.StreamRun, cause error) error {
	// Finalization must survive the script cancellation, but remains bounded.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	r := repo.NewSourceStreamRepo(finishCtx)
	status := "failed"
	ownerLost := errors.Is(context.Cause(ctx), errProgramExecutionOwnerLost)
	if epoch, ok := ctx.Value(programExecutionEpochKey{}).(*programExecutionEpoch); ok && epoch.owner.Ping(finishCtx) != nil {
		ownerLost = true
	}
	var effects int64
	effectStates := []string{"dispatching", "unknown"}
	if isStreamRequestContext(ctx) || ctx.Value(streamRequestAuthorityKey{}) != nil {
		// A completed external write cannot be rolled back when the request's
		// remaining execution or output fails. Cloud requests expose uncertainty
		// for the overall result, while retaining the exact effect receipt.
		effectStates = append(effectStates, "completed")
	}
	if err := repo.GetUnsConn(finishCtx).Model(&repo.StreamProgramEffect{}).
		Where("run_id=? AND entry_id=? AND status IN ?", run.ID, run.EntryID, effectStates).Count(&effects).Error; err != nil {
		return errors.Join(cause, err)
	}
	if ownerLost || effects > 0 || errors.Is(cause, ErrUnknownResult) {
		status = "unknown"
	}
	var summary map[string]any
	_ = json.Unmarshal([]byte(run.Summary), &summary)
	summary["phase"], summary["error"], summary["operationRepeated"] = "execution", "SourceStream.executionFailed", false
	summary["errorCode"] = executionFailureCode(cause)
	var observed observedProgramFailure
	if errors.As(cause, &observed) && projectRunError(observed.hostCode) != nil {
		summary["hostErrorCode"] = observed.hostCode
	}
	encoded, _ := json.Marshal(summary)
	if err := r.FinishRun(finishCtx, run.ID, status, string(encoded), 0); err != nil {
		if errors.Is(err, repo.ErrConflict) {
			stored, readErr := r.Run(finishCtx, run.ID)
			if readErr == nil && stored.StreamID == run.StreamID && stored.VersionID == run.VersionID && stored.EntryID == run.EntryID && stored.SourceID == run.SourceID && stored.Model == run.Model &&
				(stored.Status == "unknown" || stored.Status == "failed") {
				// Retirement/disable can finalize first. Preserve its durable
				// reason and report that result, not a misleading conflict.
				return recordedProgramFailure{cause: cause, unknown: stored.Status == "unknown"}
			}
			return errors.Join(cause, err, readErr)
		}
		return errors.Join(cause, err)
	}
	if status == "unknown" {
		return recordedProgramFailure{cause: cause, unknown: true}
	}
	return recordedProgramFailure{cause: cause}
}

// Preserve the original cause for callers while marking only a durably
// recorded failure terminal. A failed finalization remains retryable.
type recordedProgramFailure struct {
	cause   error
	unknown bool
}

func (e recordedProgramFailure) Error() string {
	if e.unknown {
		return ErrUnknownResult.Error()
	}
	return ErrExecutionFailed.Error()
}
func (e recordedProgramFailure) Unwrap() error { return e.cause }
func (e recordedProgramFailure) Is(target error) bool {
	if e.unknown {
		return target == ErrUnknownResult
	}
	return target == ErrExecutionFailed
}

func validateProgramV2Outputs(manifest streamexec.ProgramManifest, result streamexec.ProgramResult) error {
	allowed := map[string]streamexec.ProgramTopic{}
	for _, topic := range manifest.Topics {
		allowed[topic.Key()] = topic
	}
	if len(result.Outputs) > 1000 {
		return repo.ErrInvalidArgument
	}
	canonical, legacy := false, false
	for _, output := range result.Outputs {
		declared, ok := allowed[programOutputKey(output)]
		if !output.UNSReference().ValidIdentity() || !ok || !declared.Writable() || declared.Canonical() != output.UNSReference().Canonical() || declared.TopicID != output.TopicID || declared.CollectionID != output.CollectionID || len(output.Data) == 0 || !json.Valid(output.Data) {
			return repo.ErrInvalidArgument
		}
		canonical = canonical || declared.Canonical()
		legacy = legacy || !declared.Canonical()
	}
	if canonical && legacy {
		return repo.ErrInvalidArgument
	}
	return nil
}

func (s *Service) processProgramV2Raw(ctx context.Context, raw RawInput, ref ExecutionRef, sink StreamSink) error {
	version, err := repo.NewSourceStreamRepo(ctx).Version(ctx, ref.StreamID, ref.VersionID)
	if err != nil {
		return err
	}
	manifest, err := version.ProgramV2Manifest()
	if err != nil {
		return err
	}
	matched := false
	for _, entry := range manifest.Entries {
		if entry.ID != ref.EntryID {
			continue
		}
		switch entry.Kind {
		case "source-event":
			plan := ExecutionPlan{ExecutionRef: ref, Entry: &entry}
			matched = (raw.Trigger == SourceTrigger || raw.Trigger == "") && plan.MatchesSourceInput(raw.SourceID, raw.Input.ResourceRef)
		case "interval":
			matched = raw.Trigger == IntervalTrigger && raw.SourceID == 0 && raw.Input.ResourceRef == "interval"
		case "uns-event":
			inputTopicID := strconv.FormatInt(raw.TopicID, 10)
			if raw.MetadataTopicID != "" {
				inputTopicID = raw.MetadataTopicID
			}
			for _, topic := range entry.TopicIDs {
				metadata := raw.MetadataTopicID != "" || raw.CollectionID != ""
				matched = matched || (raw.Trigger == UNSTrigger && raw.SourceID == 0 && manifest.MatchesTopicReference(topic, inputTopicID, metadata))
			}
		}
	}
	if !matched {
		return repo.ErrInvalidArgument
	}
	for _, id := range raw.Lineage {
		if id == ref.StreamID {
			return nil
		}
	}
	task, _, err := s.prepareProgramV2Task(ctx, raw.MessageID, ref, raw.Input.Data, raw.Input.ReceivedAt, raw.Input.ResourceRef, raw.TopicID, false, raw.Lineage, raw.MetadataTopicID, raw.CollectionID)
	if err != nil {
		return err
	}
	_, err = s.commitProgramV2Task(ctx, task, raw.Lineage, sink, true)
	return err
}

// Output ordinal is part of the durable receipt, preserving repeated writes to
// the same Topic and allowing recovery without re-evaluating the script.
func (s *Service) commitProgramV2Task(ctx context.Context, task repo.StreamTask, lineage []int64, sink StreamSink, usesNATS bool) (streamexec.ProgramResult, error) {
	var result streamexec.ProgramResult
	if task.Model != "program" || json.Unmarshal([]byte(task.Output), &result) != nil {
		return result, repo.ErrInvalidArgument
	}
	version, err := repo.NewSourceStreamRepo(ctx).Version(ctx, task.StreamID, task.VersionID)
	if err != nil {
		return result, err
	}
	ctx, err = s.withRequestPublisher(ctx, version, task.EntryID)
	if err != nil {
		return result, err
	}
	if task.Status == "succeeded" {
		return result, nil
	}
	storedLineage, owned, err := repo.NewSourceStreamRepo(ctx).ProgramRunLineage(ctx, task.ID)
	if err != nil {
		return result, err
	}
	if owned {
		lineage = storedLineage
	}
	manifest, err := version.ProgramV2Manifest()
	if err != nil {
		return result, err
	}
	if err := manifest.ValidateRequestEffects(task.EntryID, result); err != nil {
		return result, err
	}
	if err := validateProgramV2Outputs(manifest, result); err != nil {
		return result, err
	}
	bindings, err := version.ProgramV2Bindings()
	if err != nil {
		return result, err
	}
	canonical := len(result.Outputs) > 0 && result.Outputs[0].UNSReference().Canonical()
	if canonical || result.Checkpoint != nil && len(result.Outputs) == 0 && len(bindings) > 0 {
		return s.commitProgramV2MetadataTask(ctx, task, result, lineage, usesNATS, bindings)
	}
	if sink == nil && len(result.Outputs) > 0 {
		return result, ErrUnavailable
	}
	if result.Checkpoint != nil {
		return s.commitProgramV2CheckpointTask(ctx, task, result, lineage, sink, usesNATS)
	}
	var failures []error
	targets := []map[string]any{}
	blockedTopics := map[string]bool{}
	for ordinal, output := range result.Outputs {
		if blockedTopics[output.TopicID] {
			targets = append(targets, map[string]any{"topicId": output.TopicID, "ordinal": ordinal, "status": "pending"})
			continue
		}
		id, err := strconv.ParseInt(output.TopicID, 10, 64)
		if err != nil || id <= 0 || strconv.FormatInt(id, 10) != output.TopicID {
			return result, repo.ErrInvalidArgument
		}
		node, err := repo.NewUnsRepo(ctx).GetUnsNode(ctx, id, false)
		if err == nil && node.Type != 2 {
			err = repo.ErrInvalidArgument
		}
		var data any
		if err == nil {
			err = json.Unmarshal(output.Data, &data)
		}
		if err == nil {
			err = checkRequestExecution(ctx)
		}
		if err == nil {
			event := dataingest.StreamEvent{MessageID: repo.ProgramOutputIdentity(task.ID, ordinal, id), SourceID: task.SourceID, StreamID: task.StreamID,
				VersionID: task.VersionID, TopicID: id, Data: data, ReceivedAt: time.UnixMilli(task.ReceivedAt), Lineage: append(append([]int64{}, lineage...), task.StreamID)}
			_, err = sink.CommitStreamEvent(ctx, node, event)
		}
		status := "succeeded"
		if err != nil {
			status = "failed"
			failures = append(failures, err)
		}
		targets = append(targets, map[string]any{"topicId": output.TopicID, "ordinal": ordinal, "status": status})
		// Preserve per-Topic order while letting other Topics finish independently.
		blockedTopics[output.TopicID] = err != nil
	}
	r := repo.NewSourceStreamRepo(ctx)
	run, err := r.Run(ctx, task.ID)
	if err != nil {
		return result, errors.Join(errors.Join(failures...), err)
	}
	var summary map[string]any
	if json.Unmarshal([]byte(run.Summary), &summary) != nil {
		return result, repo.ErrInvalidArgument
	}
	summary["phase"], summary["targets"], summary["usesNATS"], summary["operationRepeated"] = "uns-commit", targets, usesNATS, false
	recordCommitError(summary, errors.Join(failures...))
	encoded, _ := json.Marshal(summary)
	status := "succeeded"
	if len(failures) > 0 {
		status = "partial-failure"
	}
	if err := r.FinishProgramV2Task(ctx, task.ID, status, string(encoded), result.DurationMs); err != nil {
		failures = append(failures, err)
	}
	return result, errors.Join(failures...)
}

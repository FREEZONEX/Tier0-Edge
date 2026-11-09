package sourcestream

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"backend/internal/contextx"
	"backend/internal/domain/unsmeta"
	"backend/internal/domain/unsquery"
	"backend/internal/repo"
	"backend/internal/streamexec"
)

func programOutputKey(output streamexec.ProgramOutput) string {
	return output.UNSReference().Key()
}

func programMetadataWrites(bindings []repo.ProgramUNSBinding, outputs []streamexec.ProgramOutput) ([]unsmeta.ProgramWrite, error) {
	allowed := map[string]repo.ProgramUNSBinding{}
	for _, binding := range bindings {
		allowed[binding.Reference.Key()] = binding
	}
	writes := make([]unsmeta.ProgramWrite, 0, len(outputs))
	for _, output := range outputs {
		binding, ok := allowed[programOutputKey(output)]
		if !ok || !binding.Reference.Writable() || binding.CollectionID == "" {
			return nil, streamexec.ErrSourceDenied
		}
		var value struct {
			unsmeta.IngestValue
			unsmeta.ProgramWriteOptions
			ExpectedRevision json.RawMessage `json:"expectedRevision"`
		}
		if err := unsmeta.Parse(json.RawMessage(output.Data), &value); err != nil {
			return nil, err
		}
		write := unsmeta.ProgramWrite{TopicID: binding.Reference.TopicID, CollectionID: binding.CollectionID, Value: value.IngestValue, ProgramWriteOptions: value.ProgramWriteOptions}
		if len(value.ExpectedRevision) > 0 {
			var revision int64
			if string(value.ExpectedRevision) == "null" || json.Unmarshal(value.ExpectedRevision, &revision) != nil || revision < 0 || revision > unsmeta.MaxProgramExpectedRevision {
				return nil, unsmeta.ErrInvalid
			}
			write.ExpectedRevision = &revision
		}
		writes = append(writes, write)
	}
	return writes, nil
}

func previewProgramMetadataOutputs(ctx context.Context, manifest streamexec.ProgramManifest, outputs []streamexec.ProgramOutput, compare bool) ([]map[string]any, error) {
	bindings, err := repo.FreezeProgramV2Bindings(ctx, manifest)
	if err != nil {
		return nil, err
	}
	writes, err := programMetadataWrites(bindings, outputs)
	if err != nil {
		return nil, err
	}
	targets := make([]map[string]any, 0, len(writes))
	var previews []unsmeta.ProgramWritePreview
	if compare {
		subject, ok := contextx.SubjectFrom(ctx)
		if !ok || !contextx.HasResource(subject, "uns.read") {
			return nil, ErrForbidden
		}
		previews, err = unsmeta.PreviewProgramWrites(ctx, writes, time.Now().UnixMilli())
		if err != nil {
			return nil, err
		}
	}
	for i, write := range writes {
		if compare {
			targets = append(targets, map[string]any{"topicId": write.TopicID, "collectionId": write.CollectionID, "data": write.Value, "preview": previews[i], "dryRun": true})
			continue
		}
		normalized, err := unsmeta.PreviewProgramWrite(ctx, write, time.Now().UnixMilli())
		if err != nil {
			return nil, err
		}
		targets = append(targets, map[string]any{"topicId": write.TopicID, "collectionId": write.CollectionID, "data": normalized.Value, "dryRun": true})
	}
	return targets, nil
}

// Persisted task output is the recovery input. This never reexecutes user code
// or external operations, including after a commit/ACK interruption.
func (s *Service) commitProgramV2MetadataTask(ctx context.Context, task repo.StreamTask, result streamexec.ProgramResult, lineage []int64, usesNATS bool, bindings []repo.ProgramUNSBinding) (streamexec.ProgramResult, error) {
	writes, err := programMetadataWrites(bindings, result.Outputs)
	if err != nil {
		return result, err
	}
	batch := unsmeta.ProgramCommit{RunID: task.ID, StreamID: task.StreamID, VersionID: task.VersionID, EntryID: task.EntryID,
		ReceivedAt: task.ReceivedAt, Writes: writes, Lineage: append(append([]int64{}, lineage...), task.StreamID)}
	if result.Checkpoint != nil {
		batch.Checkpoint = &repo.ProgramCheckpointCommit{StreamID: task.StreamID, EntryID: task.EntryID, RunID: task.ID,
			ExpectedRevision: result.Checkpoint.Revision, Value: result.Checkpoint.Value}
	}
	commitErr := checkRequestExecution(ctx)
	if commitErr == nil {
		_, commitErr = unsmeta.CommitProgram(ctx, batch)
	}
	r := repo.NewSourceStreamRepo(ctx)
	run, err := r.Run(ctx, task.ID)
	if err != nil {
		return result, errors.Join(commitErr, err)
	}
	var summary map[string]any
	if json.Unmarshal([]byte(run.Summary), &summary) != nil {
		return result, errors.Join(commitErr, repo.ErrInvalidArgument)
	}
	status, targetStatus := "succeeded", "succeeded"
	if commitErr != nil {
		status, targetStatus = "partial-failure", "pending"
	}
	targets := make([]map[string]any, 0, len(writes))
	for ordinal, write := range writes {
		target := map[string]any{"topicId": write.TopicID, "collectionId": write.CollectionID, "ordinal": ordinal, "status": targetStatus}
		if policy, e := repo.CollectionStorage(ctx, write.CollectionID); e == nil && policy.Mode != "local" && commitErr == nil {
			target["status"], target["centerCommitted"] = "pendingUpload", false
			summary["commitScope"] = "localAcceptance"
		}
		targets = append(targets, target)
	}
	summary["phase"], summary["targets"], summary["usesNATS"], summary["operationRepeated"] = "uns-commit", targets, usesNATS, false
	recordCommitError(summary, commitErr)
	summary["atomicBatch"], summary["committed"] = true, commitErr == nil
	if result.Checkpoint != nil {
		summary["checkpoint"] = map[string]any{"stage": targetStatus, "atomicWithOutputs": true}
	}
	encoded, _ := json.Marshal(summary)
	err = r.FinishProgramV2Task(ctx, task.ID, status, string(encoded), result.DurationMs)
	return result, errors.Join(commitErr, err)
}

func readProgramMetadata(ctx context.Context, call streamexec.ProgramCall, expectedBindingHash ...string) (json.RawMessage, error) {
	var options unsmeta.ProgramReadOptions
	if len(call.Options) > 0 {
		if err := unsmeta.Parse(call.Options, &options); err != nil {
			return nil, err
		}
	}
	var value map[string]any
	var err error
	if call.CollectionID != "" {
		value, err = unsmeta.ReadProgramCollection(ctx, call.CollectionID, options)
	} else {
		if options != (unsmeta.ProgramReadOptions{}) {
			return nil, repo.ErrInvalidArgument
		}
		value, err = unsmeta.ReadProgramTopic(ctx, call.TopicID, expectedBindingHash...)
	}
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return programV2Sample(raw)
}

func queryProgramMetadata(ctx context.Context, call streamexec.ProgramCall, binding repo.ProgramUNSBinding, dryRun bool, sample json.RawMessage) (json.RawMessage, error) {
	var options unsquery.ProgramOptions
	if unsmeta.Parse(call.Options, &options) != nil {
		return nil, repo.ErrInvalidArgument
	}
	options.ResourceKey = binding.Reference.Key() + ":" + binding.TopicBindingHash
	if err := unsquery.ValidateProgramOptions(binding.CollectionID, options); err != nil {
		return nil, repo.ErrInvalidArgument
	}
	var page unsquery.ProgramPage
	if dryRun {
		if err := unsquery.ValidateProgramCollection(ctx, binding.CollectionID, options, binding); err != nil {
			var validation *unsquery.ValidationError
			if errors.As(err, &validation) || errors.Is(err, unsmeta.ErrInvalid) {
				return nil, repo.ErrInvalidArgument
			}
			return nil, err
		}
		// A dry run explicitly supplies pages per query. Never fall back to the
		// database when a page is absent, even for an authorized readable handle.
		var fixture struct {
			Queries []struct {
				Options unsquery.ProgramOptions `json:"options"`
				Result  unsquery.ProgramPage    `json:"result"`
			} `json:"queries"`
		}
		if unsmeta.Parse(sample, &fixture) != nil {
			return nil, repo.ErrInvalidArgument
		}
		found := false
		wanted, _ := json.Marshal(options)
		for _, query := range fixture.Queries {
			given, _ := json.Marshal(query.Options)
			if string(wanted) == string(given) {
				if found {
					return nil, repo.ErrInvalidArgument
				}
				page, found = query.Result, true
			}
		}
		limit := options.Limit
		if limit == 0 {
			limit = 50
		}
		if !found || page.Items == nil || len(page.Items) > limit || page.HasMore != (page.NextCursor != "") {
			return nil, repo.ErrInvalidArgument
		}
	} else {
		var err error
		page, err = unsquery.QueryProgramCollection(ctx, binding.CollectionID, options, binding)
		if err != nil {
			var validation *unsquery.ValidationError
			if errors.As(err, &validation) || errors.Is(err, unsmeta.ErrInvalid) {
				return nil, repo.ErrInvalidArgument
			}
			return nil, err
		}
	}
	raw, err := json.Marshal(page)
	if err != nil {
		return nil, err
	}
	return programV2Sample(raw)
}

package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"backend/internal/domain/dataingest"
	"backend/internal/repo"
	"backend/internal/streamexec"
)

type programCheckpointSink interface {
	CommitProgramCheckpoint(context.Context, []dataingest.ProgramStreamEvent, repo.ProgramCheckpointCommit) (bool, error)
}

func (s *Service) commitProgramV2CheckpointTask(ctx context.Context, task repo.StreamTask, result streamexec.ProgramResult, lineage []int64, sink StreamSink, usesNATS bool) (streamexec.ProgramResult, error) {
	writer, ok := sink.(programCheckpointSink)
	if !ok {
		return result, ErrUnavailable
	}
	events := make([]dataingest.ProgramStreamEvent, 0, len(result.Outputs))
	sum := sha256.Sum256([]byte(task.Output))
	digest := hex.EncodeToString(sum[:])
	committed, commitErr := repo.NewSourceStreamRepo(ctx).ProgramCheckpointCommitted(ctx, task.ID, digest)
	if !committed && commitErr == nil {
		for ordinal, output := range result.Outputs {
			id, err := strconv.ParseInt(output.TopicID, 10, 64)
			if err != nil || id <= 0 || strconv.FormatInt(id, 10) != output.TopicID {
				commitErr = repo.ErrInvalidArgument
				break
			}
			node, err := repo.NewUnsRepo(ctx).GetUnsNode(ctx, id, false)
			if err != nil {
				commitErr = err
				break
			}
			var data any
			if err := json.Unmarshal(output.Data, &data); err != nil {
				commitErr = err
				break
			}
			events = append(events, dataingest.ProgramStreamEvent{Node: node, Event: dataingest.StreamEvent{
				MessageID: repo.ProgramOutputIdentity(task.ID, ordinal, id), SourceID: task.SourceID, StreamID: task.StreamID,
				VersionID: task.VersionID, TopicID: id, Data: data, ReceivedAt: time.UnixMilli(task.ReceivedAt), Lineage: append(append([]int64{}, lineage...), task.StreamID),
			}})
		}
		if commitErr == nil {
			commitErr = checkRequestExecution(ctx)
		}
		if commitErr == nil {
			_, commitErr = writer.CommitProgramCheckpoint(ctx, events, repo.ProgramCheckpointCommit{StreamID: task.StreamID, EntryID: task.EntryID,
				RunID: task.ID, Digest: digest, ExpectedRevision: result.Checkpoint.Revision, Value: result.Checkpoint.Value})
		}
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
	targets := make([]map[string]any, 0, len(result.Outputs))
	for ordinal, output := range result.Outputs {
		targets = append(targets, map[string]any{"topicId": output.TopicID, "ordinal": ordinal, "status": targetStatus})
	}
	summary["phase"], summary["targets"], summary["usesNATS"], summary["operationRepeated"] = "uns-commit", targets, usesNATS, false
	recordCommitError(summary, commitErr)
	summary["checkpoint"] = map[string]any{"stage": targetStatus, "atomicWithOutputs": true}
	encoded, _ := json.Marshal(summary)
	err = r.FinishProgramV2Task(ctx, task.ID, status, string(encoded), result.DurationMs)
	return result, errors.Join(commitErr, err)
}

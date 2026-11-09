package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
)

func (s *Service) invokeScript(ctx context.Context, stream repo.Stream, version repo.StreamVersion, key string, input any, timeoutMs int) (map[string]any, error) {
	return s.runScript(ctx, stream, version, key, input, timeoutMs, nil, 0)
}

func (s *Service) runScript(ctx context.Context, stream repo.Stream, version repo.StreamVersion, key string, input any, timeoutMs int, event *SourceInput, sourceID int64) (map[string]any, error) {
	var doc any
	if json.Unmarshal([]byte(version.Document), &doc) != nil {
		return nil, repo.ErrInvalidArgument
	}
	def, _, err := s.Validate(ctx, doc, 0, true)
	if err != nil {
		return nil, err
	}
	sub, err := parseSubscription(def.Script.Code)
	if err != nil {
		return nil, err
	}
	if (sub != nil) != (event != nil) {
		return nil, ErrSubscription
	}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 1024*1024 {
		return nil, invalid("input", "invalidOrTooLarge")
	}
	subject, _ := contextx.SubjectFrom(ctx)
	identity := sha256.Sum256([]byte(fmt.Sprintf("script:%d:%d:%d:%s", stream.ID, subject.UserID, subject.APIKeyID, key)))
	runID := hex.EncodeToString(identity[:])
	requestHash := sha256.Sum256(append([]byte(fmt.Sprintf("%d:", version.ID)), raw...))
	summary := map[string]any{"requestHash": hex.EncodeToString(requestHash[:]), "input": dataSummary(raw), "phase": "script", "usesNATS": false}
	if event != nil {
		summary["phase"] = "subscription"
		summary["usesNATS"] = true
		summary["resourceRef"] = event.ResourceRef
	}
	summaryJSON, _ := json.Marshal(summary)
	run := repo.StreamRun{ID: runID, SourceID: sourceID, StreamID: stream.ID, VersionID: version.ID, Model: string(ScriptMode), Status: "processing", MessageID: key, Summary: string(summaryJSON), CreatedAt: time.Now().UnixMilli()}
	r := repo.NewSourceStreamRepo(ctx)
	created, err := r.AdmitScriptRun(ctx, run)
	if err != nil {
		return nil, err
	}
	if !created {
		previous, err := r.Run(ctx, runID)
		if err != nil {
			return nil, err
		}
		var old map[string]any
		if json.Unmarshal([]byte(previous.Summary), &old) != nil || old["requestHash"] != summary["requestHash"] {
			return nil, repo.ErrConflict
		}
		if event != nil && previous.Status != "succeeded" && previous.Status != "dropped" {
			return nil, ErrUnknownResult
		}
		return map[string]any{"run": runView(previous), "replayed": true, "operationRepeated": false, "resultAvailable": false}, nil
	}
	started := time.Now()
	operationCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	runContext := map[string]any{"streamId": stream.ID, "versionId": version.ID, "runId": runID, "dryRun": false}
	if event != nil {
		for key, value := range subscriptionContext(sourceID, *event, false) {
			runContext[key] = value
		}
	}
	result, executionErr := s.transformScript(operationCtx, def, input, runContext)
	status := "succeeded"
	if executionErr != nil {
		status = "failed"
		summary["error"] = "SourceStream.executionFailed"
	} else if result.Drop {
		status = "dropped"
	} else if output, e := json.Marshal(result.Data); e == nil {
		summary["output"] = dataSummary(output)
	}
	summary["status"] = status
	summaryJSON, _ = json.Marshal(summary)
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer finishCancel()
	if err = r.FinishRun(finishCtx, runID, status, string(summaryJSON), time.Since(started).Milliseconds()); err != nil {
		return nil, ErrUnknownResult
	}
	if executionErr != nil {
		return nil, executionErr
	}
	return map[string]any{"runId": runID, "versionId": version.ID, "status": status, "result": result, "data": result.Data, "operationRepeated": false}, nil
}

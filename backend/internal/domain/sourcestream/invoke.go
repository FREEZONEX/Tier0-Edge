package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
)

// Invoke is synchronous and deliberately has no message-bus dependency.
// The intent is committed before dispatch; retries never repeat an operation.
func (s *Service) Invoke(ctx context.Context, id, expectedVersion int64, key string, input any, timeoutMs int) (map[string]any, error) {
	return s.InvokeEntry(ctx, id, expectedVersion, "", key, input, timeoutMs)
}

func (s *Service) InvokeEntry(ctx context.Context, id, expectedVersion int64, entryID, key string, input any, timeoutMs int) (map[string]any, error) {
	if s.executor == nil {
		return nil, ErrUnavailable
	}
	if strings.TrimSpace(key) == "" || len(key) > 128 || timeoutMs < 100 || timeoutMs > 30000 {
		return nil, invalid("invoke", "invalid")
	}
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || !contextx.HasResource(subject, "uns.write") {
		return nil, ErrForbidden
	}
	r := repo.NewSourceStreamRepo(ctx)
	stream, err := r.Stream(ctx, id)
	if err != nil {
		return nil, err
	}
	if stream.Status != "enabled" || stream.ActiveVersion != expectedVersion || expectedVersion <= 0 {
		return nil, repo.ErrConflict
	}
	version, err := r.Version(ctx, id, expectedVersion)
	if err != nil {
		return nil, err
	}
	if stream.Model != string(ProgramAccess) || stream.SourceID != 0 {
		return nil, repo.ErrInvalidArgument
	}
	return s.invokeProgramV2(ctx, stream, version, entryID, key, input, timeoutMs)
}

func (s *Service) invokeProgram(ctx context.Context, stream repo.Stream, version repo.StreamVersion, key string, input any, timeoutMs int) (map[string]any, error) {
	var def StreamDefinition
	if json.Unmarshal([]byte(version.Document), &def) != nil || def.Program == nil || def.Program.Trigger.Kind != ManualTrigger || def.ValidateDeployment(0, SourceDefinition{}) != nil {
		return nil, repo.ErrInvalidArgument
	}
	grants, host, err := s.deployedProgramHost(ctx, stream.ID, version.ID)
	if err != nil {
		return nil, err
	}
	subject, _ := contextx.SubjectFrom(ctx)
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 1024*1024 {
		return nil, invalid("input", "invalidOrTooLarge")
	}
	requestHash := sha256.Sum256(append([]byte(fmt.Sprintf("%d:", version.ID)), raw...))
	identity := sha256.Sum256([]byte(fmt.Sprintf("sync:%d:%d:%d:%s", stream.ID, subject.UserID, subject.APIKeyID, key)))
	runID := hex.EncodeToString(identity[:])
	summary := map[string]any{"requestHash": hex.EncodeToString(requestHash[:]), "input": dataSummary(raw), "phase": "accepted", "transport": "synchronous", "usesNATS": false}
	summaryJSON, _ := json.Marshal(summary)
	r := repo.NewSourceStreamRepo(ctx)
	run := repo.StreamRun{ID: runID, StreamID: stream.ID, VersionID: version.ID, Model: string(RequestReply), Status: "processing", MessageID: key, Summary: string(summaryJSON), CreatedAt: time.Now().UnixMilli()}
	created, err := r.AdmitProgramRun(ctx, run)
	if err != nil {
		return nil, err
	}
	if !created {
		existing, err := r.Run(ctx, runID)
		if err != nil {
			return nil, err
		}
		var old map[string]any
		if json.Unmarshal([]byte(existing.Summary), &old) != nil || old["requestHash"] != summary["requestHash"] {
			return nil, repo.ErrConflict
		}
		return map[string]any{"run": runView(existing), "replayed": true, "operationRepeated": false, "resultAvailable": false}, nil
	}
	started := time.Now()
	opCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	metadata := map[string]any{"streamId": stream.ID, "versionId": version.ID, "runId": runID, "dryRun": false, "trigger": ManualTrigger}
	result, executeErr := s.transformProgram(opCtx, def.Script, input, metadata, grants, host)
	if executeErr == nil {
		_, executeErr = def.OutputTargets(result)
	}
	if executeErr == nil && !result.Drop && def.ResponseScript != nil {
		result, executeErr = s.transformProgram(opCtx, *def.ResponseScript, result.Data, metadata, grants, host)
		if executeErr == nil {
			_, executeErr = def.OutputTargets(result)
		}
	}
	status := "succeeded"
	if executeErr != nil {
		status = "failed"
	} else if result.Drop {
		status = "dropped"
	}
	summary["phase"], summary["status"] = "script", status
	if executeErr != nil {
		summary["error"] = "SourceStream.executionFailed"
	} else if raw, err := json.Marshal(result.Data); err == nil {
		summary["output"] = dataSummary(raw)
	}
	summaryJSON, _ = json.Marshal(summary)
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer finishCancel()
	if err := repo.NewSourceStreamRepo(finishCtx).FinishRun(finishCtx, runID, status, string(summaryJSON), time.Since(started).Milliseconds()); err != nil {
		return nil, ErrUnknownResult
	}
	if executeErr != nil {
		return nil, executeErr
	}
	if result.Drop {
		result.Data = nil
	}
	return map[string]any{"runId": runID, "versionId": version.ID, "status": status, "data": result.Data, "operationRepeated": false, "usesNATS": false}, nil
}

// Run history keeps bounded fingerprints, not potentially sensitive payloads.
func dataSummary(raw []byte) map[string]any {
	hash := sha256.Sum256(raw)
	return map[string]any{"bytes": len(raw), "sha256": hex.EncodeToString(hash[:])}
}

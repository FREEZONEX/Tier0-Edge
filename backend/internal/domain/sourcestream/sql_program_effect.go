package sourcestream

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"backend/internal/contextx"
	"backend/internal/repo"
	"backend/internal/streamexec"
)

// SQL Server lacks database-enforced read-only transactions. Treat a Source
// owner's custom statement as an effect, retaining unknown-outcome fencing.
func (s *Service) programV2SQLCall(ctx context.Context, source repo.Source, def SourceDefinition, call streamexec.ProgramCall, runID, entryID string) (json.RawMessage, error) {
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || !contextx.HasResource(subject, "uns.source.manage") || !contextx.HasResource(subject, "uns.stream.execute") || subject.AuthType == "apiKey" && subject.APIKeyPermission == "read_only" {
		return nil, ErrForbidden
	}
	request, err := json.Marshal(struct {
		Call           streamexec.ProgramCall
		ConfigRevision int64
		Definition     SourceDefinition
	}{call, source.ConfigRevision, def})
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(request)
	opHash := sha256.Sum256([]byte(call.SourceID + "\x00" + call.ResourceRef))
	effect := repo.StreamProgramEffect{RunID: runID, EntryID: entryID, CallID: strconv.Itoa(call.ID), OpID: "source.sql:" + hex.EncodeToString(opHash[:]), RequestHash: hex.EncodeToString(hash[:])}
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
	data, dispatchErr := s.programNativePage(ctx, source, def, call, false)
	status, result := "completed", string(data)
	if dispatchErr != nil {
		status, result = "unknown", `{"status":"unknown"}`
	}
	terminal, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if err := repo.NewSourceStreamRepo(terminal).FinishProgramEffect(terminal, runID, entryID, effect.CallID, effect.RequestHash, status, result); err != nil || dispatchErr != nil {
		return nil, ErrUnknownResult
	}
	return data, nil
}

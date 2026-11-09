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

// Gateway invokes a named request entry directly. Neither its device operation
// nor its response goes through NATS; buffered Topic writes use the UNS sink.
func (s *Service) invokeProgramV2(ctx context.Context, stream repo.Stream, version repo.StreamVersion, entryID, key string, input any, timeoutMs int) (map[string]any, error) {
	manifest, err := version.ProgramV2Manifest()
	if err != nil {
		return nil, err
	}
	if entryID == "" {
		for _, entry := range manifest.Entries {
			if entry.Kind != "request" {
				continue
			}
			if entryID != "" {
				return nil, invalid("entryId", "required")
			}
			entryID = entry.ID
		}
	}
	found := false
	for _, entry := range manifest.Entries {
		found = found || (entry.ID == entryID && entry.Kind == "request")
	}
	if !found {
		return nil, invalid("entryId", "requestEntryRequired")
	}
	subject, ok := contextx.SubjectFrom(ctx)
	if !ok || !contextx.HasResource(subject, "uns.stream.execute") {
		return nil, ErrForbidden
	}
	identity := sha256.Sum256([]byte(fmt.Sprintf("program-sync:%d:%d:%s", subject.UserID, subject.APIKeyID, key)))
	messageID := hex.EncodeToString(identity[:])
	operationCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	ref := ExecutionRef{StreamID: stream.ID, VersionID: version.ID, EntryID: entryID}
	task, replayed, err := s.prepareProgramV2Task(operationCtx, messageID, ref, input, time.Now().UnixMilli(), "request", 0, true, nil)
	if err != nil {
		return nil, err
	}
	result, err := s.commitProgramV2Task(operationCtx, task, nil, s.programSink, false)
	if err != nil {
		return nil, err
	}
	run, err := repo.NewSourceStreamRepo(ctx).Run(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	var data any
	if len(result.Data) > 0 {
		if json.Unmarshal(result.Data, &data) != nil {
			return nil, repo.ErrInvalidArgument
		}
	}
	return map[string]any{"run": runView(run), "result": map[string]any{"data": data, "outputs": result.Outputs, "logs": result.Logs, "durationMs": result.DurationMs},
		"entryId": entryID, "replayed": replayed, "operationRepeated": false, "resultAvailable": true, "usesNATS": false}, nil
}

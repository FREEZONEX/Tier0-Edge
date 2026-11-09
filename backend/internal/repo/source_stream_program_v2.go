package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Persisted hash domain tags below are immutable identity bytes, not runtime
// model selectors. Changing them would break receipt/effect deduplication.
// Structured JSON values prevent delimiter ambiguity in execution IDs.
func programIdentity(parts ...any) string {
	encoded, _ := json.Marshal(parts)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// ProgramRunIdentity freezes the selected entry as well as the version. The
// caller supplies its stable message/idempotency identity, never a display name.
func ProgramRunIdentity(messageID string, streamID, versionID int64, entryID string) string {
	return programIdentity("program-run-v2", messageID, streamID, versionID, entryID)
}

func ProgramTaskIdentity(messageID string, streamID, versionID int64, entryID string) string {
	return ProgramRunIdentity(messageID, streamID, versionID, entryID)
}

// Ordinal is part of the receipt: two writes to one Topic in one execution must
// not collapse into a single write. Callers must retain original output order.
func ProgramOutputIdentity(runID string, outputOrdinal int, topicID int64) string {
	return programIdentity("program-output-v2", runID, outputOrdinal, topicID)
}

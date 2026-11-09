package sourcestreamworker

import (
	"encoding/json"
	"errors"

	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"backend/internal/streamexec"
)

var errIngressJSON = errors.New("SourceStream.invalidMessageJSON")

// Decode errors include invalid timestamps, not only JSON syntax errors.
func decodeIngress(data []byte, value any) error {
	if err := json.Unmarshal(data, value); err != nil {
		return errIngressJSON
	}
	return nil
}

// Fixed reason codes never include user payloads, script text or credentials.
// Unknown infrastructure failures remain retryable; ambiguity never retries I/O.
func failureDisposition(err error) (terminal bool, class, reason string) {
	if errors.Is(err, sourcestream.ErrUnknownResult) {
		return true, "unknown", "executionResultUnknown"
	}
	// Multiple targets can fail differently. A permanent sibling must not
	// suppress recovery of another target whose database is temporarily down.
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		class, reason = "permanent", "invalidContract"
		for _, cause := range joined.Unwrap() {
			if cause == nil {
				continue
			}
			terminal, class, reason = failureDisposition(cause)
			if !terminal {
				return false, class, reason
			}
		}
		return true, class, reason
	}
	// Inspect only this error's own marker. errors.Is on an outer wrapper
	// could find one failed sibling inside a join and hide a retryable one.
	marker, marked := err.(interface{ Is(error) bool })
	if err == sourcestream.ErrExecutionFailed || marked && marker.Is(sourcestream.ErrExecutionFailed) {
		return true, "permanent", "recordedExecutionFailed"
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return failureDisposition(wrapped.Unwrap())
	}
	switch {
	case errors.Is(err, errIngressJSON):
		return true, "permanent", "invalidJSON"
	case errors.Is(err, repo.ErrInvalidArgument):
		return true, "permanent", "invalidContract"
	case errors.Is(err, unsmeta.ErrInvalid), errors.Is(err, unsmeta.ErrSchema), errors.Is(err, unsmeta.ErrData):
		return true, "permanent", "invalidResourceData"
	case errors.Is(err, streamexec.ErrInput), errors.Is(err, streamexec.ErrSyntax), errors.Is(err, streamexec.ErrOutput):
		return true, "permanent", "invalidScriptContract"
	default:
		return false, "retryable", "executionFailed"
	}
}

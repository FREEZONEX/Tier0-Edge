package sourcestream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"backend/internal/repo"
	"backend/internal/streamexec"
	"github.com/stretchr/testify/require"
)

func TestProgramV2SourceCallTimeoutIndependentOfLegacyLimit(t *testing.T) {
	for _, raw := range []string{"", `{}`, `{"timeoutMs":5000}`} {
		timeout, err := programV2Timeout(json.RawMessage(raw))
		require.NoError(t, err)
		require.Equal(t, int(streamexec.DefaultSourceCallTimeout/time.Millisecond), timeout)
	}
	timeout, err := programV2Timeout(json.RawMessage(`{"timeoutMs":1}`))
	require.NoError(t, err)
	require.Equal(t, 1, timeout)
	for _, raw := range []string{`{"timeoutMs":0}`, `{"timeoutMs":45001}`, `{"timeoutMs":60000}`, `{"timeoutMs":1,"url":"http://disallowed"}`} {
		_, err := programV2Timeout(json.RawMessage(raw))
		require.ErrorIs(t, err, streamexec.ErrSourceDenied)
	}
}

// Diagnostic codes must remain a bounded vocabulary, never raw driver errors.
func TestProgramV2CallCodeRedactsDriverDetails(t *testing.T) {
	const privateDetail = "opc.tcp://private-host:4840?password=not-for-logs"
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"deadline", context.DeadlineExceeded, "timeout"},
		{"executor timeout", streamexec.ErrTimeout, "timeout"},
		{"cancelled", context.Canceled, "cancelled"},
		{"inactive", errInactivePlan, "inactive-reference"},
		{"permission", ErrForbidden, "denied"},
		{"source denied", streamexec.ErrSourceDenied, "denied"},
		{"unknown effect", ErrUnknownResult, "result-unknown"},
		{"credential", ErrCredential, "credential-unavailable"},
		{"conflict", repo.ErrConflict, "configuration-conflict"},
		{"invalid", repo.ErrInvalidArgument, "invalid-request"},
		{"unavailable", ErrUnavailable, "unavailable"},
		{"driver", errors.New(privateDetail), "source-call-failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, err := range []error{test.err, fmt.Errorf("%s: %w", privateDetail, test.err)} {
				got := programV2CallCode(err)
				if got != test.want || strings.Contains(got, privateDetail) {
					t.Fatalf("unexpected diagnostic code %q, want %q", got, test.want)
				}
			}
		})
	}
}

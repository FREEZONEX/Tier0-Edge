package sourcestreamworker

// 验证永久合同错误立即隔离，基础设施故障有限重试，错误原因不包含输入。
import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"backend/internal/domain/sourcestream"
	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"github.com/stretchr/testify/require"
)

func TestFailureDisposition(t *testing.T) {
	for _, cause := range []error{errIngressJSON, repo.ErrInvalidArgument, unsmeta.ErrData} {
		terminal, class, reason := failureDisposition(fmt.Errorf("private detail: %w", cause))
		require.True(t, terminal)
		require.Equal(t, "permanent", class)
		require.NotContains(t, reason, "private")
	}
	terminal, class, reason := failureDisposition(sourcestream.ErrUnknownResult)
	require.True(t, terminal)
	require.Equal(t, "unknown", class)
	require.Equal(t, "executionResultUnknown", reason)
	for _, cause := range []error{context.DeadlineExceeded, errors.New("database unavailable"), sourcestream.ErrUnavailable} {
		terminal, class, _ := failureDisposition(cause)
		require.False(t, terminal)
		require.Equal(t, "retryable", class)
	}
	terminal, class, _ = failureDisposition(fmt.Errorf("targets: %w", errors.Join(repo.ErrInvalidArgument, errors.New("database unavailable"))))
	require.False(t, terminal, "a permanent target must not suppress retry of a transient sibling")
	require.Equal(t, "retryable", class)
	terminal, class, reason = failureDisposition(fmt.Errorf("private details: %w", sourcestream.ErrExecutionFailed))
	require.True(t, terminal)
	require.Equal(t, "permanent", class)
	require.Equal(t, "recordedExecutionFailed", reason)
	terminal, class, _ = failureDisposition(errors.Join(sourcestream.ErrExecutionFailed, errors.New("database unavailable")))
	require.False(t, terminal, "a recorded failed sibling must not suppress a recoverable output")
	require.Equal(t, "retryable", class)
	terminal, class, _ = failureDisposition(fmt.Errorf("targets: %w", errors.Join(sourcestream.ErrExecutionFailed, errors.New("database unavailable"))))
	require.False(t, terminal, "an outer wrapper must not hide the transient sibling inside a joined error")
	require.Equal(t, "retryable", class)
	terminal, class, reason = failureDisposition(errors.Join(repo.ErrInvalidArgument, sourcestream.ErrExecutionFailed))
	require.True(t, terminal)
	require.Equal(t, "permanent", class)
	require.Equal(t, "recordedExecutionFailed", reason)
}

func TestDecodeIngressPermanentErrors(t *testing.T) {
	for _, data := range []string{`{`, `{"at":"not-a-time"}`, `{"at":4}`} {
		var value struct {
			At time.Time `json:"at"`
		}
		require.ErrorIs(t, decodeIngress([]byte(data), &value), errIngressJSON)
	}
	var value struct {
		At time.Time `json:"at"`
	}
	require.NoError(t, decodeIngress([]byte(`{"at":"2026-10-15T00:00:00Z"}`), &value))
}

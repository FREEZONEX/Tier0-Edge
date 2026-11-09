package sourcestream

// 纯分类测试：混合目标错误不可吞掉可恢复故障；原因码不含原始错误。
import (
	"context"
	"errors"
	"fmt"
	"testing"

	"backend/internal/domain/unsmeta"
	"backend/internal/repo"
	"github.com/stretchr/testify/require"
)

func TestProgramOutputRecoveryDispositionKeepsTransientSiblings(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, errors.New("database temporarily unavailable"),
		errors.Join(unsmeta.ErrData, context.DeadlineExceeded), fmt.Errorf("outer: %w", errors.Join(repo.ErrConflict, context.Canceled))} {
		permanent, reason := programOutputRecoveryDisposition(cause)
		require.False(t, permanent)
		require.Equal(t, repo.ProgramRecoveryUnavailable, reason)
	}
	for _, cause := range []error{repo.ErrConflict, unsmeta.ErrData, fmt.Errorf("outer: %w", errors.Join(unsmeta.ErrInvalid, repo.ErrInvalidArgument))} {
		permanent, reason := programOutputRecoveryDisposition(cause)
		require.True(t, permanent)
		require.Contains(t, []string{repo.ProgramRecoveryInvalid, repo.ProgramRecoveryConflict}, reason)
	}
}

package sourcestream

// 纯错误映射测试：已确认执行失败保持422，未知/权限/基础设施状态不混淆。
import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	domain "backend/internal/domain/sourcestream"
	respx "backend/internal/httpx"
	"github.com/stretchr/testify/require"
)

func TestRecordedExecutionFailureHTTPStatus(t *testing.T) {
	for _, tc := range []struct {
		cause  error
		status int
	}{
		{domain.ErrExecutionFailed, http.StatusUnprocessableEntity},
		{domain.ErrUnknownResult, http.StatusGatewayTimeout},
		{domain.ErrForbidden, http.StatusForbidden},
		{domain.ErrRemoteControlDisabled, http.StatusForbidden},
		{domain.ErrUnavailable, http.StatusServiceUnavailable},
	} {
		t.Run(tc.cause.Error(), func(t *testing.T) {
			var translated *respx.HTTPError
			require.ErrorAs(t, apiError(tc.cause), &translated)
			require.Equal(t, tc.status, translated.Status)
			if errors.Is(tc.cause, domain.ErrExecutionFailed) {
				require.ErrorAs(t, apiError(fmt.Errorf("private detail: %w", tc.cause)), &translated)
				require.Equal(t, tc.status, translated.Status)
				require.Equal(t, domain.ErrExecutionFailed.Error(), translated.Error())
			}
		})
	}
}

func TestRemoteControlDisabledHTTPStatus(t *testing.T) {
	var translated *respx.HTTPError
	require.ErrorAs(t, controlAPIError(fmt.Errorf("wrapped: %w", domain.ErrRemoteControlDisabled)), &translated)
	require.Equal(t, http.StatusForbidden, translated.Status)
	require.Equal(t, "EDGE_CONTROL_DISABLED", translated.Error())
}

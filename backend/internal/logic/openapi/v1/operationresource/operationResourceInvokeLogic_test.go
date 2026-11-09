package operationresource_test

// 调用薄逻辑不能吞掉执行服务不可用；真实执行、拒绝和重放由 execute_test 覆盖。
import (
	"context"
	"testing"

	respx "backend/internal/httpx"
	logic "backend/internal/logic/openapi/v1/operationresource"
	"backend/internal/svc"
	"backend/internal/types"
	"github.com/stretchr/testify/require"
)

func TestOperationResourceInvokeUnavailable(t *testing.T) {
	response, err := logic.NewOperationResourceInvokeLogic(context.Background(), &svc.ServiceContext{}).OperationResourceInvoke(&types.OperationResourceInvokeReq{
		ControlCode: "device.start", IdempotencyKey: "saved-intent", Payload: map[string]any{"deviceId": "one"},
	})
	require.Nil(t, response)
	var failure *respx.HTTPError
	require.ErrorAs(t, err, &failure)
}

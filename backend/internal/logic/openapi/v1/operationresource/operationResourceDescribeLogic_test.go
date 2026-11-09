package operationresource_test

// 详情薄逻辑保留错误传播，不把未装配的服务返回为成功空对象。
import (
	"context"
	"testing"

	respx "backend/internal/httpx"
	logic "backend/internal/logic/openapi/v1/operationresource"
	"backend/internal/svc"
	"backend/internal/types"
	"github.com/stretchr/testify/require"
)

func TestOperationResourceDescribeUnavailable(t *testing.T) {
	response, err := logic.NewOperationResourceDescribeLogic(context.Background(), &svc.ServiceContext{}).OperationResourceDescribe(&types.OperationResourceDetailReq{ControlCode: "device.start"})
	require.Nil(t, response)
	var failure *respx.HTTPError
	require.ErrorAs(t, err, &failure)
}

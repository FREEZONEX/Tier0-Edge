package operationresource_test

// 列表薄逻辑必须传播服务不可用；真实权限与成功路径由 execute_test 覆盖。
import (
	"context"
	"testing"

	respx "backend/internal/httpx"
	logic "backend/internal/logic/openapi/v1/operationresource"
	"backend/internal/svc"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOperationResourceListUnavailable(t *testing.T) {
	response, err := logic.NewOperationResourceListLogic(context.Background(), &svc.ServiceContext{}).OperationResourceList(&types.OperationResourceListReq{})
	require.Nil(t, response)
	var failure *respx.HTTPError
	require.ErrorAs(t, err, &failure)
}

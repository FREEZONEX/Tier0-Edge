package sourcestream

// 真实数据库验证外部 facade 复用配置、版本检查和列表筛选，不引入业务 mock。
import (
	"backend/internal/domain/sourcestream"
	"backend/internal/svc/svctest"
	"backend/internal/testkit/scripttest"
	"backend/internal/types"
	"encoding/base64"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSourceStreamFacade(t *testing.T) {
	svc := svctest.NewTestServiceContext(t)
	svc.App.SourceStream = sourcestream.New(nil, scripttest.New(t), base64.StdEncoding.EncodeToString(make([]byte, 32)))
	ctx := svctest.CtxWithUser(1)
	doc := map[string]any{"name": "agent-mqtt", "protocol": "mqtt", "endpoint": "mqtt://broker:1883", "runtimeLocation": "local", "mqtt": map[string]any{"clientId": "test"}}
	_, err := execute(ctx, svc, "OpenapiSourceValidate", &types.SourceValidateReq{Document: doc})
	require.NoError(t, err)
	created, err := execute(ctx, svc, "OpenapiSourceCreate", &types.SourceSaveReq{Document: doc})
	require.NoError(t, err)
	row := created.Data.(map[string]any)
	id := row["id"].(int64)
	rev := row["revision"].(int64)
	_, err = execute(ctx, svc, "OpenapiSourceUpdate", &types.SourceSaveReq{SourceId: id, ExpectedRevision: rev + 1, Document: doc})
	require.Error(t, err)
	list, err := execute(ctx, svc, "OpenapiSourceList", &types.OpenapiSourceListReq{Search: "agent-mqtt", Page: 1, Size: 1})
	require.NoError(t, err)
	require.EqualValues(t, 1, list.Data.(map[string]any)["total"])
	detail, err := execute(ctx, svc, "OpenapiSourceDetail", &types.SourceIDReq{SourceId: id})
	require.NoError(t, err)
	require.NotContains(t, detail.Data, "secret")
	_, err = execute(ctx, svc, "not-an-action", &types.SourceStreamEmpty{})
	require.Error(t, err)
}

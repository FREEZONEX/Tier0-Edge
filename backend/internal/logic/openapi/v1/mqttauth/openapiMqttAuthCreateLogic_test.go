// openapiMqttAuthCreateLogic_test.go — OpenapiMqttAuthCreateLogic 测试。
//
// 测试目标：OpenAPI 创建 MQTT 设备客户端凭据的入口逻辑。
// 正常路径：创建设备客户端成功，返回 ID/Name/ClientID/Username/Password，
//   且数据库落一条 device 类型 ConnectClient。
// 异常情况：nil 请求返回 400（ErrInvalid）；空 Name 返回 400；DB 出错透传。
package mqttauth

import (
	"strconv"
	"testing"

	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiMqttAuthCreate_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewOpenapiMqttAuthCreateLogic(ctx, svcCtx)
	resp, err := l.OpenapiMqttAuthCreate(&types.OpenapiMqttAuthCreateReq{
		Name:                        "sensor-01",
		Description:                 "test device",
		ClientIDRandomSuffixEnabled: true,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	data, ok := resp.Data.(types.OpenapiMqttAuthCreateResp)
	require.True(t, ok)
	require.Positive(t, data.Id)
	require.Equal(t, "sensor-01", data.Name)
	require.Equal(t, strconv.FormatInt(data.Id, 10), data.ClientID)
	require.Equal(t, strconv.FormatInt(data.Id, 10), data.Username)
	require.NotEmpty(t, data.Password)
	require.True(t, data.ClientIDRandomSuffixEnabled)

	// 落库校验：一条 device 类型客户端，凭据与返回一致。
	var row repo.ConnectClient
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("id = ?", data.Id).First(&row).Error)
	require.Equal(t, repo.ConnectClientTypeDevice, row.Type)
	require.Equal(t, data.ClientID, row.ClientID)
	require.Equal(t, data.Password, row.Password)
	require.True(t, row.ClientIDRandomSuffixEnabled)
	require.Equal(t, int64(1), row.CreatedBy)
}

func TestOpenapiMqttAuthCreate_NilRequest(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewOpenapiMqttAuthCreateLogic(ctx, svcCtx)
	_, err := l.OpenapiMqttAuthCreate(nil)
	requireHTTPStatus(t, err, 400)
}

func TestOpenapiMqttAuthCreate_EmptyName(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewOpenapiMqttAuthCreateLogic(ctx, svcCtx)
	_, err := l.OpenapiMqttAuthCreate(&types.OpenapiMqttAuthCreateReq{Name: "  "})
	requireHTTPStatus(t, err, 400)
}

func TestOpenapiMqttAuthCreate_EmptyDescriptionOK(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewOpenapiMqttAuthCreateLogic(ctx, svcCtx)
	resp, err := l.OpenapiMqttAuthCreate(&types.OpenapiMqttAuthCreateReq{Name: "no-desc"})
	require.NoError(t, err)
	data := resp.Data.(types.OpenapiMqttAuthCreateResp)
	require.False(t, data.ClientIDRandomSuffixEnabled)
}

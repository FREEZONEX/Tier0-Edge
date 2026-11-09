// openapiMqttAuthDeleteLogic_test.go — OpenapiMqttAuthDeleteLogic 测试。
//
// 测试目标：OpenAPI 删除 MQTT 设备客户端凭据的入口逻辑。
// 正常路径：删除已存在的 device 客户端返回 success=true 且落库软删除。
// 异常情况：nil 请求或 id<=0 返回 400；删除非 device 类型客户端（如 flow 客户端）
//   按 domain 行为 success=true 但不删除（幂等）；不存在 id 同样 success=true。
package mqttauth

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiMqttAuthDelete_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	// 先创建一条 device 客户端。
	created, err := NewOpenapiMqttAuthCreateLogic(ctx, svcCtx).OpenapiMqttAuthCreate(
		&types.OpenapiMqttAuthCreateReq{Name: "delete-me"})
	require.NoError(t, err)
	id := created.Data.(types.OpenapiMqttAuthCreateResp).Id

	l := NewOpenapiMqttAuthDeleteLogic(ctx, svcCtx)
	resp, err := l.OpenapiMqttAuthDelete(&types.OpenapiMqttAuthDeleteReq{Id: id})
	require.NoError(t, err)
	require.True(t, resp.Data.(types.OpenapiMqttAuthDeleteResp).Success)

	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().
		Model(&repo.ConnectClient{}).Where("id = ? AND deleted_time = 0", id).Count(&count).Error)
	require.Zero(t, count)
}

func TestOpenapiMqttAuthDelete_NotFound(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewOpenapiMqttAuthDeleteLogic(ctx, svcCtx)
	resp, err := l.OpenapiMqttAuthDelete(&types.OpenapiMqttAuthDeleteReq{Id: 999999})
	require.NoError(t, err)
	require.True(t, resp.Data.(types.OpenapiMqttAuthDeleteResp).Success)
}

func TestOpenapiMqttAuthDelete_NilRequest(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewOpenapiMqttAuthDeleteLogic(ctx, svcCtx)
	_, err := l.OpenapiMqttAuthDelete(nil)
	requireHTTPStatus(t, err, 400)
}

func TestOpenapiMqttAuthDelete_InvalidID(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewOpenapiMqttAuthDeleteLogic(ctx, svcCtx)
	_, err := l.OpenapiMqttAuthDelete(&types.OpenapiMqttAuthDeleteReq{Id: 0})
	requireHTTPStatus(t, err, 400)
}

func TestOpenapiMqttAuthDelete_NonDeviceTypeIsIdempotent(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	// 手工插入一条 flow 类型客户端（type=2）。
	row := repo.ConnectClient{ID: 123456, Name: "flow-x", Type: repo.ConnectClientTypeFlow,
		ClientID: "123456", Username: "123456"}
	require.NoError(t, svcCtx.App.Store.CommonDB().Create(&row).Error)

	l := NewOpenapiMqttAuthDeleteLogic(ctx, svcCtx)
	resp, err := l.OpenapiMqttAuthDelete(&types.OpenapiMqttAuthDeleteReq{Id: 123456})
	require.NoError(t, err)
	require.True(t, resp.Data.(types.OpenapiMqttAuthDeleteResp).Success)

	// flow 客户端不受影响。
	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().
		Model(&repo.ConnectClient{}).Where("id = ? AND deleted_time = 0", 123456).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

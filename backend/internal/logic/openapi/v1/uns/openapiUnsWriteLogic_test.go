// openapiUnsWriteLogic_test.go — 测试。
//
// 测试目标：OpenAPI 写入节点数据 入口。
// 正常路径：命中节点写入成功。
// 异常情况：空 writes/非法 qos/不存在 topic。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

// TestOpenapiUnsWrite_MqttNotInitialized 覆盖 svctest 下写路径：
// dataingest 未连 MQTT broker（无 client / publishOverride），
// WriteOpenAPIValue 返回 "mqtt client is not initialized"，行级 success=false。
// 真实 MQTT 发布成功路径需要外部 broker，未覆盖。
func TestOpenapiUnsWrite_MqttNotInitialized(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	createOpenapiUnsFileNode(t, svcCtx, "write-node")

	l := NewOpenapiUnsWriteLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsWrite(&types.WriteReq{
		Writes: []types.WriteItem{{Topic: "State/write-node", Value: "42"}},
		Qos:    1,
	})
	require.NoError(t, err)
	body := resp.Data.(map[string]any)
	require.Equal(t, false, body["success"])
	results := body["results"].([]map[string]any)
	require.Len(t, results, 1)
	require.Equal(t, false, results[0]["success"])
	require.Equal(t, int64(500), results[0]["error"].(map[string]any)["code"])
}

func TestOpenapiUnsWrite_EmptyWrites(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsWriteLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsWrite(&types.WriteReq{})
	require.Error(t, err)
}

func TestOpenapiUnsWrite_InvalidQos(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsWriteLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsWrite(&types.WriteReq{Writes: []types.WriteItem{{Topic: "x"}}, Qos: 9})
	require.Error(t, err)
}

func TestOpenapiUnsWrite_TopicNotFound(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsWriteLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsWrite(&types.WriteReq{
		Writes: []types.WriteItem{{Topic: "State/ghost", Value: "1"}},
	})
	require.NoError(t, err)
	body := resp.Data.(map[string]any)
	require.Equal(t, false, body["success"])
	results := body["results"].([]map[string]any)
	require.Equal(t, int64(404), results[0]["error"].(map[string]any)["code"])
}
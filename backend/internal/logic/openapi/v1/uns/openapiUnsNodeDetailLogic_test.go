// openapiUnsNodeDetailLogic_test.go — OpenapiUnsNodeDetailLogic 测试。
//
// 测试目标：OpenAPI 查询 UNS 节点详情入口。
// 正常路径：按 ID 查已创建文件节点，返回归一化 data。
// 异常情况：不存在节点返回 404。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsNodeDetail_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	created := createOpenapiUnsFileNode(t, svcCtx, "detail-node")
	nodeID := created["id"].(int64)

	l := NewOpenapiUnsNodeDetailLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsNodeDetail(&types.NodeIdReq{NodeId: nodeID})
	require.NoError(t, err)
	data := resp.Data.(map[string]any)
	// normalizeOpenapiUnsNode 将 type 归一化为大写（file→TOPIC），topicType 转大写。
	require.Equal(t, "TOPIC", data["type"])
	require.Equal(t, "STATE", data["topicType"])
	require.Equal(t, "State/detail-node", data["namespace"])
}

func TestOpenapiUnsNodeDetail_NotFound(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsNodeDetailLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsNodeDetail(&types.NodeIdReq{NodeId: 99999999})
	requireHTTPStatus(t, err, 404)
}

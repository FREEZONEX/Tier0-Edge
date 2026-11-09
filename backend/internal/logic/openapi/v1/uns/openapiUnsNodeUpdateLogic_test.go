// openapiUnsNodeUpdateLogic_test.go — OpenapiUnsNodeUpdateLogic 测试。
//
// 测试目标：OpenAPI 更新 UNS 节点入口。
// 正常路径：更新显示名/描述/字段，返回归一化 data。
// 异常情况：不存在节点返回 404。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsNodeUpdate_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	created := createOpenapiUnsFileNode(t, svcCtx, "update-node")
	nodeID := created["id"].(int64)

	l := NewOpenapiUnsNodeUpdateLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsNodeUpdate(&types.OpenapiUnsNodeSaveReq{
		NodeId:      nodeID,
		Name:        "update-node",
		NodeType:    "file",
		TopicType:   "State",
		DisplayName: "Updated Name",
		Description: "new desc",
		Fields:      []types.SchemaField{{Name: "v", Type: "float"}},
	})
	require.NoError(t, err)
	data := resp.Data.(map[string]any)
	require.Equal(t, "Updated Name", data["displayName"])
	require.Equal(t, "new desc", data["description"])
	require.Equal(t, "TOPIC", data["type"])
}

func TestOpenapiUnsNodeUpdate_NotFound(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsNodeUpdateLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsNodeUpdate(&types.OpenapiUnsNodeSaveReq{NodeId: 99999999})
	requireHTTPStatus(t, err, 404)
}

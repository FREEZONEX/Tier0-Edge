// openapiUnsNodeCreateLogic_test.go — OpenapiUnsNodeCreateLogic 测试。
//
// 测试目标：OpenAPI 创建 UNS 节点入口（含 NormalizeOpenapiCreateCommand 归一化）。
// 正常路径：创建文件节点（带 State 父级自动建目录）、创建文件夹节点。
// 异常情况：非法 topicType 返回 400；空 namespace/name 返回 400。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsNodeCreate_FileUnderState(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsNodeCreateLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsNodeCreate(&types.OpenapiUnsNodeSaveReq{
		Name:      "sensor-a",
		NodeType:  "file",
		TopicType: "State",
	})
	require.NoError(t, err)
	data := resp.Data.(map[string]any)
	require.Equal(t, "file", data["type"])
	require.Equal(t, "State", data["topicType"])
	require.Equal(t, "State/sensor-a", data["namespace"])
	require.Positive(t, data["id"].(int64))
}

func TestOpenapiUnsNodeCreate_Folder(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsNodeCreateLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsNodeCreate(&types.OpenapiUnsNodeSaveReq{
		Name:     "plant",
		NodeType: "folder",
	})
	require.NoError(t, err)
	data := resp.Data.(map[string]any)
	require.Equal(t, "PATH", data["type"])
	require.Equal(t, "plant", data["namespace"])
}

func TestOpenapiUnsNodeCreate_InvalidTopicType(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsNodeCreateLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsNodeCreate(&types.OpenapiUnsNodeSaveReq{
		Name:      "bad",
		NodeType:  "file",
		TopicType: "bogus",
	})
	requireHTTPStatus(t, err, 400)
}

func TestOpenapiUnsNodeCreate_EmptyName(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsNodeCreateLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsNodeCreate(&types.OpenapiUnsNodeSaveReq{Name: "  ", NodeType: "file"})
	require.Error(t, err)
}

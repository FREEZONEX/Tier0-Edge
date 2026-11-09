// openapiUnsReadLogic_test.go — 测试。
//
// 测试目标：OpenAPI 读取节点最新值 入口。
// 正常路径：命中节点返回成功结果。
// 异常情况：空 topics/不存在 topic/文件夹 topic。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsRead_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	createOpenapiUnsFileNode(t, svcCtx, "read-node")

	l := NewOpenapiUnsReadLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsRead(&types.ReadReq{Topics: []string{"State/read-node"}})
	require.NoError(t, err)
	body := resp.Data.(map[string]any)
	require.Equal(t, true, body["success"])
	results := body["results"].([]map[string]any)
	require.Len(t, results, 1)
	require.Equal(t, true, results[0]["success"])
	require.Equal(t, "GoodNoData", results[0]["result"].(map[string]any)["quality"])
}

func TestOpenapiUnsRead_EmptyTopics(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsReadLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsRead(&types.ReadReq{})
	require.NoError(t, err)
	body := resp.Data.(map[string]any)
	require.Equal(t, false, body["success"])
	results := body["results"].([]map[string]any)
	require.Len(t, results, 1)
	require.Equal(t, "topics empty", results[0]["error"].(map[string]any)["message"])
}

func TestOpenapiUnsRead_TopicNotFound(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsReadLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsRead(&types.ReadReq{Topics: []string{"State/nope"}})
	require.NoError(t, err)
	body := resp.Data.(map[string]any)
	require.Equal(t, false, body["success"])
	results := body["results"].([]map[string]any)
	require.Equal(t, int64(404), results[0]["error"].(map[string]any)["code"])
}

func TestOpenapiUnsRead_FolderTopic(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	createOpenapiUnsFileNode(t, svcCtx, "read-folder-child")

	l := NewOpenapiUnsReadLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsRead(&types.ReadReq{Topics: []string{"State"}})
	require.NoError(t, err)
	body := resp.Data.(map[string]any)
	require.Equal(t, false, body["success"])
	results := body["results"].([]map[string]any)
	require.Equal(t, "topic is not a file node", results[0]["error"].(map[string]any)["message"])
}
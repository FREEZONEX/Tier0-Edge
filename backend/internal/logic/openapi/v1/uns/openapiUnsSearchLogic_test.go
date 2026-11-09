// openapiUnsSearchLogic_test.go — 测试。
//
// 测试目标：OpenAPI 搜索节点 入口。
// 正常路径：关键字/类型/前缀过滤。
// 异常情况：非法 topicType 400。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsSearch_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	createOpenapiUnsFileNode(t, svcCtx, "search-target")

	l := NewOpenapiUnsSearchLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsSearch(&types.SearchReq{Keyword: "search-target"})
	require.NoError(t, err)
	body := resp.Data.(map[string]any)
	objects := body["objects"].([]map[string]any)
	// 搜索会带上命中的祖先节点（State 目录），objects 至少包含目标节点。
	names := make([]string, 0, len(objects))
	for _, obj := range objects {
		names = append(names, obj["name"].(string))
	}
	require.Contains(t, names, "search-target")
	require.GreaterOrEqual(t, body["total"].(int64), int64(1))
}

func TestOpenapiUnsSearch_TopicTypeFilter(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	createOpenapiUnsFileNode(t, svcCtx, "state-node")

	l := NewOpenapiUnsSearchLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsSearch(&types.SearchReq{Keyword: "state-node", TopicType: "state"})
	require.NoError(t, err)
	body := resp.Data.(map[string]any)
	// 命中节点 + 其 State 祖先（PATH 且 topicType=STATE）都会匹配。
	require.GreaterOrEqual(t, body["total"].(int64), int64(1))
	names := make([]string, 0)
	for _, obj := range body["objects"].([]map[string]any) {
		names = append(names, obj["name"].(string))
	}
	require.Contains(t, names, "state-node")

	// Metric 类型过滤后为空。
	resp, err = l.OpenapiUnsSearch(&types.SearchReq{Keyword: "state-node", TopicType: "metric"})
	require.NoError(t, err)
	require.Equal(t, int64(0), resp.Data.(map[string]any)["total"])
}

func TestOpenapiUnsSearch_InvalidTopicType(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsSearchLogic(ctx, svcCtx)
	// openapiTopicTypeValue 返回纯文本错误，logicx.Error 不映射 HTTP 状态码。
	_, err := l.OpenapiUnsSearch(&types.SearchReq{TopicType: "bogus"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid topicType")
}

func TestOpenapiUnsSearch_PathPrefix(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	createOpenapiUnsFileNode(t, svcCtx, "prefix-node")

	l := NewOpenapiUnsSearchLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsSearch(&types.SearchReq{PathPrefix: "State"})
	require.NoError(t, err)
	body := resp.Data.(map[string]any)
	for _, obj := range body["objects"].([]map[string]any) {
		require.True(t, obj["path"].(string) == "State/prefix-node" || obj["path"].(string) == "State")
	}
}
// openapiUnsCreateLogic_test.go — 测试。
//
// 测试目标：OpenAPI 批量建树 入口。
// 正常路径：批量创建返回逐项结果。
// 异常情况：空 namespace 错误。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsCreate_BatchTree(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsCreateLogic(ctx, svcCtx)
	resp, err := l.OpenapiUnsCreate(&types.NodeCreateReq{
		Namespace: []types.NamespaceNode{
			{
				Name: "factory",
				Type: "folder",
				Children: []types.NamespaceNode{
					{Name: "line1", Type: "folder"},
					{
						Name:      "temp",
						Type:      "topic",
						TopicType: "State",
					},
				},
			},
		},
	})
	require.NoError(t, err)
	body := resp.Data.(map[string]any)
	require.Equal(t, true, body["success"])
	results := body["results"].([]map[string]any)
	require.Len(t, results, 3)
	for _, item := range results {
		require.Equal(t, true, item["success"])
	}
}

func TestOpenapiUnsCreate_EmptyNamespace(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsCreateLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsCreate(&types.NodeCreateReq{})
	require.Error(t, err)
}
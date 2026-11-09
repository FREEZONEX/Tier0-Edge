// openapiUnsLabelListLogic_test.go — OpenapiUnsLabelListLogic 测试。
//
// 测试目标：OpenAPI 标签列表入口。
// 正常路径：创建标签后列表返回 1 条。
// 异常情况：无。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsLabelList_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	resp, err := NewOpenapiUnsLabelCreateLogic(ctx, svcCtx).OpenapiUnsLabelCreate(
		&types.UnsLabelReq{Name: "list-label"})
	require.NoError(t, err)
	require.Positive(t, resp.Data.(map[string]any)["id"].(int64))

	l := NewOpenapiUnsLabelListLogic(ctx, svcCtx)
	resp, err = l.OpenapiUnsLabelList(&types.PageReq{})
	require.NoError(t, err)
	list := resp.Data.(map[string]any)["list"].([]any)
	require.Len(t, list, 1)
}

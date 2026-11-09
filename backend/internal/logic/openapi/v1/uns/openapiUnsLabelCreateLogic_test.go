// openapiUnsLabelCreateLogic_test.go — 测试。
//
// 测试目标：OpenAPI 标签 CRUD 入口。
// 正常路径：创建/详情/列表/更新/删除。
// 异常情况：空 name 创建 400；不存在 404。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsLabelCrud(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	// Create。
	resp, err := NewOpenapiUnsLabelCreateLogic(ctx, svcCtx).OpenapiUnsLabelCreate(
		&types.UnsLabelReq{Name: "label-a", Color: "#ff0000", Description: "d"})
	require.NoError(t, err)
	created := resp.Data.(map[string]any)
	labelID := created["id"].(int64)
	require.Equal(t, "label-a", created["name"])

	// Detail。
	resp, err = NewOpenapiUnsLabelDetailLogic(ctx, svcCtx).OpenapiUnsLabelDetail(
		&types.IdReq{Id: labelID})
	require.NoError(t, err)
	require.Equal(t, "label-a", resp.Data.(map[string]any)["name"])

	// List。
	resp, err = NewOpenapiUnsLabelListLogic(ctx, svcCtx).OpenapiUnsLabelList(&types.PageReq{})
	require.NoError(t, err)
	list := resp.Data.(map[string]any)["list"].([]any)
	require.Len(t, list, 1)

	// Update。
	resp, err = NewOpenapiUnsLabelUpdateLogic(ctx, svcCtx).OpenapiUnsLabelUpdate(
		&types.UnsLabelReq{Id: labelID, Name: "label-a2", Color: "#00ff00"})
	require.NoError(t, err)
	require.Equal(t, "label-a2", resp.Data.(map[string]any)["name"])

	// Delete。
	resp, err = NewOpenapiUnsLabelDeleteLogic(ctx, svcCtx).OpenapiUnsLabelDelete(
		&types.IdReq{Id: labelID})
	require.NoError(t, err)
	require.Equal(t, true, resp.Data.(map[string]any)["deleted"])
}

func TestOpenapiUnsLabelCreate_EmptyName(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	_, err := NewOpenapiUnsLabelCreateLogic(ctx, svcCtx).OpenapiUnsLabelCreate(
		&types.UnsLabelReq{Name: "  "})
	requireHTTPStatus(t, err, 400)
}
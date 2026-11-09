// TASK-028 unsLabelCrudLogic.go 单测（svctest + 真实 PG）。
// 目标：覆盖标签 Detail/Create/Update/Delete 四个方法链路
// （Logic → domain Service → uns_namespace_label_info）。
// 正常路径：创建（含 label_key 自动生成）、详情、改名/改色、删除（软删除）。
// 异常情况：空名称创建返回 400；查询/更新/删除不存在的标签返回 404。
package uns

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsLabelCrud_CreateSuccess(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	resp, err := NewUnsLabelCrudLogic(ctx, svcCtx).Create(
		&types.UnsLabelReq{Name: "critical", Color: "#ff0000", Description: "critical label"})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	data := resp.Data.(map[string]any)
	id := data["id"].(int64)
	require.Positive(t, id)

	var row repo.UnsLabel
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&row, id).Error)
	require.Equal(t, "critical", row.Name)
	require.Equal(t, "#ff0000", row.Color)
	require.Equal(t, "critical label", row.Description)
	require.NotEmpty(t, row.LabelKey)
}

func TestUnsLabelCrud_CreateBlankNameRejected(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	_, err := NewUnsLabelCrudLogic(ctx, svcCtx).Create(&types.UnsLabelReq{Name: "   "})
	require.Equal(t, 400, httpErrorStatus(t, err))
}

func TestUnsLabelCrud_DetailSuccess(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	created, err := NewUnsLabelCrudLogic(ctx, svcCtx).Create(&types.UnsLabelReq{Name: "watch"})
	require.NoError(t, err)
	id := created.Data.(map[string]any)["id"].(int64)

	resp, err := NewUnsLabelCrudLogic(ctx, svcCtx).Detail(&types.IdReq{Id: id})
	require.NoError(t, err)
	require.Equal(t, id, resp.Data.(map[string]any)["id"].(int64))
	require.Equal(t, "watch", resp.Data.(map[string]any)["name"])
}

func TestUnsLabelCrud_DetailNotFound(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	_, err := NewUnsLabelCrudLogic(ctx, svcCtx).Detail(&types.IdReq{Id: 999999})
	require.Equal(t, 404, httpErrorStatus(t, err))
}

func TestUnsLabelCrud_UpdateSuccess(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	created, err := NewUnsLabelCrudLogic(ctx, svcCtx).Create(&types.UnsLabelReq{Name: "old"})
	require.NoError(t, err)
	id := created.Data.(map[string]any)["id"].(int64)

	resp, err := NewUnsLabelCrudLogic(ctx, svcCtx).Update(
		&types.UnsLabelReq{Id: id, Name: "new", Color: "#00ff00", Description: "updated"})
	require.NoError(t, err)
	require.Equal(t, "new", resp.Data.(map[string]any)["name"])

	var row repo.UnsLabel
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&row, id).Error)
	require.Equal(t, "new", row.Name)
	require.Equal(t, "#00ff00", row.Color)
	require.Equal(t, "updated", row.Description)
}

func TestUnsLabelCrud_UpdateNotFound(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	_, err := NewUnsLabelCrudLogic(ctx, svcCtx).Update(&types.UnsLabelReq{Id: 999999, Name: "x"})
	require.Equal(t, 404, httpErrorStatus(t, err))
}

func TestUnsLabelCrud_DeleteSuccess(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	created, err := NewUnsLabelCrudLogic(ctx, svcCtx).Create(&types.UnsLabelReq{Name: "discard"})
	require.NoError(t, err)
	id := created.Data.(map[string]any)["id"].(int64)

	resp, err := NewUnsLabelCrudLogic(ctx, svcCtx).Delete(&types.IdReq{Id: id})
	require.NoError(t, err)
	require.Equal(t, true, resp.Data.(map[string]any)["deleted"])

	var row repo.UnsLabel
	require.NoError(t, svcCtx.App.Store.CommonDB().Unscoped().First(&row, id).Error)
	require.Greater(t, int64(row.DeletedTime), int64(0))

	// 删除后再次删除/详情均 404
	_, err = NewUnsLabelCrudLogic(ctx, svcCtx).Delete(&types.IdReq{Id: id})
	require.Equal(t, 404, httpErrorStatus(t, err))
	_, err = NewUnsLabelCrudLogic(ctx, svcCtx).Detail(&types.IdReq{Id: id})
	require.Equal(t, 404, httpErrorStatus(t, err))
}

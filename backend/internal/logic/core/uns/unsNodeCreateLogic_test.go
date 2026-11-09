// TASK-028 unsNodeCreateLogic.go 单测（svctest + 真实 PG）。
// 目标：覆盖 UNS 节点创建链路（Logic → domain Service → repo → uns_namespace_node_info）。
// 正常路径：根下创建文件夹落库（type=1/active/namespace 生成）；创建 metric 文件自动补建
// "Metric" 父文件夹并生成 "Metric/<name>" 命名空间。
// 异常情况：同级重名返回 409；非法节点类型返回 400。
package uns

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsNodeCreate_FolderSuccess(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	resp, err := NewUnsNodeCreateLogic(ctx, svcCtx).UnsNodeCreate(
		&types.UnsNodeSaveReq{Name: "area1", DisplayName: "Area One", NodeType: "folder"})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	data := resp.Data.(map[string]any)
	id := data["id"].(int64)
	require.Positive(t, id)
	require.Equal(t, "area1", data["name"])
	require.Equal(t, "folder", data["type"])
	require.Equal(t, "area1", data["namespace"])

	var row repo.UnsNode
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&row, id).Error)
	require.Equal(t, "area1", row.Name)
	require.Equal(t, "area1", row.Namespace)
	require.Equal(t, int16(1), row.Type)
	require.Equal(t, repo.UnsNodeStatusActive, row.Status)
	require.Equal(t, int64(1), row.CreatedBy)
}

func TestUnsNodeCreate_MetricFileAutoCreatesTopicFolder(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	resp, err := NewUnsNodeCreateLogic(ctx, svcCtx).UnsNodeCreate(
		&types.UnsNodeSaveReq{Name: "temp01", NodeType: "file", TopicType: "metric"})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	data := resp.Data.(map[string]any)
	require.Equal(t, "file", data["type"])
	// 对外 topicType 由 topicTypeName 规范化为首字母大写（topicTypeValue 仅解析输入时大小写不敏感）
	require.Equal(t, "Metric", data["topicType"])
	fileID := data["id"].(int64)

	var file repo.UnsNode
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&file, fileID).Error)
	require.Equal(t, "Metric/temp01", file.Namespace)

	// 领域层自动补建的 Metric 父文件夹
	var folder repo.UnsNode
	require.NoError(t, svcCtx.App.Store.CommonDB().
		Where("name = ? AND parent_id = 0 AND deleted_time = 0", "Metric").First(&folder).Error)
	require.Equal(t, int16(1), folder.Type)
	require.Equal(t, folder.ID, file.ParentID)
}

func TestUnsNodeCreate_DuplicateSiblingName(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)
	l := NewUnsNodeCreateLogic(ctx, svcCtx)

	_, err := l.UnsNodeCreate(&types.UnsNodeSaveReq{Name: "dup", NodeType: "folder"})
	require.NoError(t, err)

	_, err = l.UnsNodeCreate(&types.UnsNodeSaveReq{Name: "dup", NodeType: "folder"})
	require.Equal(t, 409, httpErrorStatus(t, err))
}

func TestUnsNodeCreate_InvalidNodeType(t *testing.T) {
	svcCtx, ctx := newUNSLogicEnv(t)

	_, err := NewUnsNodeCreateLogic(ctx, svcCtx).UnsNodeCreate(
		&types.UnsNodeSaveReq{Name: "x", NodeType: "bogus"})
	require.Equal(t, 400, httpErrorStatus(t, err))
}

// service.go 未导出函数（commandToNode / aliasAvailable / indexImportCreatedNode /
// importLabelIDs）的 DB 行为单元测试。
// 内部测试包（package uns），直接使用 testkit 建独立 PG 测试库 + 手工构造 Service
// （flow/payload 传 nil），避免 svctest -> uns 的 import cycle。
// 覆盖：SaveCommand -> UnsNode 转换（含 alias 解析、主题推导、schema 清洗）、
// alias 可用性查询、导入索引回填、标签 ID 映射等正常与异常路径。
package uns

import (
	"context"
	"testing"

	"backend/internal/repo"
	"backend/internal/testkit"

	"github.com/stretchr/testify/require"
)

func newTestkitUnsService(t *testing.T) (*repo.Store, *Service) {
	t.Helper()
	store, _ := testkit.NewTestDB(t)
	return store, New(context.Background(), nil)
}

func TestCommandToNode_Behavior(t *testing.T) {
	_, svc := newTestkitUnsService(t)
	ctx := context.Background()

	node, err := svc.commandToNode(ctx, SaveCommand{Name: "x", NodeType: "folder"})
	require.NoError(t, err)
	require.Equal(t, "x", node.Name)
	require.Equal(t, int16(1), node.Type)
	require.Equal(t, int16(0), node.TopicType)
	require.True(t, isAliasFormatOK(node.Alias))
	require.JSONEq(t, `[]`, string(node.Schema))
	require.JSONEq(t, `{}`, string(node.ExtendProperties))

	// file 默认 TopicType=1
	node, err = svc.commandToNode(ctx, SaveCommand{Name: "x", NodeType: "file"})
	require.NoError(t, err)
	require.Equal(t, int16(2), node.Type)
	require.Equal(t, int16(1), node.TopicType)

	node, err = svc.commandToNode(ctx, SaveCommand{Name: "x", NodeType: "file", TopicType: "metric"})
	require.NoError(t, err)
	require.Equal(t, int16(3), node.TopicType)

	// 系统主题文件夹名
	node, err = svc.commandToNode(ctx, SaveCommand{Name: "Metric", NodeType: "folder", SystemTopicFolder: true, TopicType: "Metric"})
	require.NoError(t, err)
	require.Equal(t, int16(3), node.TopicType)

	// 显式 alias
	node, err = svc.commandToNode(ctx, SaveCommand{Name: "x", NodeType: "folder", Alias: "fixed_alias"})
	require.NoError(t, err)
	require.Equal(t, "fixed_alias", node.Alias)

	// 非法输入
	_, err = svc.commandToNode(ctx, SaveCommand{Name: "", NodeType: "folder"})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = svc.commandToNode(ctx, SaveCommand{Name: "x", NodeType: "bogus"})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = svc.commandToNode(ctx, SaveCommand{Name: "x", NodeType: "file", TopicType: "bogus"})
	require.ErrorIs(t, err, ErrInvalid)
	_, err = svc.commandToNode(ctx, SaveCommand{Name: "x", NodeType: "folder", Schema: "{bad"})
	require.ErrorIs(t, err, ErrInvalidJSON)
	_, err = svc.commandToNode(ctx, SaveCommand{Name: "x", NodeType: "folder", Alias: "bad alias"})
	require.ErrorContains(t, err, "invalid alias")

	// 父节点不存在 → aliasSeed 查询失败
	_, err = svc.commandToNode(ctx, SaveCommand{Name: "x", ParentID: 999999, NodeType: "folder"})
	require.ErrorIs(t, err, repo.ErrNotFound)
}

func TestAliasAvailable_Behavior(t *testing.T) {
	_, svc := newTestkitUnsService(t)
	ctx := context.Background()

	resp, err := svc.Create(ctx, SaveCommand{Name: "aa1", NodeType: "folder", Alias: "aa_1", UserID: 1})
	require.NoError(t, err)
	nodeID := toInt64(resp["id"])

	ok, err := svc.aliasAvailable(ctx, "aa_1", 0)
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = svc.aliasAvailable(ctx, "aa_1", nodeID)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = svc.aliasAvailable(ctx, "unused", 0)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = svc.aliasAvailable(ctx, "", 0)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestIndexImportCreatedNode_Behavior(t *testing.T) {
	_, svc := newTestkitUnsService(t)
	ctx := context.Background()

	plantResp, err := svc.Create(ctx, SaveCommand{Name: "Plant", NodeType: "folder", UserID: 1})
	require.NoError(t, err)
	plantID := toInt64(plantResp["id"])
	byNamespace := map[string]repo.UnsNode{"Plant": {ID: plantID}}

	// 创建时父级被重定向到自动主题文件夹 → 一并索引父级
	fileResp, err := svc.Create(ctx, SaveCommand{ParentID: plantID, Name: "temp", NodeType: "file", TopicType: "metric", UserID: 1})
	require.NoError(t, err)
	fileID := toInt64(fileResp["id"])
	file, err := svc.uns.GetUnsNode(ctx, fileID, false)
	require.NoError(t, err)
	require.NoError(t, svc.indexImportCreatedNode(ctx, byNamespace, file, plantID))
	require.Equal(t, fileID, byNamespace["Plant/Metric/temp"].ID)
	require.Positive(t, byNamespace["Plant/Metric"].ID)

	// 父级未重定向时只索引创建节点
	byNamespace = map[string]repo.UnsNode{}
	require.NoError(t, svc.indexImportCreatedNode(ctx, byNamespace, file, file.ParentID))
	require.Equal(t, fileID, byNamespace["Plant/Metric/temp"].ID)
	require.Len(t, byNamespace, 1)
}

func TestImportLabelIDs_Behavior(t *testing.T) {
	_, svc := newTestkitUnsService(t)
	ctx := context.Background()

	resp, err := svc.CreateLabel(ctx, LabelCommand{Name: "TagA"})
	require.NoError(t, err)
	labelID := toInt64(resp["id"])

	ids, err := svc.importLabelIDs(ctx)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"taga": labelID}, ids)
}

// service.go DB 行为单元测试（TASK-028 阶段 1，外部测试包，避免与 svctest 的 import cycle）。
// 测试目标：Service 对外方法，经 svctest 装配完整领域服务并落真实 PG 测试库。
// 覆盖正常路径：创建（根节点/命名空间路径/主题父级自动创建/AddFlow）、查询（List/Detail/Dashboard/
// ActiveNodeCount/Recycle）、更新（Update/UpdateFromConsole）、软删生命周期（Delete/Restore/
// ForceDelete/回收站/挂载子树保护）、模板与标签 CRUD、导入导出、异步任务、别名唯一性。
// 覆盖异常情况：保留名、重名兄弟、非法参数、不存在、恢复冲突、不在回收站、挂载子树只读、
// 导入格式错误、非法 alias、主题父级规则冲突等。
// 说明：审计落库走 HTTP 中间件链路，领域单测不覆盖；外部服务（Node-RED/EMQX）惰性构造，
// AddFlow 仅验证流程记录落库，不验证真实部署。
package uns_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	domainuns "backend/internal/domain/uns"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/svc/svctest"

	"github.com/stretchr/testify/require"
)

func newUnsSvcCtx(t *testing.T) *svc.ServiceContext {
	t.Helper()
	return svctest.NewTestServiceContext(t)
}

func boolPtr(v bool) *bool { return &v }

func createNode(t *testing.T, svc *domainuns.Service, ctx context.Context, cmd domainuns.SaveCommand) int64 {
	t.Helper()
	resp, err := svc.Create(ctx, cmd)
	require.NoError(t, err)
	id, ok := resp["id"].(int64)
	require.True(t, ok, "resp id type = %T", resp["id"])
	require.Positive(t, id)
	return id
}

func loadNode(t *testing.T, svcCtx *svc.ServiceContext, id int64) repo.UnsNode {
	t.Helper()
	var node repo.UnsNode
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&node, id).Error)
	return node
}

func countActiveNodes(t *testing.T, svcCtx *svc.ServiceContext) int64 {
	t.Helper()
	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("uns_namespace_node_info").
		Where("deleted_time = 0").Count(&count).Error)
	return count
}

// ---------- Create ----------

func TestServiceCreate_FolderAndFileAtRoot(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	folderID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "Common", DisplayName: "Common", NodeType: "folder", UserID: 1})
	folder := loadNode(t, svcCtx, folderID)
	require.Equal(t, "Common", folder.Name)
	require.Equal(t, "Common", folder.Namespace)
	require.Equal(t, int16(1), folder.Type)
	require.Equal(t, int16(0), folder.TopicType)
	require.Equal(t, "active", folder.Status)
	require.NotEmpty(t, folder.Alias)

	// 根级文件自动落到同名主题文件夹下
	fileID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "t1", NodeType: "file", TopicType: "metric", UserID: 1})
	file := loadNode(t, svcCtx, fileID)
	require.Equal(t, int16(2), file.Type)
	require.Equal(t, int16(3), file.TopicType)
	require.Equal(t, "Metric/t1", file.Namespace)
	var metricFolder repo.UnsNode
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("namespace = ?", "Metric").First(&metricFolder).Error)
	require.Equal(t, int16(3), metricFolder.TopicType)
}

func TestServiceCreate_FileUnderFolderInfersTopicType(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	plantID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "Plant", NodeType: "folder", UserID: 1})
	fileID := createNode(t, svc, ctx, domainuns.SaveCommand{ParentID: plantID, Name: "temp", NodeType: "file", TopicType: "metric", UserID: 1})
	file := loadNode(t, svcCtx, fileID)
	require.Equal(t, "Plant/Metric/temp", file.Namespace)
	require.Equal(t, int16(3), file.TopicType)

	var metricFolder repo.UnsNode
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("namespace = ?", "Plant/Metric").First(&metricFolder).Error)
	require.Equal(t, int16(1), metricFolder.Type)
	require.Equal(t, int16(3), metricFolder.TopicType)
}

func TestServiceCreate_NamespacePathBuildsFolders(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	fileID := createNode(t, svc, ctx, domainuns.SaveCommand{
		Name: "temp", Namespace: "Common/State/temp", NodeType: "file", TopicType: "state", UserID: 1,
	})
	file := loadNode(t, svcCtx, fileID)
	require.Equal(t, "Common/State/temp", file.Namespace)
	require.Equal(t, int16(3), countActiveNodes(t, svcCtx))

	folderID := createNode(t, svc, ctx, domainuns.SaveCommand{
		Name: "sub", Namespace: "Common/sub", NodeType: "folder", UserID: 1,
	})
	folder := loadNode(t, svcCtx, folderID)
	require.Equal(t, "Common/sub", folder.Namespace)
	require.Equal(t, int16(1), folder.Type)
}

func TestServiceCreate_ReservedNameRejected(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	_, err := svc.Create(ctx, domainuns.SaveCommand{Name: "x", Namespace: "a/label/x", NodeType: "file", TopicType: "state", UserID: 1})
	require.ErrorIs(t, err, domainuns.ErrInvalid)
	_, err = svc.Create(ctx, domainuns.SaveCommand{Name: "x", Namespace: "label/x", NodeType: "file", TopicType: "state", UserID: 1})
	require.ErrorIs(t, err, domainuns.ErrInvalid)
}

func TestServiceCreate_DuplicateSiblingRejected(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	createNode(t, svc, ctx, domainuns.SaveCommand{Name: "Common", NodeType: "folder", UserID: 1})
	_, err := svc.Create(ctx, domainuns.SaveCommand{Name: "Common", NodeType: "folder", UserID: 1})
	require.ErrorIs(t, err, repo.ErrDuplicate)
}

func TestServiceCreate_InvalidInputs(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	_, err := svc.Create(ctx, domainuns.SaveCommand{Name: "", NodeType: "folder", UserID: 1})
	require.ErrorIs(t, err, domainuns.ErrInvalid)
	_, err = svc.Create(ctx, domainuns.SaveCommand{Name: "x", NodeType: "bogus", UserID: 1})
	require.ErrorIs(t, err, domainuns.ErrInvalid)
	_, err = svc.Create(ctx, domainuns.SaveCommand{Name: "x", NodeType: "file", TopicType: "bogus", UserID: 1})
	require.ErrorIs(t, err, domainuns.ErrInvalid)
	_, err = svc.Create(ctx, domainuns.SaveCommand{Name: "x", NodeType: "folder", Schema: "{bad", UserID: 1})
	require.ErrorIs(t, err, domainuns.ErrInvalidJSON)
	_, err = svc.Create(ctx, domainuns.SaveCommand{Name: strings.Repeat("x", 64), NodeType: "folder", UserID: 1})
	require.ErrorIs(t, err, domainuns.ErrInvalid)
	_, err = svc.Create(ctx, domainuns.SaveCommand{Name: "x", NodeType: "folder", Description: strings.Repeat("d", 513), UserID: 1})
	require.ErrorIs(t, err, domainuns.ErrInvalid)
}

func TestServiceCreate_AddFlowQueueFailureRollsBackUns(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	db := svcCtx.App.Store.CommonDB()
	// Break only this isolated test DB's outbox insert path.
	require.NoError(t, db.Exec("ALTER TABLE sys_outbox_event ADD CONSTRAINT reject_test_flow CHECK (event_type <> 'uns.sourceflow.create')").Error)
	_, err := svcCtx.App.UNS.Create(svctest.CtxWithUser(1), domainuns.SaveCommand{Name: "queue-failure", NodeType: "file", TopicType: "metric", AddFlow: true, UserID: 1})
	require.Error(t, err)
	var count int64
	require.NoError(t, db.Model(&repo.UnsNode{}).Where("name = ?", "queue-failure").Count(&count).Error)
	require.Zero(t, count)
}

func TestServiceCreate_AliasUniqueness(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	createNode(t, svc, ctx, domainuns.SaveCommand{Name: "a1", NodeType: "file", TopicType: "metric", Alias: "fixed_alias", UserID: 1})
	_, err := svc.Create(ctx, domainuns.SaveCommand{Name: "a2", NodeType: "file", TopicType: "metric", Alias: "fixed_alias", UserID: 1})
	require.ErrorContains(t, err, "alias already exists")

	_, err = svc.Create(ctx, domainuns.SaveCommand{Name: "a3", NodeType: "file", TopicType: "metric", Alias: "bad alias", UserID: 1})
	require.ErrorContains(t, err, "invalid alias")
}

// ---------- NormalizeOpenapiCreateCommand ----------

func TestServiceNormalizeOpenapiCreateCommand_Behavior(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	cmd, err := svc.NormalizeOpenapiCreateCommand(ctx, domainuns.SaveCommand{Namespace: "Common/State/temp"})
	require.NoError(t, err)
	require.Equal(t, "temp", cmd.Name)
	require.Equal(t, "file", cmd.NodeType)
	require.Equal(t, "State", cmd.TopicType)

	cmd, err = svc.NormalizeOpenapiCreateCommand(ctx, domainuns.SaveCommand{Name: "Metric"})
	require.NoError(t, err)
	require.Equal(t, "folder", cmd.NodeType)
	require.Equal(t, "Metric", cmd.TopicType)
	require.True(t, cmd.SystemTopicFolder)

	cmd, err = svc.NormalizeOpenapiCreateCommand(ctx, domainuns.SaveCommand{Name: "Common"})
	require.NoError(t, err)
	require.Equal(t, "folder", cmd.NodeType)
	require.Equal(t, "", cmd.TopicType)

	_, err = svc.NormalizeOpenapiCreateCommand(ctx, domainuns.SaveCommand{Name: "x", NodeType: "file"})
	require.ErrorIs(t, err, domainuns.ErrInvalid)

	metricFolderID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "Metric", NodeType: "folder", TopicType: "Metric", SystemTopicFolder: true, UserID: 1})
	cmd, err = svc.NormalizeOpenapiCreateCommand(ctx, domainuns.SaveCommand{ParentID: metricFolderID, Name: "f1", NodeType: "file"})
	require.NoError(t, err)
	require.Equal(t, "Metric", cmd.TopicType)

	_, err = svc.NormalizeOpenapiCreateCommand(ctx, domainuns.SaveCommand{ParentID: metricFolderID, Name: "f2", NodeType: "file", TopicType: "action"})
	require.ErrorIs(t, err, domainuns.ErrInvalid)

	_, err = svc.NormalizeOpenapiCreateCommand(ctx, domainuns.SaveCommand{ParentID: metricFolderID, Name: "sub", NodeType: "folder"})
	require.ErrorIs(t, err, domainuns.ErrInvalid)

	fileID := createNode(t, svc, ctx, domainuns.SaveCommand{ParentID: metricFolderID, Name: "f3", NodeType: "file", TopicType: "metric", UserID: 1})
	_, err = svc.NormalizeOpenapiCreateCommand(ctx, domainuns.SaveCommand{ParentID: fileID, Name: "f4"})
	require.ErrorIs(t, err, domainuns.ErrInvalid)
}

// ---------- 查询 ----------

func TestServiceList_Behavior(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	commonID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "Common", NodeType: "folder", UserID: 1})
	createNode(t, svc, ctx, domainuns.SaveCommand{Name: "Other", NodeType: "folder", UserID: 1})
	createNode(t, svc, ctx, domainuns.SaveCommand{ParentID: commonID, Name: "temp", NodeType: "file", TopicType: "metric", UserID: 1})

	resp, err := svc.List(ctx, 0, false, "", false)
	require.NoError(t, err)
	require.Equal(t, 3, resp["total"])

	resp, err = svc.List(ctx, commonID, true, "", false)
	require.NoError(t, err)
	require.Equal(t, 1, resp["total"])

	resp, err = svc.List(ctx, 0, false, "Other", false)
	require.NoError(t, err)
	require.Equal(t, 1, resp["total"])

	_, err = svc.Delete(ctx, commonID, 1)
	require.NoError(t, err)
	resp, err = svc.List(ctx, 0, false, "", true)
	require.NoError(t, err)
	require.Equal(t, 3, resp["total"])
}

func TestServiceActiveNodeCount_Behavior(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	count, err := svc.ActiveNodeCount(ctx)
	require.NoError(t, err)
	require.Zero(t, count)

	createNode(t, svc, ctx, domainuns.SaveCommand{Name: "a", NodeType: "folder", UserID: 1})
	createNode(t, svc, ctx, domainuns.SaveCommand{Name: "b", NodeType: "folder", UserID: 1})
	count, err = svc.ActiveNodeCount(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), count)
}

func TestServiceRecycle_Behavior(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	delID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "del", NodeType: "folder", UserID: 1})
	_, err := svc.Delete(ctx, delID, 1)
	require.NoError(t, err)

	resp, err := svc.Recycle(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, resp["total"])
	items := resp["list"].([]map[string]any)
	require.Equal(t, "del", items[0]["name"])
}

func TestServiceDetail_Behavior(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	folderID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "Common", NodeType: "folder", UserID: 1})
	fileID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "t1", NodeType: "file", TopicType: "metric", UserID: 1})

	resp, err := svc.Detail(ctx, folderID, false)
	require.NoError(t, err)
	require.Equal(t, "folder", resp["type"])
	require.Equal(t, "Common", resp["name"])

	resp, err = svc.Detail(ctx, fileID, false)
	require.NoError(t, err)
	require.Equal(t, "file", resp["type"])

	_, err = svc.Detail(ctx, 999999, false)
	require.ErrorIs(t, err, domainuns.ErrNotFound)

	_, err = svc.Delete(ctx, folderID, 1)
	require.NoError(t, err)
	_, err = svc.Detail(ctx, folderID, false)
	require.ErrorIs(t, err, domainuns.ErrNotFound)
	_, err = svc.Detail(ctx, folderID, true)
	require.NoError(t, err)
}

func TestServiceDashboard_Behavior(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	fileID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "t1", NodeType: "file", TopicType: "metric", UserID: 1})
	resp, err := svc.Dashboard(ctx, domainuns.DashboardQuery{NodeID: fileID, Limit: 100})
	require.NoError(t, err)
	require.Equal(t, int64(0), resp["total"])
	require.Empty(t, resp["list"])

	folderID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "Common", NodeType: "folder", UserID: 1})
	_, err = svc.Dashboard(ctx, domainuns.DashboardQuery{NodeID: folderID})
	require.ErrorIs(t, err, domainuns.ErrInvalid)

	_, err = svc.Dashboard(ctx, domainuns.DashboardQuery{NodeID: 999999})
	require.ErrorIs(t, err, domainuns.ErrNotFound)
}

// ---------- 更新 ----------

func TestServiceUpdate_Behavior(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	folderID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "Common", NodeType: "folder", UserID: 1})
	originalAlias := loadNode(t, svcCtx, folderID).Alias
	resp, err := svc.Update(ctx, domainuns.SaveCommand{ID: folderID, Name: "Renamed", DisplayName: "Renamed", NodeType: "folder", UserID: 1})
	require.NoError(t, err)
	require.Equal(t, "Renamed", resp["name"])
	require.Equal(t, "Renamed", loadNode(t, svcCtx, folderID).Namespace)
	require.Equal(t, originalAlias, loadNode(t, svcCtx, folderID).Alias)

	createNode(t, svc, ctx, domainuns.SaveCommand{Name: "t1", NodeType: "file", TopicType: "metric", UserID: 1})
	var metricFolder repo.UnsNode
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("namespace = ?", "Metric").First(&metricFolder).Error)
	_, err = svc.Update(ctx, domainuns.SaveCommand{ID: metricFolder.ID, Name: "Metric", NodeType: "folder", UserID: 1})
	require.NoError(t, err)
	require.Equal(t, int16(3), loadNode(t, svcCtx, metricFolder.ID).TopicType)

	_, err = svc.Update(ctx, domainuns.SaveCommand{ID: 999999, Name: "x", NodeType: "folder", UserID: 1})
	require.ErrorIs(t, err, domainuns.ErrNotFound)

	createNode(t, svc, ctx, domainuns.SaveCommand{Name: "dup", NodeType: "folder", UserID: 1})
	_, err = svc.Update(ctx, domainuns.SaveCommand{ID: folderID, Name: "dup", NodeType: "folder", UserID: 1})
	require.ErrorIs(t, err, repo.ErrDuplicate)
}

func TestServiceUpdateFromConsole_Behavior(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	fileID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "t1", NodeType: "file", TopicType: "metric", UserID: 1})
	resp, err := svc.UpdateFromConsole(ctx, domainuns.SaveCommand{ID: fileID, Name: "t1b", NodeType: "file", TopicType: "metric", UserID: 1})
	require.NoError(t, err)
	require.Equal(t, "t1b", resp["name"])

}

func TestServiceValidateParentRule_Behavior(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	metricFolderID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "Metric", NodeType: "folder", TopicType: "Metric", SystemTopicFolder: true, UserID: 1})

	_, err := svc.Create(ctx, domainuns.SaveCommand{ParentID: metricFolderID, Name: "sub", NodeType: "folder", UserID: 1})
	require.ErrorIs(t, err, domainuns.ErrInvalid)

	fileID := createNode(t, svc, ctx, domainuns.SaveCommand{ParentID: metricFolderID, Name: "t2", NodeType: "file", TopicType: "metric", UserID: 1})
	require.Equal(t, "Metric/t2", loadNode(t, svcCtx, fileID).Namespace)

	_, err = svc.Create(ctx, domainuns.SaveCommand{ParentID: metricFolderID, Name: "t3", NodeType: "file", TopicType: "action", UserID: 1})
	require.ErrorIs(t, err, domainuns.ErrInvalid)
}

// ---------- 删除/恢复/彻底删除 ----------

func TestServiceDeleteRestore_Behavior(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	delID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "del", NodeType: "folder", UserID: 1})
	createNode(t, svc, ctx, domainuns.SaveCommand{ParentID: delID, Name: "temp", NodeType: "file", TopicType: "metric", UserID: 1})

	resp, err := svc.Delete(ctx, delID, 1)
	require.NoError(t, err)
	require.Equal(t, 3, resp["deleted"])
	require.Zero(t, countActiveNodes(t, svcCtx))

	recycle, err := svc.Recycle(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, recycle["total"])

	resp, err = svc.Restore(ctx, delID, 1, false)
	require.NoError(t, err)
	require.Equal(t, 3, resp["restored"])
	require.Equal(t, int64(3), countActiveNodes(t, svcCtx))

	_, err = svc.Delete(ctx, 999999, 1)
	require.ErrorIs(t, err, domainuns.ErrNotFound)
	_, err = svc.Restore(ctx, 999999, 1, false)
	require.ErrorIs(t, err, domainuns.ErrNotFound)
}

func TestServiceRestore_Conflict(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	delID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "conflict", NodeType: "folder", UserID: 1})
	_, err := svc.Delete(ctx, delID, 1)
	require.NoError(t, err)

	createNode(t, svc, ctx, domainuns.SaveCommand{Name: "conflict", NodeType: "folder", UserID: 1})
	_, err = svc.Restore(ctx, delID, 1, false)
	require.ErrorIs(t, err, domainuns.ErrRestoreConflict)

	resp, err := svc.Restore(ctx, delID, 1, true)
	require.NoError(t, err)
	require.Equal(t, 1, resp["restored"])
	var restored repo.UnsNode
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("id = ?", delID).First(&restored).Error)
	require.NotEqual(t, "conflict", restored.Name)
	require.Equal(t, "active", restored.Status)
}

func TestServiceForceDelete_Behavior(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	activeID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "active", NodeType: "folder", UserID: 1})
	_, err := svc.ForceDelete(ctx, activeID, 1, false)
	require.ErrorIs(t, err, domainuns.ErrNotInRecycle)

	fdID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "fd", NodeType: "folder", UserID: 1})
	createNode(t, svc, ctx, domainuns.SaveCommand{ParentID: fdID, Name: "temp", NodeType: "file", TopicType: "metric", UserID: 1})
	_, err = svc.Delete(ctx, fdID, 1)
	require.NoError(t, err)

	resp, err := svc.ForceDelete(ctx, fdID, 1, false)
	require.NoError(t, err)
	require.Equal(t, 2, resp["forceDeleted"])

	recycle, err := svc.Recycle(ctx)
	require.NoError(t, err)
	require.Zero(t, recycle["total"])
	require.Zero(t, countActiveNodes(t, svcCtx))

	_, err = svc.ForceDelete(ctx, fdID, 1, false)
	require.ErrorIs(t, err, domainuns.ErrNotInRecycle)
}

// ---------- 模板 ----------

func TestServiceTemplates_CRUD(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	resp, err := svc.CreateTemplate(ctx, domainuns.TemplateCommand{
		Name: "t1", TopicType: "metric", Fields: []map[string]any{{"name": "v", "type": "INTEGER"}}, UserID: 1,
	})
	require.NoError(t, err)
	id := resp["id"].(int64)
	require.Positive(t, id)

	var row repo.UnsTemplate
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&row, id).Error)
	require.Equal(t, "t1", row.Name)
	require.Equal(t, int16(3), row.TopicType)

	detail, err := svc.TemplateDetail(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "Metric", detail["topicType"])
	require.Len(t, detail["fields"], 1)

	list, err := svc.Templates(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, list["total"])

	_, err = svc.UpdateTemplate(ctx, domainuns.TemplateCommand{ID: id, Name: "t2", UserID: 1})
	require.NoError(t, err)
	require.Equal(t, "t2", loadTemplateName(t, svcCtx, id))

	_, err = svc.DeleteTemplate(ctx, id, 1)
	require.NoError(t, err)
	_, err = svc.TemplateDetail(ctx, id)
	require.ErrorIs(t, err, domainuns.ErrNotFound)

	_, err = svc.TemplateDetail(ctx, 999999)
	require.ErrorIs(t, err, domainuns.ErrNotFound)
	_, err = svc.UpdateTemplate(ctx, domainuns.TemplateCommand{ID: 999999, Name: "x", UserID: 1})
	require.ErrorIs(t, err, domainuns.ErrNotFound)
	_, err = svc.DeleteTemplate(ctx, 999999, 1)
	require.ErrorIs(t, err, domainuns.ErrNotFound)
	_, err = svc.CreateTemplate(ctx, domainuns.TemplateCommand{Name: "", UserID: 1})
	require.ErrorIs(t, err, domainuns.ErrInvalid)
}

func loadTemplateName(t *testing.T, svcCtx *svc.ServiceContext, id int64) string {
	t.Helper()
	var row repo.UnsTemplate
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&row, id).Error)
	return row.Name
}

// ---------- 标签 ----------

func TestServiceLabels_CRUD(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	resp, err := svc.CreateLabel(ctx, domainuns.LabelCommand{Name: "l1", Color: "#fff", Description: "d"})
	require.NoError(t, err)
	id := resp["id"].(int64)
	require.Positive(t, id)

	var row repo.UnsLabel
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&row, id).Error)
	require.Equal(t, "l1", row.Name)
	require.Equal(t, "#fff", row.Color)

	detail, err := svc.LabelDetail(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "l1", detail["name"])

	list, err := svc.Labels(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, list["total"])

	_, err = svc.UpdateLabel(ctx, domainuns.LabelCommand{ID: id, Name: "l2"})
	require.NoError(t, err)
	require.Equal(t, "l2", loadLabelName(t, svcCtx, id))

	_, err = svc.DeleteLabel(ctx, id)
	require.NoError(t, err)
	_, err = svc.LabelDetail(ctx, id)
	require.ErrorIs(t, err, domainuns.ErrNotFound)

	_, err = svc.CreateLabel(ctx, domainuns.LabelCommand{Name: ""})
	require.ErrorIs(t, err, domainuns.ErrInvalid)
	_, err = svc.CreateLabel(ctx, domainuns.LabelCommand{Name: strings.Repeat("x", 64)})
	require.ErrorIs(t, err, domainuns.ErrInvalid)
	_, err = svc.UpdateLabel(ctx, domainuns.LabelCommand{ID: 999999, Name: "x"})
	require.ErrorIs(t, err, domainuns.ErrNotFound)
	_, err = svc.DeleteLabel(ctx, 999999)
	require.ErrorIs(t, err, domainuns.ErrNotFound)
}

func loadLabelName(t *testing.T, svcCtx *svc.ServiceContext, id int64) string {
	t.Helper()
	var row repo.UnsLabel
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&row, id).Error)
	return row.Name
}

// ---------- 异步任务 ----------

func TestServiceImportExportJobs_Behavior(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	resp, err := svc.CreateImportJob(ctx, true, 5, 1)
	require.NoError(t, err)
	job := resp["job"].(repo.AsyncJob)
	require.Equal(t, "uns.import", job.JobType)
	require.NotEmpty(t, job.JobKey)

	jobResp, err := svc.Job(ctx, job.JobKey)
	require.NoError(t, err)
	require.Equal(t, job.JobKey, jobResp["job"].(repo.AsyncJob).JobKey)

	_, err = svc.CreateImportJob(ctx, true, 0, 1)
	require.ErrorIs(t, err, domainuns.ErrInvalid)

	resp, err = svc.CreateExportJob(ctx, 3, 1)
	require.NoError(t, err)
	exportJob := resp["job"].(repo.AsyncJob)
	require.Equal(t, "uns.export", exportJob.JobType)

	_, err = svc.Job(ctx, "missing-key")
	require.ErrorIs(t, err, domainuns.ErrNotFound)
}

// ---------- 导出 ----------

func TestServiceExportData_Behavior(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	commonID := createNode(t, svc, ctx, domainuns.SaveCommand{Name: "Common", NodeType: "folder", UserID: 1})
	fileID := createNode(t, svc, ctx, domainuns.SaveCommand{ParentID: commonID, Name: "t1", NodeType: "file", TopicType: "metric", UserID: 1})
	createNode(t, svc, ctx, domainuns.SaveCommand{Name: "Other", NodeType: "folder", UserID: 1})

	resp, err := svc.ExportData(ctx, domainuns.ExportCommand{ExportType: "ALL"})
	require.NoError(t, err)
	tree := resp["namespace"].([]map[string]any)
	require.Len(t, tree, 2)
	require.Equal(t, "Common", tree[0]["name"])
	require.Equal(t, "Other", tree[1]["name"])

	resp, err = svc.ExportData(ctx, domainuns.ExportCommand{Folders: []int64{commonID}})
	require.NoError(t, err)
	tree = resp["namespace"].([]map[string]any)
	require.Len(t, tree, 1)
	require.Equal(t, "Common", tree[0]["name"])
	children := tree[0]["children"].([]map[string]any)
	require.Len(t, children, 1)
	require.Equal(t, "t1", children[0]["name"])
	require.Equal(t, "TOPIC", children[0]["type"])

	resp, err = svc.ExportData(ctx, domainuns.ExportCommand{Files: []int64{fileID}})
	require.NoError(t, err)
	tree = resp["namespace"].([]map[string]any)
	require.Len(t, tree, 1)
	require.Equal(t, "t1", tree[0]["name"])
}

// ---------- 导入 ----------

func TestServiceValidateImportData_Behavior(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	_, err := svc.ValidateImportData(ctx, "x.txt", []byte(`{}`), nil)
	require.ErrorIs(t, err, domainuns.ErrInvalidImportFileType)

	_, err = svc.ValidateImportData(ctx, "x.json", []byte("not-json"), nil)
	require.ErrorIs(t, err, domainuns.ErrInvalidJSON)

	_, err = svc.ValidateImportData(ctx, "x.json", []byte(`{}`), nil)
	require.ErrorIs(t, err, domainuns.ErrInvalidImportFormat)

	finished := 0
	resp, err := svc.ValidateImportData(ctx, "x.json", []byte(`{
		"labels": [{"name": "l1"}],
		"nodes": [{"namespace": "Common/Metric/t1", "type": "file", "topicType": "metric"}]
	}`), func(status domainuns.ImportStatus) {
		if status.Finished {
			finished++
		}
	})
	require.NoError(t, err)
	require.Equal(t, true, resp["valid"])
	require.Equal(t, 1, resp["labels"])
	require.Equal(t, 1, resp["nodes"])
	require.Equal(t, 1, finished)
}

func TestServiceImportDataStream_LabelsAndTree(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	raw := []byte(`{
		"labels": [{"name": "l1", "color": "#fff"}],
		"namespace": {
			"nodes": [{
				"type": "PATH", "name": "Common", "displayName": "Common",
				"children": [{
					"type": "PATH", "name": "Metric",
					"children": [{
						"type": "TOPIC", "name": "t1", "topicType": "metric",
						"fields": [{"name": "v", "type": "INTEGER"}], "enableHistory": "true"
					}]
				}]
			}]
		}
	}`)
	resp, err := svc.ImportDataStream(ctx, "x.json", raw, 1, nil)
	require.NoError(t, err)
	require.Equal(t, 4, resp["imported"])
	require.Equal(t, 0, resp["skipped"])

	var labelCount int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("uns_namespace_label_info").
		Where("name = ? AND deleted_time = 0", "l1").Count(&labelCount).Error)
	require.Equal(t, int64(1), labelCount)

	var file repo.UnsNode
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("namespace = ?", "Common/Metric/t1").First(&file).Error)
	require.Equal(t, int16(2), file.Type)
	require.Equal(t, int16(3), file.TopicType)
	require.Equal(t, int16(1), file.EnableHistory)
	require.JSONEq(t, `[{"name":"v","type":"INTEGER"}]`, string(file.Schema))

	resp, err = svc.ImportDataStream(ctx, "x.json", raw, 1, nil)
	require.NoError(t, err)
	require.Equal(t, 0, resp["imported"])
	require.Equal(t, 4, resp["skipped"])
	require.Equal(t, 0, resp["updated"])
}

func TestServiceImportDataStream_RawNodeArray(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	raw := []byte(`{
		"nodes": [
			{"namespace": "Common", "type": "PATH", "displayName": "Common"},
			{"namespace": "Common/Metric", "type": "PATH"},
			{"namespace": "Common/Metric/t1", "type": "file", "topicType": "metric"}
		]
	}`)
	resp, err := svc.ImportDataStream(ctx, "x.json", raw, 1, nil)
	require.NoError(t, err)
	require.Equal(t, 3, resp["imported"])
	require.Zero(t, resp["skipped"])

	var file repo.UnsNode
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("namespace = ?", "Common/Metric/t1").First(&file).Error)
	require.Equal(t, int16(2), file.Type)
	require.Equal(t, int16(3), file.TopicType)
}

func TestServiceImportDataStream_Errors(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	raw := []byte(`{
		"namespace": {"nodes": [{
			"type": "PATH", "name": "Common",
			"children": [{
				"type": "PATH", "name": "Metric",
				"children": [{"type": "PATH", "name": "sub"}]
			}]
		}]}
	}`)
	_, err := svc.ImportDataStream(ctx, "x.json", raw, 1, nil)
	require.ErrorIs(t, err, domainuns.ErrInvalid)

	raw = []byte(`{"nodes": [{"namespace": "x", "alias": "bad alias"}]}`)
	_, err = svc.ImportDataStream(ctx, "x.json", raw, 1, nil)
	require.ErrorContains(t, err, "invalid alias")

	_, err = svc.ImportDataStream(ctx, "x.txt", []byte(`{}`), 1, nil)
	require.ErrorIs(t, err, domainuns.ErrInvalidImportFileType)
	_, err = svc.ImportDataStream(ctx, "x.json", []byte("not-json"), 1, nil)
	require.ErrorIs(t, err, domainuns.ErrInvalidJSON)
	_, err = svc.ImportDataStream(ctx, "x.json", []byte(`{}`), 1, nil)
	require.ErrorIs(t, err, domainuns.ErrInvalidImportFormat)
}

func TestServiceNormalizeNotFound_WithRealDB(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS

	_, err := svc.Job(ctx, "nope")
	require.True(t, errors.Is(err, domainuns.ErrNotFound))
}

func TestServiceImportBatch_RollbackAndSideEffects(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS
	db := svcCtx.App.Store.CommonDB()
	cases := []string{
		`{"labels":[{"name":"rollback-label"}],"namespace":[{"name":"Plant","type":"PATH","children":[{"name":"Metric","type":"PATH","children":[{"name":"ok","type":"TOPIC","alias":"repeat","mockData":"true","enableHistory":"true"},{"name":"bad","type":"TOPIC","alias":"repeat"}]}]}]}`,
		`{"labels":[{"name":"rollback-label"}],"namespace":[{"name":"Plant","type":"PATH","children":[{"name":"Metric","type":"PATH","children":[{"name":"ok","type":"TOPIC","mockData":"true"},{"name":"bad","type":"PATH"}]}]}]}`,
		`{"nodes":[{"namespace":"First","type":"PATH"},{"namespace":"Second","type":"PATH","alias":"bad alias"}]}`,
	}
	for _, raw := range cases {
		_, err := svc.ImportData(ctx, "test.json", []byte(raw), 1)
		require.Error(t, err)
		for _, table := range []string{"uns_namespace_node_info", "uns_namespace_label_info", "uns_namespace_label_nodeid", "sys_outbox_event"} {
			var count int64
			require.NoError(t, db.Table(table).Count(&count).Error)
			require.Zero(t, count, table)
		}
	}
	raw := []byte(`{"labels":[{"name":"bound"}],"namespace":[{"name":"Plant/Line/Metric/topic","type":"TOPIC","alias":"saved_topic","topicType":"METRIC","label":"bound","mockData":"true","enableHistory":"true","fields":[{"name":"v","type":"DOUBLE"}]}]}`)
	_, err := svc.ImportData(ctx, "test.json", raw, 1)
	require.NoError(t, err)
	var node repo.UnsNode
	require.NoError(t, db.Where("alias = ?", "saved_topic").Take(&node).Error)
	require.Equal(t, "Plant/Line/Metric/topic", node.Namespace)
	var bound int64
	require.NoError(t, db.Table("uns_namespace_label_nodeid").Where("node_id = ?", node.ID).Count(&bound).Error)
	require.EqualValues(t, 1, bound)
	for _, eventType := range []string{"uns.sourceflow.create", "uns.physical.ensure"} {
		var count int64
		require.NoError(t, db.Table("sys_outbox_event").Where("event_type = ?", eventType).Count(&count).Error)
		require.EqualValues(t, 1, count)
	}
}

func TestServiceImportBatch_LargeJobAndReplay(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS
	// Match the reported scale: 1,251 topics + 1,320 PATH nodes.
	topic := func(i int) map[string]any {
		return map[string]any{"name": fmt.Sprintf("topic%d", i), "alias": fmt.Sprintf("topic_%d", i), "type": "TOPIC", "enableHistory": "true", "fields": []map[string]any{{"name": "v", "type": "DOUBLE"}}}
	}
	children := []map[string]any{}
	for i := 0; i < 659; i++ {
		children = append(children, map[string]any{"name": fmt.Sprintf("line%d", i), "type": "PATH", "children": []map[string]any{{"name": "Metric", "type": "PATH", "children": []map[string]any{topic(i)}}}})
	}
	topics := []map[string]any{}
	for i := 659; i < 1251; i++ {
		topics = append(topics, topic(i))
	}
	children = append(children, map[string]any{"name": "Metric", "type": "PATH", "children": topics})
	raw, err := json.Marshal(map[string]any{"namespace": []map[string]any{{"name": "Plant", "type": "PATH", "children": children}}})
	require.NoError(t, err)
	accepted, err := svc.CreateImportJob(ctx, false, 123, 1)
	require.NoError(t, err)
	job := accepted["job"].(repo.AsyncJob)
	started := time.Now()
	result, err := svc.ImportJob(ctx, "large.json", raw, 1, job.JobKey, nil)
	require.NoError(t, err)
	t.Logf("batch import 2571 nodes: %s", time.Since(started))
	require.Equal(t, 2571, result["imported"])
	stored, err := repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, job.JobKey)
	require.NoError(t, err)
	require.Equal(t, repo.AsyncJobStatusRunning, stored.Status)
	_, err = svc.ImportJob(ctx, "large.json", raw, 1, job.JobKey, nil)
	require.NoError(t, err)
	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Model(&repo.UnsNode{}).Count(&count).Error)
	require.EqualValues(t, 2571, count)
	var batches []repo.OutboxEvent
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("event_type = ?", "uns.physical.ensure").Order("id").Find(&batches).Error)
	require.Len(t, batches, 13)
	for i, batch := range batches {
		var payload struct {
			Nodes []repo.UnsNode `json:"nodes"`
		}
		require.NoError(t, json.Unmarshal(batch.Payload, &payload))
		require.LessOrEqual(t, len(payload.Nodes), 100)
		require.NoError(t, svc.CompleteImportHistory(ctx, job.JobKey, batch.EventID, nil))
		require.NoError(t, svc.CompleteImportHistory(ctx, job.JobKey, batch.EventID, nil))
		current, err := repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, job.JobKey)
		require.NoError(t, err)
		if i < len(batches)-1 {
			require.Equal(t, repo.AsyncJobStatusRunning, current.Status)
		}
	}
	// A late worker progress/failure must not overwrite the committed success.
	require.NoError(t, repo.NewAsyncJobRepo(ctx).ActiveOnly().UpdateAsyncJobProgress(ctx, job.JobKey, 1))
	require.NoError(t, repo.NewAsyncJobRepo(ctx).ActiveOnly().MarkAsyncJobFailed(ctx, job.JobKey, "late", nil))
	stored, err = repo.NewAsyncJobRepo(ctx).GetAsyncJob(ctx, job.JobKey)
	require.NoError(t, err)
	require.Equal(t, repo.AsyncJobStatusSucceeded, stored.Status)
	require.Equal(t, 100, stored.Progress)
	var audits []repo.AuditLog
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("res_id = ?", job.JobKey).Find(&audits).Error)
	require.Len(t, audits, 1)
	require.Equal(t, "success", audits[0].Result)
	require.Contains(t, audits[0].DetailJSON, `"imported":2571`)
	require.Contains(t, audits[0].DetailJSON, `"sourceFileId":123`)
	currentAudit, err := svcCtx.App.Audit.Detail(ctx, audits[0].ID)
	require.NoError(t, err)
	require.Contains(t, currentAudit.DetailJSON, `"status":"succeeded"`)
	require.Contains(t, currentAudit.DetailJSON, `"job":{`)
	// A genuinely new job for the same file skips all 2,571 unchanged nodes.
	accepted, err = svc.CreateImportJob(ctx, false, 124, 1)
	require.NoError(t, err)
	another := accepted["job"].(repo.AsyncJob)
	started = time.Now()
	result, err = svc.ImportJob(ctx, "large.json", raw, 1, another.JobKey, nil)
	require.NoError(t, err)
	t.Logf("batch reimport 2571 nodes: %s", time.Since(started))
	require.Equal(t, 0, result["imported"])
	require.Equal(t, 0, result["updated"])
	require.Equal(t, 2571, result["skipped"])
	audits = nil
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("res_id = ?", another.JobKey).Find(&audits).Error)
	require.Len(t, audits, 1)
	require.Contains(t, audits[0].DetailJSON, `"skipped":2571`)
	require.Contains(t, audits[0].DetailJSON, `"sourceFileId":124`)

	require.NoError(t, svcCtx.App.Store.CommonDB().Model(&repo.UnsNode{}).Count(&count).Error)
	require.EqualValues(t, 2571, count)
}

func TestServiceImportBatch_SQLFailureRollsBackEarlierBatch(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	db := svcCtx.App.Store.CommonDB()
	require.NoError(t, db.Exec(`ALTER TABLE uns_namespace_node_info ADD CONSTRAINT test_reject_last CHECK (alias <> 'reject_last')`).Error)
	nodes := []map[string]any{}
	for i := 0; i < 601; i++ {
		nodes = append(nodes, map[string]any{"name": fmt.Sprintf("folder%d", i), "type": "PATH"})
	}
	nodes[600]["alias"] = "reject_last"
	raw, err := json.Marshal(map[string]any{"namespace": nodes, "labels": []map[string]any{{"name": "rollback"}}})
	require.NoError(t, err)
	_, err = svcCtx.App.UNS.ImportData(ctx, "test.json", raw, 1)
	require.Error(t, err)
	for _, table := range []string{"uns_namespace_node_info", "uns_namespace_label_info", "sys_outbox_event"} {
		var count int64
		require.NoError(t, db.Table(table).Count(&count).Error)
		require.Zero(t, count, table)
	}
	// The same atomicity applies when later UPDATE batches fail.
	nodes[600]["alias"] = "allowed_last"
	raw, err = json.Marshal(map[string]any{"namespace": nodes})
	require.NoError(t, err)
	_, err = svcCtx.App.UNS.ImportData(ctx, "test.json", raw, 1)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`ALTER TABLE uns_namespace_node_info ADD CONSTRAINT test_reject_update CHECK (description <> 'reject_update')`).Error)
	for _, node := range nodes {
		node["description"] = "changed"
	}
	nodes[600]["description"] = "reject_update"
	raw, err = json.Marshal(map[string]any{"namespace": nodes})
	require.NoError(t, err)
	_, err = svcCtx.App.UNS.ImportData(ctx, "test.json", raw, 1)
	require.Error(t, err)
	var changed int64
	require.NoError(t, db.Model(&repo.UnsNode{}).Where("description <> ''").Count(&changed).Error)
	require.Zero(t, changed)

}

func TestServiceImportBatch_RepeatUpdatesIdentityAndRollsBack(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc := svcCtx.App.UNS
	db := svcCtx.App.Store.CommonDB()
	raw := []byte(`{"labels":[{"name":"first"},{"name":"second"}],"namespace":[{"name":"Plant/Metric/temp","alias":"same_temp","type":"TOPIC","topicType":"METRIC","displayName":"old","description":"old","label":"first","mockData":"true","enableHistory":"false","fields":[{"name":"v","type":"DOUBLE"}]}]}`)
	first, err := svc.ImportData(ctx, "repeat.json", raw, 1)
	require.NoError(t, err)
	require.Zero(t, first["updated"])
	var original repo.UnsNode
	require.NoError(t, db.Where("alias = ?", "same_temp").Take(&original).Error)
	require.NoError(t, db.Model(&repo.UnsNode{}).Where("id = ?", original.ID).Updates(map[string]any{"is_favorite": 1, "sort_key": 98}).Error)
	for i := 0; i < 3; i++ {
		result, err := svc.ImportData(ctx, "repeat.json", raw, 1)
		require.NoError(t, err)
		require.Zero(t, result["imported"])
		require.Equal(t, 0, result["updated"])
		require.Equal(t, 3, result["skipped"])
	}
	var unchanged repo.UnsNode
	require.NoError(t, db.Where("id = ?", original.ID).Take(&unchanged).Error)
	beforeUpdateTime := unchanged.UpdatedTime
	_, err = svc.ImportData(ctx, "repeat.json", raw, 1)
	require.NoError(t, err)
	require.NoError(t, db.Where("id = ?", original.ID).Take(&unchanged).Error)
	require.Equal(t, beforeUpdateTime, unchanged.UpdatedTime)
	changed := bytes.ReplaceAll(raw, []byte(`"old"`), []byte(`"new"`))
	changed = bytes.ReplaceAll(changed, []byte(`"label":"first"`), []byte(`"label":"second"`))
	changed = bytes.ReplaceAll(changed, []byte(`"enableHistory":"false"`), []byte(`"enableHistory":"true"`))
	changed = bytes.ReplaceAll(changed, []byte(`"v","type":"DOUBLE"`), []byte(`"v","type":"DOUBLE"},{"name":"new_field","type":"STRING"`))
	_, err = svc.ImportData(ctx, "repeat.json", changed, 1)
	require.NoError(t, err)
	var node repo.UnsNode
	require.NoError(t, db.Where("alias = ?", "same_temp").Take(&node).Error)
	require.Equal(t, original.ID, node.ID)
	require.Equal(t, original.ParentID, node.ParentID)
	require.Equal(t, original.IDPath, node.IDPath)
	require.Equal(t, original.CreatedTime, node.CreatedTime)
	require.EqualValues(t, 1, node.IsFavorite)
	require.EqualValues(t, 98, node.SortKey)
	require.Equal(t, "new", node.Description)
	require.Equal(t, "old", node.DisplayName)
	require.EqualValues(t, 1, node.EnableHistory)
	require.Contains(t, string(node.Schema), "new_field")
	var names []string
	require.NoError(t, db.Table("uns_namespace_label_nodeid AS b").Joins("JOIN uns_namespace_label_info AS l ON l.id=b.label_id").Where("b.node_id = ?", node.ID).Pluck("l.name", &names).Error)
	require.Equal(t, []string{"second"}, names)
	var flowTasks int64
	require.NoError(t, db.Table("sys_outbox_event").Where("event_type = ?", "uns.sourceflow.create").Count(&flowTasks).Error)
	require.EqualValues(t, 1, flowTasks)
	// A later error must roll back the earlier update and any new labels/nodes.
	invalid := []byte(`{"namespace":[{"name":"Plant/Metric/temp","alias":"same_temp","type":"TOPIC","topicType":"METRIC","description":"must_rollback"},{"name":"Other/Metric/other","alias":"same_temp","type":"TOPIC","topicType":"METRIC"}]}`)
	_, err = svc.ImportData(ctx, "invalid.json", invalid, 1)
	require.Error(t, err)
	invalid = []byte(`{"namespace":[{"name":"Plant/Metric/temp","alias":"same_temp","type":"TOPIC","topicType":"METRIC","description":"must_rollback"},{"name":"Bad/Metric/bad","type":"TOPIC","topicType":"METRIC","alias":"bad alias"}]}`)
	_, err = svc.ImportData(ctx, "invalid.json", invalid, 1)
	require.Error(t, err)
	require.NoError(t, db.Where("id = ?", node.ID).Take(&node).Error)
	require.Equal(t, "new", node.Description)
	var count int64
	require.NoError(t, db.Model(&repo.UnsNode{}).Count(&count).Error)
	require.EqualValues(t, 3, count)
	// A true collision with another location is still rejected.
	_, err = svc.ImportData(ctx, "conflict.json", []byte(`{"namespace":[{"name":"other","type":"PATH","alias":"same_temp"}]}`), 1)
	require.ErrorContains(t, err, "alias already exists")
	// Omitting alias must retain the existing identity, not generate a new one.
	_, err = svc.ImportData(ctx, "raw.json", []byte(`{"nodes":[{"namespace":"Plant/Metric/temp","type":"file","topicType":"METRIC","description":"raw_updated","enableHistory":"false","fields":[{"name":"v","type":"DOUBLE"},{"name":"new_field","type":"STRING"}]}]}`), 1)
	require.NoError(t, err)
	require.NoError(t, db.Where("id = ?", node.ID).Take(&node).Error)
	require.Equal(t, "same_temp", node.Alias)
	require.Equal(t, "raw_updated", node.Description)
	require.EqualValues(t, 2, node.EnableHistory)
}

func TestServiceImportBatch_SelectiveChangesAndHistory(t *testing.T) {
	svcCtx := newUnsSvcCtx(t)
	ctx := svctest.CtxWithUser(1)
	svc, db := svcCtx.App.UNS, svcCtx.App.Store.CommonDB()
	topic := map[string]any{"name": "Plant/Metric/value", "alias": "selective_topic", "type": "TOPIC", "topicType": "METRIC", "displayName": "original", "description": "first", "enableHistory": "true", "mockData": "false", "label": "a", "extendProperties": map[string]any{"a": "one", "b": "two"}, "dataQualityPolicy": map[string]any{"expectedIntervalMs": 1000}, "fields": []map[string]any{{"name": "v", "type": "DOUBLE"}}}
	run := func() (map[string]any, error) {
		raw, err := json.Marshal(map[string]any{"labels": []map[string]any{{"name": "a"}, {"name": "b"}}, "namespace": []map[string]any{topic}})
		require.NoError(t, err)
		return svc.ImportData(ctx, "selective.json", raw, 1)
	}
	load := func() repo.UnsNode {
		var n repo.UnsNode
		require.NoError(t, db.Where("alias = ?", "selective_topic").Take(&n).Error)
		return n
	}
	ddlCount := func() int64 {
		var n int64
		require.NoError(t, db.Table("sys_outbox_event").Where("event_type = ?", "uns.physical.ensure").Count(&n).Error)
		return n
	}
	_, err := run()
	require.NoError(t, err)
	original := load()
	require.EqualValues(t, 1, ddlCount())
	// These settings are outside the cloud import update allowlist.
	topic["displayName"] = "must_not_overwrite"
	topic["mockData"] = "true"
	topic["dataQualityPolicy"] = map[string]any{"expectedIntervalMs": 9000}
	result, err := run()
	require.NoError(t, err)
	require.Zero(t, result["updated"])
	current := load()
	require.Equal(t, original.UpdatedTime, current.UpdatedTime)
	require.Equal(t, original.DisplayName, current.DisplayName)
	require.Equal(t, original.MockData, current.MockData)
	require.Equal(t, original.DataQualityPolicy, current.DataQualityPolicy)
	require.EqualValues(t, 1, ddlCount())
	var flows int64
	require.NoError(t, db.Table("sys_outbox_event").Where("event_type = ?", "uns.sourceflow.create").Count(&flows).Error)
	require.Zero(t, flows)
	// Description/extension/labels change metadata without any physical task.
	topic["description"] = "second"
	result, err = run()
	require.NoError(t, err)
	require.Equal(t, 1, result["updated"])
	require.Equal(t, "second", load().Description)
	require.EqualValues(t, 1, ddlCount())
	topic["extendProperties"] = map[string]any{"b": "updated", "a": "one"}
	result, err = run()
	require.NoError(t, err)
	require.Equal(t, 1, result["updated"])
	require.JSONEq(t, `{"a":"one","b":"updated"}`, string(load().ExtendProperties))
	require.EqualValues(t, 1, ddlCount())
	topic["label"] = "b"
	beforeLabels := load()
	result, err = run()
	require.NoError(t, err)
	require.Equal(t, 1, result["updated"])
	require.Equal(t, beforeLabels.UpdatedTime, load().UpdatedTime)
	require.EqualValues(t, 1, ddlCount())
	result, err = run()
	require.NoError(t, err)
	require.Zero(t, result["updated"])
	// Schema additions while history is enabled must schedule structural sync.
	fields := []map[string]any{{"name": "v", "type": "DOUBLE"}, {"name": "flag", "type": "BOOLEAN"}}
	topic["fields"] = fields
	result, err = run()
	require.NoError(t, err)
	require.Equal(t, 1, result["updated"])
	require.EqualValues(t, 2, ddlCount())
	// Match cloud's schema safety boundary; rejection leaves the whole node intact.
	topic["fields"] = []map[string]any{{"name": "v", "type": "STRING"}, {"name": "flag", "type": "BOOLEAN"}}
	_, err = run()
	require.ErrorContains(t, err, "field type change")
	topic["fields"] = []map[string]any{{"name": "v", "type": "DOUBLE"}}
	_, err = run()
	require.ErrorContains(t, err, "field deletion")
	require.EqualValues(t, 2, ddlCount())
	require.Contains(t, string(load().Schema), "flag")
	topic["fields"] = fields
	topic["enableHistory"] = "false"
	_, err = run()
	require.NoError(t, err)
	require.EqualValues(t, 2, ddlCount())
	topic["fields"] = append(fields, map[string]any{"name": "note", "type": "STRING"})
	_, err = run()
	require.NoError(t, err)
	require.EqualValues(t, 2, ddlCount())
	topic["enableHistory"] = "true"
	_, err = run()
	require.NoError(t, err)
	require.EqualValues(t, 3, ddlCount())
	current = load()
	require.Equal(t, original.ID, current.ID)
	require.Equal(t, original.DisplayName, current.DisplayName)
	require.Equal(t, original.MockData, current.MockData)
	require.Equal(t, original.DataQualityPolicy, current.DataQualityPolicy)
}

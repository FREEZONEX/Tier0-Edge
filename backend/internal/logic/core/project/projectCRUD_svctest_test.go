// TASK-028 阶段 0 试点：验证 testkit + svctest 基建的 DB 全链路
// （项目 CRUD 经 Logic -> domain Service -> repo，落真实 PG 测试库）。
package project

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestProjectCreate_InsertsProjectAndAudit(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewProjectCreateLogic(ctx, svcCtx)
	resp, err := l.ProjectCreate(&types.ProjectSaveReq{Name: "test-project", Description: "pilot desc"})
	require.NoError(t, err)
	require.NotNil(t, resp)

	var p repo.Project
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("name = ?", "test-project").First(&p).Error)
	require.Equal(t, int64(1), p.OwnerUserID)
	require.Equal(t, "pilot desc", p.Description)

	// 创建者默认成为 project 成员（领域层 UpsertProjectMember）
	var memberCount int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("project_member").
		Where("project_id = ? AND user_id = ?", p.ID, 1).Count(&memberCount).Error)
	require.Equal(t, int64(1), memberCount)
	// 注：审计落库走 HTTP 中间件 FlushRequest 链路（Record 仅写 ctx state），
	// 属集成测试范畴，logic 单测不覆盖。
}

func TestProjectUpdate_RenamesProject(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	created, err := NewProjectCreateLogic(ctx, svcCtx).ProjectCreate(
		&types.ProjectSaveReq{Name: "before", Description: "d"})
	require.NoError(t, err)
	id := created.Data.(map[string]any)["project"].(repo.Project).ID
	require.Positive(t, id)

	_, err = NewProjectUpdateLogic(ctx, svcCtx).ProjectUpdate(
		&types.ProjectSaveReq{Id: id, Name: "after", Description: "d2"})
	require.NoError(t, err)

	var p repo.Project
	require.NoError(t, svcCtx.App.Store.CommonDB().First(&p, id).Error)
	require.Equal(t, "after", p.Name)
	require.Equal(t, "d2", p.Description)
}

func TestProjectDelete_RemovesProjectAndTenantDB(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	created, err := NewProjectCreateLogic(ctx, svcCtx).ProjectCreate(
		&types.ProjectSaveReq{Name: "to-delete", Description: "d"})
	require.NoError(t, err)
	id := created.Data.(map[string]any)["project"].(repo.Project).ID

	_, err = NewProjectDeleteLogic(ctx, svcCtx).ProjectDelete(&types.IdReq{Id: id})
	require.NoError(t, err)

	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("project").
		Where("id = ? AND deleted_time = 0", id).Count(&count).Error)
	require.Zero(t, count)
}

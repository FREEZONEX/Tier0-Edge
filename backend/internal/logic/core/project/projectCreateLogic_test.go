// 本文件覆盖 projectCreateLogic.go 的 ProjectCreate：
// 正常路径：admin/普通用户创建项目 → project 落库（owner=创建者、database_name 回填）、
//          创建者默认成为项目成员；
// 异常情况：空名/纯空白名 → 400；重名项目（DB 无唯一约束）允许重复创建；
//          审计落库走 HTTP 中间件链路，logic 单测不覆盖。
package project

import (
	"strconv"
	"testing"

	"backend/internal/repo"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func itoa64(v int64) string {
	return strconv.FormatInt(v, 10)
}

func TestProjectCreateLogic_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	resp, err := NewProjectCreateLogic(ctx, svcCtx).ProjectCreate(&types.ProjectSaveReq{
		Name: "alpha", Description: "alpha desc",
	})
	require.NoError(t, err)
	require.Equal(t, 200, resp.Code)

	var p repo.Project
	require.NoError(t, svcCtx.App.Store.CommonDB().Where("name = ?", "alpha").First(&p).Error)
	require.Equal(t, int64(1), p.OwnerUserID)
	require.Equal(t, "alpha desc", p.Description)
	// 创建 project 级 database 并回填 database_name
	require.Equal(t, "project_"+itoa64(p.ID), p.DatabaseName)
	// 创建者默认成为项目成员
	var memberCount int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("project_member").
		Where("project_id = ? AND user_id = ?", p.ID, 1).Count(&memberCount).Error)
	require.Equal(t, int64(1), memberCount)
}

func TestProjectCreateLogic_EmptyName(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	for _, name := range []string{"", "   ", "\t"} {
		_, err := NewProjectCreateLogic(ctx, svcCtx).ProjectCreate(&types.ProjectSaveReq{Name: name})
		require.Error(t, err)
		require.Equal(t, 400, httpErrStatus(err), "empty/blank name should be HTTP 400")
	}
}

// TestProjectCreateLogic_DuplicateNameAllowed 记录现状：project.name 无唯一索引、Create 也无预检，
// 同名项目可重复创建（project_key 才是唯一键）。若产品要求名称唯一，需在 schema/领域层另行加固。
func TestProjectCreateLogic_DuplicateNameAllowed(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	_, err := NewProjectCreateLogic(ctx, svcCtx).ProjectCreate(&types.ProjectSaveReq{Name: "dup-name"})
	require.NoError(t, err)
	_, err = NewProjectCreateLogic(ctx, svcCtx).ProjectCreate(&types.ProjectSaveReq{Name: "dup-name"})
	require.NoError(t, err, "duplicate project name is currently allowed")

	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("project").
		Where("name = ? AND deleted_time = 0", "dup-name").Count(&count).Error)
	require.Equal(t, int64(2), count)
}

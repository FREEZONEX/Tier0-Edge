// 本文件提供 project 逻辑层 DB 测试的共享助手：
//  - insertTestUser：插入普通（非 admin）用户，供权限语义用例使用（seed 只内置 tier0 管理员 user 1）；
//  - createTestProject / createTestApp：以指定身份创建项目/应用，返回主键；
//  - httpErrStatus：提取 logicx.Error 包装后的 HTTP 状态码。
package project

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	domainproject "backend/internal/domain/project"
	"backend/internal/httpx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/testkit"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// insertTestUser 向测试库插入一个普通用户（无 admin 角色）。
// user 1 是 seed 内置的 tier0 管理员；权限用例的“无权限用户”用 99/100。
func insertTestUser(t *testing.T, db *gorm.DB, userID int64, userName string) {
	t.Helper()
	testkit.Insert(t, db, &repo.User{
		ID:       userID,
		UserName: userName,
		NickName: userName,
		Password: "test-only",
		Status:   1,
	})
}

// createTestProject 以给定 ctx 身份创建项目并返回项目 ID。
func createTestProject(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, name string) int64 {
	t.Helper()
	resp, err := NewProjectCreateLogic(ctx, svcCtx).ProjectCreate(&types.ProjectSaveReq{
		Name:        name,
		Description: "desc-" + name,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	project, ok := resp.Data.(map[string]any)["project"].(repo.Project)
	require.True(t, ok, "create response should carry repo.Project")
	require.Positive(t, project.ID)
	return project.ID
}

// createTestManualApp 以给定 ctx 身份创建手动应用（proxy 模式）并返回应用 ID。
func createTestManualApp(t *testing.T, svcCtx *svc.ServiceContext, ctx context.Context, projectID int64, name string) int64 {
	t.Helper()
	resp, err := NewProjectAppManualCreateLogic(ctx, svcCtx).ProjectAppManualCreate(&types.ProjectAppManualSaveReq{
		ProjectId:     projectID,
		Name:          name,
		Description:   "desc-" + name,
		AccessMode:    "proxy",
		ContainerName: "app-" + name,
		ContainerPort: 8080,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	appID, ok := resp.Data.(map[string]any)["appId"].(int64)
	require.True(t, ok, "manual create response should carry appId")
	require.Positive(t, appID)
	return appID
}

// httpErrStatus 返回 logicx.Error 包装后的 HTTP 状态码；非 *httpx.HTTPError 时返回 -1。
func httpErrStatus(err error) int {
	var httpErr *httpx.HTTPError
	if err != nil && errors.As(err, &httpErr) {
		return httpErr.Status
	}
	return -1
}

// jsonRaw 构造 json.RawMessage 测试数据。
func jsonRaw(s string) json.RawMessage {
	return json.RawMessage(s)
}

// jsonUnmarshal 解析 JSON 字节并断言成功（测试辅助）。
func jsonUnmarshal(raw []byte, v any) error {
	return json.Unmarshal(raw, v)
}

// memberSave 构造领域层成员保存命令（MemberSaveCommand），供权限/成员用例复用。
func memberSave(projectID, userID int64, roleIDs []int64, operatorUserID int64) domainproject.MemberSaveCommand {
	return domainproject.MemberSaveCommand{
		ProjectID:      projectID,
		UserID:         userID,
		RoleIDs:        roleIDs,
		OperatorUserID: operatorUserID,
	}
}

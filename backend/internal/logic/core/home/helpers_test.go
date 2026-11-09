// TASK-028 home 模块测试共享助手：
//
//	目标：提供 Envelope 解包、HTTP 错误断言与最近访问数据直插工具。
//	正常路径：insertRecentAudit 直插审计日志；insertProject/insertProjectApp 直插
//	          project/project_app 行（避免领域层 CREATE DATABASE 的开销）。
//	异常情况：无（纯工具函数）。
package home

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	respx "backend/internal/httpx"
	"backend/internal/repo"
	"backend/internal/svc"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

// newHomeEnv 每次调用创建独立测试库 + miniredis 的完整 ServiceContext 与登录 ctx。
func newHomeEnv(t *testing.T) (*svc.ServiceContext, context.Context) {
	t.Helper()
	return svctest.NewTestServiceContext(t), svctest.CtxWithUser(1)
}

// contextWithoutUser 返回不含登录 Subject 的 ctx（logicx.UserID 返回 0）。
func contextWithoutUser() context.Context {
	return context.Background()
}

// respData 解包成功响应的 Data 为 map[string]any。
func respData(t *testing.T, resp *types.Envelope) map[string]any {
	t.Helper()
	require.NotNil(t, resp)
	require.Equal(t, 200, resp.Code)
	m, ok := resp.Data.(map[string]any)
	require.True(t, ok, "envelope data should be map[string]any")
	return m
}

// requireHTTPError 断言 err 为 logicx.Error 包装出的 HTTP 错误且状态码匹配。
func requireHTTPError(t *testing.T, err error, status int) {
	t.Helper()
	require.Error(t, err)
	var httpErr *respx.HTTPError
	require.ErrorAs(t, err, &httpErr, "logicx.Error should wrap respx.HTTPError")
	require.Equal(t, status, httpErr.Status)
}

// insertRecentAudit 直插一条最近访问审计日志。
func insertRecentAudit(t *testing.T, svcCtx *svc.ServiceContext, row repo.AuditLog) {
	t.Helper()
	require.NoError(t, svcCtx.App.Store.CommonDB().Create(&row).Error)
}

// insertProject 直插项目行（owner 为 userID=1），返回项目 ID。
func insertProject(t *testing.T, svcCtx *svc.ServiceContext, id int64, name string) int64 {
	t.Helper()
	now := time.Now().UTC()
	row := repo.Project{
		ID:          id,
		ProjectKey:  "recent_" + strconv.FormatInt(id, 10),
		Name:        name,
		OwnerUserID: 1,
	}
	row.CreatedTime = now
	row.UpdatedTime = now
	require.NoError(t, svcCtx.App.Store.CommonDB().Create(&row).Error)
	_, err := repo.NewProjectRepo(svcCtx.App.Store.CommonDB()).UpsertProjectMember(context.Background(), repo.ProjectMemberSave{ProjectID: id, UserID: 1})
	require.NoError(t, err)
	return row.ID
}

// insertProjectApp 直插项目应用行，返回应用 ID。
func insertProjectApp(t *testing.T, svcCtx *svc.ServiceContext, id, projectID int64, name string) int64 {
	t.Helper()
	now := time.Now().UTC()
	row := repo.ProjectApp{
		ID:           id,
		ProjectID:    projectID,
		AppKey:       "manual_" + strconv.FormatInt(id, 10),
		Name:         name,
		SiteType:     "manualExternal",
		Status:       "active",
		ManifestJSON: json.RawMessage("{}"),
	}
	row.CreatedTime = now
	row.UpdatedTime = now
	require.NoError(t, svcCtx.App.Store.CommonDB().Create(&row).Error)
	return row.ID
}

// detailJSON 构造仅含 path 的 detail JSON。
func detailJSON(path string) string {
	raw, _ := json.Marshal(map[string]any{"path": path, "method": "POST"})
	return string(raw)
}

// detailJSONWithProjectID 构造含 projectId 的 detail JSON。
func detailJSONWithProjectID(path string, projectID int64) string {
	raw, _ := json.Marshal(map[string]any{"path": path, "method": "POST", "projectId": projectID})
	return string(raw)
}

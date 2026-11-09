// TASK-028 AuditLogPageLogic 单元测试：
//
//	目标：覆盖审计日志分页查询。
//	正常路径：分页返回 pageNo/pageSize/total/data；ResType/Code/OperatorKeyword 过滤生效；
//	          未传分页参数时回退默认值。
//	异常情况：无。
//	说明：审计落库走 HTTP 中间件链路，本测试直插 sys_user_oper_log 行验证查询链路。
package audit

import (
	"testing"

	"backend/internal/repo"
	"backend/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditLogPageLogic_Page_Empty(t *testing.T) {
	svcCtx, ctx := newAuditEnv(t)
	l := NewAuditLogPageLogic(ctx, svcCtx)

	resp, err := l.AuditLogPage(&types.AuditLogPageReq{PageNo: 1, PageSize: 10})
	require.NoError(t, err)

	view := respData(t, resp)
	assert.Equal(t, 1, view["pageNo"])
	assert.Equal(t, 10, view["pageSize"])
	assert.Equal(t, int64(0), view["total"])
	assert.Empty(t, view["data"])
}

func TestAuditLogPageLogic_Page_WithItems(t *testing.T) {
	svcCtx, ctx := newAuditEnv(t)
	mustInsertAuditLog(t, svcCtx, 1, "UserManagement", "Create", "10", "alice", 200, 1)
	mustInsertAuditLog(t, svcCtx, 1, "Project", "Update", "20", "demo", 200, 1)
	mustInsertAuditLog(t, svcCtx, 2, "UserManagement", "Delete", "11", "bob", 500, 2)

	l := NewAuditLogPageLogic(ctx, svcCtx)
	resp, err := l.AuditLogPage(&types.AuditLogPageReq{PageNo: 1, PageSize: 10})
	require.NoError(t, err)

	view := respData(t, resp)
	assert.Equal(t, int64(3), view["total"])
	items, ok := view["data"].([]repo.AuditLog)
	require.True(t, ok, "data should be []repo.AuditLog")
	require.Len(t, items, 3)
	assert.Equal(t, "bob", items[0].ResName)
}

func TestAuditLogPageLogic_Page_FilterByResTypeAndCode(t *testing.T) {
	svcCtx, ctx := newAuditEnv(t)
	mustInsertAuditLog(t, svcCtx, 1, "UserManagement", "Create", "10", "alice", 200, 1)
	mustInsertAuditLog(t, svcCtx, 1, "Project", "Create", "20", "demo", 500, 1)

	l := NewAuditLogPageLogic(ctx, svcCtx)
	code := 200
	resp, err := l.AuditLogPage(&types.AuditLogPageReq{
		PageNo:   1,
		PageSize: 10,
		ResType:  "UserManagement",
		Code:     code,
	})
	require.NoError(t, err)

	view := respData(t, resp)
	assert.Equal(t, int64(1), view["total"])
	items := view["data"].([]repo.AuditLog)
	assert.Equal(t, "alice", items[0].ResName)
	assert.Equal(t, int64(200), items[0].Code)
}

func TestAuditLogPageLogic_Page_FilterByOperatorKeyword(t *testing.T) {
	svcCtx, ctx := newAuditEnv(t)
	mustInsertAuditLog(t, svcCtx, 1, "UserManagement", "Create", "10", "alice", 200, 1)
	mustInsertAuditLog(t, svcCtx, 2, "UserManagement", "Create", "11", "bob", 200, 1)

	l := NewAuditLogPageLogic(ctx, svcCtx)
	resp, err := l.AuditLogPage(&types.AuditLogPageReq{
		PageNo:          1,
		PageSize:        10,
		OperatorKeyword: "alice",
	})
	require.NoError(t, err)

	view := respData(t, resp)
	// operator_name 为空（直插未写），user_id 文本匹配 "alice" 不命中 => 0 条
	assert.Equal(t, int64(0), view["total"])
}

func TestAuditLogPageLogic_Page_DefaultPagination(t *testing.T) {
	svcCtx, ctx := newAuditEnv(t)
	for i := 0; i < 5; i++ {
		mustInsertAuditLog(t, svcCtx, 1, "Project", "Create", "id", "proj", 200, 1)
	}

	l := NewAuditLogPageLogic(ctx, svcCtx)
	resp, err := l.AuditLogPage(&types.AuditLogPageReq{})
	require.NoError(t, err)

	view := respData(t, resp)
	assert.Equal(t, 1, view["pageNo"])
	assert.Equal(t, 20, view["pageSize"])
	assert.Equal(t, int64(5), view["total"])
}

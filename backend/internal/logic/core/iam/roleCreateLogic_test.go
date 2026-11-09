// TASK-028 RoleCreateLogic 单元测试：
//   目标：锁定 goctl 脚手架占位行为（TODO 未实现，返回 nil 响应、无错误）。
//   正常路径：调用不 panic，返回 (nil, nil)。
//   异常情况：无（占位逻辑无分支；实现后需按业务重写本测试）。
package iam

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestRoleCreate_PlaceholderReturnsNil(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewRoleCreateLogic(ctx, svcCtx)

	resp, err := l.RoleCreate(&types.RoleSaveReq{Name: "engineer"})
	require.NoError(t, err)
	require.Nil(t, resp, "role create is a goctl placeholder, response should be nil")
}

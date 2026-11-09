// TASK-028 unsExportLogic.go 单测。
// 目标：覆盖 goctl 脚手架桩方法 UnsExport 的当前行为（返回 nil 不 panic）。
// 说明：生产代码为未实现的 scaffold（// todo），无 DB/外部依赖；
// 测试固化当前行为，待功能落地后需同步更新断言。
package uns

import (
	"context"
	"testing"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsExport_ScaffoldReturnsNil(t *testing.T) {
	l := NewUnsExportLogic(context.Background(), &svc.ServiceContext{})
	resp, err := l.UnsExport(&types.UnsExportJobReq{RootNodeId: 1})
	require.NoError(t, err)
	require.Nil(t, resp)
}

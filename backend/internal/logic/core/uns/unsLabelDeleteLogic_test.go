// TASK-028 unsLabelDeleteLogic.go 单测。
// 目标：覆盖 goctl 脚手架桩方法 UnsLabelDelete 的当前行为（返回 nil 不 panic）。
// 说明：生产代码为未实现的 scaffold（// todo），无 DB/外部依赖；
// 标签的实际删除链路由 unsLabelCrudLogic.go 的 Delete 承担（另有单测）；
// 测试固化当前行为，待功能落地后需同步更新断言。
package uns

import (
	"context"
	"testing"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsLabelDelete_ScaffoldReturnsNil(t *testing.T) {
	l := NewUnsLabelDeleteLogic(context.Background(), &svc.ServiceContext{})
	resp, err := l.UnsLabelDelete(&types.IdReq{Id: 1})
	require.NoError(t, err)
	require.Nil(t, resp)
}

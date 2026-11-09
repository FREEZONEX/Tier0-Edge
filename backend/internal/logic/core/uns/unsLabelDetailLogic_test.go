// TASK-028 unsLabelDetailLogic.go 单测。
// 目标：覆盖 goctl 脚手架桩方法 UnsLabelDetail 的当前行为（返回 nil 不 panic）。
// 说明：生产代码为未实现的 scaffold（// todo），无 DB/外部依赖；
// 标签的实际详情链路由 unsLabelCrudLogic.go 的 Detail 承担（另有单测）；
// 测试固化当前行为，待功能落地后需同步更新断言。
package uns

import (
	"context"
	"testing"

	"backend/internal/svc"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestUnsLabelDetail_ScaffoldReturnsNil(t *testing.T) {
	l := NewUnsLabelDetailLogic(context.Background(), &svc.ServiceContext{})
	resp, err := l.UnsLabelDetail(&types.IdReq{Id: 1})
	require.NoError(t, err)
	require.Nil(t, resp)
}

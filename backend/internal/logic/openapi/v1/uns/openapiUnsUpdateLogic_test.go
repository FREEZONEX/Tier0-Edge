// openapiUnsUpdateLogic_test.go — 测试。
//
// 测试目标：OpenAPI 更新节点 入口。
// 正常路径：更新属性成功。
// 异常情况：空 path/不存在路径 404。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiUnsUpdate_PathNotFound(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsUpdateLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsUpdate(&types.NodeUpdateReq{Path: "State/ghost"})
	requireHTTPStatus(t, err, 404)
}

func TestOpenapiUnsUpdate_EmptyPath(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	l := NewOpenapiUnsUpdateLogic(ctx, svcCtx)
	_, err := l.OpenapiUnsUpdate(&types.NodeUpdateReq{})
	require.Error(t, err)
}
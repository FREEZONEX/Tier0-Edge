// openapiUnsLabelDeleteLogic_test.go — 测试。
//
// 测试目标：OpenAPI 标签 CRUD 入口。
// 正常路径：创建/详情/列表/更新/删除。
// 异常情况：空 name 创建 400；不存在 404。
package uns

import (
	"testing"

	"backend/internal/svc/svctest"
	"backend/internal/types"

)

func TestOpenapiUnsLabelDelete_NotFound(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := withUserSubject(1)

	_, err := NewOpenapiUnsLabelDeleteLogic(ctx, svcCtx).OpenapiUnsLabelDelete(
		&types.IdReq{Id: 99999999})
	requireHTTPStatus(t, err, 404)
}
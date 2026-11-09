// TASK-028 OAuthClientListLogic 单元测试：
//
//	目标：覆盖 OAuth 客户端列表查询入口。
//	正常路径：空库返回空列表；创建两个客户端后返回全量（含默认字段）。
//	异常情况：无（查询类接口）。
package oauth

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOAuthClientList_Empty(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewOAuthClientListLogic(ctx, svcCtx)

	resp, err := l.OAuthClientList(&types.PageReq{})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Empty(t, view["list"])
	require.Equal(t, 0, view["total"])
}

func TestOAuthClientList_All(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	mustCreateOAuthClient(t, svcCtx, ctx, "app-a", "http://a.example.com/cb")
	mustCreateOAuthClient(t, svcCtx, ctx, "app-b", "http://b.example.com/cb")

	l := NewOAuthClientListLogic(ctx, svcCtx)
	resp, err := l.OAuthClientList(&types.PageReq{})
	require.NoError(t, err)
	view := respData(t, resp)
	require.Equal(t, 2, view["total"])
	require.Len(t, view["list"], 2)
}

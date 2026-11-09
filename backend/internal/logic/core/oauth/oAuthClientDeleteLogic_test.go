// TASK-028 OAuthClientDeleteLogic 单元测试：
//
//	目标：覆盖删除 OAuth 客户端入口。
//	正常路径：删除普通客户端成功（enabled 置 false），返回 deleted=true。
//	异常情况：不存在的 id → 404。
//	说明：system_builtin 客户端（seed 未内置，需真实系统客户端）分支未覆盖，
//	      删除 system_builtin 由 repo 层拒绝并返回 404，逻辑层无特殊分支。
package oauth

import (
	"testing"

	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOAuthClientDelete_Success(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	id, _ := mustCreateOAuthClient(t, svcCtx, ctx, "doomed", "http://d.example.com/cb")

	l := NewOAuthClientDeleteLogic(ctx, svcCtx)
	resp, err := l.OAuthClientDelete(&types.IdReq{Id: id})
	require.NoError(t, err)
	require.Equal(t, true, respData(t, resp)["deleted"])

	var count int64
	require.NoError(t, svcCtx.App.Store.CommonDB().Table("sys_oauth_client").
		Where("id = ? AND enabled = true", id).Count(&count).Error)
	require.Zero(t, count)
}

func TestOAuthClientDelete_NotFound(t *testing.T) {
	svcCtx, ctx := newSvcEnv(t)
	l := NewOAuthClientDeleteLogic(ctx, svcCtx)

	_, err := l.OAuthClientDelete(&types.IdReq{Id: 999999})
	requireHTTPError(t, err, 404)
}

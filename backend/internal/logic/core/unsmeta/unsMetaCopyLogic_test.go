// 验证复制入口的读取权限要求，以及管理员通过真实服务执行复制。
package unsmeta

import (
	"context"
	"testing"

	"backend/internal/contextx"
	domain "backend/internal/domain/unsmeta"
	"backend/internal/svc/svctest"
	"backend/internal/types"
	"github.com/stretchr/testify/require"
)

func TestUnsMetaCopyPermissions(t *testing.T) {
	service := svctest.NewTestServiceContext(t)
	ctx := context.Background()
	folder, err := service.App.UnsMeta.Save(ctx, "", "folder", 0, domain.Document{Name: "Permission test"})
	require.NoError(t, err)
	request := &types.UnsMetaCopyReq{Id: folder.ID, ExpectedRevision: folder.Revision}
	_, err = NewUnsMetaCopyLogic(ctx, service).UnsMetaCopy(request)
	require.Error(t, err)
	writeOnly := contextx.WithSubject(ctx, contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.write": {}}})
	_, err = NewUnsMetaCopyLogic(writeOnly, service).UnsMetaCopy(request)
	require.Error(t, err)
	authorized := contextx.WithSubject(ctx, contextx.Subject{UserID: 1, ResourceKeys: map[string]struct{}{"uns.write": {}, "uns.read": {}}})
	result, err := NewUnsMetaCopyLogic(authorized, service).UnsMetaCopy(request)
	require.NoError(t, err)
	require.Equal(t, 200, result.Code)
}

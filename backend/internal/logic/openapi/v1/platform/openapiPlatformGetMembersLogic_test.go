// openapiPlatformGetMembersLogic_test.go — OpenapiPlatformGetMembersLogic 测试。
//
// 测试目标：OpenAPI 查询平台成员列表入口。
// 正常路径：seed 内置 tier0 管理员可见（workspace 1），返回分页列表；
//   关键词/角色过滤生效。
// 异常情况：非法 updatedAtStart 返回 400；非法 statuses 返回 400。
package platform

import (
	"testing"

	domainiam "backend/internal/domain/iam"
	"backend/internal/svc/svctest"
	"backend/internal/types"

	"github.com/stretchr/testify/require"
)

func TestOpenapiPlatformGetMembers_Success(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewOpenapiPlatformGetMembersLogic(ctx, svcCtx)
	resp, err := l.OpenapiPlatformGetMembers(&types.PlatformGetMembersReq{Page: 1, Size: 20})
	require.NoError(t, err)
	require.NotNil(t, resp)

	// 注意：logic 直接 Envelope(domain 返回的 iam.PlatformMembersPage 结构体)，
	// 不是 map[string]any。
	data, ok := resp.Data.(domainiam.PlatformMembersPage)
	require.True(t, ok)
	require.GreaterOrEqual(t, data.Total, int64(1))
	require.NotEmpty(t, data.List)
	require.NotEmpty(t, data.List[0].UserID)
	require.Equal(t, "active", data.List[0].Status)
}

func TestOpenapiPlatformGetMembers_KeywordFilter(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewOpenapiPlatformGetMembersLogic(ctx, svcCtx)
	resp, err := l.OpenapiPlatformGetMembers(&types.PlatformGetMembersReq{Keyword: "tier0"})
	require.NoError(t, err)
	data := resp.Data.(domainiam.PlatformMembersPage)
	require.GreaterOrEqual(t, data.Total, int64(1))
	for _, member := range data.List {
		require.Contains(t, member.UserName, "tier0")
	}
}

func TestOpenapiPlatformGetMembers_RoleFilter(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewOpenapiPlatformGetMembersLogic(ctx, svcCtx)
	resp, err := l.OpenapiPlatformGetMembers(&types.PlatformGetMembersReq{RoleKey: "admin"})
	require.NoError(t, err)
	data := resp.Data.(domainiam.PlatformMembersPage)
	require.GreaterOrEqual(t, data.Total, int64(1))
	for _, member := range data.List {
		require.NotEmpty(t, member.Roles)
	}
}

func TestOpenapiPlatformGetMembers_InvalidUpdatedAtStart(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewOpenapiPlatformGetMembersLogic(ctx, svcCtx)
	_, err := l.OpenapiPlatformGetMembers(&types.PlatformGetMembersReq{UpdatedAtStart: "bogus"})
	requireHTTPStatus(t, err, 400)
}

func TestOpenapiPlatformGetMembers_InvalidStatuses(t *testing.T) {
	svcCtx := svctest.NewTestServiceContext(t)
	ctx := svctest.CtxWithUser(1)

	l := NewOpenapiPlatformGetMembersLogic(ctx, svcCtx)
	_, err := l.OpenapiPlatformGetMembers(&types.PlatformGetMembersReq{Statuses: []string{"bogus"}})
	requireHTTPStatus(t, err, 400)
}

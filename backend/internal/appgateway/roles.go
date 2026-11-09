package appgateway

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"backend/internal/repo"
)

// 项目角色门控的失败语义（对齐 cloud 网关 fail-secure，不降级放行）：
//   - errNotProjectMember / errNoValidAppRole → 403
//   - errAppNotFound → 404（路由存在但应用记录已删，拒绝代理 stale route）
//   - 其他 DB 错误 → 503
var (
	errNotProjectMember = errors.New("forbidden: not a project member")
	errNoValidAppRole   = errors.New("forbidden: no valid app role")
	errAppNotFound      = errors.New("app not found")
)

// 网关注入头（与 cloud tier0_headers.go 完全一致的三件套）
const (
	headerXTier0Runtime       = "X-Tier0-Runtime"
	headerXTier0BusinessRoles = "X-Tier0-Business-Roles"
	headerXTier0ActiveRole    = "X-Tier0-Active-Role"

	runtimeValueDeployed = "deployed"
)

const (
	rolesCacheTTL         = 2 * time.Minute
	rolesCacheNegativeTTL = 30 * time.Second // 负缓存（403/404）短 TTL，避免角色变更后长时封禁
	rolesCacheMaxSize     = 8192
)

// StripInjectedHeaders 剥离客户端伪造的网关注入头（对齐 cloud B4）。
// 必须在 proxy.go Director 的 r.Header.Clone() 之前调用。
func StripInjectedHeaders(r *http.Request) {
	for key := range r.Header {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "x-tier0-") || strings.HasPrefix(lower, "x-app-user-") {
			r.Header.Del(key)
		}
	}
	r.Header.Del("user")
}

// projectRoleStore 角色解析依赖的查询集，*repo.ProjectRepo 天然实现；测试可替换为 fake。
type projectRoleStore interface {
	GetProjectMemberWithRoles(ctx context.Context, projectID, userID int64) (repo.ProjectMember, error)
	GetProjectAppByDeployAppID(ctx context.Context, projectID int64, deployAppID string) (repo.ProjectApp, error)
	ListProjectRoleAppIDsByAppIDs(ctx context.Context, projectID int64, appIDs []int64) ([]repo.ProjectRoleApp, error)
}

type rolesCacheEntry struct {
	roles   []string
	err     error
	expires time.Time
}

// roleResolver 解析"用户在某应用下的有效项目角色"，语义对齐 cloud runtime-roles：
// 有效角色 = 成员角色 ∩ 应用绑定角色；应用无绑定角色 = 开放 App（放行但不注入角色头）；
// 非项目成员（含 owner 但非成员）一律 403（与 launchpad apps() 语义一致）。
type roleResolver struct {
	newStore func(ctx context.Context) projectRoleStore

	mu    sync.RWMutex
	cache map[string]rolesCacheEntry
}

func newRoleResolver() *roleResolver {
	return &roleResolver{
		newStore: func(ctx context.Context) projectRoleStore { return repo.NewProjectRepo(ctx) },
		cache:    map[string]rolesCacheEntry{},
	}
}

// invalidateAll 清空角色缓存。项目成员/成员角色/应用角色绑定变更后调用，
// 避免被撤销的角色在 TTL 内继续生效（缓存量小，重建成本低）。
func (rr *roleResolver) invalidateAll() {
	rr.mu.Lock()
	rr.cache = map[string]rolesCacheEntry{}
	rr.mu.Unlock()
}

// resolve 返回有效 role_key 列表（按 role_key 字母序排序，第一个即默认激活角色，
// 与 cloud launchpad_service.go 的 default_active_role_key 取法一致）。
// 返回空列表 + nil error 表示开放 App：放行但不注入角色头。
func (rr *roleResolver) resolve(ctx context.Context, route *repo.AppGatewayRoute, userID int64) ([]string, error) {
	key := route.AppID + ":" + strconv.FormatInt(userID, 10)
	now := time.Now()

	rr.mu.RLock()
	entry, ok := rr.cache[key]
	rr.mu.RUnlock()
	if ok && now.Before(entry.expires) {
		return entry.roles, entry.err
	}

	roles, err := rr.fetch(ctx, route, userID)

	// 瞬时 DB 错误不缓存，避免放大故障；403/404 负缓存 30s
	negative := errors.Is(err, errNotProjectMember) || errors.Is(err, errNoValidAppRole) || errors.Is(err, errAppNotFound)
	if err == nil || negative {
		ttl := rolesCacheTTL
		if err != nil {
			ttl = rolesCacheNegativeTTL
		}
		rr.mu.Lock()
		if len(rr.cache) >= rolesCacheMaxSize {
			rr.cache = map[string]rolesCacheEntry{}
		}
		rr.cache[key] = rolesCacheEntry{roles: roles, err: err, expires: now.Add(ttl)}
		rr.mu.Unlock()
	}
	return roles, err
}

func (rr *roleResolver) fetch(ctx context.Context, route *repo.AppGatewayRoute, userID int64) ([]string, error) {
	store := rr.newStore(ctx)

	member, err := store.GetProjectMemberWithRoles(ctx, route.ProjectID, userID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, errNotProjectMember
	}
	if err != nil {
		return nil, err
	}

	app, err := store.GetProjectAppByDeployAppID(ctx, route.ProjectID, route.AppID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, errAppNotFound
	}
	if err != nil {
		return nil, err
	}

	bound, err := store.ListProjectRoleAppIDsByAppIDs(ctx, route.ProjectID, []int64{app.ID})
	if err != nil {
		return nil, err
	}

	// 开放 App：未绑定任何角色时放行，不注入角色头（对齐 cloud P0 fix 语义）
	if len(bound) == 0 {
		return []string{}, nil
	}

	boundSet := make(map[int64]struct{}, len(bound))
	for _, b := range bound {
		boundSet[b.RoleID] = struct{}{}
	}
	keys := sortedRoleKeys(member.Roles, boundSet)
	if len(keys) == 0 {
		return nil, errNoValidAppRole
	}
	return keys, nil
}

// sortedRoleKeys 取成员角色中属于 filter 集合的 role_key，按字母序排序。
// 字母序保证默认激活角色（第一个）与 launchpad 展示顺序一致。
func sortedRoleKeys(roles []repo.ProjectRole, filter map[int64]struct{}) []string {
	keys := make([]string, 0, len(roles))
	for _, r := range roles {
		if _, ok := filter[r.ID]; !ok {
			continue
		}
		if r.RoleKey == "" {
			continue
		}
		keys = append(keys, r.RoleKey)
	}
	sort.Strings(keys)
	return keys
}
